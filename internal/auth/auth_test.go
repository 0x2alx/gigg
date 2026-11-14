package auth

import (
	"strings"
	"testing"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("format %s", h)
	}
	if !VerifyPassword(h, "correct horse battery") || VerifyPassword(h, "correct horse batterY") {
		t.Fatal("verify")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("salt not random")
	}
	for _, bad := range []string{"", "x", "$argon2i$v=19$m=1,t=1,p=1$AA$AA", "$argon2id$v=19$m=99999999,t=3,p=2$AA$AA", h + "x$"} {
		if VerifyPassword(bad, "correct horse battery") {
			t.Fatalf("matched malformed %q", bad)
		}
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("short password accepted")
	}
}

func TestKeyToken(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		tok, prefix, err := NewKeyToken()
		if err != nil {
			t.Fatal(err)
		}
		p, ok := ParseKeyToken(tok)
		if !ok || p != prefix || len(prefix) != 8 || seen[tok] {
			t.Fatalf("token %q prefix %q", tok, prefix)
		}
		seen[tok] = true
		if !TokenMatches(tok, HashToken(tok)) || TokenMatches(tok+"x", HashToken(tok)) {
			t.Fatal("match")
		}
	}
	for _, bad := range []string{"", "gigg_", "gigg_ABCDEFGH_" + strings.Repeat("a", 43), "gigg_abcdefgh_" + strings.Repeat("a", 42), "Bearer gigg_abcdefgh_" + strings.Repeat("a", 43)} {
		if _, ok := ParseKeyToken(bad); ok {
			t.Fatalf("parsed %q", bad)
		}
	}
}
