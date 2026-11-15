import { useCallback, useEffect, useRef, useState, type CSSProperties, type FormEvent, type ReactNode } from "react";
import { api, ApiError, fmtTime, hasCap, priorityNames, resumeCommand, type Comment, type CustomField, type Recurrence, type Run, type Status, type Tag, type Task, type User } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { AssigneePicker, ColorPicker, DateTimeInput, LabelInput, assignable } from "./fields";
import { DEFAULT_DUE_TIME, DEFAULT_START_TIME } from "./dates";
import { Markdown, setMentionUsers } from "./markdown";
import { MarkdownEditor } from "./MarkdownEditor";
import { ChecklistSection, CustomFieldsSection, DependenciesSection, SubtasksSection } from "./TaskSections";
import { describeRule } from "./recurrence";
import { CATEGORY_COLOR } from "./theme";
import { EffortSelect, ModelInput, ToolsInput } from "./runsettings";
import { Avatar, EXIT_MS, LabelChip, Pill, PriorityBadge, StatusBadge, TaskBadges, UserBadge, reducedMotion } from "./ui";
import { WorkspaceBadge } from "./WorkspaceBadge";
import { HelpButton } from "./help";
import { Icon } from "./icons";

const kindBadge: Record<Comment["kind"], [string, string] | null> = {
  comment: null,
  report: ["report", "#0090FF"],
  question: ["question", "#E5484D"],
  resume_request: ["resume request", "#8E4EC6"],
  system: ["system", "#8B8D98"],
};

const runColor: Record<string, string> = {
  running: "#F76B15", done: "#46A758", needs_human: "#E5484D", error: "#E5484D", timeout: "#FFC53D", interrupted: "#8B8D98", parked: "#3E63DD",
};

const FULL_KEY = "gigg.panel.full";

export function TaskPanel({ taskKey, ws, statuses, labels, canWrite, onClose, onChanged, onMoved, onOpenSchedule }: {
  taskKey: string;
  ws: WorkspaceTree;
  statuses: Status[];
  labels: Tag[];
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
  onMoved: (t: Task) => void;
  onOpenSchedule?: (id: number) => void;
}) {
  const { users, members, me } = useApp();
  setMentionUsers(users);
  const [task, setTask] = useState<Task | null>(null);
  const [fields, setFields] = useState<CustomField[]>([]);
  const [comments, setComments] = useState<Comment[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [err, setErr] = useState("");
  const [editing, setEditing] = useState<"" | "text" | "labels" | "assignees" | "color">("");
  const [schedule, setSchedule] = useState<Recurrence | "gone" | null>(null);
  // full: the panel covers the whole view next to the sidebar (remembered per browser)
  const [full, setFull] = useState(() => {
    try {
      return localStorage.getItem(FULL_KEY) === "1";
    } catch {
      return false;
    }
  });
  const toggleFull = () =>
    setFull((f) => {
      try {
        localStorage.setItem(FULL_KEY, f ? "0" : "1");
      } catch {
        /* storage unavailable: the choice lasts for this page */
      }
      return !f;
    });

  const load = useCallback(() => {
    api
      .task(taskKey)
      .then((t) => {
        setTask(t);
        setErr("");
        return Promise.all([api.comments(t.id), api.runs(t.id)]).then(([c, r]) => {
          setComments(c.items);
          setRuns(r.items);
        });
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't load the task."));
  }, [taskKey]);
  useEffect(load, [load]);

  useEffect(() => {
    let live = true;
    api.fields(ws.id).then((r) => live && setFields(r.items), () => live && setFields([]));
    return () => {
      live = false;
    };
  }, [ws.id]);

  // Live refresh: another client changed this task (or something on it), reload it (debounced).
  const taskId = task?.id ?? null;
  const loadRef = useRef(load);
  loadRef.current = load;
  useEffect(() => {
    if (taskId === null) return;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const on = (e: Event) => {
      const d = (e as CustomEvent<{ workspaceId?: number; item?: { action?: string; entity_type?: string; entity_id?: number | null; data?: unknown } }>).detail;
      const it = d?.item;
      if (!it) return;
      const data = (it.data ?? {}) as { task_id?: unknown };
      const hit = (it.entity_id === taskId && (!it.entity_type || it.entity_type === "task")) || data.task_id === taskId;
      if (!hit) return;
      if (timer) clearTimeout(timer);
      // a progress note changes the runs only; anything else reloads the task, its comments and runs
      timer = setTimeout(() => (it.action === "run.progress" ? api.runs(taskId).then((r) => setRuns(r.items), () => undefined) : loadRef.current()), 400);
    };
    window.addEventListener("gigg:activity", on);
    return () => {
      window.removeEventListener("gigg:activity", on);
      if (timer) clearTimeout(timer);
    };
  }, [taskId]);

  const recurrenceId = task?.recurrence_id ?? null;
  useEffect(() => {
    setSchedule(null);
    if (!recurrenceId) return;
    let live = true;
    api
      .recurrence(recurrenceId)
      .then((r) => live && setSchedule(r))
      .catch(() => live && setSchedule("gone"));
    return () => {
      live = false;
    };
  }, [recurrenceId]);

  // flashDirty: when a field in the panel holds unsaved text, scroll to it, focus it and flash it
  // (instead of closing the panel and losing the text). True when there was one.
  const panelRef = useRef<HTMLElement>(null);
  const flashDirty = useCallback((): boolean => {
    const el = panelRef.current?.querySelector<HTMLElement>("[data-dirty]");
    if (!el) return false;
    el.classList.remove("dirty-flash");
    void el.offsetWidth; // restart the animation
    el.classList.add("dirty-flash");
    el.scrollIntoView({ block: "nearest", behavior: "smooth" });
    (el.matches("input, textarea") ? el : el.querySelector<HTMLElement>("textarea, input"))?.focus({ preventScroll: true });
    setTimeout(() => el.classList.remove("dirty-flash"), 1300);
    return true;
  }, []);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  // closing slides the panel out first; opening another task meanwhile cancels it
  // (the closing state names the task being closed, so a newly opened task never renders as closing)
  const [closingKey, setClosingKey] = useState<string | null>(null);
  const closing = closingKey === taskKey;
  const keyRef = useRef(taskKey);
  keyRef.current = taskKey;
  const closeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const close = useCallback(() => {
    if (reducedMotion()) return closeRef.current();
    if (closeTimer.current) return;
    setClosingKey(keyRef.current);
    closeTimer.current = setTimeout(() => {
      closeTimer.current = undefined;
      closeRef.current();
    }, EXIT_MS);
  }, []);
  useEffect(() => {
    clearTimeout(closeTimer.current);
    closeTimer.current = undefined;
    setClosingKey(null);
  }, [taskKey]);
  useEffect(() => () => clearTimeout(closeTimer.current), []);
  const editingRef = useRef(editing);
  editingRef.current = editing;

  // Esc closes (not over a modal; an open inline editor closes first; unsaved text is flashed).
  useEffect(() => {
    const esc = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented || document.querySelector('[aria-modal="true"], .modal-backdrop')) return;
      if (flashDirty()) return;
      if (editingRef.current) setEditing("");
      else close();
    };
    document.addEventListener("keydown", esc);
    return () => document.removeEventListener("keydown", esc);
  }, [flashDirty, close]);

  // A click on the board outside the panel closes it, like ✕. Not when the press started inside
  // (drag-select), not in modals, popovers or menus, not when the click opened something else (the
  // URL changed: another task, another page), and not while a field holds unsaved text.
  useEffect(() => {
    let down: string | null = null;
    const outside = (t: EventTarget | null) => {
      const panel = panelRef.current;
      if (!panel || !(t instanceof Element) || !t.isConnected || panel.contains(t)) return false;
      return !t.closest('.modal-backdrop, [role="dialog"], [aria-modal="true"], .bell-pop, .menu, .mention-list, .toast, .sidebar');
    };
    const onDown = (e: PointerEvent) => {
      down = e.button === 0 && !document.querySelector('[aria-modal="true"]') && outside(e.target) ? window.location.pathname : null;
    };
    const onClick = (e: MouseEvent) => {
      const path = down;
      down = null;
      if (path === null || e.button !== 0 || !outside(e.target) || window.location.pathname !== path) return;
      if (flashDirty()) return;
      close();
    };
    document.addEventListener("pointerdown", onDown, true);
    document.addEventListener("click", onClick);
    return () => {
      document.removeEventListener("pointerdown", onDown, true);
      document.removeEventListener("click", onClick);
    };
  }, [flashDirty, close]);

  // Focus: the panel takes focus when it opens or shows another task (keyboard users land in it);
  // on close, focus goes back to what had it before (the card), if that is still on the page.
  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const panel = panelRef.current;
    return () => {
      const active = document.activeElement;
      if (opener?.isConnected && (!active || active === document.body || panel?.contains(active))) opener.focus({ preventScroll: true });
    };
  }, []);
  useEffect(() => {
    const panel = panelRef.current;
    if (panel && !panel.contains(document.activeElement)) panel.focus({ preventScroll: true });
  }, [taskKey]);

  const act = (p: Promise<Task>, after?: (t: Task) => void) =>
    p
      .then((t) => {
        setTask(t);
        setEditing("");
        onChanged();
        after?.(t);
        load();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "That didn't work."));

  if (!task) {
    return (
      <aside ref={panelRef} tabIndex={-1} className={"panel" + (full ? " panel-full" : "") + (closing ? " closing" : "")} inert={closing || undefined} aria-label="Task">
        <button type="button" className="icon-button close" onClick={close} aria-label="Close">
          <Icon name="x" />
        </button>
        <p className={err ? "danger" : "muted"}>{err || "Loading…"}</p>
      </aside>
    );
  }

  const cmd = resumeCommand(task.session_id);
  const lastRun = runs[0];
  const project = ws.projects.find((p) => p.id === task.project_id);
  const creator = users.get(task.created_by);
  const candidates = assignable(users, members.get(ws.id) ?? []);
  const labelFor = (l: Tag) => labels.find((x) => x.id === l.id) ?? l;
  const status = statuses.find((s) => s.id === task.status_id);
  const overdue = task.due_at !== null && task.due_at < Date.now() && task.status_category !== "done";
  const can = (cap: string) => canWrite && hasCap(me, cap, task.workspace_id);
  const onTask = (t: Task) => {
    setTask(t);
    setErr("");
    onChanged();
  };

  return (
    <aside ref={panelRef} tabIndex={-1} className={"panel" + (full ? " panel-full" : "") + (closing ? " closing" : "")} inert={closing || undefined} aria-label={`Task ${task.key}`} style={task.color ? ({ "--panel-c": task.color } as CSSProperties) : undefined}>
      <div className="tp-top">
        <WorkspaceBadge ws={ws} />
        <span className="pill pill-key tp-key" title={`Task key ${task.key}`}>
          {task.key}
        </span>
        <div className="tp-badges">
          <StatusBadge name={task.status_name} category={task.status_category} color={status?.color} />
          <PriorityBadge priority={task.priority} />
          <TaskBadges task={task} />
          {!!task.blocked_by && (
            <span className="pill pill-danger" title="Agents won't pick it up until its blockers are done">
              <Icon name="lock" /> Blocked by {task.blocked_by} open
            </span>
          )}
          {!!task.archived_at && <span className="pill pill-muted"><Icon name="archive" /> archived</span>}
          {task.deleted && <span className="pill pill-danger">in trash</span>}
        </div>
        <button
          type="button"
          className="icon-button tp-expand"
          onClick={toggleFull}
          aria-pressed={full}
          aria-label={full ? "Back to the side panel" : "Expand to the full view"}
          title={full ? "Back to the side panel" : "Expand to the full view"}
        >
          <Icon name={full ? "shrink" : "expand"} />
        </button>
        <button type="button" className="icon-button close" onClick={close} aria-label="Close" title="Close (Esc)">
          <Icon name="x" />
        </button>
      </div>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      {!!task.archived_at && (
        <div className="banner banner-muted" role="status">
          <Icon name="archive" /> Archived {fmtTime(task.archived_at)}: hidden from the board and lists.
          {can("archive_tasks") && (
            <button type="button" className="button secondary small" onClick={() => act(api.unarchiveTask(task.id))}>
              <Icon name="undo" /> Unarchive
            </button>
          )}
        </div>
      )}
      {!!task.blocked_by && (
        <div className="banner banner-danger" role="status">
          <Icon name="lock" /> Blocked by {task.blocked_by} open task{task.blocked_by === 1 ? "" : "s"}: agents won't pick it up until {task.blocked_by === 1 ? "it is" : "they are"} done.
        </div>
      )}

      <div className="tp-body">
        <div className="tp-main">
          {editing === "text" ? (
            <TextForm task={task} users={candidates} onCancel={() => setEditing("")} onSave={(b) => act(api.patchTask(task.id, b))} />
          ) : (
            <>
              <div className="tp-titlebar">
                <h2 className="panel-title">{task.title}</h2>
                {canWrite && (
                  <button type="button" className="icon-button" aria-label="Edit title and description" title="Edit title and description" onClick={() => setEditing("text")}>
                    <Icon name="pencil" />
                  </button>
                )}
              </div>
              {task.aliases.length > 0 && <p className="muted small tp-aliases">formerly {task.aliases.join(", ")}</p>}
              <section className="tp-desc" aria-label="Description">
                {task.description_md ? (
                  <Markdown source={task.description_md} className="description" />
                ) : canWrite ? (
                  <button type="button" className="tp-desc-empty" onClick={() => setEditing("text")}>
                    Add a description…
                  </button>
                ) : (
                  <p className="muted tp-desc-empty">No description.</p>
                )}
              </section>
            </>
          )}
          {(creator || task.assignees.length > 0) && (
            <div className="tp-people" aria-label="People">
              {creator && <Avatar user={creator} size="l" />}
              {task.assignees.length > 0 && <span className="arrow" aria-hidden="true"><Icon name="arrow-right" /></span>}
              {task.assignees.map((id) => {
                const u = users.get(id);
                return u ? <Avatar key={id} user={u} size="l" /> : null;
              })}
              <span className="muted small">
                {creator ? `${creator.display_name || creator.username}` : ""}
                {task.assignees.length > 0 ? ` → ${task.assignees.map((id) => users.get(id)?.display_name || users.get(id)?.username || "#" + id).join(", ")}` : ""}
              </span>
            </div>
          )}
          <section className="comments" aria-label="Comments">
            <h3>
              Comments <span className="count">{comments.length}</span>
            </h3>
            {comments.length === 0 && <p className="muted">No comments yet.</p>}
            {comments.map((c) => {
              const author = users.get(c.author_id ?? 0);
              const kb = kindBadge[c.kind];
              return (
                <article key={c.id} className={"comment kind-" + c.kind}>
                  <div className="comment-head">
                    {author ? <Avatar user={author} size="l" /> : null}
                    <strong title={`@${c.author_username}`}>{author?.display_name || c.author_username}</strong>
                    {c.author_kind === "agent" && <span className="pill pill-agent">agent</span>}
                    {kb && <Pill color={kb[1]}>{kb[0]}</Pill>}
                    <span className="muted small">{fmtTime(c.created_at)}</span>
                  </div>
                  {c.deleted ? <p className="muted">Deleted.</p> : <Markdown source={c.body_md} />}
                </article>
              );
            })}
            {ws.role !== "viewer" && <AddComment taskId={task.id} users={candidates} onDone={load} />}
          </section>
        </div>
        <div className="tp-side">
          <div className="tsecs">
            <ChecklistSection task={task} canEdit={can("checklists")} onTask={onTask} />
            <SubtasksSection task={task} wsId={ws.id} canCreate={can("create_tasks") && !task.deleted} onOpen={onMoved} onChanged={onChanged} />
            <DependenciesSection task={task} wsId={ws.id} canEdit={can("relations")} onTask={onTask} onOpen={onMoved} />
            <CustomFieldsSection task={task} fields={fields} canEdit={canWrite} onTask={onTask} />
          </div>
          <div className="tp-props">
            <PropGroup title="Details">
              <Row label="Workspace">
                <span className="pill pill-ws">{ws.name}</span>
              </Row>
              <Row label="Project">
                {canWrite ? (
                  <select value={task.project_id} onChange={(e) => act(api.moveTask(task.id, { project: Number(e.target.value) }), (t) => onMoved(t))} aria-label="Project">
                    {ws.projects.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.key} · {p.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  project?.name
                )}
              </Row>
              <Row label="Status">
                {canWrite ? (
                  <select value={task.status_id} onChange={(e) => act(api.moveTask(task.id, { status: Number(e.target.value) }))} aria-label="Status">
                    {statuses.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <StatusBadge name={task.status_name} category={task.status_category} />
                )}
              </Row>
              <Row label="Priority">
                {canWrite ? (
                  <select value={task.priority} onChange={(e) => act(api.patchTask(task.id, { priority: Number(e.target.value) }))} aria-label="Priority">
                    {priorityNames.map((n, i) => (
                      <option key={n} value={i}>
                        {n}
                      </option>
                    ))}
                  </select>
                ) : (
                  <PriorityBadge priority={task.priority} showNone />
                )}
              </Row>
            </PropGroup>

            <PropGroup title="Agent run" help={<HelpButton topic="agents" label="Help: how agents run tasks" />}>
              <Row tip="Model for the agent’s run (an alias like opus or sonnet, or a full model id). Empty: the agent’s default" label="Model">
                <ModelInput value={task.model ?? ""} disabled={!canWrite} onCommit={(v) => act(api.patchTask(task.id, { model: v }))} />
              </Row>
              <Row tip="Reasoning effort for the agent’s run (low to max). Empty: the agent’s default" label="Effort">
                <EffortSelect value={task.effort ?? ""} disabled={!canWrite} onChange={(v) => act(api.patchTask(task.id, { effort: v }))} />
              </Row>
              <Row tip="Run it whatever the usage budget, day or night, like a cron job. The daily cap and pauses still apply" label="Always run" wide>
                <label className="admin-check run-always" title={can("always_run") ? "Runs whatever the usage budget, day or night" : "Changing it needs the Always-run tasks permission"}>
                  <input type="checkbox" checked={!!task.always_run} disabled={!can("always_run")} onChange={(e) => act(api.patchTask(task.id, { always_run: e.target.checked }))} />
                  <span className="muted small">whatever the usage budget, day or night</span>
                </label>
              </Row>
              {(can("allowed_tools") || (task.allowed_tools ?? []).length > 0) && (
                <Row tip="The agent gets exactly these tools for this task (Claude Code permission rules), nothing else. Empty: its usual setup" label="Allowed tools" wide>
                  {can("allowed_tools") ? (
                    <ToolsInput value={task.allowed_tools ?? []} onCommit={(v) => act(api.patchTask(task.id, { allowed_tools: v }))} />
                  ) : (
                    <span className="tools-list" title="Changing this task needs the Task tool allowlists permission">
                      {(task.allowed_tools ?? []).map((r) => (
                        <code key={r}>{r}</code>
                      ))}
                    </span>
                  )}
                </Row>
              )}
            </PropGroup>

            <PropGroup title="People">
              <Row label="Assignees" wide>
                {editing === "assignees" ? (
                  <InlineAssignees task={task} candidates={candidates} onSave={(v) => act(api.patchTask(task.id, { assignees: v }))} onCancel={() => setEditing("")} />
                ) : (
                  <div className="chips editable">
                    {task.assignees.map((id) => (
                      <UserBadge key={id} user={users.get(id)} />
                    ))}
                    {task.assignees.length === 0 && <span className="muted">nobody</span>}
                    {canWrite && (
                      <button type="button" className="icon-button" aria-label="Edit assignees" onClick={() => setEditing("assignees")}>
                        <Icon name="pencil" />
                      </button>
                    )}
                  </div>
                )}
              </Row>
              <Row label="Created by" wide className="created">
                <UserBadge user={creator} /> <span className="muted small">{fmtTime(task.created_at)}</span>
              </Row>
              <Row label="Updated" wide className="muted small">
                {fmtTime(task.updated_at)}
              </Row>
            </PropGroup>

            <PropGroup title="Schedule">
              <Row wide label={task.not_after ? "Window opens" : "Start"}>
                <DateTimeInput value={task.start_at} defaultTime={DEFAULT_START_TIME} label={task.not_after ? "Window opens" : "Start"} disabled={!canWrite} onChange={(v) => act(api.patchTask(task.id, { start_at: v }))} />
              </Row>
              <Row wide label={<>Due {overdue && <Pill color="#E5484D">overdue</Pill>}</>}>
                <DateTimeInput value={task.due_at} defaultTime={DEFAULT_DUE_TIME} label="Due" disabled={!canWrite} onChange={(v) => act(api.patchTask(task.id, { due_at: v }))} />
              </Row>
              {(canWrite || !!task.not_after) && (
                <Row tip="After this moment the task can’t be started any more; untouched, it is marked missed" wide label="Window closes">
                  <DateTimeInput value={task.not_after ?? null} defaultTime={DEFAULT_DUE_TIME} label="Window closes" disabled={!canWrite} onChange={(v) => act(api.patchTask(task.id, { not_after: v }))} />
                </Row>
              )}
              {!!task.missed_at && (
                <Row tip="Its window closed before anyone started it" label="Missed" wide className="missed-row">
                  <span className="pill pill-danger">missed {fmtTime(task.missed_at)}</span>
                  {canWrite && (
                    <>
                      <button type="button" className="button secondary small" onClick={() => act(api.patchTask(task.id, { not_after: Date.now() + 3_600_000 }))}>
                        ↻ Reopen window 1 h
                      </button>
                      <button type="button" className="button secondary small" onClick={() => act(api.patchTask(task.id, { not_after: Date.now() + 86_400_000 }))}>
                        ↻ 1 day
                      </button>
                    </>
                  )}
                </Row>
              )}
              {!!task.recurrence_id && (
                <Row tip="Made by a recurring task: open it to see its template and schedule" label="Repeats" wide className="schedule-ref">
                  {schedule === "gone" ? (
                    <span className="muted">from a deleted schedule</span>
                  ) : schedule ? (
                    <>
                      <button type="button" className="link" onClick={() => onOpenSchedule?.(schedule.id)} disabled={!onOpenSchedule}>
                        ⟳ from schedule “{schedule.title}”
                      </button>
                      <span className="muted small">{describeRule(schedule)}</span>
                      {schedule.paused && <span className="pill pill-danger">paused</span>}
                    </>
                  ) : (
                    <span className="muted">…</span>
                  )}
                  {!!task.occurrence_at && <span className="pill pill-muted">occurrence {fmtTime(task.occurrence_at)}</span>}
                </Row>
              )}
            </PropGroup>

            <PropGroup title="Look">
              <Row label="Labels" wide>
                {editing === "labels" ? (
                  <InlineLabels task={task} known={labels} onSave={(v) => act(api.patchTask(task.id, { labels: v }))} onCancel={() => setEditing("")} />
                ) : (
                  <div className="chips editable">
                    {task.labels.map((l) => (
                      <LabelChip key={l.id} label={labelFor(l)} />
                    ))}
                    {task.labels.length === 0 && <span className="muted">none</span>}
                    {canWrite && (
                      <button type="button" className="icon-button" aria-label="Edit labels" onClick={() => setEditing("labels")}>
                        <Icon name="pencil" />
                      </button>
                    )}
                  </div>
                )}
              </Row>
              <Row label="Colour" wide>
                {canWrite ? (
                  <ColorPicker value={task.color} onChange={(c) => act(api.patchTask(task.id, { color: c }))} />
                ) : task.color ? (
                  <span className="swatch on" style={{ background: task.color }} />
                ) : (
                  <span className="muted">none</span>
                )}
              </Row>
            </PropGroup>
          </div>
          {(cmd || runs.length > 0) && (
            <section className="agent" aria-label="Agent session">
              <h3>Agent session</h3>
              {cmd && (
                <div className="cmd">
                  <code>{cmd}</code>
                  <button type="button" className="button secondary small" onClick={() => void navigator.clipboard?.writeText(cmd)}>
                    Copy
                  </button>
                </div>
              )}
              <div className="runs">
                {runs.slice(0, 5).map((r) => (
                  <div key={r.id} className="run">
                    <Pill color={runColor[r.status] ?? "#8B8D98"}>{r.status.replace("_", " ")}</Pill>
                    <span className="pill pill-muted">{r.mode || "run"} #{r.attempt}</span>
                    <span className="small">{r.worker_username}</span>
                    <span className="muted small">{fmtTime(r.started_at)}</span>
                    {r.duration_ms !== null && <span className="pill pill-muted">{Math.round(r.duration_ms / 1000)} s</span>}
                    {r.cost_usd !== null && <span className="pill pill-muted">${r.cost_usd.toFixed(2)}</span>}
                    {r.num_turns !== null && <span className="pill pill-muted">{r.num_turns} turns</span>}
                    {r.status === "running" && r.progress_note && (
                      <span className="small">
                        {r.progress_pct !== null && `${r.progress_pct}% · `}
                        {r.progress_note}
                      </span>
                    )}
                  </div>
                ))}
              </div>
              {lastRun?.transcript_path && (
                <p className="small">
                  <span className="muted">Transcript </span>
                  <code className="path">{lastRun.transcript_path}</code>
                </p>
              )}
              {runs.length > 0 && (
                <p className="small muted">
                  ${task.cost_usd_total.toFixed(2)} over {runs.length} run{runs.length === 1 ? "" : "s"}
                </p>
              )}
              {canWrite && task.session_id && <Resume task={task} onDone={(t) => act(Promise.resolve(t))} />}
            </section>
          )}
          {canWrite && (
            <div className="tp-actions">
              <button type="button" className="button secondary" onClick={() => act(api.cloneTask(task.id), (t) => onMoved(t))}>
                <Icon name="copy" /> Clone
              </button>
              {can("archive_tasks") &&
                !task.deleted &&
                (task.archived_at ? (
                  <button type="button" className="button secondary" onClick={() => act(api.unarchiveTask(task.id))}>
                    <Icon name="undo" /> Unarchive
                  </button>
                ) : (
                  <button type="button" className="button secondary" onClick={() => act(api.archiveTask(task.id))}>
                    <Icon name="archive" /> Archive
                  </button>
                ))}
              {task.deleted ? (
                <button type="button" className="button secondary" onClick={() => act(api.restoreTask(task.id))}>
                  <Icon name="undo" /> Restore
                </button>
              ) : (
                <button type="button" className="button danger-button" onClick={() => act(api.deleteTask(task.id))}>
                  <Icon name="trash" /> Delete
                </button>
              )}
            </div>
          )}
        </div>
      </div>
    </aside>
  );
}

function TextForm({ task, users, onCancel, onSave }: { task: Task; users: User[]; onCancel: () => void; onSave: (b: Record<string, unknown>) => void }) {
  const [title, setTitle] = useState(task.title);
  const [description, setDescription] = useState(task.description_md);
  const dirty = title !== task.title || description !== task.description_md;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    onSave({ title, description_md: description });
  };
  return (
    <form className="edit tp-edit" onSubmit={submit} aria-label="Edit task" data-dirty={dirty ? "" : undefined}>
      <input className="tp-title-input" value={title} onChange={(e) => setTitle(e.target.value)} required maxLength={250} aria-label="Title" autoFocus />
      <MarkdownEditor
        value={description}
        onChange={setDescription}
        users={users}
        minRows={6}
        aria-label="Description (Markdown)"
        placeholder="Description (Markdown) · @ to mention"
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
            e.preventDefault();
            e.currentTarget.form?.requestSubmit();
          }
        }}
      />
      <div className="row end">
        <span className="muted small grow">{"Ctrl+Enter saves · Esc with no changes cancels"}</span>
        <button type="button" className="button secondary" onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" className="button">
          Save
        </button>
      </div>
    </form>
  );
}

function InlineLabels({ task, known, onSave, onCancel }: { task: Task; known: Tag[]; onSave: (v: string[]) => void; onCancel: () => void }) {
  const [v, setV] = useState(task.labels.map((l) => l.name));
  const dirty = v.join("\n") !== task.labels.map((l) => l.name).join("\n");
  return (
    <div className="inline-edit" data-dirty={dirty ? "" : undefined}>
      <LabelInput value={v} onChange={setV} known={known} />
      <div className="row end">
        <button type="button" className="button secondary small" onClick={onCancel}>
          Cancel
        </button>
        <button type="button" className="button small" onClick={() => onSave(v)}>
          Save
        </button>
      </div>
    </div>
  );
}

function InlineAssignees({ task, candidates, onSave, onCancel }: {
  task: Task; candidates: ReturnType<typeof assignable>; onSave: (v: number[]) => void; onCancel: () => void;
}) {
  const [v, setV] = useState(task.assignees);
  const dirty = v.join() !== task.assignees.join();
  return (
    <div className="inline-edit" data-dirty={dirty ? "" : undefined}>
      <AssigneePicker value={v} onChange={setV} candidates={candidates} />
      <div className="row end">
        <button type="button" className="button secondary small" onClick={onCancel}>
          Cancel
        </button>
        <button type="button" className="button small" onClick={() => onSave(v)}>
          Save
        </button>
      </div>
    </div>
  );
}

function Resume({ task, onDone }: { task: Task; onDone: (t: Task) => void }) {
  const [text, setText] = useState("");
  const [err, setErr] = useState("");
  const queued = task.resume_requested_at !== null && task.status_category === "todo";
  const submit = (e: FormEvent) => {
    e.preventDefault();
    api
      .resumeTask(task.id, text)
      .then((t) => {
        setText("");
        onDone(t);
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't queue the resume."));
  };
  return (
    <form className="resume" onSubmit={submit} aria-label="Resume">
      {queued && <Pill color={CATEGORY_COLOR.todo!}>resume queued for the runner's next pass</Pill>}
      <textarea placeholder="Answer or instructions for the next run (Markdown)" data-dirty={text.trim() ? "" : undefined} value={text} onChange={(e) => setText(e.target.value)} rows={3} />
      {err && <p className="danger">{err}</p>}
      <button type="submit" className="button">
        ▶ Resume
      </button>
      <p className="muted small">gigg records the request and moves the task back to To-Do; the runner continues the same session.</p>
    </form>
  );
}

function AddComment({ taskId, users, onDone }: { taskId: number; users: User[]; onDone: () => void }) {
  const [text, setText] = useState("");
  const [err, setErr] = useState("");
  const submit = (e: FormEvent) => {
    e.preventDefault();
    api
      .addComment(taskId, text)
      .then(() => {
        setText("");
        onDone();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't post the comment."));
  };
  return (
    <form className="add-comment" onSubmit={submit} aria-label="Add comment">
      <MarkdownEditor
        placeholder="Comment (Markdown) · @ to mention"
        value={text}
        onChange={setText}
        users={users}
        minRows={3}
        required
        aria-label="Comment"
        dirty={!!text.trim()}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
            e.preventDefault();
            e.currentTarget.form?.requestSubmit();
          }
        }}
      />
      {err && <p className="danger">{err}</p>}
      <button type="submit" className="button">
        Comment
      </button>
    </form>
  );
}

function PropGroup({ title, children, help }: { title: string; children: ReactNode; help?: ReactNode }) {
  return (
    <section className="tp-group" aria-label={title}>
      <h4 className="tp-gh">
        {title}
        {help}
      </h4>
      <div className="tp-grid">{children}</div>
    </section>
  );
}

// Row: one label + value in a property group. wide rows take the whole width (chips, swatches).
function Row({ label, wide, className, children, tip }: { label: ReactNode; wide?: boolean; className?: string; children: ReactNode; tip?: string }) {
  return (
    <div className={"tp-row" + (wide ? " wide" : "")}>
      <span className="tp-label" data-tip={tip}>
        {label}
      </span>
      <div className={"tp-value" + (className ? " " + className : "")}>{children}</div>
    </div>
  );
}
