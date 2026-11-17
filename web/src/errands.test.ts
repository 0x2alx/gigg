import { describe, expect, it } from "vitest";
import { contentOf, detaching, errandBody, errandProblem, errandRule, defaultErrandForm, describeErrand, durMS, formOfErrand, relTime, sameTiming, schedText, type Errand, type ErrandForm, type ErrandTemplate } from "./errands";

const base = (p: Partial<ErrandForm> = {}): ErrandForm => ({ ...defaultErrandForm(new Date(2026, 10, 16)), name: "disk", command: "df -h /", tz: "UTC", ...p });

const errand = (p: Partial<Errand> = {}): Errand => ({
  id: 1,
  workspace_id: 1,
  worker_id: 3,
  worker_username: "scout",
  created_by: 3,
  on_failure_gig: false,
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
  unsandboxed_set_by: null,
  template_uses: 1,
  schedule_id: 1,
  schedule_name: "Every 5 min",
  schedule_text: "Every 5 min",
  rule: { freq: "minutes", interval: 5, weekdays: [], month_day: null, tz: "UTC", window_start: "00:00", window_minutes: 1440, lead_minutes: 0 },
  ...p,
});

describe("describeErrand", () => {
  it("names timings like the server", () => {
    expect(describeErrand(base({ freq: "minutes", interval: 5 }))).toBe("Every 5 min");
    expect(describeErrand(base({ freq: "hours", interval: 1 }))).toBe("Every hour");
    expect(describeErrand(base({ freq: "hours", interval: 2, weekdays: [1, 2, 3, 4, 5], limited: true, from: "09:00", to: "17:00" }))).toBe("Every 2 h on weekdays, 09:00–17:00");
    expect(describeErrand(base({ freq: "daily", interval: 1, at: "03:30" }))).toBe("Every day at 03:30");
    expect(describeErrand(base({ freq: "weekly", interval: 1, weekdays: [4, 1], at: "08:00" }))).toBe("Every Mon, Thu at 08:00");
    expect(describeErrand(base({ freq: "monthly", interval: 1, monthDay: -1, at: "06:00" }))).toBe("Monthly on the last day at 06:00");
  });
});

describe("errandRule", () => {
  it("minutes: all day unless limited; a window past midnight wraps", () => {
    expect(errandRule(base({ freq: "minutes", interval: 10 }))).toEqual({ freq: "minutes", interval: 10, tz: "UTC", window_start: "00:00", window_minutes: 1440 });
    expect(errandRule(base({ freq: "minutes", interval: 10, limited: true, from: "22:00", to: "02:00" }))).toMatchObject({ window_start: "22:00", window_minutes: 240 });
    expect(errandRule(base({ freq: "hours", weekdays: [1, 2, 3, 4, 5, 6, 7] })).weekdays).toBeUndefined();
  });
  it("daily runs at its time", () => {
    expect(errandRule(base({ freq: "daily", at: "03:30" }))).toMatchObject({ freq: "daily", window_start: "03:30" });
    expect(errandRule(base({ freq: "weekly", weekdays: [5, 1] })).weekdays).toEqual([1, 5]);
  });
});

describe("errandProblem", () => {
  it("refuses what the server would", () => {
    expect(errandProblem(base())).toBeNull();
    expect(errandProblem(base({ name: " " }))).toMatch(/name/);
    expect(errandProblem(base({ command: "" }))).toMatch(/command/);
    expect(errandProblem(base({ timeout: 5 }))).toMatch(/Timeout/);
    expect(errandProblem(base({ freq: "minutes", interval: 400 }))).toMatch(/1 to 360/);
    expect(errandProblem(base({ freq: "hours", interval: 25 }))).toMatch(/1 to 24/);
    expect(errandProblem(base({ freq: "weekly", weekdays: [] }))).toMatch(/weekday/);
    expect(errandProblem(base({ freq: "minutes", limited: true, from: "09:00", to: "09:00" }))).toMatch(/same/);
  });
});

describe("errandBody", () => {
  it("a new errand sends everything; the sandbox flag only when allowed", () => {
    const b = errandBody(base({ unsandboxed: true, worker: "scout" }), null, false);
    expect(b).toMatchObject({ name: "disk", command: "df -h /", timeout_s: 300, output: "gigg", worker: "scout", rule: { freq: "minutes" } });
    expect(b.unsandboxed).toBeUndefined();
    expect(errandBody(base({ unsandboxed: true }), null, true).unsandboxed).toBe(true);
  });
  it("an edit sends only what changed, and no rule when the timing is the same", () => {
    const c = errand();
    const f = formOfErrand(c);
    expect(sameTiming(f, c.rule)).toBe(true);
    expect(errandBody(f, c, true)).toEqual({});
    expect(errandBody({ ...f, name: "disk check" }, c, false)).toEqual({ name: "disk check" });
    expect(errandBody({ ...f, interval: 10 }, c, false).rule).toMatchObject({ freq: "minutes", interval: 10 });
  });
  it("round-trips a limited window and a daily time", () => {
    const c = errand({ rule: { freq: "hours", interval: 2, weekdays: [1, 2, 3, 4, 5], month_day: null, tz: "America/New_York", window_start: "22:00", window_minutes: 240, lead_minutes: 0 } });
    const f = formOfErrand(c);
    expect(f).toMatchObject({ limited: true, from: "22:00", to: "02:00" });
    expect(sameTiming(f, c.rule)).toBe(true);
    const d = errand({ rule: { freq: "daily", interval: 1, weekdays: null, month_day: null, tz: "UTC", window_start: "03:30", window_minutes: 60, lead_minutes: 0 } });
    expect(formOfErrand(d).at).toBe("03:30");
    expect(sameTiming(formOfErrand(d), d.rule)).toBe(true);
  });
});

describe("display", () => {
  it("drops the zone when it's the viewer's", () => {
    const c = errand({ schedule_text: "Every 5 min (America/New_York)", rule: { ...errand().rule, tz: "America/New_York" } });
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

describe("starting from a command", () => {
  const t: ErrandTemplate = { id: 7, workspace_id: 0, shared: true, name: "disk", description_md: "", command: "df -h", timeout_s: 600, output: "gigg", unsandboxed: false, version: 1, uses: 2 };
  const f: ErrandForm = { ...defaultErrandForm(), ...contentOf(t), worker: "scout" };
  it("sends the template as is while its content is unchanged", () => {
    const b = errandBody(f, null, false, t);
    expect(b.template_id).toBe(7);
    expect(b).not.toHaveProperty("command");
    expect(b.worker).toBe("scout");
    expect(b.rule).toMatchObject({ freq: "minutes" });
  });
  it("sends its own content once changed", () => {
    const b = errandBody({ ...f, command: "df -h /" }, null, false, t);
    expect(b.template_id).toBeUndefined();
    expect(b).toMatchObject({ name: "disk", command: "df -h /", timeout_s: 600 });
  });
});

describe("detaching", () => {
  it("names the shared template or schedule an edit leaves, and nothing otherwise", () => {
    const c = errand({ template_shared: true, schedule_shared: true });
    const f = formOfErrand(c);
    expect(detaching(c, errandBody(f, c, true))).toEqual([]);
    expect(detaching(c, errandBody({ ...f, worker: "atlas" }, c, true))).toEqual([]);
    const both = detaching(c, errandBody({ ...f, command: "df -h", interval: 10 }, c, true));
    expect(both).toHaveLength(2);
    expect(both[0]).toContain("shared errand template “disk”");
    expect(both[1]).toContain("shared schedule “Every 5 min”");
    expect(detaching(errand(), errandBody({ ...f, command: "df -h" }, errand(), true))).toEqual([]);
    expect(detaching(null, errandBody(f, null, true))).toEqual([]);
  });
});
