// Table view: every gig of all your workspaces, of one workspace or of one project, as a sortable,
// filterable table with multi-select and bulk actions. Filters run on the server; one workspace is
// sorted and paged there too, all of them are fetched per workspace and merged and sorted here.
// "Include done" is applied here because the list API takes a single category.
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode, type CSSProperties } from "react";
import { Link, useLocation, useSearch } from "wouter";
import { fmtDue } from "../dates";
import { api, fmtTime, hasCap, priorityNames, type Board, type Category, type Tag, type Gig } from "../api";
import { GigPanel } from "../GigPanel";
import { RecurringModal } from "../Recurring";
import { useApp, type WorkspaceTree } from "../App";
import { assignable } from "../fields";
import { CATEGORY_COLOR, colorFor } from "../theme";
import { ClearFilters, Avatar, LabelChip, Modal, PriorityIcon, StatusDot } from "../ui";
import { WorkspaceBadge } from "../WorkspaceBadge";
import { Icon } from "../icons";
import { CATEGORY_LABEL } from "../boardModel";
import { ALL_CATEGORIES, errMsg, isClosed, isOverdue, pool, relTime, gigHref, viewsApi, type GigQuery } from "./viewsApi";
import { compareGigs } from "./tableSort";
import { PlaceFilter } from "../places";
import { parse, PASSTHROUGH, savedViewsApi, summary, toQuery, toSearch, viewHref, type SavedFilter, type SortKey, type TableFilters } from "./savedViews";
import "./views.css";
import { HelpButton } from "../help";

interface Sort {
  key: SortKey;
  desc: boolean;
}

const PAGE = 200;
const MAX_PAGES = 5; // all workspaces: at most this many pages each (1000 gigs), then "+" says there are more

const writableWs = (w: WorkspaceTree) => w.role !== "viewer" && w.role !== "commenter";

// TableView: no ws, every workspace (/table); a ws, that workspace; a pid, that project.
export function TableView({ ws: wsSlug, pid }: { ws?: string; pid?: number }) {
  const { tree, users, members } = useApp();
  const ws = wsSlug ? tree.find((w) => w.slug === wsSlug) : undefined;
  if (wsSlug && !ws) return <p className="danger">No workspace “{wsSlug}”.</p>;
  const project = ws && pid ? ws.projects.find((p) => p.id === pid) : undefined;
  if (ws && pid && !project) return <p className="danger">That project isn't in {ws.name}.</p>;
  const memberIds = [...new Set((ws ? [ws] : tree).flatMap((w) => members.get(w.id) ?? []))];
  return <Table key={`${ws?.id ?? "all"}:${pid ?? ""}`} ws={ws} pid={pid} users={users} memberIds={memberIds} />;
}

function Table({ ws, pid, users, memberIds }: { ws?: WorkspaceTree; pid?: number; users: ReturnType<typeof useApp>["users"]; memberIds: number[] }) {
  const { tree, refresh, me, savedViews, reloadSavedViews, members } = useApp();
  const [loc, navigate] = useLocation();
  const search = useSearch();
  const project = ws && pid ? ws.projects.find((p) => p.id === pid) : undefined;
  const scope = useMemo(() => (ws ? [ws] : tree), [ws, tree]);
  const scopeKey = scope.map((w) => w.slug).join(",");
  const canWrite = scope.some(writableWs);
  const wsOfGig = (t: Gig) => scope.find((w) => w.id === t.workspace_id);
  const canWriteGig = (t: Gig) => {
    const w = wsOfGig(t);
    return !!w && writableWs(w);
  };

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

  const [rows, setRows] = useState<Gig[] | null>(null);
  const [cursor, setCursor] = useState<string | undefined>();
  const [error, setError] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const [capped, setCapped] = useState(false); // all workspaces: some had more than MAX_PAGES pages
  const [labels, setLabels] = useState<Tag[]>([]);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  // a gig opens in the panel over the table (closing it leaves the table as it was); the panel needs
  // its project's statuses and labels
  const [openGig, setOpenGig] = useState<{ pid: number; key: string } | null>(null);
  const [openBoard, setOpenBoard] = useState<Board | null>(null);
  const [recurring, setRecurring] = useState<number | null>(null);
  // a gig opens over the table when its project is in this user's tree; otherwise its link decides
  const open = (t: Gig) => (tree.some((w) => w.projects.some((p) => p.id === t.project_id)) ? setOpenGig({ pid: t.project_id, key: t.key }) : navigate(gigHref(tree, t)));
  // an edit in the panel refreshes just that row (a reload would drop the pages loaded with Load more)
  const refreshRow = (key: string) => {
    const row = rows?.find((r) => r.key === key);
    if (!row) return;
    api
      .gig(row.id)
      .then((t) => setRows((cur) => cur && cur.map((r) => (r.id === t.id ? t : r))))
      .catch(() => setRows((cur) => cur && cur.filter((r) => r.id !== row.id))); // deleted or out of reach
  };
  const openWs = openGig ? tree.find((w) => w.projects.some((p) => p.id === openGig.pid)) : undefined;
  useEffect(() => {
    if (!openGig) return setOpenBoard(null);
    let live = true;
    api
      .board(openGig.pid)
      .then((b) => live && setOpenBoard(b))
      .catch(() => live && navigate(`/board/p/${openGig.pid}/g/${openGig.key}`)); // the board page shows why
    return () => {
      live = false;
    };
  }, [openGig?.pid]); // eslint-disable-line react-hooks/exhaustive-deps

  // debounce the text search
  useEffect(() => {
    const t = setTimeout(() => setQ(text.trim()), 250);
    return () => clearTimeout(t);
  }, [text]);

  // the label filter goes by name, so one entry per name across the workspaces
  useEffect(() => {
    Promise.allSettled(scope.map((w) => api.labels(w.slug))).then((res) => {
      const seen = new Map<string, Tag>();
      for (const r of res) if (r.status === "fulfilled") for (const l of r.value.items) if (!seen.has(l.name.toLowerCase())) seen.set(l.name.toLowerCase(), l);
      setLabels([...seen.values()].sort((a, b) => a.name.localeCompare(b.name)));
    });
  }, [scopeKey]); // eslint-disable-line react-hooks/exhaustive-deps

  const query = useMemo<GigQuery>(
    () => ({
      ...(extra as GigQuery),
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

  const loadSeq = useRef(0);
  const load = useCallback(() => {
    setError("");
    const seq = ++loadSeq.current;
    const done = (items: Gig[], next: string | undefined) => {
      if (seq !== loadSeq.current) return; // a newer load (other filters) is on its way
      setRows(items);
      setCursor(next);
      setSelected((s) => new Set([...s].filter((id) => items.some((t) => t.id === id))));
    };
    if (ws) {
      viewsApi
        .gigs(ws.slug, query)
        .then((r) => done(r.items, r.next_cursor || undefined))
        .catch((e: unknown) => seq === loadSeq.current && setError(errMsg(e)));
      return;
    }
    // every workspace: each one's pages (up to MAX_PAGES), merged and sorted here
    const all = async (w: WorkspaceTree): Promise<{ items: Gig[]; more: boolean }> => {
      const items: Gig[] = [];
      let cur: string | undefined;
      for (let i = 0; i < MAX_PAGES; i++) {
        const r = await viewsApi.gigs(w.slug, cur ? { ...query, cursor: cur } : query);
        items.push(...r.items);
        cur = r.next_cursor || undefined;
        if (!cur) return { items, more: false };
      }
      return { items, more: true };
    };
    Promise.allSettled(scope.map(all)).then((res) => {
      const items: Gig[] = [];
      const bad: string[] = [];
      let more = false;
      res.forEach((r, i) => {
        if (r.status === "fulfilled") {
          items.push(...r.value.items);
          more ||= r.value.more;
        } else bad.push(scope[i]!.name);
      });
      if (seq !== loadSeq.current) return;
      done(items.sort((a, b) => compareGigs(a, b, sort)), undefined);
      setCapped(more);
      if (bad.length) setError(`Couldn't load the gigs of ${bad.join(", ")}.`);
    });
  }, [ws, scopeKey, query, sort]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(load, [load]);

  const more = () => {
    if (!cursor || !ws) return;
    const seq = loadSeq.current; // a filter change meanwhile makes this page stale
    setLoadingMore(true);
    viewsApi
      .gigs(ws.slug, { ...query, cursor })
      .then((r) => {
        if (seq !== loadSeq.current) return;
        setRows((cur) => [...(cur ?? []), ...r.items.filter((t) => !(cur ?? []).some((c) => c.id === t.id))]);
        setCursor(r.next_cursor || undefined);
      })
      .catch((e: unknown) => seq === loadSeq.current && setError(errMsg(e)))
      .finally(() => setLoadingMore(false));
  };

  // an explicit done/cancelled category filter wins over the toggle
  const visible = useMemo(() => (rows ?? []).filter((t) => includeDone || category !== "" || !isClosed(t.status_category)), [rows, includeDone, category]);
  const hidden = (rows?.length ?? 0) - visible.length;
  const projectOf = (id: number) => {
    for (const w of scope) {
      const p = w.projects.find((x) => x.id === id);
      if (p) return p;
    }
    return undefined;
  };
  const candidates = assignable(users, memberIds);

  const selectable = visible.filter(canWriteGig);
  const allOn = selectable.length > 0 && selectable.every((t) => selected.has(t.id));
  const toggleAll = () => setSelected(allOn ? new Set() : new Set(selectable.map((t) => t.id)));
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
  const views = savedViews.filter((v) => v.workspace_id === null || (ws ? v.workspace_id === ws.id : false));
  const here = loc + (search ? "?" + search : "");
  const canSave = hasCap(me, "saved_filters");

  const selectedGigs = visible.filter((t) => selected.has(t.id));
  // bulk assign offers only people who can be assigned in every workspace of the selection
  const selWs = [...new Set(selectedGigs.map((t) => t.workspace_id))];
  const bulkCandidates = candidates.filter((u) => u.is_admin || selWs.every((id) => (members.get(id) ?? []).includes(u.id)));
  const boardHref = project ? `/board?p=${project.id}` : ws ? `/board?ws=${ws.slug}` : "/board";
  // the filter at the top moves between the tables: all, a workspace, a project (same filters kept)
  const place = project ? `p:${project.id}` : ws ? `w:${ws.id}` : "";
  const goPlace = (v: string) => {
    const id = Number(v.slice(2));
    const w = v.startsWith("p:") ? tree.find((x) => x.projects.some((p) => p.id === id)) : tree.find((x) => x.id === id);
    const base = !v || !w ? "/table" : v.startsWith("p:") ? `/w/${w.slug}/p/${id}/table` : `/w/${w.slug}/table`;
    navigate(search ? `${base}?${search}` : base);
  };

  return (
    <div className="vw-page">
      <header className="page-head">
        <div>
          <div className="crumb">
            {ws ? <WorkspaceBadge ws={ws} /> : <span className="pill pill-muted">All workspaces</span>}
            {project && <span className="pill pill-key">{project.key}</span>}
            <span className="muted small">
              {rows === null ? "Loading…" : `${visible.length}${cursor || capped ? "+" : ""} gigs`}
              {hidden > 0 && ` · ${hidden} done hidden`}
            </span>
          </div>
          <h1>
            {project ? `${project.name} · Table` : ws ? `${ws.name} · All gigs` : "All gigs"}
            <HelpButton topic="views" label="Help: tables and saved views" />
          </h1>
        </div>
        <div className="row vw-head-actions">
          <PlaceFilter value={place} onChange={goPlace} tree={tree} />
          <Link href={boardHref} className="button secondary small">
            <Icon name="board" /> Board
          </Link>
        </div>
      </header>

      <div className="vw-filters" role="search" aria-label="Filter gigs">
        <input type="search" className="vw-search" placeholder="Search titles, descriptions, comments…" value={text} onChange={(e) => setText(e.target.value)} aria-label="Search" />
        <select value={category} onChange={(e) => setCategory(e.target.value as Category | "")} aria-label="Status category">
          <option value="">Any status</option>
          {ALL_CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {CATEGORY_LABEL[c]}
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
        <ClearFilters active={filtered} onClear={reset} />
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
          tree={tree}
          query={toQuery(filters, pid)}
          existing={views}
          onClose={() => setSaving(false)}
          onSaved={() => {
            setSaving(false);
            reloadSavedViews();
          }}
        />
      )}

      {canWrite && selectedGigs.length > 0 && (
        <BulkBar
          gigs={selectedGigs}
          labels={labels}
          candidates={bulkCandidates}
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
                  <input type="checkbox" checked={allOn} onChange={toggleAll} aria-label="Select all shown gigs" disabled={selectable.length === 0} />
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
              const href = gigHref(tree, t);
              const on = selected.has(t.id);
              return (
                <tr key={t.id} className={on ? "vw-on" : undefined} data-opens-gig="" onClick={(e) => !(e.target as HTMLElement).closest("a, button, input, label") && open(t)}>
                  {canWrite && (
                    <td className="vw-check">
                      {canWriteGig(t) && <input type="checkbox" checked={on} onChange={() => toggle(t.id)} aria-label={`Select ${t.key}`} />}
                    </td>
                  )}
                  <td className="vw-nowrap vw-key">{t.key}</td>
                  <td className="vw-title">
                    <Link href={href} className="vw-title-link" onClick={(e) => (e.preventDefault(), open(t))} style={t.color ? ({ "--row-c": t.color } as CSSProperties) : undefined}>
                      {t.title}
                    </Link>
                  </td>
                  <td className="vw-nowrap">
                    <StatusDot name={t.status_name} category={t.status_category} />
                  </td>
                  <td className="vw-nowrap">
                    {t.priority > 0 && (
                      <span className="vw-prio">
                        <PriorityIcon priority={t.priority} />
                        {priorityNames[t.priority]}
                      </span>
                    )}
                  </td>
                  <td>
                    <span className="vw-chips">
                      {t.labels.slice(0, 2).map((l) => (
                        <LabelChip key={l.id} label={labels.find((x) => x.id === l.id) ?? l} />
                      ))}
                      {t.labels.length > 2 && (
                        <span className="chip-more" data-tip={t.labels.slice(2).map((l) => l.name).join("\n")}>
                          +{t.labels.length - 2}
                        </span>
                      )}
                    </span>
                  </td>
                  <td>
                    <span className="vw-people">
                      {t.assignees.map((id) => {
                        const u = users.get(id);
                        return u ? <Avatar key={id} user={u} size="xs" /> : null;
                      })}
                    </span>
                  </td>
                  <td className={"vw-nowrap vw-due" + (t.due_at && isOverdue(t) ? " overdue" : "")}>{t.due_at && fmtDue(t.due_at)}</td>
                  {!project && <td className="vw-nowrap">{p && <ProjectPill project={p} />}</td>}
                  <td className="vw-nowrap muted small vw-updated" data-label="Updated" title={fmtTime(t.updated_at)}>
                    {relTime(t.updated_at)}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        {rows !== null && visible.length === 0 && <div className="vw-empty">{filtered ? "No gigs match these filters." : "No open gigs here."}</div>}
        {capped && (
          <div className="vw-more muted small">
            Some workspaces have more than {MAX_PAGES * PAGE} matching gigs; only their first {MAX_PAGES * PAGE} (in this order) are here. Narrow the filters, or open one workspace's table to page through all of it.
          </div>
        )}
        {cursor && (
          <div className="vw-more">
            <button type="button" className="button secondary small" onClick={more} disabled={loadingMore}>
              {loadingMore ? "Loading…" : "Load more"}
            </button>
          </div>
        )}
      </div>
      {openGig && openWs && openBoard && (
        <GigPanel
          gigKey={openGig.key}
          ws={openWs}
          statuses={openBoard.statuses.filter((x) => x.category !== "cancelled")}
          labels={openBoard.labels}
          canWrite={writableWs(openWs)}
          onClose={() => setOpenGig(null)}
          onChanged={() => refreshRow(openGig.key)}
          onMoved={(t) => setOpenGig({ pid: t.project_id, key: t.key })}
          onOpenSchedule={(id) => setRecurring(id)}
        />
      )}
      {recurring !== null && <RecurringModal highlight={recurring} onClose={() => setRecurring(null)} onChanged={load} />}
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
          <p>The saved view goes away; no gigs change.</p>
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

// ws unset: a view across every workspace
function SaveView({ ws, tree, query, existing, onClose, onSaved }: {
  ws?: WorkspaceTree; tree: WorkspaceTree[]; query: Record<string, string>; existing: SavedFilter[]; onClose: () => void; onSaved: () => void;
}) {
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const same = existing.find((v) => v.workspace_id === (ws?.id ?? null) && v.name.trim().toLowerCase() === name.trim().toLowerCase());
  const chips = summary(query);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const body = { workspace_id: ws?.id ?? null, name: name.trim(), query };
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
          {ws ? <WorkspaceBadge ws={ws} /> : <span className="pill pill-muted">All workspaces</span>}
          {query.project && <span className="pill pill-key">{(ws ? [ws] : tree).flatMap((w) => w.projects).find((p) => String(p.id) === query.project)?.key ?? "project"}</span>}
          {chips.length ? chips.map((c) => <span key={c} className="pill pill-muted">{c}</span>) : <span className="muted small">no filters: every open gig</span>}
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
    <span className="proj-tag" title={project.name}>
      <span className="dot" style={{ background: colorFor(project.key, project.color) }} aria-hidden="true" />
      {project.key}
    </span>
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

function BulkBar({ gigs, labels, candidates, onClear, onDone }: {
  gigs: Gig[]; labels: Tag[]; candidates: ReturnType<typeof assignable>; onClear: () => void; onDone: () => void;
}) {
  const [progress, setProgress] = useState<Progress | null>(null);
  const [labelText, setLabelText] = useState("");
  const [user, setUser] = useState(0);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const busy = progress?.running ?? false;

  const run = (op: Op, title: string) => {
    const batch = [...gigs];
    setProgress({ title, total: batch.length, done: 0, errors: [], running: true });
    void pool(batch, 4, (t) => apply(t, op), (t, err) =>
      setProgress((p) => p && { ...p, done: p.done + 1, errors: err ? [...p.errors, { key: t.key, msg: err }] : p.errors }),
    ).then(() => {
      setProgress((p) => p && { ...p, running: false });
      onDone();
    });
  };

  const n = gigs.length;
  return (
    <div className="vw-bulk" role="region" aria-label="Bulk actions">
      <span className="count big">{n}</span>
      <span className="vw-bulk-label">selected</span>
      <Act label="Move to">
        <select value="" disabled={busy} aria-label="Move selected to status category" onChange={(e) => e.target.value && run({ kind: "move", category: e.target.value as Category }, `Moving to ${CATEGORY_LABEL[e.target.value as Category] ?? e.target.value}`)}>
          <option value="">Status…</option>
          {ALL_CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {CATEGORY_LABEL[c]}
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
        <Modal title={`Delete ${n} gig${n === 1 ? "" : "s"}?`} onClose={() => setConfirmDelete(false)}>
          <p>They move to the trash and can be restored from there.</p>
          <div className="vw-chips vw-confirm-keys">
            {gigs.slice(0, 30).map((t) => (
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

// apply runs one bulk op on one gig. Label and assignee changes re-read the gig first so a
// concurrent edit isn't overwritten with the table's stale copy.
async function apply(t: Gig, op: Op): Promise<unknown> {
  switch (op.kind) {
    case "move":
      return t.status_category === op.category ? undefined : api.moveGig(t.id, { category: op.category });
    case "priority":
      return t.priority === op.priority ? undefined : api.patchGig(t.id, { priority: op.priority });
    case "delete":
      return api.deleteGig(t.id);
    case "label": {
      const cur = await api.gig(t.id);
      const names = cur.labels.map((l) => l.name);
      if (names.some((n) => n.toLowerCase() === op.name.toLowerCase())) return undefined;
      return api.patchGig(t.id, { labels: [...names, op.name] });
    }
    case "assign":
    case "unassign": {
      const cur = await api.gig(t.id);
      const has = cur.assignees.includes(op.user);
      if (op.kind === "assign" ? has : !has) return undefined;
      return api.patchGig(t.id, { assignees: op.kind === "assign" ? [...cur.assignees, op.user] : cur.assignees.filter((id) => id !== op.user) });
    }
  }
}
