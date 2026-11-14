package config

import (
	"testing"
)

func env(m map[string]string) (func(string) string, []string) {
	var list []string
	for k, v := range m {
		list = append(list, k+"="+v)
	}
	return func(k string) string { return m[k] }, list
}

func TestDefaults(t *testing.T) {
	c, err := load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Listeners) != 1 || c.Listeners[0].Name != "main" || c.Listeners[0].Addr != ":8080" || c.Listeners[0].TLS() || !c.Listeners[0].UI {
		t.Fatalf("main listener: %+v", c.Listeners)
	}
	if c.DataDir != "/data" || !c.RemoteFonts || c.DefaultTZ != "UTC" || c.PublicURL != nil {
		t.Fatalf("defaults: %+v", c)
	}
	for _, h := range []string{"localhost", "127.0.0.1", "::1"} {
		if !c.AllowedHosts[h] {
			t.Errorf("%s not allowed by default", h)
		}
	}
}

func TestListenersAndHosts(t *testing.T) {
	c, err := load(env(map[string]string{
		"GIGG_LISTEN": ":8443", "GIGG_TLS_CERT": "/c", "GIGG_TLS_KEY": "/k",
		"GIGG_LISTEN_INTERNAL": ":8080", "GIGG_LISTEN_ADMIN": "127.0.0.1:9000",
		"GIGG_PUBLIC_URL": "https://Board.Example:3443", "GIGG_ALLOWED_HOSTS": "gigg, other.example",
		"GIGG_TRUSTED_PROXIES": "10.0.0.0/8", "GIGG_REMOTE_FONTS": "off",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Listeners[0].TLS() || c.Listeners[1].Name != "admin" || c.Listeners[2].Name != "internal" || c.Listeners[2].UI {
		t.Fatalf("listeners: %+v", c.Listeners)
	}
	for _, h := range []string{"board.example", "gigg", "other.example"} {
		if !c.AllowedHosts[h] {
			t.Errorf("%s should be allowed", h)
		}
	}
	if c.RemoteFonts || len(c.TrustedProxies) != 1 {
		t.Fatalf("fonts/proxies: %+v", c)
	}
	// the backup zone follows the default zone unless set
	c, err = load(env(map[string]string{"GIGG_DEFAULT_TZ": "Europe/Bucharest"}))
	if err != nil || c.DefaultTZ != "Europe/Bucharest" || c.BackupTZ.String() != "Europe/Bucharest" {
		t.Fatalf("default zone: %v %+v", err, c)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"only cert":    {"GIGG_TLS_CERT": "/c"},
		"bad url":      {"GIGG_PUBLIC_URL": "ftp://x"},
		"url path":     {"GIGG_PUBLIC_URL": "https://x/board"},
		"bad cidr":     {"GIGG_TRUSTED_PROXIES": "10.0.0.0/99"},
		"bad addr":     {"GIGG_LISTEN": "8080"},
		"same address": {"GIGG_LISTEN": ":8080", "GIGG_LISTEN_INTERNAL": ":8080"},
		"bad zone":     {"GIGG_DEFAULT_TZ": "Mars/Olympus"},
	}
	for name, m := range cases {
		if _, err := load(env(m)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// lower-case or odd extra names are not listeners (and are ignored, not errors)
	c, err := load(env(map[string]string{"GIGG_LISTEN_bad": ":1", "GIGG_LISTEN_": ":2"}))
	if err != nil || len(c.Listeners) != 1 {
		t.Fatalf("odd names: %v %+v", err, c.Listeners)
	}
}
