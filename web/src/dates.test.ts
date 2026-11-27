import { describe, expect, it } from "vitest";
import { DEFAULT_DUE_TIME, DEFAULT_START_TIME, fillTime, fmtDue, joinDateTime, parseHM, splitDateTime } from "./dates";

describe("parseHM (the 24-hour time box)", () => {
  it("reads the usual ways of typing a time", () => {
    expect(parseHM("23:00")).toBe("23:00");
    expect(parseHM("9:30")).toBe("09:30");
    expect(parseHM("0930")).toBe("09:30");
    expect(parseHM("930")).toBe("09:30");
    expect(parseHM("9.30")).toBe("09:30");
    expect(parseHM(" 7 ")).toBe("07:00");
    expect(parseHM("00:00")).toBe("00:00");
  });
  it("refuses what isn't a time of day, never wrapping it", () => {
    for (const v of ["", "24:00", "23:60", "11:00 PM", "abc", "1:2", "12345"]) expect(parseHM(v)).toBeNull();
  });
});

describe("date and time inputs", () => {
  it("fills the default time when only a date is picked", () => {
    expect(fillTime("2026-11-12", "", DEFAULT_START_TIME)).toBe("09:00");
    expect(fillTime("2026-11-12", "", DEFAULT_DUE_TIME)).toBe("17:00");
    expect(fillTime("2026-11-12", "08:15", DEFAULT_DUE_TIME)).toBe("08:15");
    expect(fillTime("", "08:15", DEFAULT_DUE_TIME)).toBe("");
  });
  it("joins local date and time into unix ms and back", () => {
    const ms = joinDateTime("2026-11-12", "", DEFAULT_DUE_TIME)!;
    expect(ms).toBe(new Date(2026, 10, 12, 17, 0).getTime());
    expect(splitDateTime(ms)).toEqual({ date: "2026-11-12", time: "17:00" });
    expect(joinDateTime("2026-11-12", "07:30", DEFAULT_DUE_TIME)).toBe(new Date(2026, 10, 12, 7, 30).getTime());
    expect(joinDateTime("", "07:30", DEFAULT_DUE_TIME)).toBeNull();
    expect(splitDateTime(null)).toEqual({ date: "", time: "" });
  });
  it("shows the time on due pills for today and tomorrow only", () => {
    const now = new Date(2026, 10, 12, 10, 0);
    expect(fmtDue(new Date(2026, 10, 12, 17, 0).getTime(), now)).toMatch(/^Today /);
    expect(fmtDue(new Date(2026, 10, 13, 9, 0).getTime(), now)).toMatch(/^Tomorrow /);
    expect(fmtDue(new Date(2026, 10, 20, 9, 0).getTime(), now)).not.toMatch(/Today|Tomorrow|09/);
    expect(fmtDue(null, now)).toBe("");
  });
});
