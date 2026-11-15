// Notifications (the bell): API calls and the pure helpers for their text and colour.
import { call } from "./http";
import { CATEGORY_COLOR, PRIORITY_COLOR, SCHEDULE_COLOR } from "./theme";

export type NotificationKind = "mention" | "assigned" | "question" | "missed" | "comment";

export interface Notification {
  id: number;
  workspace_id: number;
  task_id: number | null;
  task_key: string | null;
  task_title: string | null;
  comment_id: number | null;
  kind: NotificationKind | string;
  actor_user_id: number | null;
  created_at: number;
  read_at: number | null;
}

export const notificationsApi = {
  list: (opts: { unread?: boolean; limit?: number } = {}) =>
    call<{ items: Notification[]; unread: number }>("GET", `/me/notifications?limit=${opts.limit ?? 50}${opts.unread ? "&unread=true" : ""}`),
  read: (ids: number[]) => call<{ marked: number }>("POST", "/me/notifications/read", { ids }),
  readAll: () => call<{ marked: number }>("POST", "/me/notifications/read", { all: true }),
};

const KIND: Record<string, { label: string; verb: string; color: string; icon: string }> = {
  mention: { label: "mention", verb: "mentioned you on", color: PRIORITY_COLOR[1]!, icon: "@" },
  assigned: { label: "assigned", verb: "assigned you to", color: CATEGORY_COLOR.done!, icon: "☑" },
  question: { label: "question", verb: "asked a question on", color: PRIORITY_COLOR[2]!, icon: "?" },
  missed: { label: "missed", verb: "", color: CATEGORY_COLOR.waiting!, icon: "◷" },
  comment: { label: "comment", verb: "commented on", color: SCHEDULE_COLOR, icon: "✎" },
};

export function kindInfo(kind: string): { label: string; verb: string; color: string; icon: string } {
  return KIND[kind] ?? { label: kind, verb: "updated", color: CATEGORY_COLOR.backlog!, icon: "•" };
}

// notificationText: one line, e.g. "Ada mentioned you on GIG-12" or "GIG-7: the window closed unclaimed".
export function notificationText(n: Pick<Notification, "kind" | "task_key">, actorName: string | null): string {
  const key = n.task_key ?? "a task";
  if (n.kind === "missed") return `${key}: the window closed unclaimed`;
  const who = actorName ?? "Someone";
  return `${who} ${kindInfo(n.kind).verb} ${key}`;
}

// badgeCount: what the bell badge shows.
export function badgeCount(n: number): string {
  return n <= 0 ? "" : n > 99 ? "99+" : String(n);
}

export interface Placement {
  top: number;
  left: number;
  arrowX: number | null; // the notch's distance from the popover's left edge; null when it can't point at the anchor
}

// placeBelow: where the bell popover goes. It hangs `gap` px under the bell with its right edge in line
// with the bell's right edge, stays `margin` px inside the viewport, and its notch points at the bell's
// middle (dropped when the clamped popover no longer spans it).
export function placeBelow(
  anchor: { left: number; right: number; bottom: number },
  size: { width: number; height: number },
  viewport: { width: number; height: number },
  gap = 10,
  margin = 8,
): Placement {
  const mid = (anchor.left + anchor.right) / 2;
  const maxLeft = Math.max(margin, viewport.width - size.width - margin);
  const left = Math.round(Math.min(Math.max(margin, anchor.right - size.width), maxLeft));
  const top = Math.round(Math.max(margin, Math.min(anchor.bottom + gap, viewport.height - size.height - margin)));
  const x = mid - left;
  return { top, left, arrowX: x >= 14 && x <= size.width - 14 && top === Math.round(anchor.bottom + gap) ? Math.round(x) : null };
}
