// Board moves as pure functions: where a dropped card goes (API body) and the optimistic board.
import type { Board, Gig, api } from "./api";

export type MoveBody = Parameters<typeof api.moveGig>[1];

// placement: the move-API body that puts a gig at `index` of a column (`others`: the column's gigs without it).
export function placement(statusId: number, others: Pick<Gig, "id">[], index: number): MoveBody {
  if (others.length === 0) return { status: statusId, position: "bottom" };
  if (index < others.length) return { status: statusId, position: "before", gig: others[index]!.id };
  return { status: statusId, position: "after", gig: others[others.length - 1]!.id };
}

// reorderIndex: the drop index for the card menu's To top / Up / Down / To bottom (card at i of n).
export function reorderIndex(where: "top" | "up" | "down" | "bottom", i: number, n: number): number {
  return { top: 0, up: Math.max(0, i - 1), down: Math.min(n - 1, i + 1), bottom: n - 1 }[where];
}

// applyMove: the board after moving a gig to `index` of a status, or null when nothing changes.
export function applyMove(board: Board, gigId: number, statusId: number, index: number): { board: Board; body: MoveBody } | null {
  const target = board.statuses.find((s) => s.id === statusId);
  const gig = board.statuses.flatMap((s) => s.gigs).find((t) => t.id === gigId);
  if (!target || !gig) return null;
  const others = target.gigs.filter((t) => t.id !== gigId);
  const at = Math.max(0, Math.min(index, others.length));
  if (gig.status_id === statusId && target.gigs.findIndex((t) => t.id === gigId) === at) return null;
  const moved: Gig = { ...gig, status_id: target.id, status_name: target.name, status_category: target.category };
  return {
    body: placement(statusId, others, at),
    board: {
      ...board,
      statuses: board.statuses.map((s) => {
        const rest = s.gigs.filter((t) => t.id !== gigId);
        return s.id === statusId ? { ...s, gigs: [...rest.slice(0, at), moved, ...rest.slice(at)] } : s.gigs.length === rest.length ? s : { ...s, gigs: rest };
      }),
    },
  };
}
