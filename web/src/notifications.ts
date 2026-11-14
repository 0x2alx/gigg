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
  arrowY: number | null; // the notch's distance from the popover's top; null when it can't point at the anchor
}

// placePopover: where the bell popover goes. It opens just right of the sidebar edge (never half over
// it), with its notch level with the bell, and stays `margin` px inside the viewport. When the
// viewport is too narrow to fit it beside the sidebar it is clamped inside and drops the notch.
export function placePopover(
  anchor: { top: number; height: number },
  edge: number,
  size: { width: number; height: number },
  viewport: { width: number; height: number },
  gap = 12,
  margin = 8,
): Placement {
  const mid = anchor.top + anchor.height / 2;
  const notch = 24; // preferred notch offset from the popover's top
  const maxTop = Math.max(margin, viewport.height - size.height - margin);
  const top = Math.round(Math.min(Math.max(margin, mid - notch), maxTop));
  let left = edge + gap;
  let fits = true;
  if (left + size.width > viewport.width - margin) {
    left = Math.max(margin, viewport.width - size.width - margin);
    fits = false;
  }
  const y = Math.min(Math.max(12, mid - top), Math.max(12, size.height - 12));
  return { top, left: Math.round(left), arrowY: fits ? Math.round(y) : null };
}
