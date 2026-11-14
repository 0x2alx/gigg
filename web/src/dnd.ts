// Board moves as pure functions: where a dropped card goes (API body) and the optimistic board.
import type { Board, Task, api } from "./api";

export type MoveBody = Parameters<typeof api.moveTask>[1];

// placement: the move-API body that puts a task at `index` of a column (`others`: the column's tasks without it).
export function placement(statusId: number, others: Pick<Task, "id">[], index: number): MoveBody {
  if (others.length === 0) return { status: statusId, position: "bottom" };
  if (index < others.length) return { status: statusId, position: "before", task: others[index]!.id };
  return { status: statusId, position: "after", task: others[others.length - 1]!.id };
}

// reorderIndex: the drop index for the card menu's To top / Up / Down / To bottom (card at i of n).
export function reorderIndex(where: "top" | "up" | "down" | "bottom", i: number, n: number): number {
  return { top: 0, up: Math.max(0, i - 1), down: Math.min(n - 1, i + 1), bottom: n - 1 }[where];
}

// applyMove: the board after moving a task to `index` of a status, or null when nothing changes.
export function applyMove(board: Board, taskId: number, statusId: number, index: number): { board: Board; body: MoveBody } | null {
  const target = board.statuses.find((s) => s.id === statusId);
  const task = board.statuses.flatMap((s) => s.tasks).find((t) => t.id === taskId);
  if (!target || !task) return null;
  const others = target.tasks.filter((t) => t.id !== taskId);
  const at = Math.max(0, Math.min(index, others.length));
  if (task.status_id === statusId && target.tasks.findIndex((t) => t.id === taskId) === at) return null;
  const moved: Task = { ...task, status_id: target.id, status_name: target.name, status_category: target.category };
  return {
    body: placement(statusId, others, at),
    board: {
      ...board,
      statuses: board.statuses.map((s) => {
        const rest = s.tasks.filter((t) => t.id !== taskId);
        return s.id === statusId ? { ...s, tasks: [...rest.slice(0, at), moved, ...rest.slice(at)] } : s.tasks.length === rest.length ? s : { ...s, tasks: rest };
      }),
    },
  };
}
