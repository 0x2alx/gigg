// The task's history: its own entries from the activity log (edits, moves, links, checklist,
// comments, agent runs), newest first, as readable lines. Only who, what and when; the admin audit
// log keeps the rest (listener, key, IP). Loaded when the section is opened, and again when a live
// event names this task.
import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, fmtTime, type Task, type User } from "./api";
import { describe } from "./historyText";
import { useApp } from "./App";
import { onActivity, type ActivityItem } from "./live";
import { request } from "./transport";
import { relTime } from "./views/viewsApi";
import { Section } from "./TaskSections";

function actorName(it: ActivityItem, users: Map<number, User>): string {
  if (it.actor_user_id === null) return it.listener === "scheduler" ? "The scheduler" : "gigg";
  const u = users.get(it.actor_user_id);
  return u?.display_name || u?.username || "Someone";
}

const LIMIT = 500; // the server's page: the newest 500 entries

export function HistorySection({ task }: { task: Task }) {
  const { users } = useApp();
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<ActivityItem[] | null>(null);
  const [err, setErr] = useState("");
  const [all, setAll] = useState(false);
  const taskId = useRef(task.id);
  taskId.current = task.id;

  const load = useCallback(() => {
    const id = task.id;
    request("GET", `/tasks/${id}/activity`).then(
      (r) => {
        if (taskId.current !== id) return; // the panel moved to another task meanwhile
        if (!r.ok) {
          const d = (r.data ?? {}) as { detail?: string };
          setErr(r.status === 403 ? "You can't see this task's history." : (d.detail ?? "Couldn't load the history."));
          return;
        }
        setErr("");
        setItems(((r.data as { items?: ActivityItem[] }).items ?? []).filter((x) => x.action !== "run.progress"));
      },
      (e: unknown) => {
        if (taskId.current === id) setErr(e instanceof ApiError ? e.message : "Couldn't load the history.");
      },
    );
  }, [task.id]);

  useEffect(() => {
    if (open) load();
  }, [open, load, task.version]);

  // live: an entry about this task (or one of its comments or runs) reloads the list
  useEffect(() => {
    if (!open) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = onActivity(({ item }) => {
      const tid = (item.data as { task_id?: unknown } | null)?.task_id;
      if ((item.entity_type === "task" && item.entity_id === task.id) || tid === task.id) {
        clearTimeout(timer);
        timer = setTimeout(load, 400);
      }
    });
    return () => {
      off();
      clearTimeout(timer);
    };
  }, [open, load, task.id]);

  const shown = items && !all ? items.slice(0, 15) : items;
  return (
    <Section title="History" label="History" defaultOpen={false} onOpenChange={setOpen}>
      <div aria-live="polite">
        {err ? (
          <p className="muted small">{err}</p>
        ) : items === null ? (
          <p className="muted small">Loading…</p>
        ) : items.length === 0 ? (
          <p className="muted small">Nothing recorded yet.</p>
        ) : (
          <ol className="history">
            {shown!.map((it) => (
              <li key={it.id}>
                <span>
                  <strong>{actorName(it, users)}</strong> {describe(it, users)}
                </span>
                <time className="muted small" dateTime={new Date(it.at).toISOString()} title={fmtTime(it.at)}>
                  {relTime(it.at)}
                </time>
              </li>
            ))}
          </ol>
        )}
        {items && items.length > 15 && (
          <button type="button" className="link small" aria-expanded={all} onClick={() => setAll(!all)}>
            {all ? "Show the latest 15" : `Show all ${items.length}`}
          </button>
        )}
        {items && items.length >= LIMIT && all && <p className="muted small">The latest {LIMIT} changes; older ones are in the admin audit log.</p>}
      </div>
    </Section>
  );
}
