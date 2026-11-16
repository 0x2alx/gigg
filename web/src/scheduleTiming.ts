// A schedule's timing and the repeat form: the body to send, the form state for a schedule, and whether
// the form still holds a schedule's exact timing (pure functions, tested on their own).
import type { TaskSchedule as Schedule, ScheduleBody } from "./api";
import { repeatFromRule, repeatRule, type RepeatState } from "./recurrence";

// scheduleBody: a schedule's timing from the repeat form.
export function scheduleBody(name: string, s: RepeatState): ScheduleBody {
  const r = repeatRule(s);
  const b: ScheduleBody = { name: name.trim(), freq: r.freq, interval: r.interval, tz: r.tz, window_start: r.window_start, window_minutes: r.window_minutes, lead_minutes: r.lead_minutes };
  if (r.weekdays) b.weekdays = r.weekdays;
  if (r.month_day !== undefined) b.month_day = r.month_day;
  return b;
}

// repeatOfSchedule: the repeat form state for a schedule (its own end/overlap/pause fields unused).
export function repeatOfSchedule(s: Schedule, base?: RepeatState): RepeatState {
  const r = repeatFromRule({ ...s, starts_on: 0, overlap: "skip", paused: false });
  return base ? { ...base, freq: r.freq, interval: r.interval, weekdays: r.weekdays, monthDay: r.monthDay, allDay: r.allDay, start: r.start, len: r.len, unit: r.unit, lead: r.lead, tz: r.tz } : r;
}

// sameTiming: the repeat form still holds exactly the schedule's timing.
export function sameTiming(s: RepeatState, sch: Pick<Schedule, "freq" | "interval" | "weekdays" | "month_day" | "tz" | "window_start" | "window_minutes" | "lead_minutes">): boolean {
  const r = repeatRule(s);
  if (r.freq !== sch.freq || r.interval !== (sch.interval || 1) || r.tz !== sch.tz || r.window_start !== sch.window_start) return false;
  if (r.window_minutes !== sch.window_minutes || r.lead_minutes !== sch.lead_minutes) return false;
  if (r.freq === "weekly" && JSON.stringify(r.weekdays ?? []) !== JSON.stringify([...(sch.weekdays ?? [])].sort((a, b) => a - b))) return false;
  if (r.freq === "monthly" && r.month_day !== sch.month_day) return false;
  return true;
}
