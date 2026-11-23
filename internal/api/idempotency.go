package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/store"
)

// Idempotency-Key on POST: an authenticated caller that sends the header gets the stored response
// back (with Idempotent-Replayed: true) when it repeats the same request within 24 h, instead of
// the request running twice. The same key with a different request is a 422; a repeat while the
// first is still running is a 409. Responses with status >= 500 aren't kept (the request didn't
// complete, so running it again is right), nor anything from routes that carry credentials. A
// response over 256 KiB isn't kept either, but its key is: a repeat gets a 409
// idempotency_unreplayable instead of running the request a second time.
const (
	idemTTL     = 24 * 3600 * 1000 // ms; Housekeeping deletes older rows
	idemMaxRows = 2000             // per principal per day; beyond that responses aren't kept
	// request bodies buffered for hashing: above every route's own limit, uploads included
	idemMaxBody = store.AttachFileMaxCeiling + 64<<10
)

var idemMaxResponse = 256 << 10 // a variable so tests can lower it

// secretBearing routes take or return credentials (passwords, key tokens, session cookies): never
// stored for replay, and their bodies never hashed into the table.
func secretBearing(path string) bool {
	for _, p := range []string{"/auth/", "/keys", "/me/keys", "/users"} {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return strings.Contains(path, "/password") || strings.HasSuffix(path, "/rotate")
}

// idemInflight holds the (principal, key) pairs being processed, per API.
var idemInflight sync.Map // string -> struct{}

type idemStored struct {
	ContentType string `json:"ct,omitempty"`
	ETag        string `json:"etag,omitempty"`
	Location    string `json:"loc,omitempty"`
	Body        []byte `json:"b"`
}

func validIdemKey(k string) bool {
	if len(k) < 1 || len(k) > 200 {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < 0x20 || k[i] > 0x7e {
			return false
		}
	}
	return true
}

// idempotent wraps the API mux (after authenticate, which puts the principal in the context).
func (a *API) idempotent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		p := access.From(r.Context())
		if r.Method != http.MethodPost || key == "" || p == nil || secretBearing(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if !validIdemKey(key) {
			writeProblem(w, fail(400, "bad_request", "Idempotency-Key must be 1-200 printable ASCII characters."))
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, idemMaxBody+1))
		if err != nil {
			writeProblem(w, fail(400, "bad_request", "Could not read the request body."))
			return
		}
		if len(body) > idemMaxBody {
			writeProblem(w, fail(413, "too_large", "Request body too large."))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		h := sha256.New()
		for _, part := range []string{r.Method, r.URL.Path, r.URL.RawQuery} {
			h.Write([]byte(part))
			h.Write([]byte{0})
		}
		h.Write(body)
		hash := hex.EncodeToString(h.Sum(nil))
		who := principalID(p)

		lock := fmt.Sprintf("%p", a) + "\x00" + who + "\x00" + key
		if _, busy := idemInflight.LoadOrStore(lock, struct{}{}); busy {
			writeProblem(w, fail(409, "idempotency_in_progress", "A request with this Idempotency-Key is still being processed; retry shortly."))
			return
		}
		defer idemInflight.Delete(lock)

		ctx := r.Context()
		var storedHash string
		var status int
		var resp []byte
		err = a.st.R.QueryRowContext(ctx, `SELECT request_hash, status, response FROM idempotency_keys WHERE principal = ? AND key = ? AND created_at > ?`,
			who, key, store.Now()-idemTTL).Scan(&storedHash, &status, &resp)
		switch {
		case err == nil && storedHash != hash:
			writeProblem(w, fail(422, "idempotency_mismatch", "This Idempotency-Key was used for a different request."))
			return
		case err == nil:
			var s idemStored
			if json.Unmarshal(resp, &s) == nil {
				hd := w.Header()
				for k, v := range map[string]string{"Content-Type": s.ContentType, "ETag": s.ETag, "Location": s.Location} {
					if v != "" {
						hd.Set(k, v)
					}
				}
				hd.Set("Idempotent-Replayed", "true")
				w.WriteHeader(status)
				_, _ = w.Write(s.Body)
				return
			}
			// unreadable row: run the request again and overwrite it
		case !errors.Is(err, sql.ErrNoRows):
			a.log.Error("idempotency lookup", "err", err)
			writeProblem(w, fail(500, "internal", "Internal error."))
			return
		}

		rec := &idemRecorder{ResponseWriter: w}
		committed := new(atomic.Bool) // set by a.tx when a write of this request committed
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), committedKey{}, committed)))
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		// not stored when it may succeed on a retry: server errors, rate limits, and a request cancelled
		// before any write committed. One cancelled after its write committed (post-commit reads failed)
		// already ran: a retry must not run it again, so it is remembered as such.
		if rec.status == statusClientClosed && committed.Load() {
			rec.over = true // stored below as "already ran" (409 idempotency_unreplayable)
		} else if rec.status >= 500 || rec.status == statusClientClosed || rec.status == http.StatusTooManyRequests {
			return
		}
		// the request already ran: store even if the client has gone (the retry is what this is for)
		ctx = context.WithoutCancel(ctx)
		var rows int
		_ = a.st.R.QueryRowContext(ctx, `SELECT count(*) FROM idempotency_keys WHERE principal = ?`, who).Scan(&rows)
		if rows >= idemMaxRows {
			return
		}
		hd := w.Header()
		status, stored := rec.status, idemStored{ContentType: hd.Get("Content-Type"), ETag: hd.Get("ETag"), Location: hd.Get("Location"), Body: rec.buf.Bytes()}
		if rec.over {
			// too big to keep: remember that it ran, so a repeat doesn't run it again
			pb, _ := json.Marshal(fail(409, "idempotency_unreplayable", "This request already ran, but its answer couldn't be kept; reload to see the result."))
			status, stored = http.StatusConflict, idemStored{ContentType: "application/problem+json", Body: append(pb, '\n')}
		}
		b, _ := json.Marshal(stored)
		if _, err := a.st.W.ExecContext(ctx, `INSERT OR REPLACE INTO idempotency_keys (principal, key, request_hash, status, response, created_at) VALUES (?,?,?,?,?,?)`,
			who, key, hash, status, b, store.Now()); err != nil {
			a.log.Error("idempotency store", "err", err)
		}
	})
}

// idemRecorder passes the response through and keeps a copy (up to idemMaxResponse).
type idemRecorder struct {
	http.ResponseWriter
	status int
	buf    bytes.Buffer
	over   bool
}

func (r *idemRecorder) WriteHeader(c int) {
	if r.status == 0 {
		r.status = c
	}
	r.ResponseWriter.WriteHeader(c)
}

func (r *idemRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if !r.over {
		if r.buf.Len()+len(b) > idemMaxResponse {
			r.over = true
			r.buf.Reset()
		} else {
			r.buf.Write(b)
		}
	}
	return r.ResponseWriter.Write(b)
}

func (r *idemRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// committedKey: the request context value (*atomic.Bool) that a.tx sets once a write committed.
type committedKey struct{}
