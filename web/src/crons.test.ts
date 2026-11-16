import { describe, expect, it } from "vitest";
import { cronBody, cronProblem, cronRule, defaultCronForm, describeCron, durMS, formOfCron, relTime, sameTiming, schedText, type Cron, type CronForm } from "./crons";

const base = (p: Partial<CronForm> = {}): CronForm => ({ ...defaultCronForm(new Date(2026, 10, 16)), name: "disk", command: "df -h /", tz: "UTC", ...p });

const cron = (p: Partial<Cron> = {}): Cron => ({
  id: 1,
  workspace_id: 1,
  worker_id: 3,
  worker_username: "scout",
  created_by: 3,
  on_failure_task: false,
  next_at: null,
  run_now_at: null,
  paused: false,
  state: "never",
  fail_streak: 0,
  failing_since: null,
  last_run: null,
  created_at: 0,
  version: 1,
  template_id: 1,
  name: "disk",
  description_md: "",
  command: "df -h /",
  timeout_s: 300,
  output: "gigg",
  unsandboxed: false,
  command_set_by: 3,
  template_uses: 1,
  schedule_id: 1,
  schedule_name: "Every 5 min",
  schedule_text: "Every 5 min",
  rule: { freq: "minutes", interval: 5, weekdays: [], month_day: null, tz: "UTC", window_start: "00:00", window_minutes: 1440, lead_minutes: 0 },
  ...p,
});

describe("describeCron", () => {
  it("names timings like the server", () => {
    expect(describeCron(base({ freq: "minutes", interval: 5 }))).toBe("Every 5 min");
    expect(describeCron(base({ freq: "hours", interval: 1 }))).toBe("Every hour");
    expect(describeCron(base({ freq: "hours", interval: 2, weekdays: [1, 2, 3, 4, 5], limited: true, from: "09:00", to: "17:00" }))).toBe("Every 2 h on weekdays, 09:00–17:00");
    expect(describeCron(base({ freq: "daily", interval: 1, at: "03:30" }))).toBe("Every day at 03:30");
    expect(describeCron(base({ freq: "weekly", interval: 1, weekdays: [4, 1], at: "08:00" }))).toBe("Every Mon, Thu at 08:00");
    expect(describeCron(base({ freq: "monthly", interval: 1, monthDay: -1, at: "06:00" }))).toBe("Monthly on the last day at 06:00");
  });
});

describe("cronRule", () => {
  it("minutes: all day unless limited; a window past midnight wraps", () => {
    expect(cronRule(base({ freq: "minutes", interval: 10 }))).toEqual({ freq: "minutes", interval: 10, tz: "UTC", window_start: "00:00", window_minutes: 1440 });
    expect(cronRule(base({ freq: "minutes", interval: 10, limited: true, from: "22:00", to: "02:00" }))).toMatchObject({ window_start: "22:00", window_minutes: 240 });
    expect(cronRule(base({ freq: "hours", weekdays: [1, 2, 3, 4, 5, 6, 7] })).weekdays).toBeUndefined();
  });
  it("daily runs at its time", () => {
    expect(cronRule(base({ freq: "daily", at: "03:30" }))).toMatchObject({ freq: "daily", window_start: "03:30" });
    expect(cronRule(base({ freq: "weekly", weekdays: [5, 1] })).weekdays).toEqual([1, 5]);
  });
});

describe("cronProblem", () => {
  it("refuses what the server would", () => {
    expect(cronProblem(base())).toBeNull();
    expect(cronProblem(base({ name: " " }))).toMatch(/name/);
    expect(cronProblem(base({ command: "" }))).toMatch(/command/);
    expect(cronProblem(base({ timeout: 5 }))).toMatch(/Timeout/);
    expect(cronProblem(base({ freq: "minutes", interval: 400 }))).toMatch(/1 to 360/);
    expect(cronProblem(base({ freq: "hours", interval: 25 }))).toMatch(/1 to 24/);
    expect(cronProblem(base({ freq: "weekly", weekdays: [] }))).toMatch(/weekday/);
    expect(cronProblem(base({ freq: "minutes", limited: true, from: "09:00", to: "09:00" }))).toMatch(/same/);
  });
});

describe("cronBody", () => {
  it("a new cron sends everything; the sandbox flag only when allowed", () => {
    const b = cronBody(base({ unsandboxed: true, worker: "scout" }), null, false);
    expect(b).toMatchObject({ name: "disk", command: "df -h /", timeout_s: 300, output: "gigg", worker: "scout", rule: { freq: "minutes" } });
    expect(b.unsandboxed).toBeUndefined();
    expect(cronBody(base({ unsandboxed: true }), null, true).unsandboxed).toBe(true);
  });
  it("an edit sends only what changed, and no rule when the timing is the same", () => {
    const c = cron();
    const f = formOfCron(c);
    expect(sameTiming(f, c.rule)).toBe(true);
    expect(cronBody(f, c, true)).toEqual({});
    expect(cronBody({ ...f, name: "disk check" }, c, false)).toEqual({ name: "disk check" });
    expect(cronBody({ ...f, interval: 10 }, c, false).rule).toMatchObject({ freq: "minutes", interval: 10 });
  });
  it("round-trips a limited window and a daily time", () => {
    const c = cron({ rule: { freq: "hours", interval: 2, weekdays: [1, 2, 3, 4, 5], month_day: null, tz: "America/New_York", window_start: "22:00", window_minutes: 240, lead_minutes: 0 } });
    const f = formOfCron(c);
    expect(f).toMatchObject({ limited: true, from: "22:00", to: "02:00" });
    expect(sameTiming(f, c.rule)).toBe(true);
    const d = cron({ rule: { freq: "daily", interval: 1, weekdays: null, month_day: null, tz: "UTC", window_start: "03:30", window_minutes: 60, lead_minutes: 0 } });
    expect(formOfCron(d).at).toBe("03:30");
    expect(sameTiming(formOfCron(d), d.rule)).toBe(true);
  });
});

describe("display", () => {
  it("drops the zone when it's the viewer's", () => {
    const c = cron({ schedule_text: "Every 5 min (America/New_York)", rule: { ...cron().rule, tz: "America/New_York" } });
    expect(schedText(c, "America/New_York")).toBe("Every 5 min");
    expect(schedText(c, "Europe/Paris")).toBe("Every 5 min (America/New_York)");
  });
  it("durations and relative times", () => {
    expect(durMS(420)).toBe("420 ms");
    expect(durMS(4200)).toBe("4.2 s");
    expect(durMS(125_000)).toBe("2 min 5 s");
    expect(durMS(60_000)).toBe("1 min");
    expect(relTime(1_000_000 + 5 * 60_000, 1_000_000)).toBe("in 5 min");
    expect(relTime(1_000_000 - 3 * 3_600_000, 1_000_000)).toBe("3 h ago");
  });
});
