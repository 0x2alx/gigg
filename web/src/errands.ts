// Errands: commands an agent's executor runs on a schedule, sandboxed, with a run history (no gigs).
// Types, API calls and the editor's pure helpers (form state <-> API body, a description of the
// timing, validation), unit-tested on their own.
import { call } from "./http";
import { WEEKDAYS, localTZ } from "./recurrence";

export type ErrandFreq = "minutes" | "hours" | "daily" | "weekly" | "monthly";
export type ErrandRunStatus = "running" | "ok" | "failed" | "timeout" | "missed" | "skipped";

export interface ErrandRule {
  freq: ErrandFreq;
  interval: number;
  weekdays: number[] | null;
  month_day: number | null;
  tz: string;
  window_start: string;
  window_minutes: number;
  lead_minutes: number;
}

export interface ErrandRun {
  id: number;
  errand_id: number;
  workspace_id: number;
  worker_id: number;
  scheduled_for: number | null; // null: a run-now request
  started_at: number | null;
  finished_at: number | null;
  status: ErrandRunStatus;
  exit_code: number | null;
  duration_ms: number | null;
  sandboxed: boolean;
  has_output: boolean;
  output_tail?: string; // only with errands:output
  output_bytes: number | null;
  error: string | null;
  created_at: number;
}

export interface Errand {
  id: number;
  workspace_id: number;
  worker_id: number;
  worker_username: string;
  created_by: number;
  on_failure_gig: boolean;
  next_at: number | null;
  run_now_at: number | null;
  paused: boolean;
  state: "never" | "ok" | "failing";
  fail_streak: number;
  failing_since: number | null;
  last_run: ErrandRun | null;
  created_at: number;
  version: number;
  template_id: number;
  name: string;
  description_md: string;
  command: string;
  timeout_s: number;
  output: "gigg" | "host";
  unsandboxed: boolean;
  command_set_by: number | null;
  unsandboxed_set_by: number | null;
  template_uses: number;
  template_shared?: boolean;
  schedule_id: number;
  schedule_name: string;
  schedule_shared?: boolean;
  schedule_text: string;
  rule: ErrandRule;
}

// ErrandTemplate: an errand's content (name, command, timeout, output, sandbox). Errands share one when
// it's the same; a shared one (workspace_id 0) is every workspace's, changed only by instance admins.
export interface ErrandTemplate {
  id: number;
  workspace_id: number;
  shared?: boolean;
  name: string;
  description_md: string;
  command: string;
  timeout_s: number;
  output: "gigg" | "host";
  unsandboxed: boolean;
  version: number;
  uses: number; // live errands using it
}

export type ErrandTemplateBody = Pick<ErrandTemplate, "name" | "description_md" | "command" | "timeout_s" | "output" | "unsandboxed">;

export interface ErrandBody {
  template_id?: number; // an existing template (its content as is) instead of the content fields
  name?: string;
  description_md?: string;
  command?: string;
  timeout_s?: number;
  output?: "gigg" | "host";
  unsandboxed?: boolean;
  rule?: Partial<ErrandRule> & { freq: ErrandFreq };
  worker?: string;
  on_failure_gig?: boolean;
  paused?: boolean;
}

export const errandApi = {
  list: (ws: string | number) => call<{ items: Errand[] }>("GET", `/workspaces/${ws}/errands`),
  get: (id: number) => call<Errand>("GET", `/errands/${id}`),
  create: (ws: string | number, body: ErrandBody) => call<Errand>("POST", `/workspaces/${ws}/errands`, body),
  patch: (id: number, body: ErrandBody, version?: number) => call<Errand>("PATCH", `/errands/${id}`, body, version),
  remove: (id: number) => call<void>("DELETE", `/errands/${id}`),
  pause: (id: number) => call<Errand>("POST", `/errands/${id}/pause`),
  resume: (id: number) => call<Errand>("POST", `/errands/${id}/resume`),
  runNow: (id: number) => call<Errand>("POST", `/errands/${id}/run-now`),
  runs: (id: number, before = 0, limit = 30) => call<{ items: ErrandRun[] }>("GET", `/errands/${id}/runs?limit=${limit}${before ? `&before=${before}` : ""}`),
  run: (id: number) => call<ErrandRun>("GET", `/errand-runs/${id}`),
  templates: (ws: string | number) => call<{ items: ErrandTemplate[] }>("GET", `/workspaces/${ws}/errand-templates`),
  sharedTemplates: () => call<{ items: ErrandTemplate[] }>("GET", `/shared/errand-templates`),
  createSharedTemplate: (body: ErrandTemplateBody) => call<ErrandTemplate>("POST", `/shared/errand-templates`, body),
  putTemplate: (id: number, body: ErrandTemplateBody, version?: number) => call<ErrandTemplate>("PUT", `/errand-templates/${id}`, body, version),
  removeTemplate: (id: number) => call<void>("DELETE", `/errand-templates/${id}`),
};

// contentOf: the form fields a template fills in.
export function contentOf(t: ErrandTemplate): Pick<ErrandForm, "name" | "description" | "command" | "timeout" | "output" | "unsandboxed"> {
  return { name: t.name, description: t.description_md, command: t.command, timeout: t.timeout_s, output: t.output, unsandboxed: t.unsandboxed };
}

// sameContent: the form still has the template's content (then the errand uses that template as is).
export function sameContent(f: ErrandForm, t: ErrandTemplate): boolean {
  const c = contentOf(t);
  return f.name.trim() === c.name && f.description === c.description && f.command === c.command && f.timeout === c.timeout && f.output === c.output && f.unsandboxed === c.unsandboxed;
}

// templateBodyOf: a template's content from the form.
export function templateBodyOf(f: ErrandForm): ErrandTemplateBody {
  return { name: f.name.trim(), description_md: f.description, command: f.command, timeout_s: f.timeout, output: f.output, unsandboxed: f.unsandboxed };
}

// ---- the editor's form

export interface ErrandForm {
  name: string;
  description: string;
  command: string;
  timeout: number; // seconds
  output: "gigg" | "host";
  unsandboxed: boolean;
  freq: ErrandFreq;
  interval: number;
  weekdays: number[]; // weekly: the days; minutes/hours: only these (empty = every day)
  monthDay: number;
  at: string; // daily/weekly/monthly: run time
  limited: boolean; // minutes/hours: only between from and to
  from: string;
  to: string;
  tz: string;
  worker: string; // username; "" = yourself
  onFailureGig: boolean;
}

export function defaultErrandForm(now: Date = new Date()): ErrandForm {
  return {
    name: "",
    description: "",
    command: "",
    timeout: 300,
    output: "gigg",
    unsandboxed: false,
    freq: "minutes",
    interval: 15,
    weekdays: [], // minutes: every day; switching to weekly picks Monday
    monthDay: now.getDate(),
    at: "03:00",
    limited: false,
    from: "09:00",
    to: "17:00",
    tz: localTZ(),
    worker: "",
    onFailureGig: false,
  };
}

const HM = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;
const pad = (n: number) => String(n).padStart(2, "0");
const toMin = (hm: string) => Number(hm.slice(0, 2)) * 60 + Number(hm.slice(3, 5));
const toHM = (m: number) => `${pad(Math.floor(m / 60) % 24)}:${pad(m % 60)}`;
export const frequent = (f: ErrandFreq) => f === "minutes" || f === "hours";

// formOfErrand: the editor's state for an existing errand.
export function formOfErrand(c: Errand): ErrandForm {
  const r = c.rule;
  const fr = frequent(r.freq);
  const allDay = r.window_start === "00:00" && r.window_minutes === 1440;
  return {
    name: c.name,
    description: c.description_md,
    command: c.command,
    timeout: c.timeout_s,
    output: c.output,
    unsandboxed: c.unsandboxed,
    freq: r.freq,
    interval: r.interval > 0 ? r.interval : 1,
    weekdays: r.weekdays ?? [],
    monthDay: r.month_day ?? 1,
    at: fr ? "03:00" : r.window_start,
    limited: fr && !allDay,
    from: fr && !allDay ? r.window_start : "09:00",
    to: fr && !allDay ? toHM(toMin(r.window_start) + r.window_minutes) : "17:00",
    tz: r.tz || localTZ(),
    worker: c.worker_username,
    onFailureGig: c.on_failure_gig,
  };
}

// windowOf: minutes/hours ticking window (from..to, past midnight when to <= from).
function windowOf(f: ErrandForm): { start: string; minutes: number } {
  if (!f.limited) return { start: "00:00", minutes: 1440 };
  let m = toMin(f.to) - toMin(f.from);
  if (m <= 0) m += 1440;
  return { start: f.from, minutes: m };
}

// errandRule: the rule for the API (fields that don't apply are left out).
export function errandRule(f: ErrandForm): ErrandBody["rule"] & { freq: ErrandFreq } {
  const r: ErrandBody["rule"] & { freq: ErrandFreq } = { freq: f.freq, interval: f.interval, tz: f.tz };
  if (frequent(f.freq)) {
    const w = windowOf(f);
    r.window_start = w.start;
    r.window_minutes = w.minutes;
    if (f.weekdays.length > 0 && f.weekdays.length < 7) r.weekdays = [...f.weekdays].sort((a, b) => a - b);
  } else {
    r.window_start = f.at;
    r.window_minutes = 60;
    if (f.freq === "weekly") r.weekdays = [...f.weekdays].sort((a, b) => a - b);
    if (f.freq === "monthly") r.month_day = f.monthDay;
  }
  return r;
}

// sameTiming: the form still holds the errand's timing (so a save doesn't move it to another schedule).
export function sameTiming(f: ErrandForm, r: ErrandRule): boolean {
  const n = errandRule(f);
  if (n.freq !== r.freq || n.interval !== (r.interval || 1) || n.tz !== r.tz || n.window_start !== r.window_start) return false;
  const days = (d?: number[] | null) => JSON.stringify([...(d ?? [])].sort((a, b) => a - b));
  if (frequent(n.freq)) {
    const rd = r.weekdays && r.weekdays.length < 7 ? r.weekdays : [];
    return n.window_minutes === r.window_minutes && days(n.weekdays) === days(rd);
  }
  if (n.freq === "weekly" && days(n.weekdays) !== days(r.weekdays)) return false;
  if (n.freq === "monthly" && n.month_day !== r.month_day) return false;
  return true;
}

// errandProblem: the first thing that stops the form from saving, or null.
export function errandProblem(f: ErrandForm): string | null {
  if (!f.name.trim()) return "Give it a name.";
  if (!f.command.trim()) return "Give it a command.";
  if (f.command.length > 16384) return "The command is limited to 16 KiB.";
  if (!Number.isInteger(f.timeout) || f.timeout < 10 || f.timeout > 43200) return "Timeout: 10 to 43200 seconds (12 h).";
  const max = { minutes: 360, hours: 24, daily: 365, weekly: 52, monthly: 12 }[f.freq];
  if (!Number.isInteger(f.interval) || f.interval < 1 || f.interval > max) return `Every 1 to ${max}.`;
  if (frequent(f.freq)) {
    if (f.limited && (!HM.test(f.from) || !HM.test(f.to))) return "Pick the times it ticks between.";
    if (f.limited && f.from === f.to) return "The from and to times are the same: untick “Only between” to run all day.";
  } else if (!HM.test(f.at)) return "Pick the time it runs.";
  if (f.freq === "weekly" && f.weekdays.length === 0) return "Pick at least one weekday.";
  if (f.freq === "monthly" && (f.monthDay === 0 || f.monthDay < -1 || f.monthDay > 31)) return "Day of the month: 1 to 31, or the last day.";
  return null;
}

function days(d: number[]): string {
  const k = [...new Set(d)].sort((a, b) => a - b).join(",");
  if (k === "1,2,3,4,5") return "weekdays";
  if (k === "6,7") return "weekends";
  return [...new Set(d)].sort((a, b) => a - b).map((x) => WEEKDAYS[x - 1]).join(", ");
}

const ord = (n: number) => n + (n % 100 >= 11 && n % 100 <= 13 ? "th" : (["th", "st", "nd", "rd"][n % 10] ?? "th"));

// describeErrand: "Every 5 min", "Every 2 h on weekdays, 09:00–17:00", "Every day at 03:30",
// "Every Mon, Thu at 08:00", "Monthly on the last day at 06:00" (as the server names it).
export function describeErrand(f: ErrandForm): string {
  const n = f.interval > 1 ? f.interval : 1;
  let s: string;
  switch (f.freq) {
    case "minutes":
    case "hours": {
      s = f.freq === "hours" ? (n === 1 ? "Every hour" : `Every ${n} h`) : n === 1 ? "Every minute" : `Every ${n} min`;
      if (f.weekdays.length > 0 && f.weekdays.length < 7) s += ` on ${days(f.weekdays)}`;
      if (f.limited && HM.test(f.from) && HM.test(f.to)) s += `, ${f.from}–${f.to === "00:00" ? "24:00" : f.to}`;
      return s;
    }
    case "daily":
      s = n === 1 ? "Every day" : `Every ${n} days`;
      break;
    case "weekly": {
      const d = days(f.weekdays);
      const every = f.weekdays.length === 7 ? "day" : d === "weekdays" ? "weekday" : d === "weekends" ? "weekend day" : d;
      s = n === 1 ? `Every ${every}` : `Every ${n} weeks on ${d}`;
      break;
    }
    case "monthly": {
      const on = f.monthDay === -1 ? "the last day" : `the ${ord(f.monthDay)}`;
      s = n === 1 ? `Monthly on ${on}` : `Every ${n} months on ${on}`;
      break;
    }
  }
  return `${s} at ${f.at}`;
}

// errandBody: what to send. A new errand gets everything; an edit only what changed (so a save that
// doesn't touch the timing keeps a shared schedule, and the sandbox flag is only sent by someone who may set it).
// A new errand whose form still has a picked template's content sends that template's id instead of the
// content (it then uses it as is; the server copies a shared one when needed).
export function errandBody(f: ErrandForm, cur: Errand | null, canUnsandbox: boolean, picked?: ErrandTemplate | null): ErrandBody {
  const b: ErrandBody = {};
  if (!cur && picked && sameContent(f, picked)) {
    b.template_id = picked.id;
    b.rule = errandRule(f);
    if (f.worker) b.worker = f.worker;
    if (f.onFailureGig) b.on_failure_gig = true;
    return b;
  }
  const content: [keyof ErrandBody, unknown, unknown][] = [
    ["name", f.name.trim(), cur?.name],
    ["description_md", f.description, cur?.description_md],
    ["command", f.command, cur?.command],
    ["timeout_s", f.timeout, cur?.timeout_s],
    ["output", f.output, cur?.output],
  ];
  for (const [k, v, old] of content) if (!cur || v !== old) (b as Record<string, unknown>)[k] = v;
  if (canUnsandbox && (!cur ? f.unsandboxed : f.unsandboxed !== cur.unsandboxed)) b.unsandboxed = f.unsandboxed;
  if (!cur || !sameTiming(f, cur.rule)) b.rule = errandRule(f);
  if (f.worker && (!cur || f.worker !== cur.worker_username)) b.worker = f.worker;
  if (!cur ? f.onFailureGig : f.onFailureGig !== cur.on_failure_gig) b.on_failure_gig = f.onFailureGig;
  return b;
}

// detaching: what saving this edit (body) takes the errand off: a shared errand template (its content
// changes) or a shared schedule (its timing changes). The server gives it copies of its own.
export function detaching(cur: Errand | null, b: ErrandBody): string[] {
  if (!cur) return [];
  const out: string[] = [];
  const content = ["name", "description_md", "command", "timeout_s", "output", "unsandboxed"] as const;
  if (cur.template_shared && content.some((k) => k in b)) out.push(`the shared errand template “${cur.name}”: it gets its own copy in this workspace`);
  if (cur.schedule_shared && b.rule) out.push(`the shared schedule “${cur.schedule_name}”: it gets a schedule of its own`);
  return out;
}

// ---- display

// schedText: the server's description, without the time zone when it's the viewer's own.
export function schedText(c: Pick<Errand, "schedule_text" | "rule">, tz: string): string {
  const suffix = ` (${c.rule.tz})`;
  return c.rule.tz === tz && c.schedule_text.endsWith(suffix) ? c.schedule_text.slice(0, -suffix.length) : c.schedule_text;
}

export function durMS(ms: number | null): string {
  if (ms === null) return "";
  if (ms < 1000) return `${ms} ms`;
  const s = ms / 1000;
  if (s < 60) return `${s < 10 ? s.toFixed(1) : Math.round(s)} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return Math.round(s % 60) ? `${m} min ${Math.round(s % 60)} s` : `${m} min`;
  return `${Math.floor(m / 60)} h ${m % 60} min`;
}

export function bytesText(n: number | null): string {
  if (n === null) return "";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10240 ? 1 : 0)} KiB`;
  return `${(n / 1024 / 1024).toFixed(1)} MiB`;
}

// relTime: "in 4 min", "3 h ago", "just now".
export function relTime(ms: number, now: number): string {
  const d = ms - now;
  const a = Math.abs(d);
  const f = a < 45_000 ? "" : a < 3_600_000 ? `${Math.round(a / 60_000)} min` : a < 86_400_000 ? `${Math.round(a / 3_600_000)} h` : `${Math.round(a / 86_400_000)} d`;
  if (!f) return d > 0 ? "in a moment" : "just now";
  return d > 0 ? `in ${f}` : `${f} ago`;
}

export const RUN_STATUS_TEXT: Record<ErrandRunStatus, string> = {
  running: "running",
  ok: "ok",
  failed: "failed",
  timeout: "timed out",
  missed: "missed",
  skipped: "skipped",
};

export const RUN_STATUS_TIP: Record<ErrandRunStatus, string> = {
  running: "Started by the executor, not reported back yet",
  ok: "Exited 0",
  failed: "Exited non-zero, or couldn't start",
  timeout: "Killed at its timeout (or never reported back)",
  missed: "Its slot passed while nothing ran it (executor down, errands paused): never caught up",
  skipped: "The previous run was still going",
};
