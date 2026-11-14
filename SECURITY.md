# Security

Please report vulnerabilities privately, not in a public issue: use GitHub's **Report a
vulnerability** button on this repository (Security → Advisories). If that isn't available, open an
issue that only asks for a private contact, without any details. Include what you found, how to
reproduce it, and which version or commit you tested.

You'll get an answer within a week. Fixes are released as soon as they're ready, with credit if you
want it.

## Scope and design notes

- gigg is meant to run on a private network or behind a reverse proxy you control. API keys are
  bearer tokens; keep agent keys on listeners and networks you trust (keys can be limited to a
  listener and to address ranges).
- The server never runs commands or makes outbound network requests; agents and runners are separate
  programs that use the API.
- Browser sessions are cookie-based with a CSRF header check; the main listener sets a strict
  Content-Security-Policy.
