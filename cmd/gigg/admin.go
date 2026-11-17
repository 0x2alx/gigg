package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/0x2alx/gigg/internal/auth"
	"github.com/0x2alx/gigg/internal/config"
	"github.com/0x2alx/gigg/internal/store"
)

const adminUsage = `gigg admin: first setup and recovery, working directly on the data directory.

  gigg admin user-add <username> [-agent] [-admin] [-name "Display Name"]
  gigg admin passwd <username> [-file F]       new password from the terminal, stdin, or a file
  gigg admin workspace-add <slug> <name>       the owner defaults to the user named <slug>, if any
  gigg admin workspace-owner <workspace> <username|none>   whose picture marks the workspace's chips
  gigg admin color user|workspace <username|workspace> <#RRGGBB|none>   its colour on chips, badges, avatars
  gigg admin member <workspace> <username> <viewer|commenter|member|manager|none>
  gigg admin access <username> <workspace|all> <preset|none> [permission=allow|deny ...]
                                               set the user's access rule there (replaces it)
  gigg admin allow|deny <username> <workspace|all|each> <permission> [...]
                                               add permissions to the user's rule there (keeps the rest)
  gigg admin project-add <workspace> <slug> <name> <KEY>
  gigg admin key-add -user U [-workspace W] -scopes a,b [-listeners x,y] [-cidrs c] [-name N] [-days 365] [-out F]
  gigg admin key-rotate <key-id> [-out F]
  gigg admin key-revoke <username> <key-name>  revoke that user's active keys with that name
  gigg admin status-rename <workspace> <category> <new name>   rename that status in every project
  gigg admin batch <file>                      run one command per line (# comments; "quotes" group words)

key-add and key-rotate write the token to -out F (mode 0600), or print it on stdout but refuse to
do so on a terminal: redirect stdout into the file that needs it (it is shown nowhere else).
`

func admin(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "help" {
		fmt.Fprint(os.Stderr, adminUsage)
		return 2
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	store.DefaultTZ = cfg.DefaultTZ
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.DataDir, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "database:", err)
		return 1
	}
	defer st.Close()
	a := store.Actor{Listener: "cli"}
	if args[0] == "batch" {
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "usage: batch <file>")
			return 2
		}
		b, err := os.ReadFile(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for n, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			words := shellWords(line)
			if err := runAdmin(ctx, st, a, words[0], words[1:]); err != nil {
				fmt.Fprintf(os.Stderr, "gigg admin: line %d (%s): %v\n", n+1, words[0], err)
				return 1
			}
		}
		return 0
	}
	if err := runAdmin(ctx, st, a, args[0], args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gigg admin:", err)
		return 1
	}
	return 0
}

func runAdmin(ctx context.Context, st *store.Store, a store.Actor, cmd string, args []string) error {
	switch cmd {
	case "user-add":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		agent := fs.Bool("agent", false, "an agent (no password)")
		isAdmin := fs.Bool("admin", false, "instance admin")
		name := fs.String("name", "", "display name")
		if err := parse(fs, args, 1); err != nil {
			return err
		}
		kind := "human"
		if *agent {
			kind = "agent"
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			u, err := store.CreateUser(ctx, tx, a, store.NewUser{Username: fs.Arg(0), Kind: kind, DisplayName: *name, IsAdmin: *isAdmin})
			if err == nil {
				fmt.Fprintf(os.Stderr, "user %s created (id %d, %s%s)\n", u.Username, u.ID, kind, map[bool]string{true: ", admin"}[*isAdmin])
			}
			return err
		})
	case "passwd":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		file := fs.String("file", "", "read the password from this file (first line)")
		if err := parse(fs, args, 1); err != nil {
			return err
		}
		args = fs.Args()
		var pw string
		var err error
		if *file != "" {
			b, ferr := os.ReadFile(*file)
			if ferr != nil {
				return ferr
			}
			pw = strings.SplitN(strings.TrimRight(string(b), "\r\n"), "\n", 2)[0]
		} else if pw, err = readPassword(); err != nil {
			return err
		}
		h, err := auth.HashPassword(pw)
		if err != nil {
			return err
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			u, err := store.GetUserByName(ctx, tx, args[0])
			if err != nil {
				return err
			}
			if err := store.SetPassword(ctx, tx, a, u.ID, h); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "password set for %s; their sessions were ended\n", u.Username)
			return nil
		})
	case "workspace-add":
		if len(args) != 2 {
			return errors.New("usage: workspace-add <slug> <name>")
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			w, err := store.CreateWorkspace(ctx, tx, a, args[0], args[1], nil)
			if err == nil {
				fmt.Fprintf(os.Stderr, "workspace %s created (id %d)\n", w.Slug, w.ID)
			}
			return err
		})
	case "workspace-owner":
		if len(args) != 2 {
			return errors.New("usage: workspace-owner <workspace> <username|none>")
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			w, err := store.GetWorkspaceBySlug(ctx, tx, args[0])
			if err != nil {
				return fmt.Errorf("workspace %s: %w", args[0], err)
			}
			var owner *int64
			if args[1] != "none" {
				u, err := store.GetUserByName(ctx, tx, args[1])
				if err != nil {
					return fmt.Errorf("user %s: %w", args[1], err)
				}
				owner = &u.ID
			}
			if _, err = store.PatchWorkspace(ctx, tx, a, w.ID, 0, store.WorkspacePatch{OwnerID: &owner}); err == nil {
				fmt.Fprintf(os.Stderr, "owner of %s: %s\n", w.Slug, args[1])
			}
			return err
		})
	case "color":
		if len(args) != 3 || (args[0] != "user" && args[0] != "workspace") {
			return errors.New("usage: color user|workspace <username|workspace> <#RRGGBB|none>")
		}
		var c *string
		if args[2] != "none" {
			c = &args[2]
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			if args[0] == "user" {
				u, err := store.GetUserByName(ctx, tx, args[1])
				if err != nil {
					return fmt.Errorf("user %s: %w", args[1], err)
				}
				if _, err = store.PatchUser(ctx, tx, a, u.ID, 0, store.UserPatch{Color: &c}); err == nil {
					fmt.Fprintf(os.Stderr, "colour of user %s: %s\n", u.Username, args[2])
				}
				return err
			}
			w, err := store.GetWorkspaceBySlug(ctx, tx, args[1])
			if err != nil {
				return fmt.Errorf("workspace %s: %w", args[1], err)
			}
			if _, err = store.PatchWorkspace(ctx, tx, a, w.ID, 0, store.WorkspacePatch{Color: &c}); err == nil {
				fmt.Fprintf(os.Stderr, "colour of workspace %s: %s\n", w.Slug, args[2])
			}
			return err
		})
	case "member":
		if len(args) != 3 {
			return errors.New("usage: member <workspace> <username> <role|none>")
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			w, err := store.GetWorkspaceBySlug(ctx, tx, args[0])
			if err != nil {
				return fmt.Errorf("workspace %s: %w", args[0], err)
			}
			u, err := store.GetUserByName(ctx, tx, args[1])
			if err != nil {
				return fmt.Errorf("user %s: %w", args[1], err)
			}
			role := args[2]
			if role == "none" {
				role = ""
			}
			return store.SetMembership(ctx, tx, a, w.ID, u.ID, role)
		})
	case "access":
		if len(args) < 3 {
			return errors.New("usage: access <username> <workspace|all> <preset|none> [permission=allow|deny ...]")
		}
		preset := args[2]
		if preset == "none" {
			preset = ""
		}
		over := map[string]bool{}
		for _, kv := range args[3:] {
			name, val, ok := strings.Cut(kv, "=")
			if !ok || (val != "allow" && val != "deny") {
				return fmt.Errorf("%q: want permission=allow or permission=deny", kv)
			}
			over[name] = val == "allow"
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			u, err := store.GetUserByName(ctx, tx, args[0])
			if err != nil {
				return fmt.Errorf("user %s: %w", args[0], err)
			}
			var ws *int64
			if args[1] != "all" {
				w, err := store.GetWorkspaceBySlug(ctx, tx, args[1])
				if err != nil {
					return fmt.Errorf("workspace %s: %w", args[1], err)
				}
				ws = &w.ID
			}
			return store.SetRule(ctx, tx, a, "user", u.ID, ws, preset, over)
		})
	case "allow", "deny":
		// adds overrides to a user's rule(s), keeping the rest; "each" = every rule the user has (a deny
		// meant for the user everywhere has to be on each: the most specific rule wins)
		if len(args) < 3 {
			return fmt.Errorf("usage: %s <username> <workspace|all|each> <permission> [...]", cmd)
		}
		val := cmd == "allow"
		done := map[bool]string{true: "allowed", false: "denied"}
		known := map[string]bool{}
		for _, p := range store.Permissions {
			known[p.Name] = true
		}
		for _, perm := range args[2:] {
			if !known[perm] {
				return fmt.Errorf("no permission %q (see gigg admin permissions in the docs)", perm)
			}
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			u, err := store.GetUserByName(ctx, tx, args[0])
			if err != nil {
				return fmt.Errorf("user %s: %w", args[0], err)
			}
			var ws *int64
			switch args[1] {
			case "all", "each":
				if _, err := store.GetWorkspaceBySlug(ctx, tx, args[1]); err == nil {
					return fmt.Errorf("a workspace is named %q: ambiguous, use the UI", args[1])
				}
			default:
				w, err := store.GetWorkspaceBySlug(ctx, tx, args[1])
				if err != nil {
					return fmt.Errorf("workspace %s: %w", args[1], err)
				}
				ws = &w.ID
			}
			rules, err := store.RulesOfUser(ctx, tx, u.ID)
			if err != nil {
				return err
			}
			var targets []*store.AccessRule
			for _, r := range rules {
				if args[1] == "each" || (args[1] == "all" && r.WorkspaceID == nil) || (ws != nil && r.WorkspaceID != nil && *r.WorkspaceID == *ws) {
					targets = append(targets, r)
				}
			}
			if len(targets) == 0 {
				return fmt.Errorf("%s has no access rule for %s (make them a member first, or use access)", u.Username, args[1])
			}
			for _, rule := range targets {
				ov := map[string]bool{}
				for k, v := range rule.Overrides {
					ov[k] = v
				}
				for _, perm := range args[2:] {
					if v, ok := ov[perm]; ok && v != val {
						fmt.Fprintf(os.Stderr, "%s: was %s for %s, now %s\n", perm, done[v], u.Username, done[val])
					}
					ov[perm] = val
				}
				if err := store.SetRule(ctx, tx, a, "user", u.ID, rule.WorkspaceID, rule.Preset, ov); err != nil {
					return err
				}
				where := "all"
				if rule.WorkspaceID != nil {
					where = fmt.Sprintf("workspace %d", *rule.WorkspaceID)
					if w, err := store.GetWorkspace(ctx, tx, *rule.WorkspaceID); err == nil {
						where = w.Slug
					}
				}
				fmt.Fprintf(os.Stderr, "%s in %s: %s %s\n", u.Username, where, strings.Join(args[2:], ", "), done[val])
			}
			return nil
		})
	case "project-add":
		if len(args) != 4 {
			return errors.New("usage: project-add <workspace> <slug> <name> <KEY>")
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			w, err := store.GetWorkspaceBySlug(ctx, tx, args[0])
			if err != nil {
				return fmt.Errorf("workspace %s: %w", args[0], err)
			}
			p, err := store.CreateProject(ctx, tx, a, w.ID, args[1], args[2], args[3], nil)
			if err == nil {
				fmt.Fprintf(os.Stderr, "project %s/%s created (id %d, key %s)\n", w.Slug, p.Slug, p.ID, p.Key)
			}
			return err
		})
	case "status-rename":
		if len(args) != 3 {
			return errors.New("usage: status-rename <workspace> <category> <new name>")
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			w, err := store.GetWorkspaceBySlug(ctx, tx, args[0])
			if err != nil {
				return err
			}
			tmpl := w.StatusTemplate
			for i := range tmpl {
				if tmpl[i].Category == args[1] {
					tmpl[i].Name = args[2]
				}
			}
			if _, err := store.PatchWorkspace(ctx, tx, a, w.ID, 0, store.WorkspacePatch{StatusTemplate: tmpl}); err != nil {
				return err
			}
			ps, err := store.ListProjects(ctx, tx, w.ID, nil)
			if err != nil {
				return err
			}
			for _, p := range ps {
				sts, err := store.Statuses(ctx, tx, p.ID)
				if err != nil {
					return err
				}
				for _, s := range sts {
					if s.Category == args[1] {
						name := args[2]
						if _, err := store.PatchStatus(ctx, tx, a, s, 0, store.StatusPatch{Name: &name}); err != nil {
							return err
						}
					}
				}
			}
			return nil
		})
	case "key-add":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		user := fs.String("user", "", "acting user")
		ws := fs.String("workspace", "", "workspace slug (empty = admin key)")
		scopes := fs.String("scopes", "", "comma-separated scopes")
		listeners := fs.String("listeners", "", "allowed listeners (comma-separated; empty = any)")
		cidrs := fs.String("cidrs", "", "allowed networks (comma-separated; empty = any)")
		name := fs.String("name", "cli", "key name")
		days := fs.Int("days", 365, "lifetime in days")
		out := fs.String("out", "", "write the token to this file (0600) instead of stdout")
		if err := parse(fs, args, 0); err != nil {
			return err
		}
		if err := tokenOutOK(*out); err != nil {
			return err
		}
		tok, prefix, err := auth.NewKeyToken()
		if err != nil {
			return err
		}
		err = st.Tx(ctx, func(tx *sql.Tx) error {
			u, err := store.GetUserByName(ctx, tx, *user)
			if err != nil {
				return fmt.Errorf("user %q: %w", *user, err)
			}
			n := store.NewKey{Name: *name, Prefix: prefix, SecretHash: auth.HashToken(tok), UserID: u.ID, Scopes: split(*scopes),
				AllowedListeners: split(*listeners), AllowedCIDRs: split(*cidrs), ExpiresAt: time.Now().Add(time.Duration(*days) * 24 * time.Hour).UnixMilli()}
			if *ws != "" {
				w, err := store.GetWorkspaceBySlug(ctx, tx, *ws)
				if err != nil {
					return fmt.Errorf("workspace %s: %w", *ws, err)
				}
				if !store.UserHas(ctx, tx, u.ID, w.ID, "view") {
					return fmt.Errorf("%s can't see %s", u.Username, w.Slug)
				}
				n.WorkspaceID = &w.ID
			}
			k, err := store.CreateKey(ctx, tx, a, n)
			if err == nil {
				fmt.Fprintf(os.Stderr, "key %d (%s) created for %s, prefix %s\n", k.ID, k.Name, u.Username, k.Prefix)
			}
			return err
		})
		if err != nil {
			return err
		}
		return writeToken(*out, tok)
	case "key-revoke":
		if len(args) != 2 {
			return errors.New("usage: key-revoke <username> <key-name>")
		}
		return st.Tx(ctx, func(tx *sql.Tx) error {
			u, err := store.GetUserByName(ctx, tx, args[0])
			if err != nil {
				return fmt.Errorf("user %s: %w", args[0], err)
			}
			keys, err := store.ListKeys(ctx, tx, u.ID)
			if err != nil {
				return err
			}
			n := 0
			for _, k := range keys {
				if k.Name == args[1] && !k.Revoked {
					if err := store.RevokeKey(ctx, tx, a, k); err != nil {
						return err
					}
					n++
				}
			}
			fmt.Fprintf(os.Stderr, "%d key(s) named %s revoked for %s\n", n, args[1], u.Username)
			return nil
		})
	case "key-rotate":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		out := fs.String("out", "", "write the token to this file (0600) instead of stdout")
		if err := parse(fs, args, 1); err != nil {
			return err
		}
		var id int64
		if _, err := fmt.Sscan(fs.Arg(0), &id); err != nil {
			return errors.New("key id is a number")
		}
		if err := tokenOutOK(*out); err != nil {
			return err
		}
		tok, prefix, err := auth.NewKeyToken()
		if err != nil {
			return err
		}
		err = st.Tx(ctx, func(tx *sql.Tx) error {
			k, err := store.GetKey(ctx, tx, id)
			if err != nil {
				return err
			}
			if k.Revoked {
				return errors.New("the key is revoked")
			}
			_, err = store.RotateKey(ctx, tx, a, k, prefix, auth.HashToken(tok))
			return err
		})
		if err != nil {
			return err
		}
		return writeToken(*out, tok)
	}
	fmt.Fprint(os.Stderr, adminUsage)
	return fmt.Errorf("unknown command %q", cmd)
}

func parse(fs *flag.FlagSet, args []string, positional int) error {
	// flags may come after positional arguments: move them first
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && i+1 < len(args) && fs.Lookup(strings.TrimLeft(args[i], "-")) != nil {
				if f := fs.Lookup(strings.TrimLeft(args[i], "-")); f != nil {
					if _, isBool := f.Value.(interface{ IsBoolFlag() bool }); !isBool {
						flags = append(flags, args[i+1])
						i++
					}
				}
			}
		} else {
			pos = append(pos, args[i])
		}
	}
	if err := fs.Parse(append(flags, pos...)); err != nil {
		return err
	}
	if fs.NArg() != positional {
		return fmt.Errorf("expected %d argument(s), got %d", positional, fs.NArg())
	}
	return nil
}

func split(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// writeToken writes the token to a fresh 0600 file (replacing any old one), or to stdout.
func writeToken(path, tok string) error {
	if path == "" {
		fmt.Println(tok)
		return nil
	}
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(tok + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// tokenOutOK refuses to print a secret on a terminal.
func tokenOutOK(path string) error {
	if path != "" {
		return nil
	}
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("refusing to print a token on a terminal: redirect stdout into the file that needs it")
	}
	return nil
}

// readPassword reads a new password twice from the terminal (no echo), or once from stdin.
func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no password on stdin")
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "New password: ")
	a, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Again: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(a) != string(b) {
		return "", errors.New("the passwords differ")
	}
	return string(a), nil
}

// shellWords splits on spaces, keeping "double-quoted" groups together.
func shellWords(s string) []string {
	var out []string
	var cur strings.Builder
	in, has := false, false
	for _, r := range s {
		switch {
		case r == '"':
			in, has = !in, true
		case r == ' ' && !in:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}
