package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCancelledRequest: a request whose client left (a page change cancels the browser's fetches)
// answers 499, not a 500 or 503 that looks like a server failure in the logs.
func TestCancelledRequest(t *testing.T) {
	e := newEnv(t)
	for _, c := range []cred{e.ada(), e.key("scout")} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := httptest.NewRequest("GET", "https://board.example"+Prefix+fmt.Sprintf("/projects/%d/tasks", e.ids["scout.p"]), nil)
		if c.token != "" {
			r.Header.Set("Authorization", "Bearer "+c.token)
		}
		if c.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "__Host-gigg_session", Value: c.cookie})
		}
		r = r.WithContext(WithInfo(ctx, RequestInfo{Listener: c.listener, RequestID: "test"}))
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		if w.Code != statusClientClosed {
			t.Fatalf("cancelled request (%v): %d %s", c.token != "", w.Code, w.Body.String())
		}
	}
}
