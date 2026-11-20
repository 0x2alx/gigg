// Metrics: the types of GET /metrics and the pure helpers of the Metrics page (periods, number formats,
// chart scales), kept out of the component so they can be tested.
import { call } from "./http";

export interface MetricPoint {
  gigs_created: number;
  gigs_done: number;
  gigs_cancelled: number;
  gigs_archived: number;
  runs: number;
  runs_done: number;
  runs_needs_human: number;
  runs_error: number;
  runs_timeout: number;
  runs_interrupted: number;
  runs_parked: number;
  runs_running: number;
  run_ms: number;
  runs_timed: number;
  cost_usd: number;
  tokens_in: number;
  tokens_out: number;
  tokens_cache: number;
  errand_runs: number;
  errand_ok: number;
  errand_failed: number;
  errand_timeout: number;
  errand_missed: number;
  errand_skipped: number;
  errand_ms: number;
  errand_output_bytes: number;
  recurring_on_time: number;
  recurring_missed: number;
  cycle_count: number;
  cycle_median_ms: number | null;
}

export interface MetricBucket extends MetricPoint {
  date: string; // YYYY-MM-DD, the local date the bucket starts on
  start: number;
  end: number;
}

export interface MetricTotals extends MetricPoint {
  open_now: number;
  run_avg_ms: number | null;
  success_rate: number | null;
  failure_rate: number | null;
  timeout_rate: number | null;
  interrupted_rate: number | null;
}

export interface MetricGroup {
  id?: number;
  name: string;
  label?: string;
  runs: number;
  cost_usd: number;
  tokens_in: number;
  tokens_out: number;
  run_ms: number;
}

export interface MetricsResult {
  from: number;
  to: number;
  bucket: Bucket;
  tz: string;
  buckets: MetricBucket[];
  totals: MetricTotals;
  by_agent: MetricGroup[];
  by_model: MetricGroup[];
  by_project: MetricGroup[];
  truncated: boolean;
}

export type Bucket = "day" | "week" | "month";
export type Period = "7d" | "30d" | "90d" | "365d" | "custom";

export const PERIODS: { key: Period; label: string; days: number }[] = [
  { key: "7d", label: "7 days", days: 7 },
  { key: "30d", label: "30 days", days: 30 },
  { key: "90d", label: "90 days", days: 90 },
  { key: "365d", label: "1 year", days: 365 },
  { key: "custom", label: "Custom", days: 0 },
];

export interface MetricsQuery {
  from: string;
  to: string;
  bucket: Bucket;
  tz?: string;
  workspace?: string;
  project?: number;
  assignee?: number;
  worker_kind?: "human" | "agent" | "";
}

export function metricsPath(q: MetricsQuery): string {
  const p = new URLSearchParams({ from: q.from, to: q.to, bucket: q.bucket });
  if (q.tz) p.set("tz", q.tz);
  if (q.workspace) p.set("workspace", q.workspace);
  if (q.project) p.set("project", String(q.project));
  if (q.assignee) p.set("assignee", String(q.assignee));
  if (q.worker_kind) p.set("worker_kind", q.worker_kind);
  return "/metrics?" + p.toString();
}

export const metricsApi = {
  get: (q: MetricsQuery) => call<MetricsResult>("GET", metricsPath(q)),
};

// today: the calendar date (YYYY-MM-DD) in a time zone.
export function today(tz: string, now: Date = new Date()): string {
  try {
    const parts = new Intl.DateTimeFormat("en-CA", { timeZone: tz, year: "numeric", month: "2-digit", day: "2-digit" }).format(now);
    if (/^\d{4}-\d{2}-\d{2}$/.test(parts)) return parts;
  } catch {
    /* an unknown zone: fall through */
  }
  return now.toISOString().slice(0, 10);
}

// addDays: a calendar date plus n days (negative: before), as YYYY-MM-DD; calendar arithmetic, no zone.
export function addDays(date: string, n: number): string {
  const [y, m, d] = date.split("-").map(Number);
  const t = new Date(Date.UTC(y!, m! - 1, d! + n));
  return t.toISOString().slice(0, 10);
}

// daysInRange: whole days from a to b (both included: the same day is 1).
export function daysInRange(from: string, to: string): number {
  const [y1, m1, d1] = from.split("-").map(Number);
  const [y2, m2, d2] = to.split("-").map(Number);
  return Math.round((Date.UTC(y2!, m2! - 1, d2!) - Date.UTC(y1!, m1! - 1, d1!)) / 86400000) + 1;
}

// periodRange: the dates a preset covers, ending today.
export function periodRange(period: Period, tz: string, now: Date = new Date()): { from: string; to: string } {
  const to = today(tz, now);
  const days = PERIODS.find((p) => p.key === period)?.days || 30;
  return { from: addDays(to, -(days - 1)), to };
}

// bucketFor: the finest bucket that still draws a readable chart for the range (about 60 bars at most).
export function bucketFor(from: string, to: string): Bucket {
  const n = daysInRange(from, to);
  return n <= 62 ? "day" : n <= 400 ? "week" : "month";
}

export const MAX_DAYS = 400;

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

// bucketLabel: "Nov 20" for a day or a week (its Monday), "Nov 2026" for a month.
export function bucketLabel(date: string, bucket: Bucket): string {
  const [y, m, d] = date.split("-").map(Number);
  const mon = MONTHS[(m ?? 1) - 1] ?? "";
  return bucket === "month" ? `${mon} ${y}` : `${mon} ${d}`;
}

// bucketTitle: the long form for a tooltip: "Week of Nov 16", "Nov 20", "November 2026".
export function bucketTitle(date: string, bucket: Bucket): string {
  return bucket === "week" ? "Week of " + bucketLabel(date, "day") : bucketLabel(date, bucket);
}

// niceMax: the next round number at or above v (1, 2, 5 times a power of ten), for an axis top.
export function niceMax(v: number): number {
  if (!(v > 0)) return 1;
  const e = Math.pow(10, Math.floor(Math.log10(v)));
  const f = v / e;
  const n = f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10;
  return n * e;
}

// fmtCount: 1234 -> "1,234"; large numbers compact: 12.3k, 4.5M.
export function fmtCount(n: number): string {
  if (Math.abs(n) >= 1_000_000) return trim(n / 1_000_000) + "M";
  if (Math.abs(n) >= 10_000) return trim(n / 1_000) + "k";
  return Math.round(n).toLocaleString("en-US");
}

function trim(v: number): string {
  return (Math.round(v * 10) / 10).toString();
}

// fmtUSD: $0.00 under ten dollars, whole dollars above that, "$0" for nothing.
export function fmtUSD(v: number): string {
  if (!v) return "$0";
  if (Math.abs(v) < 10) return "$" + v.toFixed(2);
  if (Math.abs(v) < 1000) return "$" + v.toFixed(1).replace(/\.0$/, "");
  return "$" + Math.round(v).toLocaleString("en-US");
}

// fmtDur: milliseconds as the two largest units: 45s, 12 min, 3 h 20 min, 2 d 4 h; "–" for none.
export function fmtDur(ms: number | null | undefined): string {
  if (ms == null) return "–";
  const s = Math.round(ms / 1000);
  if (ms > 0 && s < 1) return "<1s";
  if (s < 60) return `${s}s`;
  const min = Math.round(s / 60);
  if (min < 60) return `${min} min`;
  const h = Math.floor(min / 60);
  if (h < 24) return min % 60 ? `${h} h ${min % 60} min` : `${h} h`;
  const d = Math.floor(h / 24);
  return h % 24 ? `${d} d ${h % 24} h` : `${d} d`;
}

// fmtPct: a 0-1 rate as "83%"; "–" for none.
export function fmtPct(r: number | null | undefined): string {
  return r == null ? "–" : `${Math.round(r * 100)}%`;
}

// onTime: the share of recurring gigs done in time among those that finished or were missed.
export function onTime(t: Pick<MetricPoint, "recurring_on_time" | "recurring_missed">): number | null {
  const n = t.recurring_on_time + t.recurring_missed;
  return n ? t.recurring_on_time / n : null;
}

// labelEvery: how many buckets apart the x labels go so they don't touch (each needs about `need` px).
export function labelEvery(count: number, width: number, need = 56): number {
  if (count <= 0 || width <= 0) return 1;
  return Math.max(1, Math.ceil((count * need) / width));
}
