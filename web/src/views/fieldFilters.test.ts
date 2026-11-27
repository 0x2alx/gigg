import { describe, expect, it } from "vitest";
import type { CustomField } from "../api";
import { fieldChip, fieldParams, joinNum, splitNum } from "./fieldFilters";
import { DEFAULT_FILTERS, parse, summary, toQuery, toSearch, type TableFilters } from "./savedViews";

const field = (id: number, name: string, kind: CustomField["kind"], options: string[] | null = null): CustomField => ({ id, workspace_id: 1, name, kind, options, position: id });
const size = field(12, "Size", "select", ["S", "M", "L"]);
const points = field(13, "Points", "number");
const urgent = field(14, "Urgent", "checkbox");
const note = field(15, "Note", "text");

describe("number filters", () => {
  it("split and join a comparison and a number", () => {
    expect(splitNum(">=3")).toEqual({ op: ">=", n: "3" });
    expect(splitNum("<2.5")).toEqual({ op: "<", n: "2.5" });
    expect(splitNum("8")).toEqual({ op: "=", n: "8" });
    expect(splitNum("")).toEqual({ op: "=", n: "" });
    expect(joinNum(">=", "3")).toBe(">=3");
    expect(joinNum("=", " 8 ")).toBe("8");
    expect(joinNum("<=", "")).toBeUndefined();
    expect(joinNum("<=", "abc")).toBeUndefined();
  });
});

describe("fieldParams", () => {
  it("sends each filter as <id>:<value>", () => {
    expect(fieldParams({ 12: "M", 13: ">=3", 15: "" })).toEqual(["12:M", "13:>=3", "15:"]);
    expect(fieldParams({})).toEqual([]);
  });
  it("leaves out a workspace that lacks a filtered field (none of its gigs can match)", () => {
    expect(fieldParams({ 12: "M" }, [size, points])).toEqual(["12:M"]);
    expect(fieldParams({ 12: "M", 99: "x" }, [size, points])).toBeNull();
    expect(fieldParams({}, [])).toEqual([]);
  });
});

describe("fieldChip", () => {
  it("says each filter shortly", () => {
    expect(fieldChip(size, "12", "M")).toBe("Size: M");
    expect(fieldChip(size, "12", "")).toBe("Size: none");
    expect(fieldChip(urgent, "14", "true")).toBe("Urgent: ticked");
    expect(fieldChip(urgent, "14", "false")).toBe("Urgent: not ticked");
    expect(fieldChip(points, "13", ">=3")).toBe("Points ≥ 3");
    expect(fieldChip(note, "15", "legal")).toBe("Note has “legal”");
    expect(fieldChip(undefined, "99", "x")).toBe("field 99: x");
  });
});

describe("saved views keep field filters and columns", () => {
  const f = (o: Partial<TableFilters>): TableFilters => ({ ...DEFAULT_FILTERS, ...o });
  it("stores them as field.<id> and cols", () => {
    expect(toQuery(f({ fields: { 12: "M", 13: ">=3" }, cols: [12, 15] }))).toEqual({ "field.12": "M", "field.13": ">=3", cols: "12,15" });
    expect(toQuery(f({ fields: { 15: "" } }))).toEqual({ "field.15": "" }); // "no value" is a filter too
  });
  it("round-trip through the URL and a stored query", () => {
    const x = f({ label: "ops", fields: { 12: "L", 14: "true", 15: "" }, cols: [13] });
    expect(parse(new URLSearchParams(toSearch(x))).filters).toEqual(x);
    expect(parse(toQuery(x)).filters).toEqual(x);
  });
  it("drop malformed keys and columns", () => {
    const { filters } = parse({ "field.x": "1", "field.0": "1", fields: "1", cols: "a,13,-2,13" });
    expect(filters.fields).toEqual({});
    expect(filters.cols).toEqual([13]);
  });
  it("name them in a view's chips", () => {
    expect(summary({ "field.12": "M", "field.13": "<=2" }, [size, points])).toEqual(["Size: M", "Points ≤ 2"]);
  });
});
