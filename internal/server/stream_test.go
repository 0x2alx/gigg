package server

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The access-log wrapper must let streaming responses (SSE) flush and clear their write deadline.
func TestAccessLogSupportsStreaming(t *testing.T) {
	s, cfg := newTestServer(t)
	errc := make(chan error, 2)
	h := s.accessLog(cfg.Listeners[1], http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		errc <- rc.SetWriteDeadline(time.Time{})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": ok\n\n"))
		errc <- rc.Flush()
		<-r.Context().Done()
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	resp.Body.Close()
	if err != nil || line != ": ok\n" {
		t.Fatalf("first line %q, %v (not flushed?)", line, err)
	}
	for i := 0; i < 2; i++ {
		if err := <-errc; err != nil {
			t.Fatalf("response controller through accessLog: %v", err)
		}
	}
}
