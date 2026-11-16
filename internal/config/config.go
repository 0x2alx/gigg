// Package config reads gigg's configuration from environment variables. Nothing else configures
// the server; there is no config file.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Listener is one address gigg serves on. The main listener is always present; extra listeners are
// named and optional (for example "internal": plain HTTP for local API clients).
type Listener struct {
	Name    string // "main" or the lower-cased <NAME> of GIGG_LISTEN_<NAME>
	Addr    string // host:port or :port
	TLSCert string // file paths; both set means HTTPS (main listener only)
	TLSKey  string
	UI      bool // serve the web UI and accept cookie sessions
}

// TLS reports whether the listener terminates TLS itself.
func (l Listener) TLS() bool { return l.TLSCert != "" }

// Config is the whole runtime configuration.
type Config struct {
	DataDir        string
	PublicURL      *url.URL // expected origin of the main listener; nil only in dev
	AllowedHosts   map[string]bool
	TrustedProxies []netip.Prefix
	Listeners      []Listener // [0] is main
	// DefaultTZ: the IANA time zone used when neither the request nor the user's preference names
	// one (schedules), and for the nightly snapshot unless GIGG_BACKUP_TZ says otherwise.
	DefaultTZ string
	// StaleRunHours: a run with no heartbeat for this many hours is closed as interrupted.
	StaleRunHours int
	RemoteFonts   bool
	SourceURL     string // where users get this server's source (AGPL-3.0 section 13)
	// Nightly database snapshot (backups/nightly-*.db): local time of day, zone, how many to keep.
	BackupAt   string // "HH:MM", or "" when GIGG_BACKUP_AT=off
	BackupTZ   *time.Location
	BackupKeep int
}

var extraName = regexp.MustCompile(`^GIGG_LISTEN_([A-Z][A-Z0-9]{0,15})$`)

// Load reads the configuration from the environment.
func Load() (*Config, error) { return load(os.Getenv, os.Environ()) }

func load(get func(string) string, environ []string) (*Config, error) {
	c := &Config{
		DataDir:      or(get("GIGG_DATA_DIR"), "/data"),
		AllowedHosts: map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true},
		DefaultTZ:    or(get("GIGG_DEFAULT_TZ"), "UTC"),
		RemoteFonts:  get("GIGG_REMOTE_FONTS") != "off",
		SourceURL:    or(get("GIGG_SOURCE_URL"), "https://github.com/0x2alx/gigg"),
		BackupAt:     or(get("GIGG_BACKUP_AT"), "03:30"),
		BackupKeep:   7,
	}
	var errs []error
	if c.BackupAt == "off" {
		c.BackupAt = ""
	} else if t, err := time.Parse("15:04", c.BackupAt); err != nil || t.Format("15:04") != c.BackupAt {
		errs = append(errs, fmt.Errorf("GIGG_BACKUP_AT is HH:MM or off: %q", c.BackupAt))
	}
	if _, err := time.LoadLocation(c.DefaultTZ); err != nil {
		errs = append(errs, fmt.Errorf("GIGG_DEFAULT_TZ: %w", err))
	}
	if loc, err := time.LoadLocation(or(get("GIGG_BACKUP_TZ"), c.DefaultTZ)); err != nil {
		errs = append(errs, fmt.Errorf("GIGG_BACKUP_TZ: %w", err))
	} else {
		c.BackupTZ = loc
	}
	c.StaleRunHours = 6
	if v := get("GIGG_STALE_RUN_HOURS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 168 {
			errs = append(errs, fmt.Errorf("GIGG_STALE_RUN_HOURS is 1-168: %q", v))
		}
		c.StaleRunHours = n
	}
	if v := get("GIGG_BACKUP_KEEP"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 365 {
			errs = append(errs, fmt.Errorf("GIGG_BACKUP_KEEP is 1-365: %q", v))
		}
		c.BackupKeep = n
	}

	if v := get("GIGG_PUBLIC_URL"); v != "" {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Path != "" && u.Path != "/" {
			errs = append(errs, fmt.Errorf("GIGG_PUBLIC_URL must be like https://host[:port]: %q", v))
		} else {
			c.PublicURL = u
			c.AllowedHosts[strings.ToLower(u.Hostname())] = true
		}
	}
	for _, h := range splitList(get("GIGG_ALLOWED_HOSTS")) {
		c.AllowedHosts[strings.ToLower(h)] = true
	}
	for _, p := range splitList(get("GIGG_TRUSTED_PROXIES")) {
		pfx, err := netip.ParsePrefix(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("GIGG_TRUSTED_PROXIES: %q is not a CIDR", p))
			continue
		}
		c.TrustedProxies = append(c.TrustedProxies, pfx)
	}

	main := Listener{Name: "main", Addr: or(get("GIGG_LISTEN"), ":8080"), UI: true,
		TLSCert: get("GIGG_TLS_CERT"), TLSKey: get("GIGG_TLS_KEY")}
	if (main.TLSCert == "") != (main.TLSKey == "") {
		errs = append(errs, errors.New("set both GIGG_TLS_CERT and GIGG_TLS_KEY, or neither"))
	}
	if u, err := url.Parse(c.SourceURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		errs = append(errs, fmt.Errorf("GIGG_SOURCE_URL must be an http(s) URL: %q", c.SourceURL))
	}
	if main.TLS() && c.PublicURL != nil && c.PublicURL.Scheme != "https" {
		// the browser would see https while cookies and the origin check expect http
		errs = append(errs, errors.New("with GIGG_TLS_CERT, GIGG_PUBLIC_URL must start with https://"))
	}
	c.Listeners = append(c.Listeners, main)

	var extras []Listener
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		m := extraName.FindStringSubmatch(k)
		if m == nil {
			continue
		}
		if strings.ToLower(m[1]) == "main" {
			errs = append(errs, errors.New("GIGG_LISTEN_MAIN: the name main is reserved (use GIGG_LISTEN)"))
			continue
		}
		extras = append(extras, Listener{Name: strings.ToLower(m[1]), Addr: v})
	}
	sort.Slice(extras, func(i, j int) bool { return extras[i].Name < extras[j].Name })
	c.Listeners = append(c.Listeners, extras...)

	seen := map[string]bool{}
	for _, l := range c.Listeners {
		if _, _, err := net.SplitHostPort(l.Addr); err != nil {
			errs = append(errs, fmt.Errorf("listener %s: bad address %q", l.Name, l.Addr))
		}
		if seen[l.Addr] {
			errs = append(errs, fmt.Errorf("listener %s: address %s used twice", l.Name, l.Addr))
		}
		seen[l.Addr] = true
	}
	return c, errors.Join(errs...)
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func splitList(v string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
		out = append(out, f)
	}
	return out
}
