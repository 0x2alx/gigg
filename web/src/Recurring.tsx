// The project's recurring tasks (a template on a schedule): rule, next occurrence, counts; pause, edit, delete.
import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError, fmtDate, recurrenceBody, type Recurrence, type Status, type Tag } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { describeRule, fmtInstant, localTZ } from "./recurrence";
import { TaskModal } from "./TaskModal";
import { SCHEDULE_COLOR } from "./theme";
import { Avatar, LabelChip, Modal, Pill, PriorityBadge } from "./ui";
import { HelpButton } from "./help";

export function RecurringModal({ ws, projectId, statuses, labels, canWrite, highlight, onClose, onChanged }: {
  ws: WorkspaceTree;
  projectId: number;
  statuses: Status[];
  labels: Tag[];
  canWrite: boolean;
  highlight?: number;
  onClose: () => void;
  onChanged: (message?: string) => void;
}) {
  const { users } = useApp();
  const [items, setItems] = useState<Recurrence[] | null>(null);
  const [err, setErr] = useState("");
  const [editing, setEditing] = useState<Recurrence | "new" | null>(null);
  const [confirm, setConfirm] = useState<number | null>(null);
  const [busy, setBusy] = useState<number | null>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const closeEdit = useCallback(() => setEditing(null), []);

  const load = useCallback(() => {
    api
      .recurrences(ws.slug, projectId)
      .then((r) => setItems(r.items))
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't load the recurring tasks."));
  }, [ws.slug, projectId]);
  useEffect(load, [load]);

  useEffect(() => {
    if (highlight && items) listRef.current?.querySelector(`[data-schedule="${highlight}"]`)?.scrollIntoView({ block: "nearest" });
  }, [highlight, items]);

  const fail = (e: unknown) => {
    if (e instanceof ApiError && (e.status === 412 || e.status === 409)) {
      setErr("Someone changed that recurring task meanwhile; the list is reloaded.");
      load();
    } else setErr(e instanceof ApiError ? e.message : "That didn't work.");
  };

  const togglePause = (r: Recurrence) => {
    setBusy(r.id);
    setErr("");
    api
      .putRecurrence(r.id, recurrenceBody(r, { paused: !r.paused }), r.version)
      .then((n) => {
        setItems((xs) => xs?.map((x) => (x.id === n.id ? n : x)) ?? null);
        onChanged();
      })
      .catch(fail)
      .finally(() => setBusy(null));
  };

  const remove = (r: Recurrence) => {
    setBusy(r.id);
    setErr("");
    api
      .deleteRecurrence(r.id)
      .then(() => {
        setItems((xs) => xs?.filter((x) => x.id !== r.id) ?? null);
        setConfirm(null);
        onChanged();
      })
      .catch(fail)
      .finally(() => setBusy(null));
  };

  if (editing) {
    return (
      <TaskModal
        ws={ws}
        projectId={projectId}
        statuses={statuses}
        labels={labels}
        schedule={editing === "new" ? undefined : editing}
        repeat={editing === "new" ? "weekly" : undefined}
        onClose={closeEdit}
        onCreated={(msg) => {
          load();
          onChanged(msg);
        }}
      />
    );
  }

  const tz = localTZ();
  return (
    <Modal title="⟳ Recurring tasks" onClose={onClose} wide help={<HelpButton topic="recurring" label="Help: templates, schedules and recurring tasks" />}>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      {items === null && !err && <p className="muted">Loading…</p>}
      {items?.length === 0 && <p className="muted">No recurring tasks in this project yet. Make one here, or with Repeat in a new task.</p>}
      {items && items.length > 0 && (
        <ul className="schedules" ref={listRef} aria-label="Recurring tasks">
          {items.map((r) => (
            <li key={r.id} data-schedule={r.id} className={"schedule" + (r.id === highlight ? " flash highlighted" : "") + (r.paused ? " paused" : "")} style={r.color ? { borderLeftColor: r.color } : undefined}>
              <div className="schedule-top">
                <Pill color={SCHEDULE_COLOR} title="Recurring">
                  ⟳
                </Pill>
                <strong className="schedule-title">{r.title}</strong>
                {r.paused && <span className="pill pill-danger">paused</span>}
                {!r.next_at && <span className="pill pill-muted">finished</span>}
                <PriorityBadge priority={r.priority} />
              </div>
              <div className="schedule-rule">
                {describeRule(r)}
                {r.tz !== tz && <span className="pill pill-ws">{r.tz}</span>}
              </div>
              <div className="chips">
                {r.next_at && !r.paused && <span className="pill pill-accent">next {fmtInstant(r.next_at, tz)}</span>}
                {r.next_at && r.paused && <span className="pill pill-muted">next {fmtInstant(r.next_at, tz)} (paused)</span>}
                <span className="pill pill-muted" title="Tasks created">
                  ✓ {r.created_count}
                  {r.max_count ? ` / ${r.max_count}` : ""} created
                </span>
                {r.skipped_count > 0 && (
                  <span className="pill pill-muted" title="Skipped: the previous one was still open, or the window had passed">
                    ⤼ {r.skipped_count} skipped
                  </span>
                )}
                {r.ends_on && <span className="pill pill-muted">until {fmtDate(r.ends_on)}</span>}
                {r.lead_minutes > 0 && <span className="pill pill-muted">{r.lead_minutes} min early</span>}
                {r.overlap === "create" && <span className="pill pill-muted">overlap</span>}
                <span className="pill pill-muted" title="The schedule (timing) it runs on">
                  ⏱ {r.schedule_name}
                  {r.schedule_uses > 1 ? ` · ×${r.schedule_uses}` : ""}
                </span>
                {(r.template_name !== r.title || r.template_uses > 1) && (
                  <span className="pill pill-muted" title="The template its tasks are made from">
                    📋 {r.template_name}
                    {r.template_uses > 1 ? ` · ×${r.template_uses}` : ""}
                  </span>
                )}
                {(r.checklist?.length ?? 0) > 0 && <span className="pill pill-muted">☑ {r.checklist!.length} items</span>}
                {(r.labels ?? []).map((n) => (
                  <LabelChip key={n} label={labels.find((l) => l.name.toLowerCase() === n.toLowerCase()) ?? { id: 0, name: n, color: null }} />
                ))}
                {(r.assignees ?? []).map((id) => {
                  const u = users.get(id);
                  return u ? <Avatar key={id} user={u} /> : null;
                })}
              </div>
              {canWrite && (
                <div className="schedule-actions">
                  {confirm === r.id ? (
                    <>
                      <span className="small">Delete this recurring task? Its tasks, template and schedule stay.</span>
                      <button type="button" className="button danger-button small" disabled={busy === r.id} onClick={() => remove(r)}>
                        Delete
                      </button>
                      <button type="button" className="button secondary small" onClick={() => setConfirm(null)}>
                        Keep
                      </button>
                    </>
                  ) : (
                    <>
                      <button type="button" className="button secondary small" disabled={busy === r.id} onClick={() => togglePause(r)} aria-label={`${r.paused ? "Resume" : "Pause"} ${r.title}`}>
                        {r.paused ? "▶ Resume" : "⏸ Pause"}
                      </button>
                      <button type="button" className="button secondary small" onClick={() => setEditing(r)} aria-label={`Edit ${r.title}`}>
                        ✎ Edit
                      </button>
                      <button type="button" className="button danger-button small" onClick={() => setConfirm(r.id)} aria-label={`Delete ${r.title}`}>
                        🗑 Delete
                      </button>
                    </>
                  )}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      {canWrite && (
        <div className="row end">
          <button type="button" className="button" onClick={() => setEditing("new")}>
            + New recurring task
          </button>
        </div>
      )}
    </Modal>
  );
}
