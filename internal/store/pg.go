package store

// PostgreSQL support: a database/sql driver that wraps pgx's and adapts the store's SQLite-flavoured
// SQL as it goes, so the queries stay written once. What it does to each statement (cached):
//   - ? and ?N placeholders -> $n (outside string literals);
//   - "IS ?" / "IS NOT ?" -> IS [NOT] DISTINCT FROM (SQLite's null-safe compare);
//   - LIKE -> ILIKE (SQLite's LIKE ignores ASCII case);
//   - json_extract(x, '$.k') -> (x::jsonb ->> 'k'); group_concat(x) -> string_agg(x::text, ',');
//   - an INSERT into a table with an identity id, run through Exec, gets RETURNING id, so
//     LastInsertId works as with SQLite.
// Arguments: bools become 0/1 (flags are integer columns, as in SQLite).
// The schema is migrations_pg/: a baseline equal to the SQLite schema at its number, then a twin of
// each later SQLite migration.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

const pgDriverName = "gigg-pgx"

func init() { sql.Register(pgDriverName, &pgDriver{}) }

// isPostgresURL: a database URL that selects PostgreSQL.
func isPostgresURL(u string) bool {
	return strings.HasPrefix(u, "postgres://") || strings.HasPrefix(u, "postgresql://")
}

type pgDriver struct{}

func (pgDriver) Open(name string) (driver.Conn, error) {
	c, err := stdlib.GetDefaultDriver().Open(name)
	if err != nil {
		return nil, err
	}
	return &pgConn{c: c}, nil
}

// pgIdentity: the tables whose id is an identity column (filled at open from the catalog).
var pgIdentity sync.Map // table name -> true

type pgConn struct{ c driver.Conn }

func (c *pgConn) Prepare(q string) (driver.Stmt, error) { return c.c.Prepare(pgRewrite(q)) }
func (c *pgConn) Close() error                          { return c.c.Close() }
func (c *pgConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *pgConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.c.(driver.ConnBeginTx).BeginTx(ctx, o)
}

func (c *pgConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	return c.c.(driver.ConnPrepareContext).PrepareContext(ctx, pgRewrite(q))
}

func (c *pgConn) Ping(ctx context.Context) error { return c.c.(driver.Pinger).Ping(ctx) }

func (c *pgConn) ResetSession(ctx context.Context) error {
	if r, ok := c.c.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *pgConn) IsValid() bool {
	if v, ok := c.c.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

// CheckNamedValue: bools as integers, everything else as pgx takes it.
func (c *pgConn) CheckNamedValue(v *driver.NamedValue) error {
	if b, ok := v.Value.(bool); ok {
		v.Value = int64(0)
		if b {
			v.Value = int64(1)
		}
		return nil
	}
	if n, ok := c.c.(driver.NamedValueChecker); ok {
		return n.CheckNamedValue(v)
	}
	return driver.ErrSkip
}

func (c *pgConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.c.(driver.QueryerContext).QueryContext(ctx, pgRewrite(q), args)
	if err != nil {
		return nil, pgErr(err)
	}
	return pgRows{rows}, nil
}

// pgRows: a statement's error can arrive with its first row (INSERT ... RETURNING read with Scan).
type pgRows struct{ driver.Rows }

func (r pgRows) Next(dest []driver.Value) error { return pgErr(r.Rows.Next(dest)) }

func (c *pgConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if table, ok := pgInsertInto(q); ok {
		if _, id := pgIdentity.Load(table); id {
			rows, err := c.c.(driver.QueryerContext).QueryContext(ctx, pgRewrite(q)+" RETURNING id", args)
			if err != nil {
				return nil, pgErr(err)
			}
			defer rows.Close()
			var res pgInsertResult
			vals := make([]driver.Value, 1)
			for {
				err := rows.Next(vals)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return nil, pgErr(err)
				}
				res.n++
				if id, ok := vals[0].(int64); ok {
					res.id = id
				}
			}
			return res, nil
		}
	}
	r, err := c.c.(driver.ExecerContext).ExecContext(ctx, pgRewrite(q), args)
	return r, pgErr(err)
}

type pgInsertResult struct{ id, n int64 }

func (r pgInsertResult) LastInsertId() (int64, error) { return r.id, nil }
func (r pgInsertResult) RowsAffected() (int64, error) { return r.n, nil }

var pgInsertRe = regexp.MustCompile(`(?is)^\s*INSERT\s+INTO\s+"?(\w+)"?`)

// pgInsertInto: the table of a plain INSERT that doesn't already return something.
func pgInsertInto(q string) (string, bool) {
	m := pgInsertRe.FindStringSubmatch(q)
	if m == nil || pgReturning.MatchString(q) {
		return "", false
	}
	return strings.ToLower(m[1]), true
}

var (
	pgReturning   = regexp.MustCompile(`(?i)\bRETURNING\b`)
	pgIsNot       = regexp.MustCompile(`(?i)\bIS\s+NOT\s+\?`)
	pgIs          = regexp.MustCompile(`(?i)\bIS\s+\?`)
	pgLike        = regexp.MustCompile(`(?i)\bLIKE\b`)
	pgJSONExtract = regexp.MustCompile(`(?i)json_extract\(\s*([\w.]+)\s*,\s*'\$\.(\w+)'\s*\)`)
	pgCache       sync.Map // SQLite text -> PostgreSQL text
)

// pgRewrite adapts one statement (see the package comment above).
func pgRewrite(q string) string {
	if v, ok := pgCache.Load(q); ok {
		return v.(string)
	}
	out := pgOutsideQuotes(q, func(s string) string {
		s = pgIsNot.ReplaceAllString(s, "IS DISTINCT FROM ?")
		s = pgIs.ReplaceAllString(s, "IS NOT DISTINCT FROM ?")
		s = pgLike.ReplaceAllString(s, "ILIKE")
		return s
	})
	// these take string literals as arguments: rewritten on the whole text
	out = pgJSONExtract.ReplaceAllString(out, "(${1}::jsonb ->> '${2}')")
	out = pgGroupConcatRewrite(out)
	out = pgPlaceholders(out)
	pgCache.Store(q, out)
	return out
}

// pgOutsideQuotes applies f to the parts of q outside '...' literals.
func pgOutsideQuotes(q string, f func(string) string) string {
	var b strings.Builder
	for len(q) > 0 {
		i := strings.IndexByte(q, '\'')
		if i < 0 {
			b.WriteString(f(q))
			break
		}
		b.WriteString(f(q[:i]))
		j := i + 1
		for j < len(q) {
			if q[j] == '\'' {
				if j+1 < len(q) && q[j+1] == '\'' {
					j += 2
					continue
				}
				break
			}
			j++
		}
		if j >= len(q) {
			b.WriteString(q[i:])
			break
		}
		b.WriteString(q[i : j+1])
		q = q[j+1:]
	}
	return b.String()
}

// pgPlaceholders numbers ? as $1, $2... and turns ?N into $N, outside string literals.
func pgPlaceholders(q string) string {
	n := 0
	return pgOutsideQuotes(q, func(s string) string {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] != '?' {
				b.WriteByte(s[i])
				continue
			}
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j > i+1 {
				b.WriteString("$" + s[i+1:j])
				i = j - 1
				continue
			}
			n++
			b.WriteString("$" + strconv.Itoa(n))
		}
		return b.String()
	})
}

// pgErr gives PostgreSQL's constraint errors the SQLite wording the store checks for (isUnique,
// isFK), keeping the original after it.
func pgErr(err error) error {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return err
	}
	switch pe.Code {
	case "23505":
		return fmt.Errorf("UNIQUE constraint failed: %s: %w", pe.ConstraintName, err)
	case "23503":
		return fmt.Errorf("FOREIGN KEY constraint failed: %w", err)
	case "23514":
		return fmt.Errorf("CHECK constraint failed: %s: %w", pe.ConstraintName, err)
	case "23502":
		return fmt.Errorf("NOT NULL constraint failed: %s.%s: %w", pe.TableName, pe.ColumnName, err)
	}
	return err
}

// pgLoadIdentity records which tables have an identity id (for RETURNING id on insert).
func pgLoadIdentity(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND column_name = 'id' AND is_identity = 'YES'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return err
		}
		pgIdentity.Store(t, true)
	}
	return rows.Err()
}

// pgGroupConcatRewrite: group_concat(x) -> string_agg(CAST(x AS TEXT), ','), and
// group_concat(x, sep) -> string_agg(CAST(x AS TEXT), sep), x any expression.
func pgGroupConcatRewrite(q string) string {
	const fn = "group_concat("
	for {
		i := strings.Index(strings.ToLower(q), fn)
		if i < 0 {
			return q
		}
		start := i + len(fn)
		depth, inQ, comma, end := 0, false, -1, -1
		for j := start; j < len(q) && end < 0; j++ {
			switch c := q[j]; {
			case c == '\'':
				inQ = !inQ
			case inQ:
			case c == '(':
				depth++
			case c == ')' && depth == 0:
				end = j
			case c == ')':
				depth--
			case c == ',' && depth == 0 && comma < 0:
				comma = j
			}
		}
		if end < 0 {
			return q // unbalanced: leave it for PostgreSQL to report
		}
		expr, sep := q[start:end], "','"
		if comma >= 0 {
			expr, sep = q[start:comma], strings.TrimSpace(q[comma+1:end])
		}
		q = q[:i] + "string_agg(CAST(" + strings.TrimSpace(expr) + " AS TEXT), " + sep + ")" + q[end+1:]
	}
}
