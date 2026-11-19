#!/usr/bin/env python3
"""Writes dev/shot/scenarios/docs.json: the documentation's screenshots, taken from the demo instance
(dev/demo), half in the Nightwalker theme and half in Daywalker. Run the scenario with the ui-shot
runner, then copy the PNGs to docs/images/ (see dev/demo/README.md)."""
import json
import os

BASE = os.environ.get("GIGG_DEMO_URL", "https://gigg-demo:8443")


def theme(tid):
    return {"eval": "(async () => { const r = await fetch('/api/v1/me/themes-selection', { method: 'PUT', headers: {'Content-Type': 'application/json', 'X-Gigg-CSRF': '1'}, body: JSON.stringify({ selected_id: '%s', match_system: false }) }); return r.status; })()" % tid, "expect": 204}


def click_text(sel, text):
    return {"eval": "(() => { const b = [...document.querySelectorAll(%s)].find(x => x.textContent.includes(%s)); if (!b) return false; b.click(); return true; })()" % (json.dumps(sel), json.dumps(text)), "expect": True}


def page(path, wait, shot, settle=1800, **kw):
    s = [{"goto": path}, {"wait": wait}, {"wait": settle}, {"wait": 300}]
    if kw.get("el"):
        s.append({"eval": "(() => { document.querySelector(%s).scrollIntoView({ block: 'center' }); return true; })()" % json.dumps(kw["el"]), "expect": True})
        s.append({"wait": 400})
        s.append({"shot": shot, "el": kw["el"], **({"margin": kw["margin"]} if "margin" in kw else {})})
    else:
        s.append({"shot": shot, "full": False})
    return s


steps = [
    {"goto": "/"},
    {"wait": "input[autocomplete=username]"},
    {"type": ["input[autocomplete=username]", "dana"]},
    {"type": ["input[autocomplete=current-password]", "demo-password-2026"]},
    {"press": "Enter"},
    {"wait": ".board"},
    {"wait": 6000},  # the container's network settles once at the start (Chromium: ERR_NETWORK_CHANGED)
    # a fresh account's look: the filter row folded and no remembered filter (an earlier run left both)
    {"eval": "(() => { fetch('/api/v1/me', { method: 'PATCH', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ preferences: { board_filters: null, board_filter: null } }) }); return true; })()", "expect": True},
    {"wait": 800},
    # ---- Nightwalker
    theme("nightwalker"),
    *page("/board", ".board .gig-card", "board-all-night"),
    {"hover": ".gig-card .run-badge"},
    {"wait": 800},
    {"shot": "tooltip-night", "full": False},
    {"goto": "/board?p=1"},
    {"wait": ".board .gig-card"},
    click_text(".appbar a", "New gig"),
    {"wait": "[role=dialog] input"},
    {"type": ["[role=dialog] input", "Rotate the API signing keys"]},
    {"wait": 500},
    {"shot": "gig-new-night", "full": False},
    # the account menu's tools are links to the board with ?tool=
    {"goto": "/board?p=3&tool=recurring"},
    {"wait": "[role=dialog] .rec-row, [role=dialog] li, [role=dialog] article"},
    {"wait": 700},
    {"shot": "recurring-night", "el": "[role=dialog]"},
    # the dashboard's cards fill a narrower window: the shot shows them without the empty half
    {"viewport": [1100, 560]},
    *page("/dashboard", ".dash-card", "dashboard-night"),
    {"viewport": [1440, 900]},
    # the errands list (one failing, one paused), in a window as tall as the list
    {"viewport": [1440, 420]},
    *page("/errands", ".errand-table", "errands-night"),
    {"viewport": [1440, 900]},
    # a gig waiting on an agent's question: its panel
    *page("/board/p/1/g/PLT-5", "aside.panel .tp-body", "gig-question-night", settle=1200, el="aside.panel", margin=0),
    {"goto": "/board"},
    {"wait": ".board"},
    {"eval": "(() => { window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true })); return true; })()", "expect": True},
    {"wait": "input[aria-label=Command]"},
    {"wait": 2500},
    # the palette alone, empty: its placeholder says what it takes, and the list shows what it offers
    {"shot": "palette-night", "el": ".cp", "margin": 0},
    # Themes > Make with AI, with a description typed in
    {"goto": "/themes"},
    {"wait": "h1"},
    {"wait": 1200},
    click_text("button", "Make with AI"),
    {"wait": "[role=dialog] textarea"},
    {"type": ["[role=dialog] textarea", "Warm sunset colours, rounded corners, a techy font, slightly bigger text."]},
    {"wait": 500},
    {"shot": "theme-ai-night", "el": "[role=dialog]"},
    {"press": "Escape"},
    *page("/profile", ".profile", "profile-night"),
    *page("/admin/workspaces/1", "form[aria-label='Workspace settings']", "admin-workspace-night"),
    {"goto": "/board"},
    {"wait": ".board"},
    {"click": ".bf-end .help-button"},
    {"wait": ".help-body"},
    {"wait": 900},
    {"shot": "help-night", "full": False},
    # ---- Daywalker
    theme("daywalker"),
    # the filter row starts folded: open it for the filters screenshot (saved on the account, so a reload keeps it)
    {"eval": "(() => { fetch('/api/v1/me', { method: 'PATCH', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ preferences: { board_filters: 'open' } }) }); return true; })()", "expect": True},
    {"wait": 800},
    *page("/board?ws=acme&pr=2", ".board .gig-card", "board-filters-day"),
    *page("/board/p/1/g/PLT-1", "aside.panel .tp-body", "gig-panel-day", settle=1200),
    {"goto": "/board/p/1/g/PLT-4"},
    {"wait": "section[aria-label='Agent run']"},
    {"wait": 1500},
    # the section itself (the panel can't scroll it to the top: it's near the end), scrolled at once
    # (the panel scrolls smoothly, and the shot would catch it on the way)
    {"eval": "(() => { document.querySelector(\"section[aria-label='Agent run']\").scrollIntoView({ block: 'center', behavior: 'instant' }); return true; })()", "expect": True},
    {"wait": 400},
    {"shot": "agent-run-day", "el": "section[aria-label='Agent run']", "margin": 8},
    *page("/inbox", ".inbox", "inbox-day"),
    *page("/table", "table", "table-day"),
    *page("/themes", "h1", "themes-day"),
    {"goto": "/admin/users"},
    {"wait": "table"},
    {"eval": "(() => { const row = [...document.querySelectorAll('tr')].find(r => r.textContent.includes('Sam Okafor')); const b = row && [...row.querySelectorAll('button')].find(x => x.textContent.includes('Permissions')); if (!b) return false; b.click(); return true; })()", "expect": True},
    {"wait": "[role=dialog]"},
    {"wait": 900},
    click_text("[role=dialog] button", "Acme Ops"),
    {"wait": 700},
    {"shot": "admin-permissions-day", "full": False},
    # the demo's keys were last used from the container network: show a documentation address instead
    {"goto": "/admin/keys"},
    {"wait": "h1"},
    {"wait": 1800},
    {"eval": "(() => { const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT); for (let n = w.nextNode(); n; n = w.nextNode()) n.nodeValue = n.nodeValue.replace(/\\b(10|172|192)\\.\\d+\\.\\d+\\.\\d+\\b/g, '192.0.2.10'); return true; })()", "expect": True},
    {"shot": "admin-keys-day", "full": False},
    {"goto": "/board?tool=trash"},
    {"wait": ".trash-item"},
    {"wait": 600},
    {"shot": "trash-day", "el": "[role=dialog]"},
    {"viewport": [390, 844]},
    *page("/board", ".board", "board-mobile-day"),
    {"viewport": [1440, 900]},
    theme("nightwalker"),
]

out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "scenarios", "docs.json")
with open(out, "w") as f:
    json.dump({"base": BASE, "viewport": [1440, 900], "waitUntil": "domcontentloaded", "steps": steps}, f, indent=1)
print(f"wrote {out}: {len(steps)} steps")
