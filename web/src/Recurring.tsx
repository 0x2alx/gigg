// Every recurring task in reach (a template on a schedule), across the workspaces, narrowed by an optional
// workspace/project filter: rule, next occurrence, counts; pause, edit, delete.
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { api, ApiError, fmtDate, hasCap, recurrenceBody, type Recurrence, type Tag } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { PlaceFilter, placeMatch, placeStart, type Place } from "./places";
import { WorkspaceBadge } from "./WorkspaceBadge";
import { describeRule, fmtInstant, localTZ } from "./recurrence";
import { TaskModal } from "./TaskModal";
import { SCHEDULE_COLOR } from "./theme";
import { ClearFilters, Avatar, LabelChip, Modal, Pill, PriorityBadge } from "./ui";
import { HelpButton } from "./help";
import { Icon } from "./icons";

const canWriteIn = (ws: WorkspaceTree) => ws.role !== "viewer" && ws.role !== "commenter";

export function RecurringModal({ initialPlace = "", highlight, onClose, onChanged }: {
  initialPlace?: Place;
  highlight?: number;
  onClose: () => void;
  onChanged: (message?: string) => void;
}) {
  const { users, tree, me } = useApp();
  const [items, setItems] = useState<Recurrence[] | null>(null);
  const [labelsOf, setLabelsOf] = useState<Map<number, Tag[]>>(new Map());
  const [place, setPlace] = useState<Place>(initialPlace);
  const [err, setErr] = useState("");
  const [editing, setEditing] = useState<Recurrence | "new" | null>(null);
  const [confirm, setConfirm] = useState<number | null>(null);
  // the confirm's "also delete" ticks (start unticked each time it opens)
  const [also, setAlso] = useState<{ template: boolean; schedule: boolean }>({ template: false, schedule: false });
  const askDelete = (id: number) => {
    setAlso({ template: false, schedule: false });
    setConfirm(id);
  };
  const [busy, setBusy] = useState<number | null>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const closeEdit = useCallback(() => setEditing(null), []);

  // one call per workspace; a workspace that fails is named, the others still show
  const wsKey = tree.map((w) => w.id).join(",");
  const loadSeq = useRef(0);
  const load = useCallback(() => {
    const seq = ++loadSeq.current;
    Promise.allSettled(tree.map((w) => api.recurrences(w.slug))).then((res) => {
      if (seq !== loadSeq.current) return; // a newer load is on its way
      const all: Recurrence[] = [];
      const bad: string[] = [];
      res.forEach((r, i) => (r.status === "fulfilled" ? all.push(...r.value.items) : bad.push(tree[i]!.name)));
      setItems(all.sort((a, b) => a.title.localeCompare(b.title)));
      setErr(bad.length ? `Couldn't load the recurring tasks of ${bad.join(", ")}.` : "");
    });
  }, [wsKey]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(load, [load]);
  useEffect(() => {
    Promise.allSettled(tree.map((w) => api.labels(w.slug))).then((res) =>
      setLabelsOf(new Map(res.flatMap((r, i) => (r.status === "fulfilled" ? [[tree[i]!.id, r.value.items] as [number, Tag[]]] : [])))),
    );
  }, [wsKey]); // eslint-disable-line react-hooks/exhaustive-deps
  const wsOf = useCallback((id: number) => tree.find((w) => w.id === id), [tree]);
  const shown = useMemo(() => (items ?? []).filter((r) => placeMatch(place, tree, r.workspace_id, r.project_id)), [items, place, tree]);
  const writable = useMemo(() => tree.filter((w) => canWriteIn(w) && hasCap(me, "schedules", w.id)), [tree, me]); // the server wants Schedules for recurring tasks
  const start = placeStart(place, tree, (w) => writable.includes(w));

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
      .deleteRecurrence(r.id, also)
      .then(() => {
        setItems((xs) => xs?.filter((x) => x.id !== r.id) ?? null);
        setConfirm(null);
        onChanged();
      })
      .catch(fail)
      .finally(() => setBusy(null));
  };

  const editWs = editing && editing !== "new" ? wsOf(editing.workspace_id) : undefined;
  if (editing && (editing === "new" ? start : editWs)) {
    return (
      <TaskModal
        ws={editing === "new" ? start!.ws : editWs!}
        projectId={editing === "new" ? start!.projectId : editing.project_id}
        statuses={[]}
        labels={labelsOf.get(editing === "new" ? start!.ws.id : editing.workspace_id) ?? []}
        choices={editing === "new" ? writable : undefined}
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
    <Modal title="Recurring tasks" onClose={onClose} wide help={<HelpButton topic="recurring" label="Help: templates, schedules and recurring tasks" />}>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      <div className="list-bar">
        <PlaceFilter value={place} onChange={setPlace} tree={tree} />
        <ClearFilters active={place !== ""} onClear={() => setPlace("")} />
        {items && <span className="muted small">{shown.length === items.length ? `${items.length} recurring` : `${shown.length} of ${items.length}`}</span>}
      </div>
      {items === null && !err && <p className="muted">Loading…</p>}
      {items !== null && shown.length === 0 && <p className="muted">{items.length === 0 ? "No recurring tasks yet. Make one here, or with Repeat in a new task." : "None here. Show everything to see the others."}</p>}
      {shown.length > 0 && (
        <ul className="schedules" ref={listRef} aria-label="Recurring tasks">
          {shown.map((r) => {
            const rws = wsOf(r.workspace_id);
            const canWrite = !!rws && canWriteIn(rws) && hasCap(me, "schedules", rws.id);
            const labels = labelsOf.get(r.workspace_id) ?? [];
            return (
            <li key={r.id} data-schedule={r.id} className={"schedule" + (r.id === highlight ? " flash highlighted" : "") + (r.paused ? " paused" : "")} style={r.color ? ({ "--row-c": r.color } as CSSProperties) : undefined}>
              <div className="schedule-top">
                <Pill color={SCHEDULE_COLOR} title="Recurring">
                  ⟳
                </Pill>
                <strong className="schedule-title">{r.title}</strong>
                {rws && tree.length > 1 && <WorkspaceBadge ws={rws} />}
                <span className="pill pill-key">{rws?.projects.find((p) => p.id === r.project_id)?.key ?? "?"}</span>
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
                {/* the schedule, unless it is just the rule shown above (a schedule named after its timing, used once) */}
                {(r.schedule_name !== describeRule(r) || r.schedule_uses > 1) && (
                  <span className="pill pill-muted" title="The schedule (timing) it runs on">
                    <Icon name="clock" /> {r.schedule_name}
                    {r.schedule_uses > 1 ? ` · ×${r.schedule_uses}` : ""}
                  </span>
                )}
                {(r.template_name !== r.title || r.template_uses > 1) && (
                  <span className="pill pill-muted" title="The template its tasks are made from">
                    <Icon name="template" /> {r.template_name}
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
                    <div className="rec-delete">
                      <span className="small">Delete this recurring task? The tasks it already made stay.</span>
                      {/* its template and schedule can go too, when no other recurring task uses them */}
                      {r.template_uses <= 1 ? (
                        (r.template_created_by === me.user.id || hasCap(me, "templates", r.workspace_id)) && (
                          <label className="check small">
                            <input type="checkbox" checked={also.template} onChange={(e) => setAlso((a) => ({ ...a, template: e.target.checked }))} />
                            Also delete its template “{r.template_name}”
                          </label>
                        )
                      ) : (
                        <span className="muted small">Its template “{r.template_name}” stays: {r.template_uses - 1} other recurring task{r.template_uses === 2 ? " uses" : "s use"} it.</span>
                      )}
                      {r.schedule_uses <= 1 ? (
                        <label className="check small">
                          <input type="checkbox" checked={also.schedule} onChange={(e) => setAlso((a) => ({ ...a, schedule: e.target.checked }))} />
                          Also delete its schedule “{r.schedule_name}”
                        </label>
                      ) : (
                        <span className="muted small">Its schedule “{r.schedule_name}” stays: {r.schedule_uses - 1} other recurring task{r.schedule_uses === 2 ? " uses" : "s use"} it.</span>
                      )}
                      <div className="row">
                        <button type="button" className="button danger-button small" disabled={busy === r.id} onClick={() => remove(r)}>
                          Delete
                        </button>
                        <button type="button" className="button secondary small" onClick={() => setConfirm(null)}>
                          Keep
                        </button>
                      </div>
                    </div>
                  ) : (
                    <>
                      <button type="button" className="button secondary small" disabled={busy === r.id} onClick={() => togglePause(r)} aria-label={`${r.paused ? "Resume" : "Pause"} ${r.title}`}>
                        <Icon name={r.paused ? "play" : "pause"} />
                        {r.paused ? "Resume" : "Pause"}
                      </button>
                      <button type="button" className="button secondary small" onClick={() => setEditing(r)} aria-label={`Edit ${r.title}`}>
                        <Icon name="pencil" /> Edit
                      </button>
                      <button type="button" className="button danger-button small" onClick={() => askDelete(r.id)} aria-label={`Delete ${r.title}`}>
                        <Icon name="trash" /> Delete
                      </button>
                    </>
                  )}
                </div>
              )}
            </li>
            );
          })}
        </ul>
      )}
      {start && (
        <div className="row end">
          <button type="button" className="button" onClick={() => setEditing("new")}>
            + New recurring task
          </button>
        </div>
      )}
    </Modal>
  );
}
