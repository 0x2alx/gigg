// Table view: every task of a workspace (or one project) as a sortable, filterable table with
// multi-select and bulk actions. Sorting and most filters run on the server; "include done" is
// applied here because the list API takes a single category.
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react";
import { Link, useLocation, useSearch } from "wouter";
import { fmtDue } from "../dates";
import { api, fmtTime, hasCap, priorityNames, type Category, type Tag, type Task } from "../api";
import { useApp, type WorkspaceTree } from "../App";
import { assignable } from "../fields";
import { CATEGORY_COLOR, colorFor } from "../theme";
import { Avatar, LabelChip, Modal, Pill, PriorityBadge, StatusBadge } from "../ui";
import { ALL_CATEGORIES, errMsg, isClosed, isOverdue, pool, relTime, taskHref, viewsApi, type TaskQuery } from "./viewsApi";
import { parse, PASSTHROUGH, savedViewsApi, summary, toQuery, toSearch, viewHref, type SavedFilter, type SortKey, type TableFilters } from "./savedViews";
import "./views.css";
import { HelpButton } from "../help";

interface Sort {
  key: SortKey;
  desc: boolean;
}

const PAGE = 200;

export function TableView({ ws: wsSlug, pid }: { ws: string; pid?: number }) {
  const { tree, users, members } = useApp();
  const ws = tree.find((w) => w.slug === wsSlug);
  if (!ws) return <p className="danger">No workspace “{wsSlug}”.</p>;
  const project = pid ? ws.projects.find((p) => p.id === pid) : undefined;
  if (pid && !project) return <p className="danger">That project isn't in {ws.name}.</p>;
  return <Table key={`${ws.id}:${pid ?? ""}`} ws={ws} pid={pid} users={users} memberIds={members.get(ws.id) ?? []} />;
}

function Table({ ws, pid, users, memberIds }: { ws: WorkspaceTree; pid?: number; users: ReturnType<typeof useApp>["users"]; memberIds: number[] }) {
  const { tree, refresh, me, savedViews, reloadSavedViews } = useApp();
  const [loc, navigate] = useLocation();
  const search = useSearch();
  const project = pid ? ws.projects.find((p) => p.id === pid) : undefined;
  const canWrite = ws.role !== "viewer" && ws.role !== "commenter";

  // the filters live in the URL (shareable, and what saved views link to)
  const initial = useMemo(() => parse(new URLSearchParams(search), me.user.id).filters, []); // eslint-disable-line react-hooks/exhaustive-deps
  const [sort, setSort] = useState<Sort>(initial.sort);
  const [text, setText] = useState(initial.q);
  const [q, setQ] = useState(initial.q);
  const [category, setCategory] = useState<Category | "">(initial.category);
  const [label, setLabel] = useState(initial.label);
  const [assignee, setAssignee] = useState(initial.assignee);
  const [prioMin, setPrioMin] = useState(initial.prioMin);
  const [includeDone, setIncludeDone] = useState(initial.includeDone);
  const [extra, setExtra] = useState<Record<string, string>>(initial.extra);
  const [saving, setSaving] = useState(false);

  const filters = useMemo<TableFilters>(
    () => ({ q, category, label, assignee, prioMin, sort, includeDone, extra }),
    [q, category, label, assignee, prioMin, sort, includeDone, extra],
  );
  const written = useRef(search);
  // URL -> state when the search changes from outside (a saved view, back/forward)
  useEffect(() => {
    if (search === written.current) return;
    written.current = search;
    const f = parse(new URLSearchParams(search), me.user.id).filters;
    setSort(f.sort);
    setText(f.q);
    setQ(f.q);
    setCategory(f.category);
    setLabel(f.label);
    setAssignee(f.assignee);
    setPrioMin(f.prioMin);
    setIncludeDone(f.includeDone);
    setExtra(f.extra);
  }, [search, me.user.id]);
  // state -> URL (replace, so filter tweaks don't pile up in the history)
  useEffect(() => {
    const s = toSearch(filters);
    if (s === written.current) return;
    written.current = s;
    navigate(s ? `${loc}?${s}` : loc, { replace: true });
  }, [filters]); // eslint-disable-line react-hooks/exhaustive-deps

  const [rows, setRows] = useState<Task[] | null>(null);
  const [cursor, setCursor] = useState<string | undefined>();
  const [error, setError] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const [labels, setLabels] = useState<Tag[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());

  // debounce the text search
  useEffect(() => {
    const t = setTimeout(() => setQ(text.trim()), 250);
    return () => clearTimeout(t);
  }, [text]);

  useEffect(() => {
    api
      .labels(ws.slug)
      .then((r) => setLabels(r.items))
      .catch(() => setLabels([]));
  }, [ws.slug]);

  const query = useMemo<TaskQuery>(
    () => ({
      ...(extra as TaskQuery),
      project: pid,
      category,
      label,
      assignee,
      priority_min: prioMin,
      q,
      sort: (sort.desc ? "-" : "") + sort.key,
      limit: PAGE,
    }),
    [pid, category, label, assignee, prioMin, q, sort, extra],
  );

  const load = useCallback(() => {
    setError("");
    viewsApi
      .tasks(ws.slug, query)
      .then((r) => {
        setRows(r.items);
        setCursor(r.next_cursor || undefined);
        setSelected((s) => new Set([...s].filter((id) => r.items.some((t) => t.id === id))));
      })
      .catch((e: unknown) => setError(errMsg(e)));
  }, [ws.slug, query]);
  useEffect(load, [load]);

  const more = () => {
    if (!cursor) return;
    setLoadingMore(true);
    viewsApi
      .tasks(ws.slug, { ...query, cursor })
      .then((r) => {
        setRows((cur) => [...(cur ?? []), ...r.items.filter((t) => !(cur ?? []).some((c) => c.id === t.id))]);
        setCursor(r.next_cursor || undefined);
      })
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setLoadingMore(false));
  };

  // an explicit done/cancelled category filter wins over the toggle
  const visible = useMemo(() => (rows ?? []).filter((t) => includeDone || category !== "" || !isClosed(t.status_category)), [rows, includeDone, category]);
  const hidden = (rows?.length ?? 0) - visible.length;
  const projectOf = (id: number) => ws.projects.find((p) => p.id === id);
  const candidates = assignable(users, memberIds);

  const allOn = visible.length > 0 && visible.every((t) => selected.has(t.id));
  const toggleAll = () => setSelected(allOn ? new Set() : new Set(visible.map((t) => t.id)));
  const toggle = (id: number) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });

  const clickSort = (key: SortKey) => setSort((s) => (s.key === key ? { key, desc: !s.desc } : { key, desc: key === "updated" || key === "created" || key === "priority" }));
  const filtered = text !== "" || category !== "" || label !== "" || assignee !== 0 || prioMin !== 0 || Object.keys(extra).length > 0;
  const reset = () => {
    setText("");
    setCategory("");
    setLabel("");
    setAssignee(0);
    setPrioMin(0);
    setExtra({});
  };
  const views = savedViews.filter((v) => v.workspace_id === ws.id || v.workspace_id === null);
  const here = loc + (search ? "?" + search : "");
  const canSave = hasCap(me, "saved_filters");

  const selectedTasks = visible.filter((t) => selected.has(t.id));
  const boardHref = project ? `/w/${ws.slug}/p/${project.id}` : null;

  return (
    <div className="vw-page">
      <header className="page-head">
        <div>
          <div className="crumb">
            <span className="pill pill-ws">{ws.name}</span>
            {project && <span className="pill pill-key">{project.key}</span>}
            <span className="muted small">
              {rows === null ? "Loading…" : `${visible.length}${cursor ? "+" : ""} tasks`}
              {hidden > 0 && ` · ${hidden} done hidden`}
            </span>
          </div>
          <h1>
            {project ? `${project.name} · Table` : `${ws.name} · All tasks`}
            <HelpButton topic="views" label="Help: tables and saved views" />
          </h1>
        </div>
        <div className="row vw-head-actions">
          {project ? (
            <>
              <Link href={`/w/${ws.slug}/table`} className="button secondary small">
                Whole workspace
              </Link>
              <Link href={boardHref!} className="button secondary small">
                ▦ Board
              </Link>
            </>
          ) : (
            ws.projects.length > 0 && (
              <select aria-label="Limit to a project" value="" onChange={(e) => e.target.value && navigate(`/w/${ws.slug}/p/${e.target.value}/table`)}>
                <option value="">Limit to project…</option>
                {ws.projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.key} · {p.name}
                  </option>
                ))}
              </select>
            )
          )}
        </div>
      </header>

      <div className="vw-filters" role="search" aria-label="Filter tasks">
        <input type="search" className="vw-search" placeholder="Search titles, descriptions, comments…" value={text} onChange={(e) => setText(e.target.value)} aria-label="Search" />
        <select value={category} onChange={(e) => setCategory(e.target.value as Category | "")} aria-label="Status category">
          <option value="">Any status</option>
          {ALL_CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {cap(c)}
            </option>
          ))}
        </select>
        <select value={label} onChange={(e) => setLabel(e.target.value)} aria-label="Label">
          <option value="">Any label</option>
          {labels.map((l) => (
            <option key={l.id} value={l.name}>
              {l.name}
            </option>
          ))}
        </select>
        <select value={assignee} onChange={(e) => setAssignee(Number(e.target.value))} aria-label="Assignee">
          <option value={0}>Anyone</option>
          {candidates.map((u) => (
            <option key={u.id} value={u.id}>
              {u.display_name || u.username}
              {u.kind === "agent" ? " (agent)" : ""}
            </option>
          ))}
        </select>
        <select value={prioMin} onChange={(e) => setPrioMin(Number(e.target.value))} aria-label="Minimum priority">
          <option value={0}>Any priority</option>
          {priorityNames.slice(1).map((n, i) => (
            <option key={n} value={i + 1}>
              {n}+
            </option>
          ))}
        </select>
        <label className="vw-toggle">
          <input type="checkbox" checked={includeDone} onChange={(e) => setIncludeDone(e.target.checked)} disabled={category !== ""} />
          Include done
        </label>
        {PASSTHROUGH.filter((k) => extra[k]).map((k) => (
          <span key={k} className="pill pill-muted" title="Set by a saved view">
            {k.replace("_", " ")}: {extra[k]}
          </span>
        ))}
        {filtered && (
          <button type="button" className="link small" onClick={reset}>
            Clear filters
          </button>
        )}
        {canSave && (
          <button type="button" className="button secondary small vw-save" onClick={() => setSaving(true)}>
            ★ Save view
          </button>
        )}
      </div>

      {views.length > 0 && (
        <div className="vw-views" role="group" aria-label="Saved views">
          {views.map((v) => (
            <ViewChip key={v.id} view={v} href={viewHref(tree, v, me.user.id)} active={viewHref(tree, v, me.user.id) === here} onDeleted={reloadSavedViews} />
          ))}
        </div>
      )}
      {saving && (
        <SaveView
          ws={ws}
          query={toQuery(filters, pid)}
          existing={views}
          onClose={() => setSaving(false)}
          onSaved={() => {
            setSaving(false);
            reloadSavedViews();
          }}
        />
      )}

      {canWrite && selectedTasks.length > 0 && (
        <BulkBar
          tasks={selectedTasks}
          labels={labels}
          candidates={candidates}
          onClear={() => setSelected(new Set())}
          onDone={() => {
            load();
            refresh();
          }}
        />
      )}

      {error && (
        <p className="danger" role="alert">
          {error}
        </p>
      )}

      <div className="card vw-table-card">
        <table className="vw-table">
          <thead>
            <tr>
              {canWrite && (
                <th className="vw-check">
                  <input type="checkbox" checked={allOn} onChange={toggleAll} aria-label="Select all shown tasks" disabled={visible.length === 0} />
                </th>
              )}
              <SortTh label="Key" k="number" sort={sort} onSort={clickSort} />
              <SortTh label="Title" k="title" sort={sort} onSort={clickSort} />
              <th>Status</th>
              <SortTh label="Priority" k="priority" sort={sort} onSort={clickSort} />
              <th>Labels</th>
              <th>Assignees</th>
              <SortTh label="Due" k="due" sort={sort} onSort={clickSort} />
              {!project && <th>Project</th>}
              <SortTh label="Updated" k="updated" sort={sort} onSort={clickSort} />
            </tr>
          </thead>
          <tbody>
            {visible.map((t) => {
              const p = projectOf(t.project_id);
              const href = taskHref(tree, t);
              const on = selected.has(t.id);
              return (
                <tr key={t.id} className={on ? "vw-on" : undefined} onClick={(e) => !(e.target as HTMLElement).closest("a, button, input, label") && navigate(href)}>
                  {canWrite && (
                    <td className="vw-check">
                      <input type="checkbox" checked={on} onChange={() => toggle(t.id)} aria-label={`Select ${t.key}`} />
                    </td>
                  )}
                  <td className="vw-nowrap">
                    <span className="pill pill-key">{t.key}</span>
                  </td>
                  <td className="vw-title">
                    <Link href={href} className="vw-title-link" style={t.color ? { borderLeftColor: t.color } : undefined}>
                      {t.title}
                    </Link>
                  </td>
                  <td className="vw-nowrap">
                    <StatusBadge name={t.status_name} category={t.status_category} />
                  </td>
                  <td className="vw-nowrap">
                    <PriorityBadge priority={t.priority} />
                  </td>
                  <td>
                    <span className="vw-chips">
                      {t.labels.map((l) => (
                        <LabelChip key={l.id} label={labels.find((x) => x.id === l.id) ?? l} />
                      ))}
                    </span>
                  </td>
                  <td>
                    <span className="vw-people">
                      {t.assignees.map((id) => {
                        const u = users.get(id);
                        return u ? <Avatar key={id} user={u} /> : null;
                      })}
                    </span>
                  </td>
                  <td className="vw-nowrap">{t.due_at && <span className={"pill " + (isOverdue(t) ? "pill-danger" : "pill-muted")}>⏱ {fmtDue(t.due_at)}</span>}</td>
                  {!project && <td className="vw-nowrap">{p && <ProjectPill project={p} />}</td>}
                  <td className="vw-nowrap muted small vw-updated" data-label="Updated" title={fmtTime(t.updated_at)}>
                    {relTime(t.updated_at)}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        {rows !== null && visible.length === 0 && <div className="vw-empty">{filtered ? "No tasks match these filters." : "No open tasks here."}</div>}
        {cursor && (
          <div className="vw-more">
            <button type="button" className="button secondary small" onClick={more} disabled={loadingMore}>
              {loadingMore ? "Loading…" : "Load more"}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

function ViewChip({ view, href, active, onDeleted }: { view: SavedFilter; href: string; active: boolean; onDeleted: () => void }) {
  const [confirm, setConfirm] = useState(false);
  const [error, setError] = useState("");
  const chips = summary(view.query);
  return (
    <span className={"vw-view" + (active ? " on" : "")}>
      <Link href={href} className="vw-view-link" aria-current={active ? "page" : undefined} title={chips.join(" · ") || "No filters"}>
        ★ {view.name}
      </Link>
      <button type="button" className="vw-view-x" aria-label={`Delete saved view ${view.name}`} onClick={() => setConfirm(true)}>
        ×
      </button>
      {confirm && (
        <Modal title={`Delete “${view.name}”?`} onClose={() => setConfirm(false)}>
          <p>The saved view goes away; no tasks change.</p>
          {error && (
            <p className="danger" role="alert">
              {error}
            </p>
          )}
          <div className="row end">
            <button type="button" className="button secondary" onClick={() => setConfirm(false)}>
              Cancel
            </button>
            <button
              type="button"
              className="button vw-danger-solid"
              onClick={() =>
                savedViewsApi
                  .remove(view.id)
                  .then(() => {
                    setConfirm(false);
                    onDeleted();
                  })
                  .catch((e: unknown) => setError(errMsg(e)))
              }
            >
              Delete
            </button>
          </div>
        </Modal>
      )}
    </span>
  );
}

function SaveView({ ws, query, existing, onClose, onSaved }: {
  ws: WorkspaceTree; query: Record<string, string>; existing: SavedFilter[]; onClose: () => void; onSaved: () => void;
}) {
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const same = existing.find((v) => v.workspace_id === ws.id && v.name.trim().toLowerCase() === name.trim().toLowerCase());
  const chips = summary(query);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const body = { workspace_id: ws.id, name: name.trim(), query };
    setBusy(true);
    (same ? savedViewsApi.update(same.id, body) : savedViewsApi.create(body))
      .then(onSaved)
      .catch((err: unknown) => setError(errMsg(err)))
      .finally(() => setBusy(false));
  };
  return (
    <Modal title="Save view" onClose={onClose}>
      <form className="form-grid" onSubmit={submit}>
        <label className="span2">
          Name
          <input value={name} required maxLength={60} placeholder="e.g. Urgent ops" onChange={(e) => setName(e.target.value)} />
        </label>
        <div className="span2 vw-chips">
          <span className="pill pill-ws">{ws.name}</span>
          {query.project && <span className="pill pill-key">{ws.projects.find((p) => String(p.id) === query.project)?.key ?? "project"}</span>}
          {chips.length ? chips.map((c) => <span key={c} className="pill pill-muted">{c}</span>) : <span className="muted small">no filters: every open task</span>}
          {query.sort && <span className="pill pill-muted">sort {query.sort}</span>}
        </div>
        {same && <p className="muted small span2">Replaces your saved view “{same.name}”.</p>}
        {error && (
          <p className="danger span2" role="alert">
            {error}
          </p>
        )}
        <div className="row end span2">
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="button" disabled={busy || !name.trim()}>
            {same ? "Replace" : "Save"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function SortTh({ label, k, sort, onSort }: { label: string; k: SortKey; sort: Sort; onSort: (k: SortKey) => void }) {
  const on = sort.key === k;
  return (
    <th aria-sort={on ? (sort.desc ? "descending" : "ascending") : "none"}>
      <button type="button" className={"vw-sort" + (on ? " on" : "")} onClick={() => onSort(k)}>
        {label}
        <span className="vw-sort-arrow" aria-hidden="true">
          {on ? (sort.desc ? "▼" : "▲") : "↕"}
        </span>
      </button>
    </th>
  );
}

export function ProjectPill({ project }: { project: { key: string; name: string; color: string | null } }) {
  return (
    <Pill color={colorFor(project.key, project.color)} title={project.name}>
      {project.key}
    </Pill>
  );
}

// ---- bulk actions ----

type Op =
  | { kind: "move"; category: Category }
  | { kind: "priority"; priority: number }
  | { kind: "label"; name: string }
  | { kind: "assign"; user: number }
  | { kind: "unassign"; user: number }
  | { kind: "delete" };

interface Progress {
  title: string;
  total: number;
  done: number;
  errors: { key: string; msg: string }[];
  running: boolean;
}

function BulkBar({ tasks, labels, candidates, onClear, onDone }: {
  tasks: Task[]; labels: Tag[]; candidates: ReturnType<typeof assignable>; onClear: () => void; onDone: () => void;
}) {
  const [progress, setProgress] = useState<Progress | null>(null);
  const [labelText, setLabelText] = useState("");
  const [user, setUser] = useState(0);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const busy = progress?.running ?? false;

  const run = (op: Op, title: string) => {
    const batch = [...tasks];
    setProgress({ title, total: batch.length, done: 0, errors: [], running: true });
    void pool(batch, 4, (t) => apply(t, op), (t, err) =>
      setProgress((p) => p && { ...p, done: p.done + 1, errors: err ? [...p.errors, { key: t.key, msg: err }] : p.errors }),
    ).then(() => {
      setProgress((p) => p && { ...p, running: false });
      onDone();
    });
  };

  const n = tasks.length;
  return (
    <div className="vw-bulk" role="region" aria-label="Bulk actions">
      <span className="count big">{n}</span>
      <span className="vw-bulk-label">selected</span>
      <Act label="Move to">
        <select value="" disabled={busy} aria-label="Move selected to status category" onChange={(e) => e.target.value && run({ kind: "move", category: e.target.value as Category }, `Moving to ${e.target.value}`)}>
          <option value="">Status…</option>
          {ALL_CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {cap(c)}
            </option>
          ))}
        </select>
      </Act>
      <Act label="Priority">
        <select value="" disabled={busy} aria-label="Set priority of selected" onChange={(e) => e.target.value !== "" && run({ kind: "priority", priority: Number(e.target.value) }, `Setting priority ${priorityNames[Number(e.target.value)]}`)}>
          <option value="">Priority…</option>
          {priorityNames.map((p, i) => (
            <option key={p} value={i}>
              {p}
            </option>
          ))}
        </select>
      </Act>
      <form
        className="vw-act"
        onSubmit={(e) => {
          e.preventDefault();
          const name = labelText.trim();
          if (!name) return;
          run({ kind: "label", name }, `Adding label ${name}`);
          setLabelText("");
        }}
      >
        <input list="vw-bulk-labels" className="vw-bulk-input" placeholder="Add label…" value={labelText} onChange={(e) => setLabelText(e.target.value)} disabled={busy} aria-label="Label to add to selected" />
        <datalist id="vw-bulk-labels">
          {labels.map((l) => (
            <option key={l.id} value={l.name} />
          ))}
        </datalist>
        <button type="submit" className="button small" disabled={busy || !labelText.trim()}>
          Add
        </button>
      </form>
      <span className="vw-act">
        <select value={user} onChange={(e) => setUser(Number(e.target.value))} disabled={busy} aria-label="User to assign or unassign">
          <option value={0}>User…</option>
          {candidates.map((u) => (
            <option key={u.id} value={u.id}>
              {u.display_name || u.username}
            </option>
          ))}
        </select>
        <button type="button" className="button small" disabled={busy || !user} onClick={() => run({ kind: "assign", user }, "Assigning")}>
          Assign
        </button>
        <button type="button" className="button secondary small" disabled={busy || !user} onClick={() => run({ kind: "unassign", user }, "Unassigning")}>
          Unassign
        </button>
      </span>
      <button type="button" className="button danger-button small" disabled={busy} onClick={() => setConfirmDelete(true)}>
        🗑 Delete
      </button>
      <button type="button" className="link small vw-bulk-clear" onClick={onClear} disabled={busy}>
        Clear selection
      </button>

      {progress && <ProgressLine p={progress} onDismiss={() => setProgress(null)} />}

      {confirmDelete && (
        <Modal title={`Delete ${n} task${n === 1 ? "" : "s"}?`} onClose={() => setConfirmDelete(false)}>
          <p>They move to the trash and can be restored from there.</p>
          <div className="vw-chips vw-confirm-keys">
            {tasks.slice(0, 30).map((t) => (
              <span key={t.id} className="pill pill-key">
                {t.key}
              </span>
            ))}
            {n > 30 && <span className="muted small">and {n - 30} more</span>}
          </div>
          <div className="row end">
            <button type="button" className="button secondary" onClick={() => setConfirmDelete(false)}>
              Cancel
            </button>
            <button
              type="button"
              className="button vw-danger-solid"
              onClick={() => {
                setConfirmDelete(false);
                run({ kind: "delete" }, "Deleting");
              }}
            >
              Delete
            </button>
          </div>
        </Modal>
      )}
    </div>
  );
}

function Act({ label, children }: { label: string; children: ReactNode }) {
  return (
    <span className="vw-act" title={label}>
      {children}
    </span>
  );
}

function ProgressLine({ p, onDismiss }: { p: Progress; onDismiss: () => void }) {
  const pct = p.total ? Math.round((p.done / p.total) * 100) : 100;
  return (
    <div className="vw-progress" role="status" aria-live="polite">
      <div className="vw-progress-head">
        <span>
          {p.title}: {p.done}/{p.total}
          {!p.running && (p.errors.length ? ` · ${p.errors.length} failed` : " · done")}
        </span>
        {!p.running && (
          <button type="button" className="icon-button" onClick={onDismiss} aria-label="Dismiss">
            ✕
          </button>
        )}
      </div>
      <div className="vw-bar" role="progressbar" aria-valuemin={0} aria-valuemax={p.total} aria-valuenow={p.done}>
        <span style={{ width: `${pct}%`, background: p.errors.length ? "var(--danger)" : CATEGORY_COLOR.done }} />
      </div>
      {p.errors.length > 0 && (
        <ul className="vw-errors">
          {p.errors.map((e) => (
            <li key={e.key}>
              <span className="pill pill-key">{e.key}</span> <span className="danger">{e.msg}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// apply runs one bulk op on one task. Label and assignee changes re-read the task first so a
// concurrent edit isn't overwritten with the table's stale copy.
async function apply(t: Task, op: Op): Promise<unknown> {
  switch (op.kind) {
    case "move":
      return t.status_category === op.category ? undefined : api.moveTask(t.id, { category: op.category });
    case "priority":
      return t.priority === op.priority ? undefined : api.patchTask(t.id, { priority: op.priority });
    case "delete":
      return api.deleteTask(t.id);
    case "label": {
      const cur = await api.task(t.id);
      const names = cur.labels.map((l) => l.name);
      if (names.some((n) => n.toLowerCase() === op.name.toLowerCase())) return undefined;
      return api.patchTask(t.id, { labels: [...names, op.name] });
    }
    case "assign":
    case "unassign": {
      const cur = await api.task(t.id);
      const has = cur.assignees.includes(op.user);
      if (op.kind === "assign" ? has : !has) return undefined;
      return api.patchTask(t.id, { assignees: op.kind === "assign" ? [...cur.assignees, op.user] : cur.assignees.filter((id) => id !== op.user) });
    }
  }
}

function cap(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
