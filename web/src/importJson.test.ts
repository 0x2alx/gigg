import { describe, expect, it } from "vitest";
import { creationOrder, exportObject, finishBody, findInBatch, MAX_OBJECTS, needsOrder, parseImport, toBody, type ImportCtx, type Kind } from "./importJson";

const ctx: ImportCtx = {
  userId: (v) => ({ ada: 1, scout: 2 } as Record<string, number>)[String(v)] ?? (v === 7 ? 7 : undefined),
  ref: (what, name, id) => (name === "Daily" && what === "schedule" ? 11 : name === "Report" && what === "template" ? 21 : name === undefined && id === 5 ? 5 : undefined),
};
const ALL: Kind[] = ["schedule", "gig_template", "errand_template", "gig", "errand", "recurring_gig"];

const schedule = { id: 3, workspace_id: 1, name: "Weekly", freq: "weekly", interval: 1, weekdays: [1], month_day: null, tz: "UTC", window_start: "09:00", window_minutes: 60, lead_minutes: 0, version: 4, uses: 2, shared: false };

describe("exportObject", () => {
  it("keeps what create accepts, drops server fields and zero values", () => {
    const o = exportObject("schedule", schedule as never);
    expect(o).toEqual({ kind: "schedule", name: "Weekly", freq: "weekly", interval: 1, weekdays: [1], tz: "UTC", window_start: "09:00", window_minutes: 60 });
    expect(o).not.toHaveProperty("id");
    expect(o).not.toHaveProperty("workspace_id");
    expect(o).not.toHaveProperty("uses");
  });
  it("writes usernames for assignees and label names for a gig", () => {
    const g = { id: 9, key: "A-1", title: "Do it", description_md: "x", priority: 2, status_category: "todo", assignees: [1], followers: [], labels: [{ id: 1, name: "bug", color: null }], created_by: 1, version: 3 };
    const o = exportObject("gig", g as never, { userName: (id) => (id === 1 ? "ada" : undefined) });
    expect(o).toEqual({ kind: "gig", title: "Do it", description_md: "x", priority: 2, category: "todo", labels: ["bug"], assignees: ["ada"] });
  });
  it("names a recurring gig's template and schedule next to their ids", () => {
    const r = { id: 1, template_id: 21, template_name: "Report", schedule_id: 11, schedule_name: "Daily", starts_on: 1000, ends_on: null, max_count: null, overlap: "skip", paused: false, title: "T", freq: "daily", next_at: 5 };
    expect(exportObject("recurring_gig", r as never)).toEqual({ kind: "recurring_gig", template_id: 21, template: "Report", schedule_id: 11, schedule: "Daily", starts_on: 1000, overlap: "skip" });
  });
  it("exports an errand with its worker's username and a clean rule", () => {
    const e = { id: 1, name: "Backup", command: "true", timeout_s: 300, output: "gigg", unsandboxed: false, worker_username: "scout", on_failure_gig: true, paused: false, rule: { freq: "hours", interval: 2, weekdays: null, month_day: null, tz: "UTC", window_start: "00:00", window_minutes: 1440, lead_minutes: 0 } };
    expect(exportObject("errand", e as never)).toEqual({ kind: "errand", name: "Backup", command: "true", timeout_s: 300, output: "gigg", worker: "scout", on_failure_gig: true, rule: { freq: "hours", interval: 2, tz: "UTC", window_start: "00:00", window_minutes: 1440 } });
  });
});

describe("round trip", () => {
  it("a copy imports to the same create body", () => {
    const copy = exportObject("schedule", schedule as never);
    const { rows, error } = parseImport(JSON.stringify(copy), ["schedule"]);
    expect(error).toBeUndefined();
    expect(rows).toHaveLength(1);
    expect(rows[0]!.problems).toEqual([]);
    expect(rows[0]!.dropped).toEqual([]);
    const { kind: _k, ...want } = copy;
    expect(toBody(rows[0]!, ctx).body).toEqual(want);
  });
  it("round-trips a gig template with its people", () => {
    const t = { id: 2, name: "N", title: "T", description_md: "", priority: 1, color: null, labels: ["a"], assignees: [2], checklist: ["x", "y"], version: 1, workspace_id: 1, project_id: 4 };
    const copy = exportObject("gig_template", t as never, { userName: (id) => (id === 2 ? "scout" : undefined) });
    const row = parseImport(JSON.stringify([copy]), ["gig_template"]).rows[0]!;
    expect(toBody(row, ctx).body).toEqual({ name: "N", title: "T", priority: 1, labels: ["a"], assignees: [2], checklist: ["x", "y"] });
  });
});

describe("parseImport", () => {
  it("takes one object or a list", () => {
    expect(parseImport('{"kind":"gig","title":"a"}', ["gig"]).rows).toHaveLength(1);
    expect(parseImport('[{"kind":"gig","title":"a"},{"kind":"gig","title":"b"}]', ["gig"]).rows).toHaveLength(2);
  });
  it("drops unknown and server-only fields with a note", () => {
    const r = parseImport('{"kind":"gig","title":"a","id":5,"workspace_id":1,"created_by":2,"bogus":true}', ["gig"]).rows[0]!;
    expect(r.dropped).toEqual(["id", "workspace_id", "created_by", "bogus"]);
    expect(r.body).toEqual({ title: "a" });
  });
  it("refuses a kind that doesn't belong on the page", () => {
    const r = parseImport('{"kind":"schedule","freq":"daily"}', ["gig_template"]).rows[0]!;
    expect(r.kind).toBeNull();
    expect(r.problems[0]).toMatch(/doesn't belong on this page/);
    expect(parseImport('{"kind":"nope"}', ALL).rows[0]!.problems[0]).toMatch(/Unknown kind/);
  });
  it("takes a missing kind as the page's fallback, else refuses it", () => {
    expect(parseImport('{"freq":"daily"}', ["schedule"], "schedule").rows[0]!.kind).toBe("schedule");
    expect(parseImport('{"freq":"daily"}', ["schedule", "gig"]).rows[0]!.problems[0]).toMatch(/No "kind"/);
  });
  it("flags what create would refuse", () => {
    expect(parseImport('{"kind":"schedule","name":"x"}', ["schedule"]).rows[0]!.problems).toEqual(['"freq" is missing.']);
    expect(parseImport('{"kind":"schedule","freq":"minutes"}', ["schedule"]).rows[0]!.problems[0]).toMatch(/daily, weekly or monthly/);
    expect(parseImport('{"kind":"gig"}', ["gig"]).rows[0]!.problems).toEqual(['"title" is missing.']);
  });
  it("rejects bad input as a whole", () => {
    expect(parseImport("", ["gig"]).error).toBeTruthy();
    expect(parseImport("{nope", ["gig"]).error).toMatch(/valid JSON/);
    expect(parseImport("[]", ["gig"]).error).toMatch(/empty/);
    const many = JSON.stringify(Array.from({ length: MAX_OBJECTS + 1 }, () => ({ kind: "gig", title: "a" })));
    expect(parseImport(many, ["gig"]).error).toMatch(/At most 100/);
    expect(parseImport(JSON.stringify([{ kind: "gig", title: "x".repeat(1024 * 1024) }]), ["gig"]).error).toMatch(/1 MB/);
  });
  it("reports a row that isn't an object and keeps the others", () => {
    const { rows } = parseImport('[1,{"kind":"gig","title":"ok"}]', ["gig"]);
    expect(rows[0]!.problems).toEqual(["Not an object."]);
    expect(rows[1]!.problems).toEqual([]);
  });
});

describe("toBody", () => {
  const row = (o: object, kind: Kind) => parseImport(JSON.stringify({ kind, ...o }), ALL).rows[0]!;
  it("turns usernames into ids and flags unknown people", () => {
    const r = toBody(row({ title: "a", assignees: ["ada", "ghost"], followers: [7] }, "gig"), ctx);
    expect(r.body.assignees).toEqual([1]);
    expect(r.body.followers).toEqual([7]);
    expect(r.problems).toEqual(["Unknown assignee: ghost."]);
  });
  it("resolves a recurring gig's template and schedule by name", () => {
    const r = toBody(row({ template: "Report", schedule: "Daily" }, "recurring_gig"), ctx);
    expect(r.problems).toEqual([]);
    expect(r.body).toEqual({ template_id: 21, schedule_id: 11 });
  });
  it("accepts an id when there is no name, and flags what doesn't resolve", () => {
    expect(toBody(row({ template: "Report", schedule_id: 5 }, "recurring_gig"), ctx).body).toEqual({ template_id: 21, schedule_id: 5 });
    const r = toBody(row({ template: "Nope", schedule_id: 99 }, "recurring_gig"), ctx);
    expect(r.problems).toEqual(["No template named “Nope” in reach.", "No schedule with id 99 in reach."]);
  });
});

describe("a list that holds what it uses", () => {
  const list = JSON.stringify([
    { kind: "gig", title: "g" },
    { kind: "recurring_gig", template: "Weekday report", schedule: "Weekday 07:00" },
    { kind: "gig_template", name: "Weekday report", title: "Report" },
    { kind: "errand", name: "e", command: "true", rule: { freq: "hours" } },
    { kind: "schedule", name: "Weekday 07:00", freq: "weekly", weekdays: [1, 2, 3, 4, 5] },
    { kind: "errand_template", name: "et", command: "true" },
  ]);
  const rows = parseImport(list, ALL).rows;

  it("creates schedules, then templates, then what uses them, then gigs; the pasted order stays inside a step", () => {
    expect(creationOrder(rows).map((r) => r.kind)).toEqual(["schedule", "gig_template", "errand_template", "recurring_gig", "errand", "gig"]);
    expect(rows.map((r) => r.index)).toEqual([0, 1, 2, 3, 4, 5]); // the preview keeps the pasted order
    expect(needsOrder(rows)).toBe(true);
    expect(needsOrder(rows.filter((r) => r.kind === "schedule"))).toBe(false);
  });

  const entries = rows.map((row) => ({ row, ws: 1 }));
  const inBatch: ImportCtx = { userId: () => undefined, ref: (what, name) => (name ? findInBatch(entries, 1, what, name) : undefined) };

  it("a name in the same list is pending, not an error", () => {
    const r = toBody(rows[1]!, inBatch);
    expect(r.problems).toEqual([]);
    expect(r.after).toEqual([
      { key: "template_id", row: 2, what: "template", name: "Weekday report" },
      { key: "schedule_id", row: 4, what: "schedule", name: "Weekday 07:00" },
    ]);
    expect(r.body).toEqual({});
  });

  it("fills the ids in as the rows are created", () => {
    const { after } = toBody(rows[1]!, inBatch);
    expect(finishBody({ paused: true }, after, new Map([[4, 11], [2, 21]]))).toEqual({ body: { paused: true, template_id: 21, schedule_id: 11 } });
  });

  it("a dependent fails with a clear message when the row it names wasn't created", () => {
    const { after } = toBody(rows[1]!, inBatch);
    expect(finishBody({}, after, new Map([[2, 21]]))).toEqual({ problem: "schedule 'Weekday 07:00' wasn't created" });
  });

  it("only a row for the same workspace (or a shared one) counts, and a skipped row is flagged", () => {
    const other = rows.map((row) => ({ row, ws: 2 }));
    expect(findInBatch(other, 1, "schedule", "Weekday 07:00")).toBeUndefined();
    expect(findInBatch(rows.map((row) => ({ row, ws: 0 })), 1, "schedule", "weekday 07:00")).toEqual({ pending: 4 });
    const bad = parseImport('[{"kind":"schedule","name":"S"}]', ALL).rows;
    expect(findInBatch([{ row: bad[0]!, ws: 1 }], 1, "schedule", "S")).toEqual({ problem: "The schedule “S” in this list is skipped." });
  });

  it("a name that is not in the list still goes to what is in reach", () => {
    const r = toBody(parseImport('{"kind":"recurring_gig","template":"Report","schedule":"Daily"}', ALL).rows[0]!, { ...ctx, ref: (what, name, id) => findInBatch(entries, 1, what, name!) ?? ctx.ref(what, name, id) });
    expect(r.after).toEqual([]);
    expect(r.body).toEqual({ template_id: 21, schedule_id: 11 });
  });
});
