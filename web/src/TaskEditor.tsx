// TaskEditor: the one task editor. The open task's panel, New task, New/Edit recurring task and
// New/Edit template all render this component, so they have the same boxes in the same order with
// the same controls: the task (title and description), Checklist and Subtasks on the left; Where,
// When, Details, Agent run, Dependencies and Fields on the right.
// What differs is only where a change goes (`set`): the panel saves it at once, a form keeps it until
// Create/Save. A mode hides the boxes and rows that don't apply (a template has no When) and the
// caller adds its own (comments, the agent session and history in the panel; Repeat in a form).
import { useEffect, useRef, useState, type ReactNode } from "react";
import { priorityNames, type Status, type Tag, type User } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { AssigneePicker, ColorPicker, DateTimeInput, LabelInput, withCurrent } from "./fields";
import { DEFAULT_DUE_TIME, DEFAULT_START_TIME } from "./dates";
import { Markdown } from "./markdown";
import { MarkdownEditor } from "./MarkdownEditor";
import { Clamp } from "./Clamp";
import { AgentRunGroup, PropGroup, Row } from "./TaskSections";
import type { RunSettings } from "./runsettings";
import { LabelChip, Pill, PriorityBadge, StatusBadge, UserBadge } from "./ui";
import { Icon } from "./icons";

export type EditorMode = "task" | "new" | "recurring" | "template";

// EditorValue: what the editor shows. The keys are the task API's, so the panel can send a change as is.
export interface EditorValue {
  title: string;
  description_md: string;
  priority: number;
  labels: string[];
  assignees: number[];
  color: string | null;
  start_at: number | null;
  due_at: number | null;
  not_after: number | null;
  project: number;
  status: number;
  run: RunSettings;
}
export type EditorChange = Partial<Omit<EditorValue, "run">>;

export function TaskEditor(p: {
  mode: EditorMode;
  v: EditorValue;
  set: (c: EditorChange) => void;
  setRun: (c: Partial<RunSettings>) => void;
  onToolsError?: (bad: string | null | undefined) => void;
  // saving the title and description: the panel saves both at once (returns whether it worked)
  saveText?: (title: string, description: string) => Promise<boolean>;
  canWrite: boolean;
  ws: WorkspaceTree;
  places?: WorkspaceTree[]; // a new task may move to these workspaces
  onWorkspace?: (id: number) => void;
  anyProject?: { on: boolean; set: (on: boolean) => void }; // a template may be for any project
  projectLocked?: boolean;
  statuses?: Status[]; // the status row (none for a template or a recurring task)
  statusBusy?: boolean;
  currentStatus?: { name: string; category: Status["category"] }; // shown read-only when it isn't in statuses (cancelled)
  labelTags?: Tag[]; // the task's own labels (their colours, when the workspace list is stale)
  dates?: boolean; // Start, Due, Window closes (not for a recurring task: the schedule sets them)
  knownLabels: Tag[];
  candidates: User[];
  alwaysLocked: boolean;
  canTools: boolean;
  overdue?: boolean;
  head?: ReactNode; // in the task box, above the text (template name, from template)
  people?: ReactNode; // in the task box, below the text (who made it, who it's for)
  aliases?: string[];
  leftSecs: ReactNode; // Checklist, Subtasks
  mainEnd?: ReactNode; // the panel's comments
  whenEnd?: ReactNode; // more When rows (missed, repeats; Repeat in a form)
  detailsEnd?: ReactNode; // more Details rows (created by, updated)
  sideMid?: ReactNode; // the panel's agent session
  rightSecs: ReactNode; // Dependencies, Fields (and the panel's History)
  sideEnd?: ReactNode; // the panel's actions
}) {
  const { users } = useApp();
  const { mode, v, set, canWrite, ws } = p;
  const tpl = mode === "template";
  const status = p.statuses?.find((s) => s.id === v.status);
  return (
    <div className="tp-body">
      <div className="tp-main">
        <section className="tp-box tp-task" aria-label={tpl ? "Template" : "Task"}>
          {p.head}
          <TaskText
            key={mode === "task" ? "live" : "draft"}
            title={v.title}
            description={v.description_md}
            live={mode === "task"}
            canWrite={canWrite}
            users={p.candidates}
            titleLabel={tpl ? "Task title" : "Title"}
            titlePlaceholder={tpl ? "Task title: what needs doing?" : "Title: what needs doing?"}
            aliases={p.aliases}
            onDraft={(title, description_md) => set({ title, description_md })}
            onSave={p.saveText}
          />
          {p.people}
        </section>
        <div className="tsecs">{p.leftSecs}</div>
        {p.mainEnd}
      </div>
      <div className="tp-side">
        <div className="tp-props">
          <PropGroup icon="layers" title="Where">
            <Row label="Workspace">
              {p.places && p.places.length > 1 && p.onWorkspace ? (
                <select value={ws.id} onChange={(e) => p.onWorkspace!(Number(e.target.value))} aria-label="Workspace">
                  {p.places.map((w) => (
                    <option key={w.id} value={w.id}>
                      {w.name}
                    </option>
                  ))}
                </select>
              ) : (
                <span className="pill pill-ws">{ws.name}</span>
              )}
            </Row>
            <Row label="Project">
              {canWrite ? (
                <select
                  value={p.anyProject?.on ? "any" : v.project}
                  onChange={(e) => {
                    if (e.target.value === "any") p.anyProject?.set(true);
                    else {
                      p.anyProject?.set(false);
                      set({ project: Number(e.target.value) });
                    }
                  }}
                  disabled={p.projectLocked}
                  aria-label="Project"
                >
                  {p.anyProject && <option value="any">Any project</option>}
                  {ws.projects.map((x) => (
                    <option key={x.id} value={x.id}>
                      {x.key} · {x.name}
                    </option>
                  ))}
                </select>
              ) : (
                ws.projects.find((x) => x.id === v.project)?.name
              )}
            </Row>
            {p.statuses && (
              <Row label="Status">
                {canWrite ? (
                  <select value={v.status} onChange={(e) => set({ status: Number(e.target.value) })} disabled={p.statusBusy} aria-busy={p.statusBusy || undefined} aria-label="Status">
                    {p.statuses.map((x) => (
                      <option key={x.id} value={x.id}>
                        {x.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  (status ?? p.currentStatus) && <StatusBadge name={(status ?? p.currentStatus)!.name} category={(status ?? p.currentStatus)!.category} />
                )}
              </Row>
            )}
          </PropGroup>

          {!tpl && (
            <PropGroup icon="calendar" title="When">
              {p.dates && (
                <>
                  <Row wide label={v.not_after ? "Window opens" : "Start"}>
                    <DateTimeInput value={v.start_at} defaultTime={DEFAULT_START_TIME} label={v.not_after ? "Window opens" : "Start"} disabled={!canWrite} live={mode !== "task"} onChange={(x) => set({ start_at: x })} />
                  </Row>
                  <Row wide label={<>Due {p.overdue && <Pill color="#E5484D">overdue</Pill>}</>}>
                    <DateTimeInput value={v.due_at} defaultTime={DEFAULT_DUE_TIME} label="Due" disabled={!canWrite} live={mode !== "task"} onChange={(x) => set({ due_at: x })} />
                  </Row>
                  {(canWrite || !!v.not_after) && (
                    <Row tip="After this moment the task can’t be started any more; untouched, it is marked missed" wide label="Window closes">
                      <DateTimeInput value={v.not_after} defaultTime={DEFAULT_DUE_TIME} label="Window closes" disabled={!canWrite} live={mode !== "task"} onChange={(x) => set({ not_after: x })} />
                    </Row>
                  )}
                </>
              )}
              {p.whenEnd}
            </PropGroup>
          )}

          <PropGroup icon="users" title="Details">
            <Row label="Priority">
              {canWrite ? (
                <select value={v.priority} onChange={(e) => set({ priority: Number(e.target.value) })} aria-label="Priority">
                  {priorityNames.map((n, i) => (
                    <option key={n} value={i}>
                      {n}
                    </option>
                  ))}
                </select>
              ) : (
                <PriorityBadge priority={v.priority} showNone />
              )}
            </Row>
            <Row label="Labels" wide>
              {canWrite ? (
                <LabelInput value={v.labels} onChange={(labels) => set({ labels })} known={p.knownLabels} autosave={mode === "task"} />
              ) : (
                <div className="chips">
                  {v.labels.map((n) => (
                    <LabelChip key={n} label={p.labelTags?.find((l) => l.name === n) ?? p.knownLabels.find((l) => l.name === n) ?? { id: 0, name: n, color: null }} />
                  ))}
                  {v.labels.length === 0 && <span className="muted">none</span>}
                </div>
              )}
            </Row>
            <Row label="Assignees" wide>
              {canWrite ? (
                <AssigneePicker value={v.assignees} onChange={(assignees) => set({ assignees })} candidates={withCurrent(p.candidates, v.assignees, users)} />
              ) : (
                <div className="chips">
                  {v.assignees.map((id) => (
                    <UserBadge key={id} user={users.get(id)} />
                  ))}
                  {v.assignees.length === 0 && <span className="muted">nobody</span>}
                </div>
              )}
            </Row>
            <Row label="Colour" wide>
              {canWrite ? (
                <ColorPicker value={v.color} onChange={(color) => set({ color })} />
              ) : v.color ? (
                <span className="swatch on" style={{ background: v.color }} />
              ) : (
                <span className="muted">none</span>
              )}
            </Row>
            {p.detailsEnd}
          </PropGroup>

          <AgentRunGroup value={v.run} canWrite={canWrite} alwaysLocked={p.alwaysLocked} canTools={p.canTools} onChange={p.setRun} onToolsError={p.onToolsError} />
        </div>
        {p.sideMid}
        <div className="tsecs">{p.rightSecs}</div>
        {p.sideEnd}
      </div>
    </div>
  );
}

// TaskText: the title and the description, shown as the task reads (the description folded when
// long, "show more" opens it) with a pencil to edit. The panel saves the edit with Save; a form
// keeps it as typed (an empty one, a new task, starts in the editor).
function TaskText({ title, description, live, canWrite, users, titleLabel, titlePlaceholder, aliases, onDraft, onSave }: {
  title: string;
  description: string;
  live: boolean;
  canWrite: boolean;
  users: User[];
  titleLabel: string;
  titlePlaceholder: string;
  aliases?: string[];
  onDraft: (title: string, description: string) => void;
  onSave?: (title: string, description: string) => Promise<boolean>;
}) {
  const [editing, setEditing] = useState(!live && !title.trim());
  // focus follows the switch the user made: into the title when they open the editor, back to the
  // pencil when they close it (not on the first render: the form puts focus where it starts)
  const touched = useRef(false);
  const pencil = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (touched.current && !editing) pencil.current?.focus();
  }, [editing]);
  const open = () => {
    touched.current = true;
    setEditing(true);
  };
  const shut = () => {
    touched.current = true;
    setEditing(false);
  };
  if (editing && canWrite) {
    return live ? (
      <TextForm title={title} description={description} users={users} onCancel={shut} onSave={(t, d) => void onSave?.(t, d).then((ok) => ok && shut())} />
    ) : (
      <div className="edit tp-edit">
        <input className="tp-title-input" value={title} onChange={(e) => onDraft(e.target.value, description)} required maxLength={250} placeholder={titlePlaceholder} aria-label={titleLabel} data-autofocus="" autoFocus={touched.current} />
        <MarkdownEditor value={description} onChange={(d) => onDraft(title, d)} users={users} minRows={6} aria-label="Description" placeholder="Description (Markdown) · @ to mention" />
        <div className="row end">
          <button type="button" className="button secondary small" onClick={shut} disabled={!title.trim()}>
            Done
          </button>
        </div>
      </div>
    );
  }
  return (
    <>
      <div className="tp-titlebar">
        <h2 className="panel-title">{title}</h2>
        {canWrite && (
          <button ref={pencil} type="button" className="icon-button" aria-label="Edit title and description" title="Edit title and description" onClick={open} data-autofocus={live ? undefined : ""}>
            <Icon name="pencil" />
          </button>
        )}
      </div>
      {aliases && aliases.length > 0 && <p className="muted small tp-aliases">formerly {aliases.join(", ")}</p>}
      <section className="tp-desc" aria-label="Description">
        {description ? (
          <Clamp label="description" maxRem={18}>
            <Markdown source={description} className="description" />
          </Clamp>
        ) : canWrite ? (
          <button type="button" className="tp-desc-empty" onClick={open}>
            Add a description…
          </button>
        ) : (
          <p className="muted tp-desc-empty">No description.</p>
        )}
      </section>
    </>
  );
}

// TextForm: the panel's title and description editor (its own form: Save sends both).
function TextForm({ title: t0, description: d0, users, onCancel, onSave }: { title: string; description: string; users: User[]; onCancel: () => void; onSave: (t: string, d: string) => void }) {
  const [title, setTitle] = useState(t0);
  const [description, setDescription] = useState(d0);
  const dirty = title !== t0 || description !== d0;
  return (
    <form
      className="edit tp-edit"
      onSubmit={(e) => {
        e.preventDefault();
        onSave(title, description);
      }}
      aria-label="Edit task"
      data-dirty={dirty ? "" : undefined}
      onKeyDown={(e) => {
        // Esc with no changes closes the editor (not the panel); with changes the panel flashes them
        if (e.key === "Escape" && !dirty) {
          e.preventDefault();
          onCancel();
        }
      }}
    >
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
        <button type="button" className="button secondary" onClick={onCancel} data-cancel="">
          Cancel
        </button>
        <button type="submit" className="button">
          Save
        </button>
      </div>
    </form>
  );
}
