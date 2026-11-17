// Live updates: one EventSource per workspace (at most MAX_STREAMS; the server allows 24 per user, enough for a few tabs),
// each activity event re-dispatched on window as
//   new CustomEvent("gigg:activity", { detail: { workspaceId, item } })
// for the board, the gig panel, the bell and the shell to react to. The browser reconnects on its
// own after the server's 10-minute stream end (with Last-Event-ID); when a stream is closed for
// good (an HTTP error such as 401/429) or keeps failing, we close it and retry with a backoff,
// resuming after the last id seen.
import { useEffect, useState } from "react";
import { invalidate, noteEvent } from "./transport";

export const ACTIVITY_EVENT = "gigg:activity";
export const MAX_STREAMS = 8;

export interface ActivityItem {
  id: number;
  at: number;
  actor_user_id: number | null;
  key_id: number | null;
  listener: string;
  action: string;
  entity_type: string;
  entity_id: number | null;
  project_id: number | null;
  data: unknown;
  request_id?: string; // only on this user's own entries
}

export interface ActivityDetail {
  workspaceId: number;
  item: ActivityItem;
}

export type LiveState = "off" | "connecting" | "live";

// backoff: delay before manual reconnect attempt n (1-based): 5 s doubling, capped at 5 minutes.
export function backoff(n: number): number {
  return Math.min(300_000, 5_000 * 2 ** Math.max(0, n - 1));
}

// combine: one indicator for several streams — live when any is open.
export function combine(states: LiveState[]): LiveState {
  if (states.includes("live")) return "live";
  if (states.includes("connecting")) return "connecting";
  return "off";
}

// countsChange: actions after which the inbox count and the bell may have moved.
export function countsChange(action: string): boolean {
  return action.startsWith("gig.") || action.startsWith("comment.");
}

// treeChange: actions after which the sidebar tree (projects, memberships) may be stale.
export function treeChange(action: string): boolean {
  return /^(project|workspace|member)\./.test(action);
}

export function onActivity(fn: (d: ActivityDetail) => void): () => void {
  const h = (e: Event) => fn((e as CustomEvent<ActivityDetail>).detail);
  window.addEventListener(ACTIVITY_EVENT, h);
  return () => window.removeEventListener(ACTIVITY_EVENT, h);
}

// stream keeps one workspace's EventSource alive and reports its state; returns a closer.
// Last event id per workspace, kept across reopenings so a stream picks up where it stopped.
const lastIds = new Map<number, string>();

function stream(ws: { id: number; slug: string }, report: (s: LiveState) => void): () => void {
  let es: EventSource | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let lastId = lastIds.get(ws.id) ?? "";
  let failures = 0; // consecutive errors without an open in between
  let attempts = 0; // manual reconnects since the last successful open
  let stopped = false;

  const open = () => {
    if (stopped) return;
    report("connecting");
    const q = lastId ? `?after_id=${encodeURIComponent(lastId)}` : "";
    const src = new EventSource(`/api/v1/workspaces/${encodeURIComponent(ws.slug)}/events${q}`, { withCredentials: true });
    es = src;
    src.onopen = () => {
      failures = 0;
      attempts = 0;
      report("live");
    };
    src.addEventListener("activity", (e) => {
      const me = e as MessageEvent<string>;
      if (me.lastEventId) {
        lastId = me.lastEventId;
        lastIds.set(ws.id, lastId);
      }
      try {
        const item = JSON.parse(me.data) as ActivityItem;
        // empty the shared read cache first (unless it's this tab's own echo), so the screens that
        // react to the event fetch fresh answers
        noteEvent(item.request_id);
        window.dispatchEvent(new CustomEvent<ActivityDetail>(ACTIVITY_EVENT, { detail: { workspaceId: ws.id, item } }));
      } catch {
        invalidate(); // a malformed message is skipped, but something changed
      }
    });
    src.onerror = () => {
      failures++;
      invalidate(); // events may be missed until the stream is back: trust nothing cached
      // CONNECTING: the browser retries by itself (retry: 3000) — fine for the 10-minute stream end.
      // CLOSED (HTTP error) or repeated failures: take over with a growing delay.
      if (src.readyState === EventSource.CLOSED || failures >= 3) {
        src.close();
        es = null;
        report("off");
        failures = 0;
        attempts++;
        timer = setTimeout(open, backoff(attempts));
      } else {
        report("connecting");
      }
    };
  };
  open();
  return () => {
    stopped = true;
    clearTimeout(timer);
    es?.close();
  };
}

// useLive opens the streams for the given workspaces (first MAX_STREAMS) while mounted and
// returns the combined connection state.
export function useLive(workspaces: { id: number; slug: string }[]): LiveState {
  const list = workspaces.slice(0, MAX_STREAMS);
  const key = list.map((w) => `${w.id}:${w.slug}`).join(",");
  const [states, setStates] = useState<Record<number, LiveState>>({});
  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    let closers: (() => void)[] = [];
    const start = () => {
      closers = list.map((w) => stream(w, (s) => setStates((cur) => (cur[w.id] === s ? cur : { ...cur, [w.id]: s }))));
    };
    const stop = () => {
      closers.forEach((c) => c());
      closers = [];
    };
    // A page kept in the back/forward cache would hold its streams open (the server allows 5 per
    // user): close them when the page is hidden, reopen (resuming from the last id) when it returns.
    const onHide = () => stop();
    const onShow = (e: PageTransitionEvent) => {
      if (e.persisted) start();
    };
    start();
    window.addEventListener("pagehide", onHide);
    window.addEventListener("pageshow", onShow);
    return () => {
      window.removeEventListener("pagehide", onHide);
      window.removeEventListener("pageshow", onShow);
      stop();
      setStates({});
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return combine(Object.values(states));
}
