import { describe, expect, it } from "vitest";
import { addDays, bucketFor, bucketLabel, daysInRange, fmtCount, fmtDur, fmtUSD, labelEvery, metricsPath, niceMax, onTime, periodRange, today } from "./metrics";

describe("metrics helpers", () => {
  it("counts days of a range, both ends included", () => {
    expect(daysInRange("2026-03-01", "2026-03-01")).toBe(1);
    expect(daysInRange("2026-02-28", "2026-03-02")).toBe(3);
    expect(addDays("2026-03-01", -1)).toBe("2026-02-28");
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01");
  });
  it("finds today in a zone", () => {
    const now = new Date("2026-03-02T03:30:00Z");
    expect(today("UTC", now)).toBe("2026-03-02");
    expect(today("America/New_York", now)).toBe("2026-03-01");
    expect(today("Mars/Olympus", now)).toBe("2026-03-02");
  });
  it("makes the preset ranges end today", () => {
    const now = new Date("2026-03-10T12:00:00Z");
    expect(periodRange("7d", "UTC", now)).toEqual({ from: "2026-03-04", to: "2026-03-10" });
    expect(periodRange("30d", "UTC", now).from).toBe("2026-02-09");
    expect(daysInRange(periodRange("365d", "UTC", now).from, "2026-03-10")).toBe(365);
  });
  it("picks a readable bucket", () => {
    expect(bucketFor("2026-03-01", "2026-03-10")).toBe("day");
    expect(bucketFor("2026-01-01", "2026-06-01")).toBe("week");
    expect(bucketFor("2025-01-01", "2026-06-01")).toBe("month");
  });
  it("labels buckets", () => {
    expect(bucketLabel("2026-11-02", "day")).toBe("Nov 2");
    expect(bucketLabel("2026-11-01", "month")).toBe("Nov 2026");
  });
  it("rounds axes up to 1, 2, 5", () => {
    expect([0, 0.3, 1, 1.5, 3, 7, 12, 480].map(niceMax)).toEqual([1, 0.5, 1, 2, 5, 10, 20, 500]);
  });
  it("formats numbers", () => {
    expect(fmtCount(1234)).toBe("1,234");
    expect(fmtCount(12345)).toBe("12.3k");
    expect(fmtCount(4_500_000)).toBe("4.5M");
    expect(fmtUSD(0)).toBe("$0");
    expect(fmtUSD(1.5)).toBe("$1.50");
    expect(fmtUSD(23.46)).toBe("$23.5");
    expect(fmtUSD(1234.5)).toBe("$1,235");
    expect(fmtDur(null)).toBe("–");
    expect(fmtDur(300)).toBe("<1s");
    expect(fmtDur(45_000)).toBe("45s");
    expect(fmtDur(12 * 60_000)).toBe("12 min");
    expect(fmtDur((3 * 60 + 20) * 60_000)).toBe("3 h 20 min");
    expect(fmtDur((2 * 24 + 4) * 3600_000)).toBe("2 d 4 h");
  });
  it("rates the recurring gigs", () => {
    expect(onTime({ recurring_on_time: 0, recurring_missed: 0 })).toBeNull();
    expect(onTime({ recurring_on_time: 3, recurring_missed: 1 })).toBe(0.75);
  });
  it("spaces labels", () => {
    expect(labelEvery(10, 600)).toBe(1);
    expect(labelEvery(30, 330)).toBe(6);
  });
  it("builds the query", () => {
    expect(metricsPath({ from: "2026-03-01", to: "2026-03-10", bucket: "week", workspace: "scout", project: 4, worker_kind: "agent" })).toBe(
      "/metrics?from=2026-03-01&to=2026-03-10&bucket=week&workspace=scout&project=4&worker_kind=agent",
    );
  });
});
