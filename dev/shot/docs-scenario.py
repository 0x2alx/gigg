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
    # wait for the live connection too, so the sidebar's footer says Live rather than Connecting
    s = [{"goto": path}, {"wait": wait}, {"wait": settle}, {"wait": ".live-dot.live-live"}, {"wait": 300}]
    if kw.get("el"):
        s.append({"eval": "(() => { document.querySelector(%s).scrollIntoView({ block: 'center' }); return true; })()" % json.dumps(kw["el"]), "expect": True})
        s.append({"wait": 400})
        s.append({"shot": shot, "el": kw["el"]})
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
    # ---- Nightwalker
    theme("nightwalker"),
    *page("/board", ".board .task-card", "board-all-night"),
    {"hover": ".task-card .run-badge"},
    {"wait": 800},
    {"shot": "tooltip-night", "full": False},
    {"goto": "/board?p=1"},
    {"wait": ".board .task-card"},
    click_text(".head-actions button", "New task"),
    {"wait": "[role=dialog] input"},
    {"type": ["[role=dialog] input", "Rotate the API signing keys"]},
    {"wait": 500},
    {"shot": "task-new-night", "full": False},
    {"goto": "/board?p=3"},
    {"wait": ".board"},
    click_text(".head-actions .head-more > button", "⋯"),
    {"wait": 200},
    click_text(".head-more .menu button", "Recurring"),
    {"wait": "[role=dialog] .rec-row, [role=dialog] li, [role=dialog] article"},
    {"wait": 700},
    {"shot": "recurring-night", "el": "[role=dialog]"},
    *page("/dashboard", ".dash-card", "dashboard-night"),
    *page("/my", "main, .vw-list-row, h1", "my-tasks-night"),
    {"goto": "/board"},
    {"wait": ".board"},
    {"eval": "(() => { window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true })); return true; })()", "expect": True},
    {"wait": "input[aria-label=Command]"},
    {"type": ["input[aria-label=Command]", "rate"]},
    {"wait": 700},
    {"shot": "palette-night", "full": False},
    *page("/profile", ".profile", "profile-night"),
    *page("/admin/workspaces/1", "form[aria-label='Workspace settings']", "admin-workspace-night"),
    {"goto": "/board"},
    {"wait": ".board"},
    {"click": "h1 .help-button"},
    {"wait": ".help-body"},
    {"wait": 900},
    {"shot": "help-night", "full": False},
    # ---- Daywalker
    theme("daywalker"),
    *page("/board?ws=acme&pr=2", ".board .task-card", "board-filters-day"),
    *page("/board/p/1/t/PLT-1", "aside.panel .tp-body", "task-panel-day", settle=1200),
    {"goto": "/board/p/1/t/PLT-4"},
    {"wait": "section[aria-label='Agent run']"},
    {"wait": 1500},
    {"eval": "(() => { document.querySelector(\"section[aria-label='Agent run']\").scrollIntoView({ block: 'start' }); return true; })()", "expect": True},
    {"wait": 400},
    {"shot": "agent-run-day", "el": "aside.panel", "margin": 0},
    *page("/inbox", ".inbox", "inbox-day"),
    *page("/w/acme/table", "table", "table-day"),
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
    {"goto": "/board"},
    {"wait": ".board .task-card"},
    click_text(".head-actions .head-more > button", "⋯"),
    {"wait": 200},
    click_text(".head-more .menu button", "Trash"),
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
