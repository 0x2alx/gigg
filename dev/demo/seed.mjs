// Fills a fresh demo instance (bootstrapped with dev/demo/bootstrap.txt) with believable tasks for the
// documentation's screenshots: workspaces with colours, labels, priorities, due dates, checklists,
// dependencies, a custom field, agent runs with costs, a question waiting for a person, mentions and
// recurring tasks. Generic names only.
//
//   node seed.mjs <base URL of the internal listener, e.g. http://gigg-demo:8080> <dir with the *.token files>
import { readFileSync } from "node:fs";
import { randomUUID } from "node:crypto";

const [base, dir] = process.argv.slice(2);
if (!base || !dir) throw new Error("usage: node seed.mjs <base> <token dir>");
const token = (who) => readFileSync(`${dir}/${who}.token`, "utf8").trim();
const DAY = 86400000;
const now = Date.now();

function as(who) {
  const key = token(who);
  return async (method, path, body) => {
    const r = await fetch(`${base}/api/v1${path}`, {
      method,
      headers: { Authorization: `Bearer ${key}`, ...(body ? { "Content-Type": "application/json" } : {}) },
      body: body ? JSON.stringify(body) : undefined,
    });
    const text = await r.text();
    if (!r.ok) throw new Error(`${who} ${method} ${path}: ${r.status} ${text}`);
    return text ? JSON.parse(text) : null;
  };
}
const dana = as("dana");
const sam = as("sam");
const scout = as("scout");

const users = Object.fromEntries((await dana("GET", "/users")).items.map((u) => [u.username, u.id]));
const projects = {};
for (const ws of ["acme", "scout", "home"]) for (const p of (await dana("GET", `/workspaces/${ws}/projects`)).items) projects[p.key] = p.id;

for (const [ws, color] of [["acme", "#3E63DD"], ["scout", "#8E4EC6"], ["home", "#12A594"]]) await dana("PATCH", `/workspaces/${ws}`, { color });
const area = await dana("POST", "/workspaces/acme/fields", { name: "Area", kind: "select", options: ["Backend", "Frontend", "Ops"] });

const tasks = {};
async function mk(key, title, x = {}, who = dana) {
  const { area: a, checked, ...body } = x;
  const t = await who("POST", `/projects/${projects[key]}/tasks`, { title, ...body });
  if (a) await dana("PATCH", `/tasks/${t.id}/custom`, { values: { [area.id]: a } });
  if (checked) {
    const items = (await dana("GET", `/tasks/${t.id}/checklist`)).items;
    for (const i of items.slice(0, checked)) await dana("PATCH", `/tasks/${t.id}/checklist/${i.id}`, { done: true });
  }
  tasks[title] = t;
  return t;
}

// Acme Ops / Platform
await mk("PLT", "Rotate the TLS certificates", {
  category: "todo", priority: 3, due_at: now + 2 * DAY, labels: ["ops"], assignees: [users.atlas], area: "Ops",
  checklist: ["Generate the new certificates", "Deploy to staging", "Deploy to production", "Remove the old ones"], checked: 1,
  description_md: "The current certificates expire at the end of the month.\n\n- staging first, then production\n- keep the old ones until the new ones are verified",
});
await mk("PLT", "Upgrade the database to version 17", { category: "backlog", priority: 2, labels: ["db"], area: "Backend" });
await mk("PLT", "Fix the flaky login test", { category: "todo", priority: 3, labels: ["tests", "bug"], assignees: [users.scout], area: "Backend" });
await mk("PLT", "Nightly dependency audit", {
  category: "todo", priority: 2, always_run: true, model: "sonnet", effort: "medium", assignees: [users.scout], labels: ["security"],
  allowed_tools: ["Read", "Grep", "Bash(npm audit *)", "Bash(go list -m -u all)"],
  description_md: "Check every service's dependencies for known vulnerabilities and outdated majors. Report only what changed since yesterday.",
});
await mk("PLT", "Add rate limiting to the public API", { category: "todo", priority: 4, labels: ["api"], assignees: [users.scout], area: "Backend" });
await mk("PLT", "Write the incident runbook", { category: "todo", priority: 1, labels: ["docs"], assignees: [users.scout] });
await mk("PLT", "Move CI to the new build machines", { category: "backlog", priority: 2, labels: ["ops"], area: "Ops" });
await dana("POST", `/tasks/${tasks["Rotate the TLS certificates"].id}/relations`, { task: tasks["Move CI to the new build machines"].key, direction: "blocks" });

// Acme Ops / Website
await mk("WEB", "Redesign the pricing page", { category: "doing", priority: 2, labels: ["design"], assignees: [users.sam], due_at: now + 5 * DAY, area: "Frontend" }, sam);
await mk("WEB", "Compress the hero images", { category: "todo", priority: 1, labels: ["performance"], area: "Frontend" }, sam);
await mk("WEB", "Broken link in the footer", { category: "todo", priority: 3, labels: ["bug"], due_at: now - DAY, assignees: [users.sam] }, sam);
await mk("WEB", "Publish the November changelog", { category: "done", priority: 1, labels: ["docs"] }, sam);
await sam("POST", `/tasks/${tasks["Redesign the pricing page"].id}/comments`, { body_md: "First draft is up. @dana can you check the wording of the team plan?" });

// Scout (an agent's own workspace) / Research
await mk("RES", "Compare vector databases for search", { category: "doing", priority: 2, labels: ["research"], assignees: [users.scout] });
await mk("RES", "Read the new MCP specification", { category: "backlog", priority: 1, labels: ["research"] });

// Home / Household
await mk("HOM", "Book the dentist", { category: "todo", priority: 2, due_at: now + 3 * DAY, assignees: [users.dana] });
await mk("HOM", "Renew the car insurance", { category: "todo", priority: 3, due_at: now + 9 * DAY, assignees: [users.dana], labels: ["admin"] });
await mk("HOM", "Fix the garden tap", { category: "backlog", priority: 1 });
await mk("HOM", "Plan the summer trip", { category: "todo", priority: 2, checklist: ["Pick the dates", "Book the flights", "Find a place to stay"], checked: 1, assignees: [users.sam] }, sam);

// Something in the trash
await mk("WEB", "Old landing page experiment", { category: "backlog", priority: 1, labels: ["design"] }, sam);
await dana("DELETE", `/tasks/${tasks["Old landing page experiment"].id}`);

// Agent runs (as Scout): one done, one with a question for a person, one in progress, one interrupted
async function run(title, finish, usage) {
  const t = tasks[title];
  const sid = randomUUID();
  await scout("POST", `/tasks/${t.id}/move`, { category: "doing" });
  const r = await scout("POST", `/tasks/${t.id}/runs`, { session_id: sid, mode: "new", attempt: 1, model: "claude-sonnet" });
  if (finish) await scout("POST", `/runs/${r.id}/finish`, { session_id: sid, model: "claude-sonnet", ...finish });
  if (usage) await scout("POST", `/runs/${r.id}/usage`, { usage_pct: usage });
  return r;
}
await run("Write the incident runbook", {
  status: "done", move_to_category: "done", cost_usd: 0.84, num_turns: 23, duration_ms: 410000,
  report_md: "Wrote `docs/runbooks/incident.md`: who to page, the first ten minutes, the status page, and the review template.\n\nSTATUS: done",
}, 1.4);
await run("Add rate limiting to the public API", {
  status: "needs_human", move_to_category: "waiting", comment_kind: "question", cost_usd: 0.31, num_turns: 9, duration_ms: 160000,
  report_md: "Should the limit be **per API key** or **per IP address**? Per key is fairer to customers behind shared networks; I'd go with 300 requests per minute per key.",
}, 0.6);
const progress = await run("Fix the flaky login test");
await scout("POST", `/runs/${progress.id}/progress`, { pct: 60, note: "Reproduced it: the test races the session cookie. Trying a fix." });
await run("Compare vector databases for search", {
  status: "interrupted", move_to_category: "doing", cost_usd: 1.12, num_turns: 31, duration_ms: 3600000,
  report_md: "Benchmarked three of five; the run timed out while indexing the large sample. Continuing next pass.",
}, 2.1);

// Recurring tasks: a template + a schedule each
await dana("POST", `/projects/${projects.RES}/recurrences`, {
  title: "Summarise this week's issues", freq: "weekly", weekdays: [5], tz: "UTC", window_start: "09:00", window_minutes: 180,
  always_run: true, model: "sonnet", labels: ["report"], assignees: [users.scout],
  description_md: "Read the week's new and closed issues across the workspace and post a short summary: what changed, what's stuck, what needs a decision.",
});
await dana("POST", `/projects/${projects.PLT}/recurrences`, {
  title: "Check the backups", freq: "daily", tz: "UTC", window_start: "06:00", window_minutes: 120, priority: 2, labels: ["ops"], assignees: [users.atlas],
  checklist: ["Last night's backup exists", "Its size is plausible", "A test restore works"],
});
await dana("POST", `/projects/${projects.HOM}/recurrences`, {
  title: "Water the plants", freq: "weekly", weekdays: [1, 4], tz: "UTC", window_start: "08:00", window_minutes: 720, priority: 1, assignees: [users.sam],
});

console.log(`seeded ${Object.keys(tasks).length} tasks`);
