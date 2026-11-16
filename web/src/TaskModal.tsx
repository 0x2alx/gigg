// The task form: New task, New/Edit recurring task, New/Edit template. It is laid out exactly like
// the task panel (the same boxes in the same order, the same rows and controls): the task, Checklist
// and Subtasks on the left; Where, When, Details, Agent run, Dependencies and Fields on the right.
// The panel saves each change at once; the form keeps them until Create/Save.
// With "Repeat" set, submitting creates a recurring task (the server then creates one task per
// occurrence) instead of a single task. Its task content is a template: its own one (made from this
// form), or a picked template left unchanged, which it then uses as is.
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { runBody, type RunSettings } from "./runsettings";
import { api, ApiError, hasCap, priorityNames, type Category, type CustomField, type Recurrence, type RecurrenceBody, type Schedule, type Status, type Tag, type Template, type TemplateBody, type TemplateSubtask } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { AssigneePicker, ColorPicker, DateTimeInput, LabelInput, assignable } from "./fields";
import { DEFAULT_DUE_TIME, DEFAULT_START_TIME } from "./dates";
import { cleanCustom, normSubs, sameAsTemplate } from "./items";
import { MarkdownEditor } from "./MarkdownEditor";
import { RepeatFields, RfRow } from "./RepeatFields";
import { repeatOfSchedule, sameTiming } from "./scheduleTiming";
import { AgentRunGroup, ChecklistDraft, DependenciesDraft, FieldsDraft, PropGroup, Row, SubtasksDraft, type DraftDep } from "./TaskSections";
import { defaultRepeat, fmtInstant, localTZ, repeatFromRule, repeatProblem, repeatRule, type RepeatState } from "./recurrence";
import { Modal } from "./ui";
import { HelpButton } from "./help";
import { Icon } from "./icons";

export function TaskModal({ ws: ws0, projectId, statuses, labels, choices, schedule, template, repeat: initialRepeat, initialStatus, initialCategory, onClose, onCreated }: {
  ws: WorkspaceTree; // where the form starts (with projectId)
  projectId: number;
  statuses: Status[]; // the start project's open statuses ([]: fetched)
  initialStatus?: number; // a new task starts in this status (the column its "+" was clicked in)
  initialCategory?: Category; // ...or, in another project, the first status of this stage
  labels: Tag[]; // the start workspace's labels ([]: fetched)
  choices?: WorkspaceTree[]; // the workspaces (and their projects) the form may move it to; default: just ws
  schedule?: Recurrence; // edit this schedule
  template?: Template | "new"; // edit (or create) a template instead of a task
  repeat?: RepeatState["freq"]; // preset the Repeat control (new schedule)
  onClose: () => void;
  onCreated: (message?: string) => void;
}) {
  const { users, members, me } = useApp();
  const tpl = template && template !== "new" ? template : null;
  const tplMode = !!template;
  const editing = !!schedule;
  const [project, setProject] = useState(schedule?.project_id ?? projectId);
  // the workspace follows the project picked
  // (the start workspace always among them, so its select has a matching option)
  const places = useMemo(() => (choices && choices.length > 0 ? (choices.some((w) => w.id === ws0.id) ? choices : [ws0, ...choices]) : [ws0]), [choices, ws0]);
  const ws = places.find((w) => w.projects.some((p) => p.id === project)) ?? ws0;
  const [known, setKnown] = useState<Tag[]>(labels);
  const [tplName, setTplName] = useState(tpl?.name ?? "");
  const [tplAnyProject, setTplAnyProject] = useState(tpl ? tpl.project_id === null : false);
  const [checklist, setChecklist] = useState<string[]>(schedule?.checklist ?? tpl?.checklist ?? []);
  const [subtasks, setSubtasks] = useState<TemplateSubtask[]>(normSubs(schedule?.subtasks ?? tpl?.subtasks ?? []));
  const [deps, setDeps] = useState<DraftDep[]>([]);
  const [custom, setCustom] = useState<Record<string, unknown>>(schedule?.custom ?? tpl?.custom ?? {});
  const [fields, setFields] = useState<CustomField[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [picked, setPicked] = useState<Template | null>(null);
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [pickedSchedule, setPickedSchedule] = useState<Schedule | null>(null);
  const [saveAs, setSaveAs] = useState<string | null>(null); // the name being typed for "Save as template"
  const [note, setNote] = useState("");
  const [projectStatuses, setProjectStatuses] = useState<Status[]>(statuses);
  const [title, setTitle] = useState(schedule?.title ?? tpl?.title ?? "");
  const [description, setDescription] = useState(schedule?.description_md ?? tpl?.description_md ?? "");
  const [priority, setPriority] = useState(schedule?.priority ?? tpl?.priority ?? 0);
  const [status, setStatus] = useState<number>(
    (initialStatus !== undefined && statuses.some((s) => s.id === initialStatus) ? initialStatus : undefined) ?? statuses.find((s) => s.category === "todo")?.id ?? statuses[0]?.id ?? 0,
  );
  const [taskLabels, setTaskLabels] = useState<string[]>(schedule?.labels ?? tpl?.labels ?? []);
  const [assignees, setAssignees] = useState<number[]>(schedule?.assignees ?? tpl?.assignees ?? []);
  const [due, setDue] = useState<number | null>(null);
  const [start, setStart] = useState<number | null>(null);
  const [notAfter, setNotAfter] = useState<number | null>(null);
  const [color, setColor] = useState<string | null>(schedule?.color ?? tpl?.color ?? null);
  const src = schedule ?? tpl;
  const [run, setRun] = useState<RunSettings>({ model: src?.model ?? "", effort: src?.effort ?? "", always_run: !!src?.always_run, allowed_tools: src?.allowed_tools ?? [] });
  const [repeat, setRepeat] = useState<RepeatState>(() => (schedule ? repeatFromRule(schedule) : { ...defaultRepeat(), freq: initialRepeat ?? "" }));
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  // the project's statuses: the ones given for the start project, else fetched (not for a template or a
  // recurring task being edited: no status there). The status picked stays when the project has it,
  // else the same stage's first (else To-Do, else the first). Until they're in, Create waits: the old
  // project's status would be refused by the new one.
  const givenStatuses = useRef(statuses);
  givenStatuses.current = statuses;
  const statusKey = statuses.map((x) => x.id).join(",");
  const [statusesOf, setStatusesOf] = useState<number | null>(statuses.length > 0 ? projectId : null); // the project projectStatuses belong to
  const [statusErr, setStatusErr] = useState("");
  const shownStatuses = useRef(projectStatuses);
  shownStatuses.current = projectStatuses;
  const noStatus = tplMode || editing;
  useEffect(() => {
    if (noStatus) return;
    const settle = (open: Status[]) => {
      const was = shownStatuses.current;
      setProjectStatuses(open);
      setStatusesOf(project);
      setStatusErr("");
      setStatus((cur) => {
        if (open.some((x) => x.id === cur)) return cur;
        // back in the start project: the exact status it started in (the column's)
        if (project === projectId && initialStatus !== undefined && open.some((x) => x.id === initialStatus)) return initialStatus;
        const stage = was.find((x) => x.id === cur)?.category ?? initialCategory ?? "todo";
        return (open.find((x) => x.category === stage) ?? open.find((x) => x.category === "todo") ?? open[0])?.id ?? 0;
      });
    };
    if (project === projectId && givenStatuses.current.length > 0) {
      settle(givenStatuses.current);
      return;
    }
    setStatusesOf(null);
    let live = true;
    api.statuses(project).then(
      (r) => live && settle(r.items.filter((x) => x.category !== "cancelled")),
      (e: unknown) => live && setStatusErr(`Couldn't load this project's statuses${e instanceof ApiError ? `: ${e.message}` : ""}.`),
    );
    return () => {
      live = false;
    };
  }, [project, projectId, statusKey, initialCategory, initialStatus, noStatus]);
  const statusReady = statusesOf === project;
  // a template for one project doesn't follow the task to another project
  useEffect(() => {
    setPicked((t) => (t && t.project_id !== null && t.project_id !== project && !schedule ? null : t));
  }, [project, schedule]);
  // the workspace's labels: given for the start workspace, else fetched
  const givenLabels = useRef(labels);
  givenLabels.current = labels;
  const labelKey = labels.map((l) => l.id).join(",");
  useEffect(() => {
    if (ws.id === ws0.id && givenLabels.current.length > 0) {
      setKnown(givenLabels.current);
      return;
    }
    let live = true;
    api.labels(ws.slug).then(
      (r) => live && setKnown(r.items),
      () => live && setKnown([]),
    );
    return () => {
      live = false;
    };
  }, [ws.id, ws.slug, ws0.id, labelKey]);
  // moving to another workspace: its fields, templates and schedules differ, and only its members can be
  // assigned (custom values, the picked template and schedule are cleared; assignees kept where they can)
  const lastWs = useRef(ws.id);
  useEffect(() => {
    if (lastWs.current === ws.id) return;
    lastWs.current = ws.id;
    setCustom({});
    setDeps([]);
    setPicked(null);
    setPickedSchedule(null);
    const ok = new Set(assignable(users, members.get(ws.id) ?? []).map((u) => u.id));
    setAssignees((a) => a.filter((id) => ok.has(id)));
    // run settings and Repeat only as far as this workspace lets you
    const always = hasCap(me, "always_run", ws.id);
    const tools = hasCap(me, "allowed_tools", ws.id);
    setRun((r) => ({ ...r, always_run: r.always_run && always, allowed_tools: tools ? r.allowed_tools : [] }));
    if (!hasCap(me, "schedules", ws.id)) setRepeat((r) => ({ ...r, freq: "" }));
  }, [ws.id, users, members, me]);

  useEffect(() => {
    let live = true;
    api.fields(ws.id).then((r) => live && setFields(r.items), () => undefined);
    return () => {
      live = false;
    };
  }, [ws.id]);
  useEffect(() => {
    if (tplMode) return;
    let live = true;
    api.templates(ws.id, project).then(
      (r) => {
        if (!live) return;
        setTemplates(r.items);
        // editing: the recurring task's own template is the one picked
        if (schedule) setPicked(r.items.find((t) => t.id === schedule.template_id) ?? null);
      },
      () => live && setTemplates([]),
    );
    return () => {
      live = false;
    };
  }, [ws.id, project, tplMode, schedule]);
  const wantSchedules = !tplMode && (editing || !!repeat.freq);
  useEffect(() => {
    if (!wantSchedules) return;
    let live = true;
    api.schedules(ws.id).then(
      (r) => {
        if (!live) return;
        setSchedules(r.items);
        if (schedule) setPickedSchedule(r.items.find((x) => x.id === schedule.schedule_id) ?? null);
      },
      () => live && setSchedules([]),
    );
    return () => {
      live = false;
    };
  }, [ws.id, wantSchedules, schedule]);

  const candidates = assignable(users, members.get(ws.id) ?? []);
  const repeating = !tplMode && !!repeat.freq;
  const problem = run.tools_error ? `Allowed tools: “${run.tools_error}” isn't one permission rule.` : tplMode ? null : repeatProblem(repeat);
  const canTemplates = hasCap(me, "templates", ws.id);
  // a picked template (editing: the current one), unchanged: the recurring task uses it as is
  const usesPicked = repeating && !!picked && sameAsTemplate(picked, { title, description, priority, taskLabels, assignees, color, run, checklist, subtasks, custom });
  // likewise a picked schedule whose timing wasn't changed
  const usesSchedule = repeating && !!pickedSchedule && sameTiming(repeat, pickedSchedule);
  const pickSchedule = (id: number) => {
    const x = schedules.find((s) => s.id === id) ?? null;
    setPickedSchedule(x);
    if (x) setRepeat((r) => repeatOfSchedule(x, r));
  };
  const canCreate = hasCap(me, "create_tasks", ws.id);
  const canRepeat = editing || hasCap(me, "schedules", ws.id);

  // pick: fills the form from a template (the user can still change everything).
  const pick = (t: Template | null) => {
    setPicked(t);
    if (!t) return;
    setTitle(t.title);
    setDescription(t.description_md);
    setPriority(t.priority);
    setTaskLabels(t.labels ?? []);
    setAssignees(t.assignees ?? []);
    setColor(t.color);
    setRun({ model: t.model ?? "", effort: t.effort ?? "", always_run: !!t.always_run, allowed_tools: t.allowed_tools ?? [] });
    setChecklist(t.checklist ?? []);
    setSubtasks(normSubs(t.subtasks ?? []));
    setCustom(t.custom ?? {});
  };

  const templateBody = (name: string): TemplateBody => {
    const b: TemplateBody = { name, title, description_md: description, priority, labels: taskLabels, assignees, ...runBody(run) };
    if (tpl) b.allowed_tools = run.allowed_tools; // an edit sends the list, so clearing it clears it
    if (!(tplMode && tplAnyProject)) b.project_id = project;
    if (color) b.color = color;
    if (checklist.length) b.checklist = checklist;
    if (subtasks.length) b.subtasks = normSubs(subtasks);
    const c = cleanCustom(custom);
    if (c) b.custom = c;
    return b;
  };

  const saveAsTemplate = () => {
    const name = (saveAs ?? "").trim();
    if (run.tools_error) {
      setErr(`Allowed tools: “${run.tools_error}” isn't one permission rule.`);
      return;
    }
    if (!name || !title.trim()) {
      setErr(title.trim() ? "Give the template a name." : "A template needs a title.");
      return;
    }
    setErr("");
    api
      .createTemplate(ws.id, templateBody(name))
      .then((t) => {
        setSaveAs(null);
        setNote(`Saved template “${t.name}”.`);
        setTemplates((xs) => [...xs, t]);
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't save the template."));
  };

  const createFromTemplate = () => {
    if (!picked) return;
    setBusy(true);
    setErr("");
    api
      .useTemplate(picked.id, project, title.trim() && title !== picked.title ? title.trim() : undefined)
      .then((t) => {
        onCreated(`Created ${t.key} from “${picked.name}”`);
        onClose();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't create from the template."))
      .finally(() => setBusy(false));
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    // a checklist item or subtask typed but not added yet would be lost: point at it instead
    const pending = (e.currentTarget as HTMLFormElement).querySelector<HTMLInputElement>(".ck-add[data-dirty], .quick-add input[data-dirty], .dep-add input[data-dirty]");
    if (pending) {
      setErr(`“${pending.value.trim()}” isn't added yet: press Enter to add it, or clear the box.`);
      pending.focus();
      pending.classList.remove("dirty-flash");
      void pending.offsetWidth; // restart the animation
      pending.classList.add("dirty-flash");
      return;
    }
    if (problem) {
      setErr(problem);
      return;
    }
    setBusy(true);
    setErr("");
    const fail = (e: unknown) => {
      if (e instanceof ApiError && e.status === 412) setErr("Someone changed this recurring task meanwhile. Close and reopen it to see their version.");
      else setErr(e instanceof ApiError ? e.message : repeating ? "Couldn't save the recurring task." : "Couldn't create the task.");
    };
    let done: Promise<string | undefined>;
    if (tplMode) {
      const body = templateBody(tplName.trim());
      done = (tpl ? api.putTemplate(tpl.id, body) : api.createTemplate(ws.id, body)).then((t) => `Template “${t.name}” saved`);
    } else if (repeating) {
      const content: Partial<RecurrenceBody> = usesPicked
        ? { template_id: picked!.id }
        : {
            title,
            description_md: description,
            priority,
            labels: taskLabels,
            assignees,
            ...(color ? { color } : {}),
            ...runBody(run),
            ...(editing ? { model: run.model, effort: run.effort, always_run: run.always_run, allowed_tools: run.allowed_tools } : {}),
            // on an edit, unchanged lists are left out (kept as the template has them)
            ...(!schedule || JSON.stringify(checklist) !== JSON.stringify(schedule.checklist ?? []) ? { checklist } : {}),
            ...(!schedule || JSON.stringify(normSubs(subtasks)) !== JSON.stringify(normSubs(schedule.subtasks ?? [])) ? { subtasks: normSubs(subtasks) } : {}),
            ...(!schedule || JSON.stringify(cleanCustom(custom) ?? {}) !== JSON.stringify(cleanCustom(schedule.custom ?? {}) ?? {}) ? { custom: cleanCustom(custom) ?? {} } : {}),
          };
      // the timing fields go along; with a schedule_id the server ignores them
      const body: RecurrenceBody = { ...content, ...repeatRule(repeat), ...(usesSchedule ? { schedule_id: pickedSchedule!.id } : {}) };
      done = (schedule ? api.putRecurrence(schedule.id, body, schedule.version) : api.createRecurrence(project, body)).then((r) =>
        r.next_at ? `Recurring task saved · next ${fmtInstant(r.next_at, localTZ())}` : "Recurring task saved",
      );
    } else {
      done = api
        .createTask(project, {
          title,
          description_md: description,
          priority,
          status,
          labels: taskLabels,
          assignees,
          ...(color ? { color } : {}),
          ...(due !== null ? { due_at: due } : {}),
          ...(start !== null ? { start_at: start } : {}),
          ...(notAfter !== null ? { not_after: notAfter } : {}),
          ...(checklist.length ? { checklist } : {}),
          ...(subtasks.length ? { subtasks: normSubs(subtasks) } : {}),
          ...(deps.length ? { relations: deps.map((d) => ({ task: String(d.id), direction: d.dir })) } : {}),
          ...(cleanCustom(custom) ? { custom: cleanCustom(custom) } : {}),
          ...runBody(run),
        })
        .then(() => undefined);
    }
    done
      .then((msg) => {
        if (!tplMode && !editing) rememberProject(project);
        onCreated(msg);
        onClose();
      })
      .catch(fail)
      .finally(() => setBusy(false));
  };

  const heading = tplMode ? (tpl ? "Edit template" : "New template") : editing ? "Edit recurring task" : repeating ? "New recurring task" : "New task";
  const moveWs = (id: number) => {
    const w = places.find((x) => x.id === id);
    if (w?.projects[0]) {
      setTplAnyProject(false);
      setProject(w.projects[0].id);
    }
  };
  const canRelations = hasCap(me, "relations", ws.id);
  const single = !tplMode && !editing && !repeating; // one task, made now
  const alwaysOk = hasCap(me, "always_run", ws.id);
  return (
    <Modal title={heading} onClose={onClose} wide expandable help={<HelpButton topic={repeating || editing ? "recurring" : "tasks"} label={repeating || editing ? "Help: recurring tasks" : "Help: tasks"} />}>
      <form className="tm-form" onSubmit={submit} aria-label={heading}>
        {tpl && (tpl.recurring ?? 0) > 0 && (
          <p className="small muted sched-tpl" role="note">
            ⟳ {tpl.recurring} recurring task{tpl.recurring === 1 ? " creates its" : "s create their"} tasks from this template: changes apply to the tasks they create from now on.
          </p>
        )}
        {editing && schedule && <ScheduleTemplateNote schedule={schedule} />}
        <div className="tp-body">
          <div className="tp-main">
            <section className="tp-box tp-task" aria-label={tplMode ? "Template" : "Task"}>
              {(tplMode || templates.length > 0) && (
                <div className="tp-grid te-head">
                  {tplMode && (
                    <Row label="Template name" wide>
                      <input value={tplName} onChange={(e) => setTplName(e.target.value)} required maxLength={80} placeholder="e.g. Bug report" aria-label="Template name" />
                    </Row>
                  )}
                  {!tplMode && templates.length > 0 && (
                    <Row label={editing ? "Template" : "From template"} wide className="tpl-pick">
                      <select value={picked?.id ?? ""} onChange={(e) => pick(templates.find((t) => t.id === Number(e.target.value)) ?? null)} aria-label="From template">
                        {!editing && <option value="">— blank task —</option>}
                        {templates.map((t) => (
                          <option key={t.id} value={t.id}>
                            {t.name}
                            {t.project_id === null ? " (any project)" : ""}
                          </option>
                        ))}
                      </select>
                      {picked && canCreate && !repeating && (
                        <button type="button" className="button secondary small" onClick={createFromTemplate} disabled={busy}>
                          ⚡ Create from template
                        </button>
                      )}
                      {picked && !repeating && <span className="muted small rf-note">The form is filled from “{picked.name}”; change anything before creating, or create it as is.</span>}
                      {picked && repeating && !editing && (
                        <span className="muted small rf-note">
                          {usesPicked
                            ? `Uses template “${picked.name}” itself: later edits to that template change the tasks it creates. Change anything here to save it as a new template instead.`
                            : `Changed from “${picked.name}”: saved as a new template with what's here.`}
                        </span>
                      )}
                      {picked && editing && schedule && picked.id !== schedule.template_id && (
                        <span className="muted small rf-note">
                          {usesPicked ? `Switches to template “${picked.name}”.` : `Changed from “${picked.name}”: the changes are saved to the current template “${schedule.template_name}” instead.`}
                        </span>
                      )}
                    </Row>
                  )}
                </div>
              )}
              <div className="edit tp-edit te-text">
                <input className="tp-title-input" value={title} onChange={(e) => setTitle(e.target.value)} required maxLength={250} placeholder={tplMode ? "Task title: what needs doing?" : "Title: what needs doing?"} aria-label={tplMode ? "Task title" : "Title"} data-autofocus="" />
                <MarkdownEditor value={description} onChange={setDescription} users={candidates} minRows={6} aria-label="Description" placeholder="Description (Markdown) · @ to mention" />
              </div>
            </section>
            <div className="tsecs">
              <ChecklistDraft items={checklist} onChange={setChecklist} note={repeating ? "Every task it creates gets this checklist." : tplMode ? "Every task made from the template gets this checklist." : undefined} />
              <SubtasksDraft
                items={subtasks}
                onChange={setSubtasks}
                canGate={canRelations}
                note={
                  repeating || tplMode
                    ? `Every task ${repeating ? "it creates" : "made from the template"} gets these subtasks, each made like it (priority, assignees, window, agent run). Tick “blocks parent” to make the task wait for one.`
                    : undefined
                }
              />
            </div>
          </div>
          <div className="tp-side">
            <div className="tp-props">
              <PropGroup icon="layers" title="Where">
                <Row label="Workspace">
                  {places.length > 1 && !editing && !tpl ? (
                    <select value={ws.id} onChange={(e) => moveWs(Number(e.target.value))} aria-label="Workspace">
                      {places.map((w) => (
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
                  <select
                    value={tplMode && tplAnyProject ? "any" : project}
                    onChange={(e) => {
                      if (e.target.value === "any") setTplAnyProject(true);
                      else {
                        setTplAnyProject(false);
                        setProject(Number(e.target.value));
                      }
                    }}
                    disabled={editing}
                    aria-label="Project"
                  >
                    {tplMode && <option value="any">Any project</option>}
                    {ws.projects.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.key} · {p.name}
                      </option>
                    ))}
                  </select>
                </Row>
                {!repeating && !tplMode && (
                  <Row label="Status">
                    <select value={status} onChange={(e) => setStatus(Number(e.target.value))} disabled={!statusReady} aria-busy={!statusReady && !statusErr} aria-label="Status">
                      {projectStatuses.map((x) => (
                        <option key={x.id} value={x.id}>
                          {x.name}
                        </option>
                      ))}
                    </select>
                  </Row>
                )}
              </PropGroup>

              {!tplMode && (
                <PropGroup icon="calendar" title="When">
                  {!repeating && (
                    <>
                      <Row wide label={notAfter ? "Window opens" : "Start"}>
                        <DateTimeInput value={start} onChange={setStart} defaultTime={DEFAULT_START_TIME} label={notAfter ? "Window opens" : "Start"} live />
                      </Row>
                      <Row wide label="Due">
                        <DateTimeInput value={due} onChange={setDue} defaultTime={DEFAULT_DUE_TIME} label="Due" live />
                      </Row>
                      <Row tip="After this moment the task can’t be started any more; untouched, it is marked missed" wide label="Window closes">
                        <DateTimeInput value={notAfter} onChange={setNotAfter} defaultTime={DEFAULT_DUE_TIME} label="Window closes" live />
                      </Row>
                    </>
                  )}
                  {canRepeat && (
                    <RepeatFields value={repeat} onChange={setRepeat} allowNever={!editing} showPaused={editing}>
                      {schedules.length > 0 && (
                        <RfRow label="Schedule">
                          <select value={pickedSchedule?.id ?? ""} onChange={(e) => (e.target.value ? pickSchedule(Number(e.target.value)) : setPickedSchedule(null))} aria-label="Schedule">
                            {!editing && <option value="">custom timing</option>}
                            {schedules.map((x) => (
                              <option key={x.id} value={x.id}>
                                {x.name}
                              </option>
                            ))}
                          </select>
                          {(() => {
                            // the select already says which schedule it runs on; say only what saving will do
                            const hint = usesSchedule
                              ? editing && schedule && pickedSchedule!.id !== schedule.schedule_id
                                ? `Switches to “${pickedSchedule!.name}”.`
                                : ""
                              : editing && schedule
                                ? `Timing changes are saved to “${schedule.schedule_name}”.`
                                : "Saved as a new schedule (or the existing one with exactly this timing).";
                            return hint && <span className="muted small rf-note">{hint}</span>;
                          })()}
                        </RfRow>
                      )}
                    </RepeatFields>
                  )}
                </PropGroup>
              )}

              <PropGroup icon="users" title="Details">
                <Row label="Priority">
                  <select value={priority} onChange={(e) => setPriority(Number(e.target.value))} aria-label="Priority">
                    {priorityNames.map((n, i) => (
                      <option key={n} value={i}>
                        {n}
                      </option>
                    ))}
                  </select>
                </Row>
                <Row label="Labels" wide>
                  <LabelInput value={taskLabels} onChange={setTaskLabels} known={known} />
                </Row>
                <Row label="Assignees" wide>
                  <AssigneePicker value={assignees} onChange={setAssignees} candidates={candidates} />
                </Row>
                <Row label="Colour" wide>
                  <ColorPicker value={color} onChange={setColor} />
                </Row>
              </PropGroup>

              <AgentRunGroup
                value={run}
                canWrite
                alwaysLocked={!alwaysOk && !run.always_run}
                canTools={hasCap(me, "allowed_tools", ws.id)}
                onChange={(p) => setRun((r) => ({ ...r, ...p, ...(p.allowed_tools ? { tools_error: null } : {}) }))}
                onToolsError={(bad) => setRun((r) => ((bad ?? null) === (r.tools_error ?? null) ? r : { ...r, tools_error: bad }))}
              />
            </div>
            <div className="tsecs">
              {single && canRelations && <DependenciesDraft wsId={ws.id} items={deps} onChange={setDeps} />}
              <FieldsDraft fields={fields} values={custom} onChange={setCustom} />
            </div>
          </div>
        </div>
        {(err || (statusErr && !repeating && !noStatus)) && (
          <p className="danger" role="alert">
            {err || statusErr}
          </p>
        )}
        <div className="tm-foot">
          {single && canTemplates && (
            <div className="save-tpl">
              {saveAs === null ? (
                <button type="button" className="link small" onClick={() => setSaveAs(title)}>
                  <Icon name="template" /> Save as template
                </button>
              ) : (
                <div className="quick-add">
                  <input value={saveAs} onChange={(e) => setSaveAs(e.target.value)} maxLength={80} placeholder="Template name" aria-label="Template name" autoFocus />
                  <button type="button" className="button secondary small" onClick={saveAsTemplate}>
                    Save template
                  </button>
                  <button type="button" className="link small" onClick={() => setSaveAs(null)}>
                    cancel
                  </button>
                </div>
              )}
              {note && <span className="pill pill-done">{note}</span>}
            </div>
          )}
          <span className="grow" />
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="button" disabled={busy || (repeating && !!problem) || (single && !statusReady)}>
            {tplMode ? "Save template" : editing ? "Save recurring task" : repeating ? "⟳ Create recurring task" : "Create task"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

// lastProject / rememberProject: the project the last new task went to (this device), where the next
// one starts when the board shows several.
const LAST_KEY = "gigg:last-project";
export function lastProject(): number | null {
  try {
    const n = Number(localStorage.getItem(LAST_KEY));
    return Number.isInteger(n) && n > 0 ? n : null;
  } catch {
    return null;
  }
}
function rememberProject(id: number) {
  try {
    localStorage.setItem(LAST_KEY, String(id));
  } catch {
    // storage blocked: the board starts in the first project in view
  }
}

// ScheduleTemplateNote: where an edited recurring task's content and timing live, and who shares them.
function ScheduleTemplateNote({ schedule: s }: { schedule: Recurrence }) {
  const { me } = useApp();
  const tOthers = Math.max(0, (s.template_uses ?? 1) - 1);
  const sOthers = Math.max(0, (s.schedule_uses ?? 1) - 1);
  const others = (n: number) => `${n} other recurring task${n === 1 ? "" : "s"}`;
  return (
    <p className="small muted sched-tpl" role="note">
      <Icon name="template" /> Content: template <strong>“{s.template_name}”</strong>
      {tOthers > 0 && <> (also used by {others(tOthers)}: content changes apply to them too)</>}
      {s.template_created_by !== me.user.id && !hasCap(me, "templates", s.workspace_id) && <> · changing the content needs the Task templates permission</>}
      <br /><Icon name="clock" /> Timing: schedule <strong>“{s.schedule_name}”</strong>
      {sOthers > 0 && <> (also used by {others(sOthers)}: timing changes move them too)</>}
    </p>
  );
}
