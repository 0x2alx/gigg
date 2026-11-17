// Sorting the all-workspaces table on the client. Each workspace's list comes sorted (and paged) by the
// server; merged, the rows need the server's exact order, or a capped list would show the wrong top
// rows. So this mirrors internal/store/gigs.go (sortCols): number (project key, then the number), title
// (ASCII case-insensitive, like SQLite's NOCASE), priority, due (no due date last either way), updated,
// created; ties by id, ascending.
import type { Gig } from "../api";
import type { SortKey } from "./savedViews";

type Row = Pick<Gig, "key" | "title" | "priority" | "due_at" | "updated_at" | "created_at" | "id">;

const keyParts = (key: string): [string, number] => {
  const i = key.lastIndexOf("-");
  return i < 0 ? [key, 0] : [key.slice(0, i), Number(key.slice(i + 1)) || 0];
};

// SQLite NOCASE folds only A-Z, then compares bytes; code units are close enough for the table.
const nocase = (s: string) => s.replace(/[A-Z]/g, (c) => c.toLowerCase());
const cmp = (a: string, b: string) => (a < b ? -1 : a > b ? 1 : 0);

export function compareGigs(a: Row, b: Row, sort: { key: SortKey; desc: boolean }): number {
  const dir = sort.desc ? -1 : 1;
  let c = 0;
  switch (sort.key) {
    case "number": {
      const [pa, na] = keyParts(a.key);
      const [pb, nb] = keyParts(b.key);
      c = cmp(pa, pb) || na - nb;
      break;
    }
    case "title":
      c = cmp(nocase(a.title), nocase(b.title));
      break;
    case "priority":
      c = a.priority - b.priority;
      break;
    case "due":
      if (a.due_at === null || b.due_at === null) {
        if (a.due_at !== b.due_at) return a.due_at === null ? 1 : -1; // no due date: last, whichever the direction
        c = 0;
      } else c = a.due_at - b.due_at;
      break;
    case "updated":
      c = a.updated_at - b.updated_at;
      break;
    case "created":
      c = a.created_at - b.created_at;
      break;
  }
  return c * dir || a.id - b.id;
}
