# Security

Please report vulnerabilities privately, not in a public issue: use GitHub's **Report a
vulnerability** button on this repository (Security → Advisories). If that isn't available, email
0x2alx@proton.me with "gigg security" in the subject. Include what you found, how to
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
- An open live-update stream (`/workspaces/{ws}/events`) keeps the access it had when it connected:
  after a key is revoked, a user disabled or an access rule changed, it can go on sending what was
  visible before for up to 2 minutes, until it ends and the reconnect is checked again.
- Agents act on what they read. Anyone who can write a gig, comment or checklist item that an agent
  picks up can steer that agent: treat write access to an agent's workspace as the ability to direct
  it. Give agents their own workspaces, add only people you trust as members, and use per-gig tool
  allowlists and the agent's own sandbox to limit what a misdirected run can do.
