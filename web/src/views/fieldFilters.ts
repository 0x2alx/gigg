// Custom field filters and columns of the table: the filter values as the gig list takes them
// (field=<id>:<value>), which ones apply to a workspace, and short chips for saved views. Pure, tested.
import type { CustomField } from "../api";

// the kinds the gig list can filter on (dates can't be)
export const FILTER_KINDS: ReadonlySet<CustomField["kind"]> = new Set(["select", "checkbox", "number", "text", "url"]);

export type NumOp = "=" | ">=" | "<=" | ">" | "<";
export const NUM_OPS: [NumOp, string][] = [
  ["=", "="],
  [">=", "≥"],
  ["<=", "≤"],
  [">", ">"],
  ["<", "<"],
];

// splitNum: a number filter's comparison and number ("" while none is typed).
export function splitNum(v: string): { op: NumOp; n: string } {
  const m = /^(>=|<=|>|<|=)?([^]*)$/.exec(v)!;
  return { op: (m[1] as NumOp | undefined) ?? "=", n: m[2]!.trim() };
}

// joinNum: the filter value for a comparison and a number; undefined (no filter) without a number.
export function joinNum(op: NumOp, n: string): string | undefined {
  const t = n.trim();
  if (t === "" || !Number.isFinite(Number(t))) return undefined;
  return op === "=" ? t : op + t;
}

// fieldParams: the filters (field id -> value) for one workspace's gig list, as field=<id>:<value>
// values. null: a filter names a field this workspace doesn't have, so none of its gigs can match.
// known undefined: the workspace's fields aren't known; every filter is sent (the list refuses a
// field it doesn't have).
export function fieldParams(filters: Record<string, string>, known?: CustomField[]): string[] | null {
  const out: string[] = [];
  for (const [id, v] of Object.entries(filters)) {
    if (known && !known.some((f) => String(f.id) === id)) return null;
    out.push(`${id}:${v}`);
  }
  return out;
}

// fieldChip: a filter as a short chip: "Size: M", "Size: none", "Urgent: ticked", "Points ≥ 3",
// "Note has “x”". An unknown field shows as "field 12".
export function fieldChip(f: Pick<CustomField, "name" | "kind"> | undefined, id: string, v: string): string {
  const name = f?.name ?? `field ${id}`;
  if (v === "") return `${name}: none`;
  switch (f?.kind) {
    case "checkbox":
      return `${name}: ${v === "true" ? "ticked" : "not ticked"}`;
    case "number": {
      const { op, n } = splitNum(v);
      return `${name} ${NUM_OPS.find(([o]) => o === op)![1]} ${n}`;
    }
    case "text":
    case "url":
      return `${name} has “${v}”`;
    default:
      return `${name}: ${v}`;
  }
}
