// Start / due date-and-time helpers. Values are unix ms; the inputs are a local date ("YYYY-MM-DD")
// plus a local time ("HH:MM"). Picking only a date fills in a default time (start 09:00, due 17:00)
// so the user sees which instant is stored.

export const DEFAULT_START_TIME = "09:00";
export const DEFAULT_DUE_TIME = "17:00";

const pad = (n: number) => String(n).padStart(2, "0");

// splitDateTime: unix ms -> the local date and time strings for <input type="date"> / type="time".
export function splitDateTime(ms: number | null | undefined): { date: string; time: string } {
  if (!ms) return { date: "", time: "" };
  const d = new Date(ms);
  return { date: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`, time: `${pad(d.getHours())}:${pad(d.getMinutes())}` };
}

// parseHM: a typed 24-hour time as "HH:MM", or null. Takes "9:30", "09:30", "0930", "930", "9.30"
// and "9" (= 09:00); never wraps an hour or minute that's out of range.
export function parseHM(v: string): string | null {
  const m = /^(\d{1,2})(?:[:.h]?(\d{2}))?$/.exec(v.trim());
  if (!m) return null;
  const h = Number(m[1]), min = Number(m[2] ?? "0");
  return h < 24 && min < 60 ? `${pad(h)}:${pad(min)}` : null;
}

// fillTime: the time to show after the date changed. A new date with no time yet gets the default;
// clearing the date clears the time.
export function fillTime(date: string, time: string, defaultTime: string): string {
  if (!date) return "";
  return time || defaultTime;
}

// joinDateTime: local date + time -> unix ms (null without a valid date). A missing time uses the default.
export function joinDateTime(date: string, time: string, defaultTime: string): number | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return null;
  const t = /^\d{2}:\d{2}$/.test(time) ? time : defaultTime;
  const ms = new Date(`${date}T${t}:00`).getTime();
  return Number.isNaN(ms) ? null : ms;
}

// fmtDue: a short due label for pills. Today and tomorrow show the time ("Today 14:30"); other
// days show the date only.
export function fmtDue(ms: number | null, now: Date = new Date()): string {
  if (!ms) return "";
  const d = new Date(ms);
  const day0 = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const dayOf = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((dayOf - day0) / 86_400_000);
  const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  if (days === 0) return `Today ${time}`;
  if (days === 1) return `Tomorrow ${time}`;
  return d.toLocaleDateString(undefined, { dateStyle: "medium" });
}
