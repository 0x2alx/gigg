import { describe, expect, it } from "vitest";
import type { Board, Gig } from "./api";
import { applyMove, placement, reorderIndex } from "./dnd";

const gig = (id: number, status: number): Gig => ({ id, key: `T-${id}`, status_id: status, status_name: `S${status}`, status_category: "todo" }) as Gig;
const board = (): Board =>
  ({
    project: {} as Board["project"],
    labels: [],
    statuses: [
      { id: 1, project_id: 1, name: "To do", category: "todo", color: null, position: 1, gigs: [gig(1, 1), gig(2, 1), gig(3, 1)] },
      { id: 2, project_id: 1, name: "Doing", category: "doing", color: null, position: 2, gigs: [gig(4, 2)] },
      { id: 3, project_id: 1, name: "Done", category: "done", color: null, position: 3, gigs: [] },
    ],
  }) as Board;
const ids = (b: Board, s: number) => b.statuses.find((x) => x.id === s)!.gigs.map((t) => t.id);

describe("placement", () => {
  const others = [{ id: 7 }, { id: 8 }];
  it("before a neighbour, after the last, bottom of an empty column", () => {
    expect(placement(2, others, 0)).toEqual({ status: 2, position: "before", gig: 7 });
    expect(placement(2, others, 1)).toEqual({ status: 2, position: "before", gig: 8 });
    expect(placement(2, others, 2)).toEqual({ status: 2, position: "after", gig: 8 });
    expect(placement(2, [], 0)).toEqual({ status: 2, position: "bottom" });
  });
});

describe("applyMove", () => {
  it("moves across columns at the drop index", () => {
    const r = applyMove(board(), 2, 2, 0)!;
    expect(ids(r.board, 1)).toEqual([1, 3]);
    expect(ids(r.board, 2)).toEqual([2, 4]);
    expect(r.board.statuses[1]!.gigs[0]).toMatchObject({ status_id: 2, status_name: "Doing", status_category: "doing" });
    expect(r.body).toEqual({ status: 2, position: "before", gig: 4 });
  });
  it("into an empty column", () => {
    const r = applyMove(board(), 4, 3, 0)!;
    expect(ids(r.board, 2)).toEqual([]);
    expect(ids(r.board, 3)).toEqual([4]);
    expect(r.body).toEqual({ status: 3, position: "bottom" });
  });
  it("reorders within a column", () => {
    const r = applyMove(board(), 1, 1, 2)!;
    expect(ids(r.board, 1)).toEqual([2, 3, 1]);
    expect(r.body).toEqual({ status: 1, position: "after", gig: 3 });
    const up = applyMove(board(), 3, 1, 1)!;
    expect(ids(up.board, 1)).toEqual([1, 3, 2]);
    expect(up.body).toEqual({ status: 1, position: "before", gig: 2 });
  });
  it("is a no-op where it was, and leaves the input alone", () => {
    const b = board();
    expect(applyMove(b, 2, 1, 1)).toBeNull();
    applyMove(b, 2, 2, 0);
    expect(ids(b, 1)).toEqual([1, 2, 3]);
  });
});

describe("reorderIndex", () => {
  it("menu moves", () => {
    expect(reorderIndex("top", 2, 3)).toBe(0);
    expect(reorderIndex("up", 2, 3)).toBe(1);
    expect(reorderIndex("down", 0, 3)).toBe(1);
    expect(reorderIndex("bottom", 0, 3)).toBe(2);
    const r = applyMove(board(), 1, 1, reorderIndex("down", 0, 3))!;
    expect(ids(r.board, 1)).toEqual([2, 1, 3]);
  });
});
