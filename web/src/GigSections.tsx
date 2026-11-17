// The gig editor's building blocks, shared by the gig panel (each change saves straight to the
// server and hands the updated gig back through onGig) and the gig form (New gig, templates,
// recurring gigs: the "draft" versions hold the values until Create or Save). Both lay a gig out
// the same way: the property groups (PropGroup of Rows) and the folding boxes (Section).
import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { api, ApiError, type ChecklistItem, type CustomField, type Relation, type RelationDirection, type Gig, type TemplateSubgig } from "./api";
import { formatCustom, fromInput, moveAfter, progress, reorder, splitLines, toInput } from "./items";
import { CATEGORY_COLOR } from "./theme";
import { EffortSelect, ModelInput, ToolsInput, type RunSettings } from "./runsettings";
import { Pill, StatusBadge } from "./ui";
import { HelpButton } from "./help";
import { Icon, type IconName } from "./icons";

export const errText = (e: unknown, fallback = "That didn't work.") => (e instanceof ApiError ? e.message : fallback);

// PropGroup: a titled box of label/value rows (Where, When, Details, Agent run).
export function PropGroup({ icon, title, children, help }: { icon: IconName; title: string; children: ReactNode; help?: ReactNode }) {
  return (
    <section className="tp-box tp-group" aria-label={title}>
      <h4 className="tp-gh">
        <Icon name={icon} /> {title}
        {help}
      </h4>
      <div className="tp-grid">{children}</div>
    </section>
  );
}

// Row: one label + value in a property group. wide rows take the whole width (chips, swatches).
export function Row({ label, wide, className, children, tip }: { label: ReactNode; wide?: boolean; className?: string; children: ReactNode; tip?: string }) {
  return (
    <div className={"tp-row" + (wide ? " wide" : "")}>
      <span className="tp-label" data-tip={tip}>
        {label}
      </span>
      <div className={"tp-value" + (className ? " " + className : "")}>{children}</div>
    </div>
  );
}

// Section: a compact collapsible block with a title row of badges (the badges show while it is folded).
// openOn: a value that opens the section each time it changes to a new non-zero value. keepMounted:
// folding hides the body instead of removing it, so text typed in it survives.
export function Section({ title, icon, badges, children, defaultOpen = true, label, onOpenChange, openOn = 0, keepMounted = false }: { title: string; icon: IconName; badges?: ReactNode; children: ReactNode; defaultOpen?: boolean; label: string; onOpenChange?: (open: boolean) => void; openOn?: number; keepMounted?: boolean }) {
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
    <section className={"tp-box tsec" + (open ? " open" : "")} aria-label={label}>
      <button type="button" className="tsec-head" aria-expanded={open} aria-controls={open || keepMounted ? bodyId : undefined} onClick={() => setOpen(!open)}>
        <Icon name={icon} />
        <span className="tsec-lead">
          <span className="tsec-title">{title}</span>
          {badges}
        </span>
        <span className="tsec-caret" aria-hidden="true">
          <Icon name={open ? "chevron-up" : "chevron-down"} strokeWidth={2.2} />
        </span>
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

function Problem({ text }: { text: string }) {
  return text ? (
    <p className="danger small" role="alert">
      {text}
    </p>
  ) : null;
}

// ---- agent run

// AgentRunGroup: model, effort, always run and the tool allowlist. onChange gets the one setting
// changed. alwaysLocked: the Always run box can't be changed (no permission).
export function AgentRunGroup({ value, canWrite, alwaysLocked, canTools, onChange, onToolsError }: {
  value: RunSettings; canWrite: boolean; alwaysLocked: boolean; canTools: boolean; onChange: (p: Partial<RunSettings>) => void; onToolsError?: (bad: string | null) => void;
}) {
  return (
    <PropGroup icon="bot" title="Agent run" help={<HelpButton topic="agents" label="Help: how agents run gigs" />}>
      <Row tip="Model for the agent’s run (an alias like opus or sonnet, or a full model id). Empty: the agent’s default" label="Model">
        <ModelInput value={value.model} disabled={!canWrite} onCommit={(model) => onChange({ model })} />
      </Row>
      <Row tip="Reasoning effort for the agent’s run (low to max). Empty: the agent’s default" label="Effort">
        <EffortSelect value={value.effort} disabled={!canWrite} onChange={(effort) => onChange({ effort })} />
      </Row>
      <Row tip="Run it whatever the usage budget, day or night, like a cron job. The daily cap and pauses still apply" label="Always run" wide>
        <label className="admin-check run-always" title={alwaysLocked ? "Changing it needs the Always-run gigs permission" : "Runs whatever the usage budget, day or night"}>
          <input type="checkbox" checked={value.always_run} disabled={!canWrite || alwaysLocked} onChange={(e) => onChange({ always_run: e.target.checked })} />
          <span className="muted small">whatever the usage budget, day or night</span>
        </label>
      </Row>
      {(canTools || value.allowed_tools.length > 0) && (
        <Row tip="The agent gets exactly these tools for this gig (Claude Code permission rules), nothing else. Empty: its usual setup" label="Allowed tools" wide>
          {canTools && canWrite ? (
            <ToolsInput value={value.allowed_tools} onCommit={(allowed_tools) => onChange({ allowed_tools })} onError={onToolsError} />
          ) : (
            <span className="tools-list" title="Changing it needs the Gig tool allowlists permission">
              {value.allowed_tools.map((r) => (
                <code key={r}>{r}</code>
              ))}
            </span>
          )}
        </Row>
      )}
    </PropGroup>
  );
}

// ---- checklist

interface CheckRow {
  key: string;
  text: string;
  done: boolean;
}

// ChecklistBox: the checklist list and its add box. Draft items (checkable false) show their box
// unticked and disabled: they can be ticked once the gig exists.
function ChecklistBox({ rows, canEdit, checkable, busy, err, onToggle, onEdit, onMove, onDelete, onAdd }: {
  rows: CheckRow[] | null; canEdit: boolean; checkable: boolean; busy: boolean; err: string;
  onToggle: (i: number) => void; onEdit: (i: number, text: string) => void; onMove: (i: number, dir: -1 | 1) => void; onDelete: (i: number) => void; onAdd: (lines: string[]) => void;
}) {
  const [text, setText] = useState("");
  const [editing, setEditing] = useState<{ key: string; text: string } | null>(null);
  const add = (lines: string[]) => {
    if (!lines.length) return;
    setText("");
    onAdd(lines);
  };
  const saveEdit = () => {
    if (!editing) return;
    const i = rows?.findIndex((r) => r.key === editing.key) ?? -1;
    const t = editing.text.trim();
    setEditing(null);
    if (i < 0 || !t || t === rows![i]!.text) return;
    onEdit(i, t);
  };
  return (
    <>
      <Problem text={err} />
      <ul className="checklist">
        {rows?.map((it, i) => (
          <li key={it.key} data-check={it.key} className={it.done ? "done" : undefined}>
            <input type="checkbox" checked={it.done} disabled={!canEdit || !checkable} onChange={() => onToggle(i)} aria-label={`Done: ${it.text}`} />
            {editing?.key === it.key ? (
              <input
                className="ck-edit"
                value={editing.text}
                autoFocus
                maxLength={500}
                aria-label="Edit item"
                onChange={(e) => setEditing({ key: it.key, text: e.target.value })}
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
              <button type="button" className="ck-text" onClick={() => setEditing({ key: it.key, text: it.text })} aria-label={`Edit item: ${it.text}`}>
                {it.text}
              </button>
            ) : (
              <span className="ck-text">{it.text}</span>
            )}
            {canEdit && (
              <span className="ck-actions">
                <button type="button" className="icon-button ck-up" disabled={i === 0 || busy} onClick={() => onMove(i, -1)} aria-label={`Move up: ${it.text}`}>
                  ↑
                </button>
                <button type="button" className="icon-button ck-down" disabled={i === rows.length - 1 || busy} onClick={() => onMove(i, 1)} aria-label={`Move down: ${it.text}`}>
                  ↓
                </button>
                <button type="button" className="icon-button" onClick={() => onDelete(i)} aria-label={`Delete item: ${it.text}`}>
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
    </>
  );
}

const focusMoved = (key: string, dir: -1 | 1) => requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-check="${key}"] .ck-${dir < 0 ? "up" : "down"}`)?.focus());

export function ChecklistSection({ gig, canEdit, onGig }: { gig: Gig; canEdit: boolean; onGig: (t: Gig) => void }) {
  const [items, setItems] = useState<ChecklistItem[] | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let live = true;
    api
      .checklist(gig.id)
      .then((r) => live && setItems(r.items))
      .catch((e: unknown) => live && setErr(errText(e, "Couldn't load the checklist.")));
    return () => {
      live = false;
    };
  }, [gig.id, gig.version]);

  const run = (p: Promise<Gig>) => {
    setBusy(true);
    setErr("");
    return p
      .then(onGig)
      .catch((e: unknown) => {
        setErr(errText(e));
        api.checklist(gig.id).then((r) => setItems(r.items), () => undefined);
      })
      .finally(() => setBusy(false));
  };
  const at = (i: number) => items![i]!;

  const total = items?.length ?? gig.checklist_total ?? 0;
  const done = items ? items.filter((x) => x.done_at).length : (gig.checklist_done ?? 0);
  if (!canEdit && total === 0) return null;
  return (
    <Section
      defaultOpen={false}
      label="Checklist"
      title="Checklist"
      icon="checklist"
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
      <ChecklistBox
        rows={items?.map((x) => ({ key: String(x.id), text: x.text, done: !!x.done_at })) ?? null}
        canEdit={canEdit}
        checkable
        busy={busy}
        err={err}
        onToggle={(i) => {
          const it = at(i);
          setItems((xs) => xs?.map((x) => (x.id === it.id ? { ...x, done_at: x.done_at ? null : Date.now() } : x)) ?? null);
          void run(api.patchChecklist(gig.id, it.id, { done: !it.done_at }));
        }}
        onEdit={(i, t) => {
          const it = at(i);
          setItems((xs) => xs?.map((x) => (x.id === it.id ? { ...x, text: t } : x)) ?? null);
          void run(api.patchChecklist(gig.id, it.id, { text: t }));
        }}
        onMove={(i, dir) => {
          if (!items) return;
          const after = moveAfter(items.map((x) => x.id), i, dir);
          if (after === null) return;
          const id = at(i).id;
          setItems(reorder(items, i, dir));
          void run(api.patchChecklist(gig.id, id, { after })).then(() => focusMoved(String(id), dir));
        }}
        onDelete={(i) => {
          const it = at(i);
          setItems((xs) => xs?.filter((x) => x.id !== it.id) ?? null);
          void run(api.deleteChecklist(gig.id, it.id));
        }}
        onAdd={(lines) => void run(api.addChecklist(gig.id, lines))}
      />
    </Section>
  );
}

// ChecklistDraft: the checklist of a gig (or template) not saved yet.
export function ChecklistDraft({ items, onChange, note }: { items: string[]; onChange: (v: string[]) => void; note?: string }) {
  // stable keys for the rows, so an item being edited keeps its box while others move
  const ids = useRef<string[]>([]);
  const seq = useRef(0);
  while (ids.current.length < items.length) ids.current.push("d" + seq.current++);
  ids.current.length = items.length;
  const swap = (i: number, j: number) => {
    const xs = [...items];
    [xs[i], xs[j]] = [xs[j]!, xs[i]!];
    [ids.current[i], ids.current[j]] = [ids.current[j]!, ids.current[i]!];
    onChange(xs);
  };
  return (
    <Section defaultOpen={false} label="Checklist" title="Checklist" icon="checklist" badges={items.length > 0 && <span className="tsec-count">{items.length}</span>}>
      {note && <p className="muted small tsec-note">{note}</p>}
      <ChecklistBox
        rows={items.map((text, i) => ({ key: ids.current[i]!, text, done: false }))}
        canEdit
        checkable={false}
        busy={false}
        err=""
        onToggle={() => undefined}
        onEdit={(i, t) => onChange(items.map((x, j) => (j === i ? t : x)))}
        onMove={(i, dir) => {
          const j = i + dir;
          if (j < 0 || j >= items.length) return;
          const key = ids.current[i]!;
          swap(i, j);
          focusMoved(key, dir);
        }}
        onDelete={(i) => {
          ids.current.splice(i, 1);
          onChange(items.filter((_, j) => j !== i));
        }}
        onAdd={(lines) => onChange([...items, ...lines].slice(0, 200))}
      />
      {items.length >= 200 && <p className="muted small tsec-note">That's the most a checklist can hold (200).</p>}
    </Section>
  );
}

// ---- subgigs

const GATE_TIP = "While this subgig is open, its parent waits: agents don’t pick the parent up until it is done or cancelled";

// GateBox: the "blocks parent" switch of one subgig.
function GateBox({ on, disabled, onChange, title }: { on: boolean; disabled?: boolean; onChange: (v: boolean) => void; title: string }) {
  return (
    <label className="check sub-gate" title={GATE_TIP}>
      <input type="checkbox" checked={on} disabled={disabled} onChange={(e) => onChange(e.target.checked)} aria-label={`Blocks the parent: ${title}`} />
      <Icon name="lock" /> blocks parent
    </label>
  );
}

function SubgigAdd({ onAdd }: { onAdd: (title: string) => Promise<unknown> | void }) {
  const [title, setTitle] = useState("");
  const add = () => {
    const t = title.trim();
    if (!t) return;
    const p = onAdd(t);
    if (p) void p.then(() => setTitle(""), () => undefined);
    else setTitle("");
  };
  return (
    <div className="quick-add">
      <input
        value={title}
        data-dirty={title.trim() ? "" : undefined}
        placeholder="Subgig title"
        maxLength={250}
        aria-label="New subgig title"
        onChange={(e) => setTitle(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            add();
          }
        }}
      />
      <button type="button" className="button secondary small" onClick={add} disabled={!title.trim()}>
        + Subgig
      </button>
    </div>
  );
}

const SUB_NOTE = "Each subgig is a gig of its own, made like this one (priority, assignees, dates, agent run). Tick “blocks parent” to make this gig wait for it.";

// canEdit: may switch "blocks parent" (the relations permission). assignable: who can be assigned in
// the workspace (a new subgig gets the parent's assignees, but not former members).
export function SubgigsSection({ gig, wsId, canCreate, canEdit, assignable, onOpen, onGig, onChanged }: {
  gig: Gig; wsId: number; canCreate: boolean; canEdit: boolean; assignable: number[]; onOpen: (t: Gig) => void; onGig: (t: Gig) => void; onChanged: () => void;
}) {
  const [subs, setSubs] = useState<Gig[] | null>(null);
  const [parent, setParent] = useState<Gig | null>(null);
  const [err, setErr] = useState("");
  const load = () =>
    api
      .wsGigs(wsId, { parent: gig.id, archived: "include", limit: 200, sort: "number" })
      .then((r) => setSubs(r.items))
      .catch((e: unknown) => setErr(errText(e, "Couldn't load the subgigs.")));
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [gig.id, gig.version, wsId]);
  useEffect(() => {
    setParent(null);
    if (gig.parent_id) api.gig(gig.parent_id).then(setParent, () => undefined);
  }, [gig.parent_id]);
  // the parent's blocked count changes with its subgigs: reload it after each change
  const refresh = () => {
    void load();
    onChanged();
    api.gig(gig.id).then(onGig, () => undefined);
  };

  const add = (title: string) => {
    setErr("");
    return api
      .createGig(gig.project_id, {
        title,
        parent_id: gig.id,
        priority: gig.priority,
        assignees: gig.assignees.filter((id) => assignable.includes(id)),
        ...(gig.start_at ? { start_at: gig.start_at } : {}),
        ...(gig.due_at ? { due_at: gig.due_at } : {}),
        ...(gig.not_after ? { not_after: gig.not_after } : {}),
        ...(gig.model ? { model: gig.model } : {}),
        ...(gig.effort ? { effort: gig.effort } : {}),
      })
      .then(refresh)
      .catch((e: unknown) => {
        setErr(errText(e, "Couldn't add the subgig."));
        throw e;
      });
  };
  const gate = (s: Gig, on: boolean) => {
    setErr("");
    setSubs((xs) => xs?.map((x) => (x.id === s.id ? { ...x, blocks_parent: on } : x)) ?? null);
    api.patchGig(s.id, { blocks_parent: on }).then(refresh, (e: unknown) => {
      setErr(errText(e));
      void load();
    });
  };

  const total = subs?.length ?? gig.subgig_count ?? 0;
  const done = subs ? subs.filter((s) => s.status_category === "done" || s.status_category === "cancelled").length : (gig.subgig_done ?? 0);
  const gates = (subs ?? []).filter((s) => s.blocks_parent && !s.archived_at && s.status_category !== "done" && s.status_category !== "cancelled").length;
  if (!canCreate && total === 0 && !gig.parent_id) return null;
  return (
    <Section
      defaultOpen={false}
      label="Subgigs"
      title="Subgigs"
      icon="subgigs"
      badges={
        <>
          {total > 0 && (
            <>
              <ProgressBar done={done} total={total} label="Subgig progress" />
              <span className={"tsec-count" + (done === total ? " done" : "")}>
                {done}/{total} done
              </span>
            </>
          )}
          {gates > 0 && (
            <span className="pill pill-danger" title="Open subgigs this gig waits for">
              <Icon name="lock" /> {gates}
            </span>
          )}
        </>
      }
    >
      {gig.parent_id && (
        <p className="sub-parent small">
          <span className="muted">Subgig of </span>
          {parent ? (
            <button type="button" className="link" onClick={() => onOpen(parent)}>
              <span className="pill pill-key">{parent.key}</span> {parent.title}
            </button>
          ) : (
            <span className="muted">…</span>
          )}
          <GateBox on={!!gig.blocks_parent} disabled={!canEdit} title={gig.title} onChange={(on) => api.patchGig(gig.id, { blocks_parent: on }).then(onGig, (e: unknown) => setErr(errText(e)))} />
        </p>
      )}
      <Problem text={err} />
      {subs && subs.length > 0 && (
        <ul className="rel-list sub-list">
          {subs.map((s) => (
            <li key={s.id} className={s.archived_at ? "archived" : undefined}>
              <button type="button" className="rel-open" onClick={() => onOpen(s)}>
                <span className="pill pill-key">{s.key}</span>
                <StatusBadge name={s.status_name} category={s.status_category} />
                <span className="rel-title">{s.title}</span>
                {!!s.archived_at && <span className="pill pill-muted">archived</span>}
              </button>
              <GateBox on={!!s.blocks_parent} disabled={!canEdit} title={s.title} onChange={(on) => gate(s, on)} />
            </li>
          ))}
        </ul>
      )}
      {canCreate && <SubgigAdd onAdd={add} />}
    </Section>
  );
}

// SubgigsDraft: subgigs for a gig (or template) not saved yet; created with it.
// canGate: may tick "blocks parent" (the relations permission).
export function SubgigsDraft({ items, onChange, canGate, note = SUB_NOTE }: { items: TemplateSubgig[]; onChange: (v: TemplateSubgig[]) => void; canGate: boolean; note?: string }) {
  const gates = items.filter((s) => s.blocks_parent).length;
  return (
    <Section
      defaultOpen={false}
      label="Subgigs"
      title="Subgigs"
      icon="subgigs"
      badges={
        <>
          {items.length > 0 && <span className="tsec-count">{items.length}</span>}
          {gates > 0 && (
            <span className="pill pill-danger" title="Subgigs this gig will wait for">
              <Icon name="lock" /> {gates}
            </span>
          )}
        </>
      }
    >
      <p className="muted small tsec-note">{note}</p>
      {items.length > 0 && (
        <ul className="rel-list sub-list">
          {items.map((s, i) => (
            <li key={i}>
              <span className="rel-open rel-static">
                <span className="rel-title">{s.title}</span>
              </span>
              <GateBox on={!!s.blocks_parent} disabled={!canGate && !s.blocks_parent} title={s.title} onChange={(on) => onChange(items.map((x, j) => (j === i ? { ...x, blocks_parent: on } : x)))} />
              <button type="button" className="icon-button" onClick={() => onChange(items.filter((_, j) => j !== i))} aria-label={`Remove subgig: ${s.title}`}>
                <Icon name="x" />
              </button>
            </li>
          ))}
        </ul>
      )}
      {items.length < 50 ? <SubgigAdd onAdd={(title) => onChange([...items, { title, blocks_parent: false }])} /> : <p className="muted small tsec-note">That's the most it can have (50).</p>}
    </Section>
  );
}

// ---- dependencies

interface Dep {
  other: number;
  key: string;
  title: string;
  open: boolean;
  dir: RelationDirection; // seen from this gig
  subgig?: boolean; // an open subgig that blocks its parent (not a link: untick it in Subgigs)
}

export function splitRelations(gigId: number, rels: Relation[]): Record<RelationDirection, Dep[]> {
  const out: Record<RelationDirection, Dep[]> = { blocked_by: [], blocks: [], relates: [] };
  for (const r of rels) {
    const fromMe = r.from_gig_id === gigId;
    const other = fromMe ? r.to_gig_id : r.from_gig_id;
    const key = fromMe ? r.to_key : r.from_key;
    const dir: RelationDirection = r.kind === "relates" ? "relates" : fromMe ? "blocks" : "blocked_by";
    out[dir].push({ other, key, title: r.title, open: r.open, dir });
  }
  return out;
}

const DIR_LABEL: Record<RelationDirection, string> = { blocked_by: "🔒 Blocked by", blocks: "⛔ Blocks", relates: "↔ Related" };

// DepList: the dependencies grouped by direction.
function DepList({ groups, canEdit, onOpen, onRemove }: { groups: Record<RelationDirection, Dep[]>; canEdit: boolean; onOpen?: (d: Dep) => void; onRemove: (d: Dep) => void }) {
  return (
    <>
      {(["blocked_by", "blocks", "relates"] as const).map(
        (d) =>
          groups[d].length > 0 && (
            <div key={d} className="dep-group">
              <div className="dep-head small">{DIR_LABEL[d]}</div>
              <ul className="rel-list">
                {groups[d].map((x) => {
                  const body = (
                    <>
                      <span className="pill pill-key">{x.key}</span>
                      {x.other < 0 ? null : x.open ? <Pill color={d === "blocked_by" ? "#E5484D" : CATEGORY_COLOR.todo!}>open</Pill> : <Pill color={CATEGORY_COLOR.done!}>done</Pill>}
                      <span className="rel-title">{x.title}</span>
                      {x.subgig && <span className="pill pill-muted">subgig</span>}
                    </>
                  );
                  return (
                    <li key={(x.subgig ? "s" : "r") + x.other + x.key}>
                      {onOpen ? (
                        <button type="button" className="rel-open" onClick={() => onOpen(x)}>
                          {body}
                        </button>
                      ) : (
                        <span className="rel-open rel-static">{body}</span>
                      )}
                      {canEdit && !x.subgig && (
                        <button type="button" className="icon-button" onClick={() => onRemove(x)} aria-label={`Remove ${DIR_LABEL[d].replace(/^\S+\s/, "").toLowerCase()} ${x.key}`}>
                          <Icon name="x" />
                        </button>
                      )}
                    </li>
                  );
                })}
              </ul>
            </div>
          ),
      )}
    </>
  );
}

// DepPicker: "+ Dependency", then a direction and a gig search. onPick resolves when it was added.
function DepPicker({ wsId, exclude, onPick }: { wsId: number; exclude?: number; onPick: (ref: string, dir: RelationDirection, t?: Gig) => Promise<unknown> }) {
  const [adding, setAdding] = useState(false);
  const [dir, setDir] = useState<RelationDirection>("blocked_by");
  const [q, setQ] = useState("");
  const [found, setFound] = useState<Gig[]>([]);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (timer.current) clearTimeout(timer.current);
    const s = q.trim();
    if (!s) {
      setFound([]);
      return;
    }
    timer.current = setTimeout(() => {
      api
        .wsGigs(wsId, { q: s, limit: 10 })
        .then((r) => setFound(r.items.filter((t) => t.id !== exclude)))
        .catch(() => setFound([]));
    }, 250);
  }, [q, wsId, exclude]);
  const pick = (ref: string, t?: Gig) =>
    onPick(ref, dir, t).then(
      () => {
        setQ("");
        setFound([]);
        setAdding(false);
      },
      () => undefined,
    );
  if (!adding) {
    return (
      <button type="button" className="button secondary small" onClick={() => setAdding(true)}>
        + Dependency
      </button>
    );
  }
  return (
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
          data-dirty={q.trim() ? "" : undefined}
          placeholder="Search gigs or type a key"
          aria-label="Find a gig"
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault(); // never submits the form around it
              if (q.trim()) void pick(found[0]?.key ?? q.trim(), found[0]);
            } else if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              setAdding(false);
            }
          }}
        />
      </div>
      {found.length > 0 && (
        <ul className="rel-list search-results" aria-label="Matching gigs">
          {found.map((t) => (
            <li key={t.id}>
              <button type="button" className="rel-open" onClick={() => void pick(t.key, t)}>
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
  );
}

const cycleText = (e: unknown) => (e instanceof ApiError && e.status === 409 ? `That would make a cycle: ${e.message}` : errText(e));

export function DependenciesSection({ gig, wsId, canEdit, onGig, onOpen }: { gig: Gig; wsId: number; canEdit: boolean; onGig: (t: Gig) => void; onOpen: (t: Gig) => void }) {
  const [rels, setRels] = useState<Relation[] | null>(null);
  const [gates, setGates] = useState<Gig[]>([]); // open subgigs that block this gig
  const [err, setErr] = useState("");
  // a link added or removed doesn't change the gig's version: reload the list after each one
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let live = true;
    api
      .relations(gig.id)
      .then((r) => live && setRels(r.items))
      .catch((e: unknown) => live && setErr(errText(e, "Couldn't load the dependencies.")));
    return () => {
      live = false;
    };
  }, [gig.id, gig.version, reload]);
  const nGates = gig.blocked_by_subgigs ?? 0;
  useEffect(() => {
    if (!nGates) {
      setGates([]);
      return;
    }
    let live = true;
    api.wsGigs(wsId, { parent: gig.id, archived: "include", limit: 200, sort: "number" }).then(
      (r) => live && setGates(r.items.filter((s) => s.blocks_parent && !s.archived_at && s.status_category !== "done" && s.status_category !== "cancelled")),
      () => live && setGates([]),
    );
    return () => {
      live = false;
    };
  }, [gig.id, gig.version, wsId, nGates]);

  const fail = (e: unknown) => setErr(cycleText(e));
  const remove = (d: Dep) => {
    setErr("");
    api
      .removeRelation(gig.id, d.other, d.dir)
      .then((t) => {
        setReload((n) => n + 1);
        onGig(t);
      })
      .catch(fail);
  };
  const open = (d: Dep) => void api.gig(d.other).then(onOpen, fail);

  const groups = splitRelations(gig.id, rels ?? []);
  groups.blocked_by.push(...gates.map((s) => ({ other: s.id, key: s.key, title: s.title, open: true, dir: "blocked_by" as const, subgig: true })));
  const count = (rels ?? []).length + gates.length;
  if (!canEdit && count === 0) return null;
  return (
    <Section
      defaultOpen={false}
      label="Dependencies"
      title="Dependencies"
      icon="link"
      badges={
        <>
          {!!gig.blocked_by && (
            <span className="pill pill-danger">
              <Icon name="lock" /> {gig.blocked_by} open
            </span>
          )}
          {count > 0 && <span className="tsec-count">{count}</span>}
        </>
      }
    >
      <Problem text={err} />
      <DepList groups={groups} canEdit={canEdit} onOpen={open} onRemove={remove} />
      {canEdit && (
        <DepPicker
          wsId={wsId}
          exclude={gig.id}
          onPick={(ref, dir) => {
            setErr("");
            return api.addRelation(gig.id, ref, dir).then(
              (t) => {
                setReload((n) => n + 1);
                onGig(t);
              },
              (e: unknown) => {
                fail(e);
                throw e;
              },
            );
          }}
        />
      )}
    </Section>
  );
}

export interface DraftDep {
  id: number;
  key: string;
  title: string;
  open: boolean;
  dir: RelationDirection;
}

// DependenciesDraft: links a new gig starts with (made when it is created).
export function DependenciesDraft({ wsId, items, onChange }: { wsId: number; items: DraftDep[]; onChange: (v: DraftDep[]) => void }) {
  const [err, setErr] = useState("");
  const groups: Record<RelationDirection, Dep[]> = { blocked_by: [], blocks: [], relates: [] };
  for (const d of items) groups[d.dir].push({ other: d.id, key: d.key, title: d.title, open: d.open, dir: d.dir });
  const blockers = items.filter((d) => d.dir === "blocked_by" && d.open).length;
  return (
    <Section
      defaultOpen={false}
      label="Dependencies"
      title="Dependencies"
      icon="link"
      badges={
        <>
          {blockers > 0 && (
            <span className="pill pill-danger">
              <Icon name="lock" /> {blockers} open
            </span>
          )}
          {items.length > 0 && <span className="tsec-count">{items.length}</span>}
        </>
      }
    >
      <Problem text={err} />
      <DepList groups={groups} canEdit onRemove={(d) => onChange(items.filter((x) => !(x.id === d.other && x.dir === d.dir)))} />
      <DepPicker
        wsId={wsId}
        onPick={(ref, dir, found) => {
          setErr("");
          return (found ? Promise.resolve(found) : api.gig(ref)).then(
            (t) => {
              if (t.workspace_id !== wsId) throw new ApiError(422, "invalid", `${t.key} is in another workspace.`);
              if (items.some((x) => x.id === t.id && x.dir === dir)) return;
              const open = t.status_category !== "done" && t.status_category !== "cancelled";
              onChange([...items, { id: t.id, key: t.key, title: t.title, open, dir }]);
            },
          ).catch((e: unknown) => {
            setErr(e instanceof ApiError && e.status === 404 ? `No gig ${ref}.` : errText(e));
            throw e;
          });
        }}
      />
    </Section>
  );
}

// ---- custom fields

function FieldsBox({ fields, values, canEdit, err, onSave }: { fields: CustomField[]; values: Record<string, unknown>; canEdit: boolean; err: string; onSave: (f: CustomField, v: unknown) => void }) {
  const set = fields.filter((f) => formatCustom(f, values[String(f.id)]) !== "").length;
  return (
    <Section defaultOpen={false} label="Custom fields" title="Fields" icon="fields" badges={<span className="tsec-count">{set}/{fields.length} set</span>}>
      <Problem text={err} />
      <dl className="props custom-props">
        {fields.map((f) => (
          <CustomRow key={f.id} field={f} value={values[String(f.id)] ?? null} canEdit={canEdit} onSave={(v) => onSave(f, v)} />
        ))}
      </dl>
    </Section>
  );
}

export function CustomFieldsSection({ gig, fields, canEdit, onGig }: { gig: Gig; fields: CustomField[]; canEdit: boolean; onGig: (t: Gig) => void }) {
  const [err, setErr] = useState("");
  if (fields.length === 0) return null;
  const save = (f: CustomField, v: unknown) => {
    if (v === undefined) {
      setErr(`${f.name}: not a valid ${f.kind}.`);
      return;
    }
    const cur = gig.custom?.[String(f.id)] ?? null;
    if (v === cur) return;
    setErr("");
    api
      .setCustom(gig.id, { [String(f.id)]: v })
      .then(onGig)
      .catch((e: unknown) => setErr(errText(e)));
  };
  return <FieldsBox fields={fields} values={gig.custom ?? {}} canEdit={canEdit} err={err} onSave={save} />;
}

// FieldsDraft: custom field values for a gig (or template) not saved yet.
export function FieldsDraft({ fields, values, onChange }: { fields: CustomField[]; values: Record<string, unknown>; onChange: (v: Record<string, unknown>) => void }) {
  const [err, setErr] = useState("");
  if (fields.length === 0) return null;
  const save = (f: CustomField, v: unknown) => {
    if (v === undefined) {
      setErr(`${f.name}: not a valid ${f.kind}.`);
      return;
    }
    setErr("");
    onChange({ ...values, [String(f.id)]: v });
  };
  return <FieldsBox fields={fields} values={values} canEdit err={err} onSave={save} />;
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
