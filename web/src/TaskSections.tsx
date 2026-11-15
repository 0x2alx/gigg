// Task panel sections: checklist, subtasks, dependencies and custom fields. Each saves straight to
// the server and hands the updated task back through onTask.
import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { api, ApiError, type ChecklistItem, type CustomField, type Relation, type RelationDirection, type Task } from "./api";
import { formatCustom, fromInput, moveAfter, progress, reorder, splitLines, toInput } from "./items";
import { CATEGORY_COLOR } from "./theme";
import { Pill, StatusBadge } from "./ui";
import { Icon } from "./icons";

export const errText = (e: unknown, fallback = "That didn't work.") => (e instanceof ApiError ? e.message : fallback);

// Section: a compact collapsible block with a title row of badges (the badges show while it is folded).
// openOn: a value that opens the section each time it changes to a new non-zero value. keepMounted:
// folding hides the body instead of removing it, so text typed in it survives.
export function Section({ title, badges, children, defaultOpen = true, label, onOpenChange, openOn = 0, keepMounted = false }: { title: string; badges?: ReactNode; children: ReactNode; defaultOpen?: boolean; label: string; onOpenChange?: (open: boolean) => void; openOn?: number; keepMounted?: boolean }) {
  const [open, setOpenState] = useState(defaultOpen);
  const bodyId = useId();
  const setOpen = (v: boolean) => {
    setOpenState(v);
    onOpenChange?.(v);
  };
  const seen = useRef(openOn);
  useEffect(() => {
    if (openOn && openOn !== seen.current) {
      setOpenState(true);
      onOpenChange?.(true);
    }
    seen.current = openOn;
  }, [openOn, onOpenChange]);
  return (
    <section className={"tsec" + (open ? " open" : "")} aria-label={label}>
      <button type="button" className="tsec-head" aria-expanded={open} aria-controls={open || keepMounted ? bodyId : undefined} onClick={() => setOpen(!open)}>
        <span className="tsec-caret" aria-hidden="true">
          <Icon name={open ? "chevron-down" : "chevron-right"} strokeWidth={2.2} />
        </span>
        <span className="tsec-title">{title}</span>
        {badges}
      </button>
      {(open || keepMounted) && (
        <div className="tsec-body" id={bodyId} hidden={!open}>
          {children}
        </div>
      )}
    </section>
  );
}

export function ProgressBar({ done, total, label }: { done: number; total: number; label: string }) {
  const pct = progress(done, total);
  return (
    <span className="progress" role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={total} aria-valuenow={done}>
      <span className="progress-fill" style={{ width: `${pct}%`, background: pct === 100 ? CATEGORY_COLOR.done : undefined }} />
    </span>
  );
}

// ---- checklist

export function ChecklistSection({ task, canEdit, onTask }: { task: Task; canEdit: boolean; onTask: (t: Task) => void }) {
  const [items, setItems] = useState<ChecklistItem[] | null>(null);
  const [text, setText] = useState("");
  const [editing, setEditing] = useState<{ id: number; text: string } | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let live = true;
    api
      .checklist(task.id)
      .then((r) => live && setItems(r.items))
      .catch((e: unknown) => live && setErr(errText(e, "Couldn't load the checklist.")));
    return () => {
      live = false;
    };
  }, [task.id, task.version]);

  const run = (p: Promise<Task>) => {
    setBusy(true);
    setErr("");
    return p
      .then(onTask)
      .catch((e: unknown) => {
        setErr(errText(e));
        api.checklist(task.id).then((r) => setItems(r.items), () => undefined);
      })
      .finally(() => setBusy(false));
  };
  const add = (lines: string[]) => {
    if (!lines.length) return;
    setText("");
    void run(api.addChecklist(task.id, lines));
  };
  const toggle = (it: ChecklistItem) => {
    setItems((xs) => xs?.map((x) => (x.id === it.id ? { ...x, done_at: x.done_at ? null : Date.now() } : x)) ?? null);
    void run(api.patchChecklist(task.id, it.id, { done: !it.done_at }));
  };
  const move = (i: number, dir: -1 | 1) => {
    if (!items) return;
    const after = moveAfter(items.map((x) => x.id), i, dir);
    if (after === null) return;
    const id = items[i]!.id;
    setItems(reorder(items, i, dir));
    void run(api.patchChecklist(task.id, id, { after })).then(() =>
      requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-check="${id}"] .ck-${dir < 0 ? "up" : "down"}`)?.focus()),
    );
  };
  const saveEdit = () => {
    if (!editing) return;
    const t = editing.text.trim();
    const it = items?.find((x) => x.id === editing.id);
    setEditing(null);
    if (!t || !it || t === it.text) return;
    setItems((xs) => xs?.map((x) => (x.id === it.id ? { ...x, text: t } : x)) ?? null);
    void run(api.patchChecklist(task.id, it.id, { text: t }));
  };

  const total = items?.length ?? task.checklist_total ?? 0;
  const done = items ? items.filter((x) => x.done_at).length : (task.checklist_done ?? 0);
  if (!canEdit && total === 0) return null;
  return (
    <Section
      defaultOpen={false}
      label="Checklist"
      title="Checklist"
      badges={
        total > 0 && (
          <>
            <ProgressBar done={done} total={total} label="Checklist progress" />
            <span className={"tsec-count" + (done === total ? " done" : "")}>
              {done}/{total}
            </span>
          </>
        )
      }
    >
      {err && (
        <p className="danger small" role="alert">
          {err}
        </p>
      )}
      <ul className="checklist">
        {items?.map((it, i) => (
          <li key={it.id} data-check={it.id} className={it.done_at ? "done" : undefined}>
            <input type="checkbox" checked={!!it.done_at} disabled={!canEdit} onChange={() => toggle(it)} aria-label={`Done: ${it.text}`} />
            {editing?.id === it.id ? (
              <input
                className="ck-edit"
                value={editing.text}
                autoFocus
                maxLength={500}
                aria-label="Edit item"
                onChange={(e) => setEditing({ id: it.id, text: e.target.value })}
                onBlur={saveEdit}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    saveEdit();
                  } else if (e.key === "Escape") {
                    e.preventDefault();
                    e.stopPropagation();
                    setEditing(null);
                  }
                }}
              />
            ) : canEdit ? (
              <button type="button" className="ck-text" onClick={() => setEditing({ id: it.id, text: it.text })} aria-label={`Edit item: ${it.text}`}>
                {it.text}
              </button>
            ) : (
              <span className="ck-text">{it.text}</span>
            )}
            {canEdit && (
              <span className="ck-actions">
                <button type="button" className="icon-button ck-up" disabled={i === 0 || busy} onClick={() => move(i, -1)} aria-label={`Move up: ${it.text}`}>
                  ↑
                </button>
                <button type="button" className="icon-button ck-down" disabled={i === items.length - 1 || busy} onClick={() => move(i, 1)} aria-label={`Move down: ${it.text}`}>
                  ↓
                </button>
                <button
                  type="button"
                  className="icon-button"
                  onClick={() => {
                    setItems((xs) => xs?.filter((x) => x.id !== it.id) ?? null);
                    void run(api.deleteChecklist(task.id, it.id));
                  }}
                  aria-label={`Delete item: ${it.text}`}
                >
                  <Icon name="x" />
                </button>
              </span>
            )}
          </li>
        ))}
      </ul>
      {canEdit && (
        <input
          className="ck-add"
          value={text}
          data-dirty={text.trim() ? "" : undefined}
          placeholder="Add an item… (paste several lines to add many)"
          aria-label="Add checklist item"
          maxLength={500}
          onChange={(e) => setText(e.target.value)}
          onPaste={(e) => {
            const t = e.clipboardData.getData("text");
            if (t.includes("\n")) {
              e.preventDefault();
              add(splitLines(text + t));
            }
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              add(splitLines(text));
            }
          }}
        />
      )}
    </Section>
  );
}

// ---- subtasks

export function SubtasksSection({ task, wsId, canCreate, onOpen, onChanged }: { task: Task; wsId: number; canCreate: boolean; onOpen: (t: Task) => void; onChanged: () => void }) {
  const [subs, setSubs] = useState<Task[] | null>(null);
  const [parent, setParent] = useState<Task | null>(null);
  const [title, setTitle] = useState("");
  const [err, setErr] = useState("");
  const load = () =>
    api
      .wsTasks(wsId, { parent: task.id, archived: "include", limit: 200, sort: "number" })
      .then((r) => setSubs(r.items))
      .catch((e: unknown) => setErr(errText(e, "Couldn't load the subtasks.")));
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task.id, task.version, wsId]);
  useEffect(() => {
    setParent(null);
    if (task.parent_id) api.task(task.parent_id).then(setParent, () => undefined);
  }, [task.parent_id]);

  const add = () => {
    const t = title.trim();
    if (!t) return;
    setErr("");
    api
      .createTask(task.project_id, { title: t, parent_id: task.id })
      .then(() => {
        setTitle("");
        void load();
        onChanged();
      })
      .catch((e: unknown) => setErr(errText(e, "Couldn't add the subtask.")));
  };

  const total = subs?.length ?? task.subtask_count ?? 0;
  const done = subs ? subs.filter((s) => s.status_category === "done" || s.status_category === "cancelled").length : (task.subtask_done ?? 0);
  if (!canCreate && total === 0 && !task.parent_id) return null;
  return (
    <Section
      defaultOpen={false}
      label="Subtasks"
      title="Subtasks"
      badges={
        total > 0 && (
          <>
            <ProgressBar done={done} total={total} label="Subtask progress" />
            <span className={"tsec-count" + (done === total ? " done" : "")}>
              {done}/{total} done
            </span>
          </>
        )
      }
    >
      {task.parent_id && (
        <p className="sub-parent small">
          <span className="muted">Subtask of </span>
          {parent ? (
            <button type="button" className="link" onClick={() => onOpen(parent)}>
              <span className="pill pill-key">{parent.key}</span> {parent.title}
            </button>
          ) : (
            <span className="muted">…</span>
          )}
        </p>
      )}
      {err && (
        <p className="danger small" role="alert">
          {err}
        </p>
      )}
      {subs && subs.length > 0 && (
        <ul className="rel-list">
          {subs.map((s) => (
            <li key={s.id} className={s.archived_at ? "archived" : undefined}>
              <button type="button" className="rel-open" onClick={() => onOpen(s)}>
                <span className="pill pill-key">{s.key}</span>
                <StatusBadge name={s.status_name} category={s.status_category} />
                <span className="rel-title">{s.title}</span>
                {!!s.archived_at && <span className="pill pill-muted">archived</span>}
              </button>
            </li>
          ))}
        </ul>
      )}
      {canCreate && (
        <div className="quick-add">
          <input
            value={title}
            data-dirty={title.trim() ? "" : undefined}
            placeholder="Subtask title"
            maxLength={250}
            aria-label="New subtask title"
            onChange={(e) => setTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
          />
          <button type="button" className="button secondary small" onClick={add} disabled={!title.trim()}>
            + Subtask
          </button>
        </div>
      )}
    </Section>
  );
}

// ---- dependencies

interface Dep {
  other: number;
  key: string;
  title: string;
  open: boolean;
  dir: RelationDirection; // seen from this task
}

export function splitRelations(taskId: number, rels: Relation[]): Record<RelationDirection, Dep[]> {
  const out: Record<RelationDirection, Dep[]> = { blocked_by: [], blocks: [], relates: [] };
  for (const r of rels) {
    const fromMe = r.from_task_id === taskId;
    const other = fromMe ? r.to_task_id : r.from_task_id;
    const key = fromMe ? r.to_key : r.from_key;
    const dir: RelationDirection = r.kind === "relates" ? "relates" : fromMe ? "blocks" : "blocked_by";
    out[dir].push({ other, key, title: r.title, open: r.open, dir });
  }
  return out;
}

const DIR_LABEL: Record<RelationDirection, string> = { blocked_by: "🔒 Blocked by", blocks: "⛔ Blocks", relates: "↔ Related" };

export function DependenciesSection({ task, wsId, canEdit, onTask, onOpen }: { task: Task; wsId: number; canEdit: boolean; onTask: (t: Task) => void; onOpen: (t: Task) => void }) {
  const [rels, setRels] = useState<Relation[] | null>(null);
  const [err, setErr] = useState("");
  const [adding, setAdding] = useState(false);
  const [dir, setDir] = useState<RelationDirection>("blocked_by");
  const [q, setQ] = useState("");
  const [found, setFound] = useState<Task[]>([]);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  // a link added or removed doesn't change the task's version: reload the list after each one
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let live = true;
    api
      .relations(task.id)
      .then((r) => live && setRels(r.items))
      .catch((e: unknown) => live && setErr(errText(e, "Couldn't load the dependencies.")));
    return () => {
      live = false;
    };
  }, [task.id, task.version, reload]);
  useEffect(() => {
    if (timer.current) clearTimeout(timer.current);
    const s = q.trim();
    if (!s) {
      setFound([]);
      return;
    }
    timer.current = setTimeout(() => {
      api
        .wsTasks(wsId, { q: s, limit: 10 })
        .then((r) => setFound(r.items.filter((t) => t.id !== task.id)))
        .catch(() => setFound([]));
    }, 250);
  }, [q, wsId, task.id]);

  const fail = (e: unknown) => setErr(e instanceof ApiError && e.status === 409 ? `That would make a cycle: ${e.message}` : errText(e));
  const add = (other: string) => {
    setErr("");
    api
      .addRelation(task.id, other, dir)
      .then((t) => {
        setQ("");
        setFound([]);
        setAdding(false);
        setReload((n) => n + 1);
        onTask(t);
      })
      .catch(fail);
  };
  const remove = (d: Dep) => {
    setErr("");
    api
      .removeRelation(task.id, d.other, d.dir)
      .then((t) => {
        setReload((n) => n + 1);
        onTask(t);
      })
      .catch(fail);
  };
  const open = (id: number) => api.task(id).then(onOpen, fail);

  const groups = splitRelations(task.id, rels ?? []);
  const count = (rels ?? []).length;
  if (!canEdit && count === 0) return null;
  return (
    <Section
      defaultOpen={false}
      label="Dependencies"
      title="Dependencies"
      badges={
        <>
          {!!task.blocked_by && (
            <span className="pill pill-danger">
              <Icon name="lock" /> {task.blocked_by} open
            </span>
          )}
          {count > 0 && <span className="tsec-count">{count}</span>}
        </>
      }
    >
      {err && (
        <p className="danger small" role="alert">
          {err}
        </p>
      )}
      {(["blocked_by", "blocks", "relates"] as const).map(
        (d) =>
          groups[d].length > 0 && (
            <div key={d} className="dep-group">
              <div className="dep-head small">{DIR_LABEL[d]}</div>
              <ul className="rel-list">
                {groups[d].map((x) => (
                  <li key={x.other}>
                    <button type="button" className="rel-open" onClick={() => void open(x.other)}>
                      <span className="pill pill-key">{x.key}</span>
                      {x.open ? <Pill color={d === "blocked_by" ? "#E5484D" : CATEGORY_COLOR.todo!}>open</Pill> : <Pill color={CATEGORY_COLOR.done!}>done</Pill>}
                      <span className="rel-title">{x.title}</span>
                    </button>
                    {canEdit && (
                      <button type="button" className="icon-button" onClick={() => remove(x)} aria-label={`Remove ${DIR_LABEL[d].slice(2).toLowerCase()} ${x.key}`}>
                        <Icon name="x" />
                      </button>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ),
      )}
      {canEdit &&
        (adding ? (
          <div className="dep-add">
            <div className="quick-add">
              <select value={dir} onChange={(e) => setDir(e.target.value as RelationDirection)} aria-label="Direction">
                <option value="blocked_by">is blocked by</option>
                <option value="blocks">blocks</option>
                <option value="relates">relates to</option>
              </select>
              <input
                value={q}
                autoFocus
                placeholder="Search tasks or type a key"
                aria-label="Find a task"
                onChange={(e) => setQ(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && q.trim()) {
                    e.preventDefault();
                    add(found[0]?.key ?? q.trim());
                  } else if (e.key === "Escape") {
                    e.preventDefault();
                    e.stopPropagation();
                    setAdding(false);
                  }
                }}
              />
            </div>
            {found.length > 0 && (
              <ul className="rel-list search-results" aria-label="Matching tasks">
                {found.map((t) => (
                  <li key={t.id}>
                    <button type="button" className="rel-open" onClick={() => add(t.key)}>
                      <span className="pill pill-key">{t.key}</span>
                      <StatusBadge name={t.status_name} category={t.status_category} />
                      <span className="rel-title">{t.title}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
            <button type="button" className="link small" onClick={() => setAdding(false)}>
              cancel
            </button>
          </div>
        ) : (
          <button type="button" className="button secondary small" onClick={() => setAdding(true)}>
            + Dependency
          </button>
        ))}
    </Section>
  );
}

// ---- custom fields

export function CustomFieldsSection({ task, fields, canEdit, onTask }: { task: Task; fields: CustomField[]; canEdit: boolean; onTask: (t: Task) => void }) {
  const [err, setErr] = useState("");
  if (fields.length === 0) return null;
  const save = (f: CustomField, v: unknown) => {
    if (v === undefined) {
      setErr(`${f.name}: not a valid ${f.kind}.`);
      return;
    }
    const cur = task.custom?.[String(f.id)] ?? null;
    if (v === cur) return;
    setErr("");
    api
      .setCustom(task.id, { [String(f.id)]: v })
      .then(onTask)
      .catch((e: unknown) => setErr(errText(e)));
  };
  const set = fields.filter((f) => formatCustom(f, task.custom?.[String(f.id)]) !== "").length;
  return (
    <Section defaultOpen={false} label="Custom fields" title="Fields" badges={<span className="tsec-count">{set}/{fields.length} set</span>}>
      {err && (
        <p className="danger small" role="alert">
          {err}
        </p>
      )}
      <dl className="props custom-props">
        {fields.map((f) => (
          <CustomRow key={f.id} field={f} value={task.custom?.[String(f.id)] ?? null} canEdit={canEdit} onSave={(v) => save(f, v)} />
        ))}
      </dl>
    </Section>
  );
}

export function CustomInput({ field: f, value, onSave, autoFocus, onDone }: { field: CustomField; value: unknown; onSave: (v: unknown) => void; autoFocus?: boolean; onDone?: () => void }) {
  const [text, setText] = useState(toInput(f, value));
  useEffect(() => setText(toInput(f, value)), [f, value]);
  const label = f.name;
  if (f.kind === "checkbox") return <input type="checkbox" checked={value === true} onChange={(e) => onSave(e.target.checked)} aria-label={label} />;
  if (f.kind === "select") {
    return (
      <select value={typeof value === "string" ? value : ""} onChange={(e) => onSave(e.target.value || null)} aria-label={label}>
        <option value="">—</option>
        {(f.options ?? []).map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>
    );
  }
  if (f.kind === "date") return <input type="date" value={text} onChange={(e) => onSave(fromInput(f, e.target.value))} aria-label={label} />;
  const commit = () => {
    onSave(fromInput(f, text));
    onDone?.();
  };
  return (
    <input
      type={f.kind === "number" ? "number" : f.kind === "url" ? "url" : "text"}
      step={f.kind === "number" ? "any" : undefined}
      value={text}
      autoFocus={autoFocus}
      placeholder={f.kind === "url" ? "https://…" : undefined}
      onChange={(e) => setText(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          (e.target as HTMLInputElement).blur();
        }
      }}
      aria-label={label}
    />
  );
}

function CustomRow({ field: f, value, canEdit, onSave }: { field: CustomField; value: unknown; canEdit: boolean; onSave: (v: unknown) => void }) {
  const [editUrl, setEditUrl] = useState(false);
  const shown = formatCustom(f, value);
  let body: ReactNode;
  if (!canEdit) {
    body = f.kind === "url" && shown ? <UrlLink href={String(value)} text={shown} /> : shown ? <span className="pill pill-muted">{shown}</span> : <span className="muted">—</span>;
  } else if (f.kind === "url" && value && !editUrl) {
    body = (
      <span className="url-row">
        <UrlLink href={String(value)} text={shown} />
        <button type="button" className="icon-button" onClick={() => setEditUrl(true)} aria-label={`Edit ${f.name}`}>
          ✎
        </button>
      </span>
    );
  } else {
    body = <CustomInput field={f} value={value} onSave={onSave} autoFocus={editUrl} onDone={() => setEditUrl(false)} />;
  }
  return (
    <>
      <dt>{f.name}</dt>
      <dd>{body}</dd>
    </>
  );
}

function UrlLink({ href, text }: { href: string; text: string }) {
  if (!/^https?:\/\//i.test(href)) return <span>{text}</span>;
  return (
    <a href={href} target="_blank" rel="noopener noreferrer" className="url-link">
      🔗 {text}
    </a>
  );
}
