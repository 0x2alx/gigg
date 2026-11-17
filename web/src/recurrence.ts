// Pure helpers for recurring gigs: the form state <-> API body, human-readable rules, a client-side
// preview of the next occurrences, and the window badge on cards. No React here, so it is unit-tested.

export type Freq = "daily" | "weekly" | "monthly";

// The rule part of a schedule, as the API stores it.
export interface Rule {
  freq: Freq;
  interval: number;
  weekdays: number[] | null; // 1=Mon .. 7=Sun
  month_day: number | null; // 1-31, -1 = last day
  tz: string;
  window_start: string; // HH:MM, in tz
  window_minutes: number;
  starts_on: number;
  ends_on?: number | null;
  max_count?: number | null;
}

export const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const DAY = 86_400_000;

const pad = (n: number) => String(n).padStart(2, "0");

// The user's time zone preference (Profile), set by the app once preferences load; "" = the browser's.
let preferredTZ = "";
export function setPreferredTZ(tz: unknown): void {
  preferredTZ = typeof tz === "string" && validTZ(tz) ? tz : "";
}

export function validTZ(tz: string): boolean {
  try {
    new Intl.DateTimeFormat("en", { timeZone: tz });
    return true;
  } catch {
    return false;
  }
}

// browserTZ: the device's own zone (UTC when the browser won't say).
export function browserTZ(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

// localTZ: the zone new schedules start in and times are described in: the user's preference, else the browser's.
export function localTZ(): string {
  return preferredTZ || browserTZ();
}

export function ordinal(n: number): string {
  const s = n % 100 >= 11 && n % 100 <= 13 ? "th" : (["th", "st", "nd", "rd"][n % 10] ?? "th");
  return n + s;
}

// durText: 45 min, 2 h, 1 h 30 min, 3 d.
export function durText(minutes: number): string {
  if (minutes < 60) return `${minutes} min`;
  if (minutes % 1440 === 0) return `${minutes / 1440} d`;
  if (minutes % 60 === 0) return `${minutes / 60} h`;
  return `${Math.floor(minutes / 60)} h ${minutes % 60} min`;
}

function hmToMin(hm: string): number {
  const [h, m] = hm.split(":").map(Number);
  return (h ?? 0) * 60 + (m ?? 0);
}

function minToHM(min: number): string {
  return `${pad(Math.floor(min / 60))}:${pad(min % 60)}`;
}

export function isAllDay(windowStart: string, minutes: number): boolean {
  return hmToMin(windowStart || "00:00") === 0 && minutes === 1440;
}

// windowText: "all day", "09:00–11:00", or "from 22:00 for 4 h" when it runs past midnight.
export function windowText(windowStart: string, minutes: number): string {
  const ws = windowStart || "00:00";
  if (isAllDay(ws, minutes)) return "all day";
  const end = hmToMin(ws) + minutes;
  if (end <= 1440) return `${ws}–${end === 1440 ? "24:00" : minToHM(end)}`;
  return `from ${ws} for ${durText(minutes)}`;
}

function weekdayList(days: number[]): string {
  const d = [...new Set(days)].sort((a, b) => a - b);
  const key = d.join(",");
  if (key === "1,2,3,4,5") return "weekday";
  if (key === "6,7") return "weekend day";
  if (key === "1,2,3,4,5,6,7") return "day";
  return d.map((x) => WEEKDAYS[x - 1]).join(", ");
}

// describeRule: "Every weekday 09:00–11:00", "Every 2 weeks on Mon, Thu all day", "Monthly on the last day 09:00–10:00".
export function describeRule(r: Pick<Rule, "freq" | "interval" | "weekdays" | "month_day" | "window_start" | "window_minutes">): string {
  const n = r.interval > 1 ? r.interval : 1;
  let s = "";
  switch (r.freq) {
    case "daily":
      s = n === 1 ? "Every day" : `Every ${n} days`;
      break;
    case "weekly": {
      const days = weekdayList(r.weekdays ?? []);
      s = n === 1 ? `Every ${days}` : `Every ${n} weeks on ${days === "weekday" ? "weekdays" : days === "weekend day" ? "weekends" : days === "day" ? "every day" : days}`;
      break;
    }
    case "monthly": {
      const md = r.month_day ?? 1;
      const on = md === -1 ? "the last day" : `the ${ordinal(md)}`;
      s = n === 1 ? `Monthly on ${on}` : `Every ${n} months on ${on}`;
      break;
    }
  }
  return `${s} ${windowText(r.window_start, r.window_minutes)}`;
}

// ---- wall clock: dates as "fake UTC" ms, so calendar math ignores the browser's zone

interface Wall {
  y: number;
  m: number; // 1-12
  d: number;
  hh: number;
  mm: number;
}

export function wallParts(ms: number, tz: string): Wall {
  let f: Intl.DateTimeFormat;
  try {
    f = new Intl.DateTimeFormat("en-US", { timeZone: tz, year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "numeric", hourCycle: "h23" });
  } catch {
    f = new Intl.DateTimeFormat("en-US", { year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "numeric", hourCycle: "h23" });
  }
  const p: Record<string, number> = {};
  for (const x of f.formatToParts(new Date(ms))) if (x.type !== "literal") p[x.type] = Number(x.value);
  return { y: p.year!, m: p.month!, d: p.day!, hh: (p.hour ?? 0) % 24, mm: p.minute ?? 0 };
}

const wallMs = (w: Wall) => Date.UTC(w.y, w.m - 1, w.d, w.hh, w.mm);

// instantOfWall: the moment the clock in tz shows that date and time (two corrections cover DST).
export function instantOfWall(y: number, m: number, d: number, hh: number, mm: number, ss: number, tz: string): number {
  const target = Date.UTC(y, m - 1, d, hh, mm, ss);
  let t = target;
  for (let i = 0; i < 2; i++) {
    const w = wallParts(t, tz);
    t += target - Date.UTC(w.y, w.m - 1, w.d, w.hh, w.mm, ss);
  }
  return t;
}
const dayOf = (ms: number) => Math.floor(ms / DAY) * DAY;
const isoWeekday = (dayMs: number) => ((new Date(dayMs).getUTCDay() + 6) % 7) + 1;

function matches(r: Rule, day: number, startDay: number): boolean {
  const n = r.interval > 1 ? r.interval : 1;
  const d = new Date(day);
  switch (r.freq) {
    case "daily": {
      const days = Math.round((day - startDay) / DAY);
      return days >= 0 && days % n === 0;
    }
    case "weekly": {
      if (!(r.weekdays ?? []).includes(isoWeekday(day))) return false;
      const monday = (x: number) => x - (isoWeekday(x) - 1) * DAY;
      const weeks = Math.round((monday(day) - monday(startDay)) / (7 * DAY));
      return weeks >= 0 && weeks % n === 0;
    }
    case "monthly": {
      const s = new Date(startDay);
      const months = (d.getUTCFullYear() - s.getUTCFullYear()) * 12 + d.getUTCMonth() - s.getUTCMonth();
      if (months < 0 || months % n !== 0) return false;
      const last = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0)).getUTCDate();
      let want = r.month_day ?? 1;
      if (want === -1 || want > last) want = last;
      return d.getUTCDate() === want;
    }
  }
}

// previewOccurrences mirrors the server's NextOccurrence on wall-clock time in the schedule's zone and
// returns the next `count` window starts as wall-clock ms (format them with fmtWallWindow).
export function previewOccurrences(r: Rule, now: number, count: number): number[] {
  const tz = r.tz || localTZ();
  const nowW = wallMs(wallParts(now, tz));
  const startW = wallMs(wallParts(r.starts_on || now, tz));
  const endW = r.ends_on ? wallMs(wallParts(r.ends_on, tz)) : null;
  const startDay = dayOf(startW);
  const hm = hmToMin(r.window_start || "00:00") * 60_000;
  const max = r.max_count && r.max_count > 0 ? Math.min(count, r.max_count) : count;
  const out: number[] = [];
  let day = Math.max(dayOf(nowW), startDay);
  const limit = 366 * Math.max(1, r.interval) + 62;
  for (let i = 0; i < limit && out.length < max; i++, day += DAY) {
    if (!matches(r, day, startDay)) continue;
    const at = day + hm;
    if (at < nowW || at < startW) continue;
    if (endW !== null && at > endW) break;
    out.push(at);
  }
  return out;
}

const fmtWallDay = (ms: number) => {
  const d = new Date(ms);
  return `${WEEKDAYS[(d.getUTCDay() + 6) % 7]} ${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`;
};
const fmtWallHM = (ms: number) => {
  const d = new Date(ms);
  return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
};

// fmtWallWindow: "Mon 17 Nov 09:00–11:00", "Mon 17 Nov · all day", "Mon 17 Nov 22:00 → Tue 18 Nov 02:00".
export function fmtWallWindow(start: number, minutes: number): string {
  const day = fmtWallDay(start);
  if (isAllDay(fmtWallHM(start), minutes)) return `${day} · all day`;
  const end = start + minutes * 60_000;
  if (dayOf(end) === dayOf(start)) return `${day} ${fmtWallHM(start)}–${fmtWallHM(end)}`;
  if (end === dayOf(start) + DAY) return `${day} ${fmtWallHM(start)}–24:00`;
  return `${day} ${fmtWallHM(start)} → ${fmtWallDay(end)} ${fmtWallHM(end)}`;
}

// fmtInstant formats a unix-ms instant as wall clock in tz ("Mon 17 Nov 09:00").
export function fmtInstant(ms: number, tz: string): string {
  const w = wallMs(wallParts(ms, tz));
  return `${fmtWallDay(w)} ${fmtWallHM(w)}`;
}

// ---- form state

export interface RepeatState {
  freq: "" | Freq;
  interval: number;
  weekdays: number[];
  monthDay: number;
  allDay: boolean;
  start: string; // HH:MM
  len: number;
  unit: "h" | "m";
  lead: number; // minutes
  endMode: "never" | "date" | "count";
  endDate: string; // YYYY-MM-DD
  count: number;
  overlap: "skip" | "create";
  paused: boolean;
  tz: string;
  startsOn: number | null;
}

export function defaultRepeat(now: Date = new Date()): RepeatState {
  return {
    freq: "",
    interval: 1,
    weekdays: [((now.getDay() + 6) % 7) + 1],
    monthDay: now.getDate(),
    allDay: true,
    start: "09:00",
    len: 2,
    unit: "h",
    lead: 0,
    endMode: "never",
    endDate: "",
    count: 10,
    overlap: "skip",
    paused: false,
    tz: localTZ(),
    startsOn: null,
  };
}

function toDate(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export interface RecurrenceRuleFields {
  freq: Freq;
  interval: number;
  weekdays?: number[];
  month_day?: number;
  tz: string;
  window_start: string;
  window_minutes: number;
  lead_minutes: number;
  starts_on?: number;
  ends_on?: number;
  max_count?: number;
  overlap: "skip" | "create";
  paused: boolean;
}

// wallDate: YYYY-MM-DD of an instant in tz.
function wallDate(ms: number, tz: string): string {
  const w = wallParts(ms, tz);
  return `${w.y}-${pad(w.m)}-${pad(w.d)}`;
}

export function repeatFromRule(r: Rule & { lead_minutes: number; overlap: string; paused: boolean }): RepeatState {
  const all = isAllDay(r.window_start, r.window_minutes);
  const hours = r.window_minutes % 60 === 0;
  return {
    freq: r.freq,
    interval: r.interval > 1 ? r.interval : 1,
    weekdays: r.weekdays ?? [],
    monthDay: r.month_day ?? 1,
    allDay: all,
    start: all ? "09:00" : r.window_start || "00:00",
    len: all ? 2 : hours ? r.window_minutes / 60 : r.window_minutes,
    unit: all || hours ? "h" : "m",
    lead: r.lead_minutes,
    endMode: r.ends_on ? "date" : r.max_count ? "count" : "never",
    endDate: r.ends_on ? wallDate(r.ends_on, r.tz) : "",
    count: r.max_count ?? 10,
    overlap: r.overlap === "create" ? "create" : "skip",
    paused: r.paused,
    tz: r.tz || localTZ(),
    startsOn: r.starts_on || null,
  };
}

export function windowMinutes(s: RepeatState): number {
  return s.allDay ? 1440 : Math.round(s.len * (s.unit === "h" ? 60 : 1));
}

// repeatProblem: the first thing that stops the form from submitting, or null.
export function repeatProblem(s: RepeatState): string | null {
  if (!s.freq) return null;
  if (!Number.isInteger(s.interval) || s.interval < 1 || s.interval > 365) return "Repeat every 1 to 365.";
  if (s.freq === "weekly" && s.weekdays.length === 0) return "Pick at least one weekday.";
  const wm = windowMinutes(s);
  if (!s.allDay && !/^[0-2][0-9]:[0-5][0-9]$/.test(s.start)) return "Pick a start time.";
  if (wm < 5 || wm > 44640) return "The window must be 5 minutes to 31 days long.";
  if (s.lead < 0 || s.lead > 10080) return "Create early: 0 to 10080 minutes.";
  if (s.endMode === "date" && !s.endDate) return "Pick an end date.";
  if (s.endMode === "count" && (!Number.isInteger(s.count) || s.count < 1)) return "End after at least 1 time.";
  return null;
}

// repeatRule: the rule fields for the API (fields that don't apply are omitted, never null).
export function repeatRule(s: RepeatState): RecurrenceRuleFields {
  const freq = (s.freq || "daily") as Freq;
  const out: RecurrenceRuleFields = {
    freq,
    interval: s.interval,
    tz: s.tz,
    window_start: s.allDay ? "00:00" : s.start,
    window_minutes: windowMinutes(s),
    lead_minutes: s.lead,
    overlap: s.overlap,
    paused: s.paused,
  };
  if (freq === "weekly") out.weekdays = [...s.weekdays].sort((a, b) => a - b);
  if (freq === "monthly") out.month_day = s.monthDay;
  if (s.startsOn) out.starts_on = s.startsOn;
  if (s.endMode === "date" && s.endDate) {
    const [y, m, d] = s.endDate.split("-").map(Number);
    out.ends_on = instantOfWall(y!, m!, d!, 23, 59, 59, s.tz); // the end of that day where the schedule runs
  }
  if (s.endMode === "count") out.max_count = s.count;
  return out;
}

export function ruleOf(s: RepeatState, now: number): Rule {
  const f = repeatRule(s);
  return {
    freq: f.freq,
    interval: f.interval,
    weekdays: f.weekdays ?? null,
    month_day: f.month_day ?? null,
    tz: f.tz,
    window_start: f.window_start,
    window_minutes: f.window_minutes,
    starts_on: f.starts_on ?? now,
    ends_on: f.ends_on ?? null,
    max_count: f.max_count ?? null,
  };
}

// ---- gig windows (cards and the gig panel), in the browser's local time

export interface WindowedGig {
  start_at: number | null;
  not_after?: number | null;
  missed_at?: number | null;
}

// clock: "09:00" today, "Tue 09:00" within a week, "17 Nov 09:00" otherwise.
export function clock(ms: number, now: number): string {
  const d = new Date(ms);
  const n = new Date(now);
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  if (d.toDateString() === n.toDateString()) return hm;
  if (Math.abs(ms - now) < 6 * DAY) return `${WEEKDAYS[(d.getDay() + 6) % 7]} ${hm}`;
  return `${d.getDate()} ${MONTHS[d.getMonth()]} ${hm}`;
}

export type WindowState = "upcoming" | "open" | "closed";

export function windowBadge(t: WindowedGig, now: number): { text: string; state: WindowState } | null {
  if (!t.not_after || t.missed_at) return null;
  if (t.start_at && now < t.start_at) return { text: `opens ${clock(t.start_at, now)}`, state: "upcoming" };
  if (now < t.not_after) return { text: `closes ${clock(t.not_after, now)}`, state: "open" };
  return { text: "window closed", state: "closed" };
}

// toDateTimeInput / fromDateTimeInput: <input type="datetime-local"> values in local time.
export function toDateTimeInput(ms: number | null | undefined): string {
  if (!ms) return "";
  const d = new Date(ms);
  return `${toDate(ms)}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function fromDateTimeInput(v: string): number | null {
  if (!v) return null;
  const t = new Date(v).getTime();
  return Number.isNaN(t) ? null : t;
}
