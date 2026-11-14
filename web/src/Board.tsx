import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type DragEvent } from "react";
import { Link, useLocation, useSearch } from "wouter";
import { api, ApiError, fmtTime, hasCap, priorityNames, type Board, type Category, type CustomField, type Status, type Tag, type Task } from "./api";
import { fmtDue } from "./dates";
import { useApp, type WorkspaceTree } from "./App";
import {
  activeFilters,
  CATEGORY_LABEL,
  categoryColumns,
  EMPTY_FILTER,
  filterQuery,
  isFiltered,
  matches,
  moveToCategory,
  parseFilter,
  scope,
  statusColumns,
  type BoardColumn,
  type BoardFilter,
} from "./boardModel";
import { loadCollapsed, saveCollapsed, toggleId } from "./collapse";
import { applyMove, reorderIndex, type MoveBody } from "./dnd";
import { useNarrow } from "./narrow";
import { setMentionUsers } from "./markdown";
import { RecurringModal } from "./Recurring";
import { SchedulesModal } from "./Schedules";
import { TemplatesModal } from "./Templates";
import { TaskModal } from "./TaskModal";
import { TaskPanel } from "./TaskPanel";
import { viewsApi } from "./views/viewsApi";
import { CATEGORY_COLOR, colorFor } from "./theme";
import { Avatar, LabelChip, Modal, MoreMenu, PriorityIcon, TaskBadges } from "./ui";
import { WorkspaceBadge } from "./WorkspaceBadge";
import { HelpButton } from "./help";
import { Icon } from "./icons";
import { cardState, elapsed, plainText } from "./cardState";



const openStatuses = (b: Board): Status[] => b.statuses.filter((s) => s.category !== "cancelled");
const canWriteIn = (ws: WorkspaceTree) => ws.role !== "viewer" && ws.role !== "commenter";

type ProjectAction = "new" | "recurring" | "templates" | "archive";

// BoardPage: one board over every workspace and project in reach, narrowed by the filter in the URL
// (/board?ws=..&p=..&a=..&l=..&pr=..&q=..&arch=1). One project in view: its own status columns and
// manual order. Several: one column per category, ordered by priority and due date.
export function BoardPage({ taskPid, taskKey }: { taskPid?: number; taskKey?: string }) {
  const { tree, refresh, me, users, members, prefs, setPref } = useApp();
  setMentionUsers(users);
  const search = useSearch();
  const [loc, navigate] = useLocation();
  const filter = useMemo(() => parseFilter(search), [search]);
  const qs = filterQuery(filter);
  const boardURL = `/board${qs}`;
  const setFilter = useCallback(
    (f: BoardFilter) => {
      const q = filterQuery(f);
      navigate(loc + q, { replace: true });
      setPref("board_filter", q);
    },
    [loc, navigate, setPref],
  );
  // a visit without a filter brings back the last one used (the URL stays shareable)
  const restored = useRef(false);
  useEffect(() => {
    if (restored.current) return;
    restored.current = true;
    const saved = typeof prefs.board_filter === "string" ? prefs.board_filter : "";
    if (!search && saved.startsWith("?")) navigate(loc + saved, { replace: true });
  }, [search, prefs.board_filter, loc, navigate]);

  const inScope = useMemo(() => scope(tree, filter), [tree, filter]);
  const single = inScope.length === 1 ? inScope[0]! : null;
  const scopeIds = useMemo(() => inScope.map((x) => x.project.id), [inScope]);
  const wsInScope = useMemo(() => [...new Map(inScope.map((x) => [x.ws.id, x.ws])).values()], [inScope]);
  const projOf = useCallback(
    (pid: number) => {
      for (const ws of tree) {
        const project = ws.projects.find((p) => p.id === pid);
        if (project) return { ws, project };
      }
      return null;
    },
    [tree],
  );

  const [boards, setBoards] = useState<Map<number, Board>>(new Map());
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState<string[]>([]);
  const [archived, setArchived] = useState<Task[]>([]);
  const [fields, setFields] = useState<Map<number, CustomField[]>>(new Map());
  const [toast, setToast] = useState("");
  const [moveErr, setMoveErr] = useState("");
  const [drag, setDrag] = useState<number | null>(null);
  const [drop, setDrop] = useState<{ col: string; index: number } | null>(null);
  const [activeCol, setActiveCol] = useState<string | null>(null);
  const [pick, setPick] = useState<ProjectAction | "schedules" | null>(null);
  const [adding, setAdding] = useState<number | null>(null);
  // a column's "+": the new task starts in that column (its status, or its stage's first status in the picked project)
  const [newIn, setNewIn] = useState<{ category: Category; statusId?: number } | null>(null);
  const [recurring, setRecurring] = useState<{ pid: number; highlight?: number } | null>(null);
  const [templates, setTemplates] = useState<number | null>(null);
  const [archiveDone, setArchiveDone] = useState<number[] | null>(null);
  const [timings, setTimings] = useState<number | null>(null);
  const [trash, setTrash] = useState(false);
  const narrow = useNarrow();

  // the boards to load: the projects in view, plus the open task's project
  const wantKey = [...scopeIds, ...(taskPid && !scopeIds.includes(taskPid) ? [taskPid] : [])].join(",");
  const loadSeq = useRef(0);
  const load = useCallback(() => {
    const ids = wantKey ? wantKey.split(",").map(Number) : [];
    const seq = ++loadSeq.current;
    Promise.allSettled(ids.map((id) => api.board(id))).then((res) => {
      if (seq !== loadSeq.current) return; // a newer load (another filter) is on its way
      const m = new Map<number, Board>();
      const bad: string[] = [];
      res.forEach((r, i) => {
        if (r.status === "fulfilled") m.set(ids[i]!, r.value);
        else bad.push(projOf(ids[i]!)?.project.name ?? `project ${ids[i]}`);
      });
      setBoards(m);
      setFailed(bad);
      setLoaded(true);
    });
  }, [wantKey, projOf]);
  useEffect(load, [load]);
  // archived tasks: the board endpoint hides them, so the toggle lists them separately
  const archKey = filter.arch ? scopeIds.join(",") : "";
  useEffect(() => {
    if (!archKey) {
      setArchived([]);
      return;
    }
    let live = true;
    Promise.allSettled(archKey.split(",").map((id) => api.projectTasks(Number(id), { archived: "only", limit: 500 }))).then((res) => {
      if (live) setArchived(res.flatMap((r) => (r.status === "fulfilled" ? r.value.items.filter((t) => !t.deleted) : [])));
    });
    return () => {
      live = false;
    };
  }, [archKey, boards]);
  const wsKey = wsInScope.map((w) => w.id).join(",");
  useEffect(() => {
    let live = true;
    const ids = wsKey ? wsKey.split(",").map(Number) : [];
    Promise.allSettled(ids.map((id) => api.fields(id))).then((res) => {
      if (!live) return;
      const m = new Map<number, CustomField[]>();
      res.forEach((r, i) => m.set(ids[i]!, r.status === "fulfilled" ? r.value.items : []));
      setFields(m);
    });
    return () => {
      live = false;
    };
  }, [wsKey]);

  // live refresh: reload (debounced) on activity in a workspace in view, never mid-drag
  const dragRef = useRef<number | null>(null);
  const dragTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  dragRef.current = drag;
  const loadRef = useRef(load);
  loadRef.current = load;
  const movesInFlight = useRef(0);
  // patchTask: put a freshly fetched task in place on the board (same status: same column, same order)
  const patchRef = useRef((t: Task) => {
    void t;
  });
  patchRef.current = (t: Task) =>
    setBoards((cur) => {
      const b = cur.get(t.project_id);
      if (!b || !b.statuses.some((s) => s.tasks.some((x) => x.id === t.id && x.status_id === t.status_id))) return cur;
      const nb = { ...b, statuses: b.statuses.map((s) => ({ ...s, tasks: s.tasks.map((x) => (x.id === t.id ? t : x)) })) };
      return new Map(cur).set(t.project_id, nb);
    });
  useEffect(() => {
    const ids = new Set(wsKey ? wsKey.split(",").map(Number) : []);
    if (taskPid) {
      const w = projOf(taskPid)?.ws.id;
      if (w) ids.add(w);
    }
    let timer: ReturnType<typeof setTimeout> | null = null;
    const fire = () => {
      // never mid-drag, nor while a move is on its way (a reload answered first would undo it on screen)
      if (dragRef.current !== null || movesInFlight.current > 0) {
        timer = setTimeout(fire, 400);
        return;
      }
      timer = null;
      loadRef.current();
    };
    // progress of a running agent changes one card: fetch that task alone (debounced per task)
    const progress = new Map<number, ReturnType<typeof setTimeout>>();
    const on = (e: Event) => {
      const d = (e as CustomEvent<{ workspaceId?: number; item?: { action?: string; data?: unknown } }>).detail;
      if (d?.workspaceId === undefined || !ids.has(d.workspaceId)) return;
      const tid = (d.item?.data as { task_id?: unknown } | undefined)?.task_id;
      if (d.item?.action === "run.progress" && typeof tid === "number") {
        clearTimeout(progress.get(tid));
        progress.set(
          tid,
          setTimeout(() => {
            progress.delete(tid);
            api.task(tid).then((t) => patchRef.current(t), () => undefined);
          }, 400),
        );
        return;
      }
      if (timer) clearTimeout(timer);
      timer = setTimeout(fire, 400);
    };
    window.addEventListener("gigg:activity", on);
    return () => {
      window.removeEventListener("gigg:activity", on);
      if (timer) clearTimeout(timer);
      progress.forEach((t) => clearTimeout(t));
    };
  }, [wsKey, taskPid, projOf]);

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(""), 4000);
    return () => clearTimeout(t);
  }, [toast]);
  const changed = useCallback(
    (message?: string) => {
      load();
      refresh();
      if (message) setToast(message);
    },
    [load, refresh],
  );

  // project actions: straight to the one project in view, or pick one first
  const runAction = useCallback((what: ProjectAction, pid: number) => {
    setPick(null);
    if (what === "new") setAdding(pid);
    else if (what === "recurring") setRecurring({ pid });
    else if (what === "templates") setTemplates(pid);
    else setArchiveDone([pid]);
  }, []);
  const archivable = inScope.filter((x) => canWriteIn(x.ws) && hasCap(me, "archive_tasks", x.ws.id));
  const action = (what: ProjectAction | "schedules") => {
    if (what === "archive") {
      // one dialog for any number of projects: all of those in view are ticked to start with
      setArchiveDone(archivable.map((x) => x.project.id));
    } else if (what === "schedules") {
      if (wsInScope.length === 1) setTimings(wsInScope[0]!.id);
      else setPick("schedules");
    } else if (single) runAction(what, single.project.id);
    else setPick(what);
  };
  // ?new=1 (the command palette's "New task") opens the new-task flow
  useEffect(() => {
    const s = new URLSearchParams(search);
    if (s.get("new") !== "1") return;
    s.delete("new");
    const rest = s.toString();
    navigate(loc + (rest ? "?" + rest : ""), { replace: true });
    setNewIn(null);
    if (inScope.length === 1) setAdding(inScope[0]!.project.id);
    else if (inScope.length > 1) setPick("new");
  }, [search, loc, navigate, inScope]);

  const collapseKey = single ? single.project.id : 0;
  const [collapsed, setCollapsed] = useState<number[]>(() => loadCollapsed(collapseKey));
  useEffect(() => setCollapsed(loadCollapsed(collapseKey)), [collapseKey]);
  const toggleCollapsed = (id: number) => {
    setCollapsed((cur) => {
      const next = toggleId(cur, id);
      saveCollapsed(collapseKey, next);
      return next;
    });
    // the clicked control goes away: keep the keyboard where the column went (its chip, or its header)
    requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-col-focus="${id}"]`)?.focus());
  };

  const allTasks = useMemo(() => [...boards.values()].flatMap((b) => b.statuses.flatMap((s) => s.tasks)), [boards]);
  const taskById = (id: number) => allTasks.find((t) => t.id === id);

  // moves: optimistic, then the server places the task; a failure puts the boards back
  const sendMove = useCallback(
    (taskId: number, body: MoveBody, next: Map<number, Board>) => {
      const before = boards;
      const key = allTasks.find((t) => t.id === taskId)?.key ?? "the task";
      setBoards(next);
      setMoveErr("");
      movesInFlight.current++;
      loadSeq.current++; // a load already on its way predates the move: drop its answer
      api
        .moveTask(taskId, body)
        .finally(() => movesInFlight.current--)
        .then(() => changed())
        .catch((e: unknown) => {
          setBoards(before);
          const conflict = e instanceof ApiError && (e.status === 409 || e.status === 412);
          setMoveErr(conflict ? `${key} changed meanwhile; the board is reloaded.` : e instanceof ApiError ? `Couldn't move ${key}: ${e.message}` : `Couldn't move ${key}.`);
          if (conflict) load();
        });
    },
    [boards, allTasks, changed, load],
  );
  const moveInProject = (taskId: number, statusId: number, index: number) => {
    const t = taskById(taskId);
    const b = t && boards.get(t.project_id);
    if (!t || !b) return;
    const m = applyMove(b, taskId, statusId, index);
    if (!m) return;
    sendMove(taskId, m.body, new Map(boards).set(t.project_id, m.board));
  };
  const moveToCat = (taskId: number, category: Category) => {
    const t = taskById(taskId);
    if (!t || t.status_category === category) return;
    const next = moveToCategory(boards, taskId, category);
    if (!next) {
      setMoveErr(`${t.key}'s project has no ${CATEGORY_LABEL[category]} column.`);
      return;
    }
    sendMove(taskId, { category }, next);
  };

  if (tree.every((w) => w.projects.length === 0)) return <p className="muted">No projects yet.</p>;
  if (!loaded) return <p className="muted">Loading…</p>;

  const me_ = me.user.id;
  // a manual order only makes sense on the whole column: with a task filter on, drops go last
  const manual = !!single && !(filter.a || filter.l || filter.pr || filter.q);
  const columns: BoardColumn[] = single
    ? boards.get(single.project.id)
      ? statusColumns(boards.get(single.project.id)!, filter, me_)
      : []
    : categoryColumns(scopeIds.map((id) => boards.get(id)).filter((b): b is Board => !!b), filter, me_);
  const open = columns.filter((c) => c.category !== "done").reduce((n, c) => n + c.tasks.length, 0);
  const active = columns.find((c) => c.key === activeCol) ?? columns.find((c) => c.category === "todo") ?? columns[0];
  const shown = narrow && active ? [active] : columns;
  // collapsed columns leave the grid and become small chips above it (wide screens; a phone shows one column)
  const openCols = narrow ? shown : shown.filter((c) => !collapsed.includes(c.id));
  const closedCols = narrow ? [] : shown.filter((c) => collapsed.includes(c.id));
  // the agent side of the tasks in view, in one line above the columns
  const pulse = { running: 0, answer: 0, failed: 0, queued: 0, total: 0 };
  for (const c of columns)
    for (const t of c.tasks) {
      const st = cardState(t);
      if (st) {
        pulse[st.kind]++;
        pulse.total++;
      }
    }
  const archivedIn = (c: BoardColumn) =>
    archived
      .filter((t) => (c.statusId !== undefined ? t.status_id === c.statusId : t.status_category === c.category))
      .filter((t) => scopeIds.includes(t.project_id) && matches(t, filter, me_));
  const labelNames = [...new Set([...boards.values()].flatMap((b) => b.labels.map((l) => l.name)))].sort((a, b) => a.localeCompare(b));
  const people = [...new Set(wsInScope.flatMap((w) => members.get(w.id) ?? []))]
    .map((id) => users.get(id))
    .filter((u): u is NonNullable<typeof u> => !!u)
    .sort((a, b) => (a.display_name || a.username).localeCompare(b.display_name || b.username));
  const anyWrite = inScope.some((x) => canWriteIn(x.ws));
  const wsArchive = inScope.some((x) => canWriteIn(x.ws) && hasCap(me, "archive_tasks", x.ws.id));
  const wsTemplates = inScope.some((x) => hasCap(me, "templates", x.ws.id));
  const tableHref = (w: WorkspaceTree) => `/w/${w.slug}${single && single.ws.id === w.id ? `/p/${single.project.id}` : ""}/table`;

  const endDrag = () => {
    clearTimeout(dragTimer.current); // a drag that ends before it registered must not stay "dragging"
    setDrag(null);
    setDrop(null);
  };
  const dragOver = (e: DragEvent<HTMLElement>, c: BoardColumn, atEnd = false) => {
    if (drag === null) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    let index = -1;
    if (manual && !atEnd) {
      const cards = [...e.currentTarget.querySelectorAll<HTMLElement>("[data-task-id]")].filter((el) => Number(el.dataset.taskId) !== drag);
      index = cards.findIndex((el) => {
        const r = el.getBoundingClientRect();
        return e.clientY < r.top + r.height / 2;
      });
      if (index < 0) index = cards.length;
    }
    if (drop?.col !== c.key || drop.index !== index) setDrop({ col: c.key, index });
  };
  const dropOn = (e: DragEvent<HTMLElement>, c: BoardColumn) => {
    if (drag === null) return;
    e.preventDefault();
    if (c.statusId !== undefined) {
      const all = boards.get(single!.project.id)?.statuses.find((s) => s.id === c.statusId)?.tasks ?? c.tasks; // unfiltered
      const index = manual && drop?.col === c.key && drop.index >= 0 ? drop.index : all.filter((t) => t.id !== drag).length;
      moveInProject(drag, c.statusId, index);
    } else moveToCat(drag, c.category);
    endDrag();
  };
  const dragLeave = (e: DragEvent<HTMLElement>, c: BoardColumn) => {
    if (drop?.col === c.key && !e.currentTarget.contains(e.relatedTarget as Node | null)) setDrop(null);
  };
  const colColor = (c: BoardColumn): string => c.color ?? CATEGORY_COLOR[c.category] ?? "var(--muted)";
  // a column's "+": one project in view opens the form in that status; several ask which project first
  const writable = inScope.filter((x) => canWriteIn(x.ws));
  const canAddIn = (c: BoardColumn) => writable.length > 0 && c.category !== "cancelled";
  const addIn = (c: BoardColumn) => {
    setNewIn({ category: c.category, statusId: c.statusId });
    if (single) runAction("new", single.project.id);
    else if (writable.length === 1) runAction("new", writable[0]!.project.id);
    else setPick("new");
  };
  const cardFor = (t: Task, c: BoardColumn, i: number, archivedCard: boolean) => {
    const p = projOf(t.project_id);
    const b = boards.get(t.project_id);
    if (!p || !b) return null;
    const write = !archivedCard && canWriteIn(p.ws);
    return (
      <Card
        task={t}
        statuses={openStatuses(b)}
        ws={p.ws}
        showWs={wsInScope.length > 1}
        labels={b.labels}
        canWrite={write}
        canReorder={write && c.statusId !== undefined && manual}
        statusLabel={c.statusId === undefined && t.status_name !== c.name ? t.status_name : undefined}
        dragging={t.id === drag}
        onDragStart={() => {
          dragTimer.current = setTimeout(() => setDrag(t.id), 0);
        }}
        onDragEnd={endDrag}
        onReorder={(where) => c.statusId !== undefined && moveInProject(t.id, c.statusId, reorderIndex(where, i, c.tasks.length))}
        first={i === 0}
        last={i === c.tasks.length - 1}
        onOpen={() => navigate(`/board/p/${t.project_id}/t/${t.key}${qs}`)}
        onChanged={changed}
        fields={fields.get(p.ws.id)}
      />
    );
  };

  const panel = taskPid && taskKey ? projOf(taskPid) : null;
  const panelBoard = taskPid ? boards.get(taskPid) : undefined;
  const ctxFor = (pid: number | null) => {
    if (pid === null) return null;
    const p = projOf(pid);
    const b = boards.get(pid);
    return p && b ? { ...p, board: b } : null;
  };
  const addCtx = ctxFor(adding);
  const recCtx = ctxFor(recurring?.pid ?? null);
  const tplCtx = ctxFor(templates);
  const timingsWs = timings !== null ? tree.find((w) => w.id === timings) : undefined;

  // the board's less frequent actions: the "⋯" menu of the page head (on a phone also the table links)
  const tools = (
    <>
      <button type="button" role="menuitem" data-tip="Tasks that come back on a schedule (a template + a schedule)" onClick={() => action("recurring")}>
        <Icon name="refresh" />
        Recurring
      </button>
      <button type="button" role="menuitem" data-tip="Timings (every day 9:00, every Friday…) that recurring tasks share" onClick={() => action("schedules")}>
        <Icon name="clock" />
        Schedules
      </button>
      {wsTemplates && (
        <button type="button" role="menuitem" data-tip="Reusable task contents: start a task or a recurring task from one" onClick={() => action("templates")}>
          <Icon name="template" />
          Templates
        </button>
      )}
      {wsArchive && (
        <button type="button" role="menuitem" data-tip="Archive the tasks in Done: they leave the board but stay searchable" onClick={() => action("archive")}>
          <Icon name="archive" />
          Archive done
        </button>
      )}
      <button type="button" role="menuitem" data-tip="Deleted tasks of the projects in view: restore them from here" onClick={() => setTrash(true)}>
        <Icon name="trash" />
        Trash
      </button>
    </>
  );

  return (
    <div className="board-page">
      <header className="page-head">
        <div className="head-title">
          <h1>
            {single ? single.project.name : "Board"}
            <HelpButton topic="board" label="Help: the board" />
          </h1>
          <div className="head-sub">
            {wsInScope.length === 1 && <WorkspaceBadge ws={wsInScope[0]!} />}
            {single && <span className="key">{single.project.key}</span>}
            <span className="muted">
              {open} open task{open === 1 ? "" : "s"}
              {!single && ` · ${inScope.length} projects`}
            </span>
          </div>
        </div>
        <div className="head-actions">
          {narrow ? (
            <MoreMenu label="More">
              {wsInScope.map((w) => (
                <Link key={w.id} href={tableHref(w)} role="menuitem">
                  ☰ Table{wsInScope.length > 1 ? ` · ${w.name}` : ""}
                </Link>
              ))}
              {tools}
            </MoreMenu>
          ) : (
            <div className="view-switch" role="group" aria-label="View">
              <span className="vs-item on" aria-current="page">
                Board
              </span>
              {wsInScope.length === 1 ? (
                <Link href={tableHref(wsInScope[0]!)} className="vs-item">
                  Table
                </Link>
              ) : (
                <MoreMenu label="Table" className="vs-menu" buttonClass="vs-item">
                  {wsInScope.map((w) => (
                    <Link key={w.id} href={tableHref(w)} role="menuitem">
                      {w.name}
                    </Link>
                  ))}
                </MoreMenu>
              )}
            </div>
          )}
          {anyWrite && (
            <button
              type="button"
              className="button"
              onClick={() => {
                setNewIn(null);
                action("new");
              }}
            >
              + New task
            </button>
          )}
          {!narrow && (
            <MoreMenu label="More actions" icon className="head-more">
              {tools}
            </MoreMenu>
          )}
        </div>
      </header>
      <BoardFilters filter={filter} setFilter={setFilter} tree={tree} labelNames={labelNames} people={people} narrow={narrow} />
      {failed.length > 0 && (
        <p className="danger small" role="alert">
          Couldn't load {failed.join(", ")}.
        </p>
      )}
      {moveErr && (
        <p className="danger move-error" role="alert">
          {moveErr}{" "}
          <button type="button" className="link" onClick={() => setMoveErr("")}>
            dismiss
          </button>
        </p>
      )}
      {narrow && (
        <div className="col-tabs" role="tablist" aria-label="Columns">
          {columns.map((c) => (
            <button
              key={c.key}
              type="button"
              role="tab"
              aria-selected={c.key === active?.key}
              className={"col-tab" + (c.key === active?.key ? " on" : "") + (drag !== null && drop?.col === c.key ? " drag-over" : "")}
              onClick={() => setActiveCol(c.key)}
              onDragOver={(e) => {
                if (drag === null) return;
                e.preventDefault();
                if (drop?.col !== c.key) setDrop({ col: c.key, index: -1 });
              }}
              onDrop={(e) => dropOn(e, c)}
            >
              <span className="dot" style={{ background: colColor(c) }} />
              <span className="col-tab-name">{c.name}</span>
              <span className="count">{c.tasks.length}</span>
            </button>
          ))}
        </div>
      )}
      {pulse.total > 0 && (
        <div className="board-pulse" role="status" aria-label="Agent activity on this board">
          {pulse.running > 0 && (
            <span className="pulse-item pulse-running">
              <span className="pulse-dot" aria-hidden="true" />
              {pulse.running} running
            </span>
          )}
          {pulse.answer > 0 && (
            <Link href="/inbox" className="pulse-item pulse-answer" data-tip="Tasks whose agent asked a question: answer them from the Inbox or the task">
              <Icon name="question" />
              {pulse.answer} need{pulse.answer === 1 ? "s" : ""} an answer
            </Link>
          )}
          {pulse.failed > 0 && (
            <span className="pulse-item pulse-failed" tabIndex={0} data-tip="Tasks whose agent's last run failed, timed out or was interrupted: Retry on the card resumes it">
              <Icon name="alert" />
              {pulse.failed} failed
            </span>
          )}
          {pulse.queued > 0 && <span className="pulse-item">{pulse.queued} resume queued</span>}
        </div>
      )}
      {!narrow && closedCols.length > 0 && (
        <div className="collapsed-row" role="group" aria-label="Collapsed columns">
          {closedCols.map((c) => (
            <button
              key={c.key}
              type="button"
              className={"collapsed-chip" + (drop?.col === c.key ? " drag-over" : "")}
              aria-expanded={false}
              data-tip={`Show the ${c.name} column${drag !== null ? " (or drop the card here)" : ""}`}
              onClick={() => toggleCollapsed(c.id)}
              data-col-focus={c.id}
              onDragOver={(e) => dragOver(e, c, true)}
              onDrop={(e) => dropOn(e, c)}
              onDragLeave={(e) => dragLeave(e, c)}
            >
              <StageIcon category={c.category} color={colColor(c)} />
              <span className="collapsed-chip-name">{c.name}</span>
              <span className="count-pill">{c.tasks.length}</span>
            </button>
          ))}
        </div>
      )}
      {openCols.length === 0 && closedCols.length > 0 && <p className="muted">Every column is collapsed: click one above to show it.</p>}
      <div className="board" style={{ gridTemplateColumns: `repeat(${Math.max(1, openCols.length)}, minmax(0, 1fr))` }}>
        {openCols.map((c) => {
          const others = c.tasks.filter((t) => t.id !== drag);
          const line = drop?.col === c.key ? drop.index : -1;
          let k = -1;
          const addHere = canAddIn(c);
          return (
            <section
              key={c.key}
              className={"column" + (drop?.col === c.key ? " drag-over" : "")}
              aria-label={c.name}
              role={narrow ? "tabpanel" : undefined}
              onDragOver={(e) => dragOver(e, c)}
              onDrop={(e) => dropOn(e, c)}
              onDragLeave={(e) => dragLeave(e, c)}
            >
              <div className="column-head">
                <h2 className="column-title" tabIndex={-1} data-col-focus={c.id}>
                  <StageIcon category={c.category} color={colColor(c)} />
                  <span className="column-name">{c.name}</span>
                  <span className="count-pill" aria-label={`${c.tasks.length} task${c.tasks.length === 1 ? "" : "s"}`}>
                    {c.tasks.length}
                  </span>
                </h2>
                <span className="column-tools">
                  {addHere && (
                    <button type="button" className="icon-button" aria-label={`New task in ${c.name}`} onClick={() => addIn(c)}>
                      <Icon name="plus" strokeWidth={2} />
                    </button>
                  )}
                  {!narrow && (
                    <button type="button" className="icon-button" aria-expanded={true} aria-label={`Collapse ${c.name}: it becomes a small button above the board`} onClick={() => toggleCollapsed(c.id)}>
                      <Icon name="shrink" />
                    </button>
                  )}
                </span>
              </div>
              <div className="cards">
                {c.tasks.map((t, i) => {
                  if (t.id !== drag) k++;
                  const showLine = manual && c.statusId !== undefined && t.id !== drag && k === line;
                  return (
                    <div key={t.id} className="card-slot">
                      {showLine && <div className="drop-line" aria-hidden="true" />}
                      {cardFor(t, c, i, false)}
                    </div>
                  );
                })}
                {archivedIn(c).map((t) => (
                  <div key={t.id} className="card-slot archived-slot">
                    {cardFor(t, c, 0, true)}
                  </div>
                ))}
                {manual && c.statusId !== undefined && line === others.length && <div className="drop-line" aria-hidden="true" />}
                {c.tasks.length === 0 && <div className="empty">{drag !== null ? "Drop here" : "Nothing here"}</div>}
                {addHere && c.category !== "done" && (
                  <button type="button" className="column-add" onClick={() => addIn(c)}>
                    <Icon name="plus" strokeWidth={2} /> New task
                  </button>
                )}
              </div>
            </section>
          );
        })}
      </div>
      {pick && (
        <PickModal
          what={pick}
          options={pick === "schedules" ? [] : inScope.filter((x) => (pick === "templates" ? hasCap(me, "templates", x.ws.id) : pick === "archive" ? canWriteIn(x.ws) && hasCap(me, "archive_tasks", x.ws.id) : pick === "new" ? canWriteIn(x.ws) : true))}
          workspaces={wsInScope}
          onPick={(id) => (pick === "schedules" ? (setPick(null), setTimings(id)) : runAction(pick, id))}
          onClose={() => {
            setPick(null);
            setNewIn(null);
          }}
        />
      )}
      {addCtx && (
        <TaskModal
          ws={addCtx.ws}
          projectId={addCtx.project.id}
          statuses={openStatuses(addCtx.board)}
          labels={addCtx.board.labels}
          initialStatus={newIn ? (newIn.statusId ?? openStatuses(addCtx.board).find((s) => s.category === newIn.category)?.id) : undefined}
          onClose={() => {
            setAdding(null);
            setNewIn(null);
          }}
          onCreated={changed}
        />
      )}
      {recCtx && (
        <RecurringModal
          ws={recCtx.ws}
          projectId={recCtx.project.id}
          statuses={openStatuses(recCtx.board)}
          labels={recCtx.board.labels}
          canWrite={canWriteIn(recCtx.ws)}
          highlight={recurring?.highlight}
          onClose={() => setRecurring(null)}
          onChanged={changed}
        />
      )}
      {timingsWs && <SchedulesModal ws={timingsWs} canWrite={canWriteIn(timingsWs) && hasCap(me, "schedules", timingsWs.id)} onClose={() => setTimings(null)} onChanged={changed} />}
      {tplCtx && (
        <TemplatesModal
          ws={tplCtx.ws}
          projectId={tplCtx.project.id}
          statuses={openStatuses(tplCtx.board)}
          labels={tplCtx.board.labels}
          canEdit={canWriteIn(tplCtx.ws)}
          onClose={() => setTemplates(null)}
          onChanged={changed}
        />
      )}
      {archiveDone !== null && (
        <ArchiveDoneModal
          options={archivable}
          initial={archiveDone}
          onClose={() => setArchiveDone(null)}
          onDone={(n) => changed(n === 0 ? "Nothing to archive" : `Archived ${n} done task${n === 1 ? "" : "s"}`)}
        />
      )}
      {trash && <TrashModal scope={inScope} onClose={() => setTrash(false)} onRestored={(key) => changed(`Restored ${key}`)} />}
      {panel && taskKey && panelBoard && (
        <TaskPanel
          taskKey={taskKey}
          ws={panel.ws}
          statuses={openStatuses(panelBoard)}
          labels={panelBoard.labels}
          canWrite={canWriteIn(panel.ws)}
          onClose={() => navigate(boardURL)}
          onChanged={changed}
          onMoved={(t) => navigate(`/board/p/${t.project_id}/t/${t.key}${qs}`)}
          onOpenSchedule={(id) => setRecurring({ pid: panel.project.id, highlight: id })}
        />
      )}
      {toast && (
        <div className="toast" role="status">
          {toast}
        </div>
      )}
    </div>
  );
}

// BoardFilters: one toolbar row (search, assignee, label, priority, the archived toggle, Clear filters) and under it
// the workspaces and projects as compact toggle chips (none on = all).
function BoardFilters({ filter, setFilter, tree, labelNames, people, narrow }: {
  filter: BoardFilter;
  setFilter: (f: BoardFilter) => void;
  tree: WorkspaceTree[];
  labelNames: string[];
  people: { id: number; username: string; display_name: string }[];
  narrow: boolean;
}) {
  const { users, prefs, setPref } = useApp();
  // folded: one line with the filters that are on (removable), or just the Filters button. On wide screens the
  // choice is saved; on a phone the row starts folded each time.
  const [narrowOpen, setNarrowOpen] = useState(false);
  const shut = narrow ? !narrowOpen : prefs.board_filters === "folded";
  const fold = (v: boolean) => (narrow ? setNarrowOpen(!v) : setPref("board_filters", v ? "folded" : "open"));
  const [text, setText] = useState(filter.q);
  useEffect(() => setText(filter.q), [filter.q]);
  const filterRef = useRef(filter);
  filterRef.current = filter;
  useEffect(() => {
    if (text === filterRef.current.q) return;
    const t = setTimeout(() => setFilter({ ...filterRef.current, q: text }), 250);
    return () => clearTimeout(t);
  }, [text, setFilter]);
  const wsShown = filter.ws.length ? tree.filter((w) => filter.ws.includes(w.slug)) : tree;
  const projects = wsShown.flatMap((w) => w.projects.map((p) => ({ w, p })));
  const toggleWs = (slug: string) => {
    const next = filter.ws.includes(slug) ? filter.ws.filter((s) => s !== slug) : [...filter.ws, slug];
    const all = next.length === tree.length;
    const keepP = filter.p.filter((id) => (all || next.length === 0 ? tree : tree.filter((w) => next.includes(w.slug))).some((w) => w.projects.some((p) => p.id === id)));
    setFilter({ ...filter, ws: all ? [] : next, p: keepP });
  };
  const toggleP = (id: number) => {
    const next = filter.p.includes(id) ? filter.p.filter((x) => x !== id) : [...filter.p, id];
    setFilter({ ...filter, p: next.length === projects.length ? [] : next });
  };
  const sel = (v: string | number) => (v === "" || v === 0 ? "" : " active");
  const active = activeFilters(filter, {
    ws: (slug) => {
      const w = tree.find((x) => x.slug === slug);
      return w && { name: w.name, color: colorFor(w.slug, w.color) };
    },
    project: (id) => tree.flatMap((w) => w.projects).find((p) => p.id === id)?.name,
    user: (id) => {
      const u = users.get(id);
      return u && (u.display_name || u.username);
    },
    priority: (n) => priorityNames[n] ?? String(n),
  });
  const foldButton = (
    <button
      type="button"
      className={"bf-fold" + (shut && active.length ? " on" : "")}
      aria-expanded={!shut}
      aria-controls="board-filters"
      data-tip={shut ? "Show the filters" : "Fold the filters: only those that are on stay in view"}
      onClick={() => fold(!shut)}
    >
      <Icon name="settings" />
      Filters
      {shut && active.length > 0 && <span className="count">{active.length}</span>}
      <Icon name={shut ? "chevron-right" : "chevron-down"} />
    </button>
  );
  if (shut) {
    return (
      <div className="board-filters bf-folded" role="group" aria-label="Board filters (folded)">
        <div className="bf-row">
          {foldButton}
          {active.map((x) => (
            <span key={x.key} className={"bf-active" + (x.color ? " bf-active-c" : "")} style={x.color ? ({ "--c": x.color } as CSSProperties) : undefined}>
              {x.color && <span className="dot" style={{ background: x.color }} aria-hidden="true" />}
              {x.label}
              <button type="button" className="bf-active-x" aria-label={`Remove the filter ${x.label}`} onClick={() => setFilter(x.without)}>
                <Icon name="x" />
              </button>
            </span>
          ))}
          {active.length > 1 && (
            <button type="button" className="link bf-clear" onClick={() => setFilter({ ...EMPTY_FILTER })}>
              Clear all
            </button>
          )}
        </div>
      </div>
    );
  }
  return (
    <div className="board-filters" id="board-filters" role="group" aria-label="Board filters">
      <div className="bf-row bf-fields">
        {foldButton}
        <span className="bf-search">
          <svg viewBox="0 0 16 16" width="1em" height="1em" aria-hidden="true">
            <circle cx="7" cy="7" r="4.5" fill="none" stroke="currentColor" strokeWidth="1.5" />
            <path d="M10.5 10.5 14 14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          <input type="search" aria-label="Search tasks" placeholder="Search key, title, description" value={text} onChange={(e) => setText(e.target.value)} />
        </span>
        <select className={sel(filter.a)} aria-label="Assignee" value={filter.a} onChange={(e) => setFilter({ ...filter, a: e.target.value })}>
          <option value="">Anyone</option>
          <option value="me">Assigned to me</option>
          <option value="none">Unassigned</option>
          {people.map((u) => (
            <option key={u.id} value={String(u.id)}>
              {u.display_name || u.username}
            </option>
          ))}
        </select>
        <select className={sel(filter.l)} aria-label="Label" value={filter.l} onChange={(e) => setFilter({ ...filter, l: e.target.value })}>
          <option value="">Any label</option>
          {labelNames.map((l) => (
            <option key={l} value={l}>
              {l}
            </option>
          ))}
        </select>
        <select className={sel(filter.pr)} aria-label="Priority" value={filter.pr} onChange={(e) => setFilter({ ...filter, pr: Number(e.target.value) })}>
          <option value={0}>Any priority</option>
          {priorityNames.slice(1).map((n, i) => (
            <option key={n} value={i + 1}>
              {i + 1 === 4 ? n : `${n} or higher`}
            </option>
          ))}
        </select>
        <button
          type="button"
          className={"bf-toggle" + (filter.arch ? " on" : "")}
          aria-pressed={filter.arch}
          data-tip="Show archived tasks too, greyed out, in their columns"
          onClick={() => setFilter({ ...filter, arch: !filter.arch })}
        >
          <Icon name="archive" />
          Archived
        </button>
        {isFiltered(filter) && (
          <button type="button" className="link bf-clear" onClick={() => setFilter({ ...EMPTY_FILTER, arch: filter.arch })}>
            Clear filters
          </button>
        )}
      </div>
      {(tree.length > 1 || projects.length > 1) && (
        <div className="bf-row bf-scope">
          {tree.length > 1 && (
            <div className="bf-group" role="group" aria-label="Workspaces">
              <button
                type="button"
                className={"bf-chip" + (filter.ws.length === 0 ? " on" : "")}
                aria-pressed={filter.ws.length === 0}
                data-tip="Show every workspace (clears the workspace and project filters)"
                onClick={() => setFilter({ ...filter, ws: [], p: [] })}
              >
                All workspaces
              </button>
              {tree.map((w) => {
                const on = filter.ws.includes(w.slug);
                const c = colorFor(w.slug, w.color);
                const owner = w.owner_id != null ? users.get(w.owner_id) : undefined;
                return (
                  <button
                    key={w.id}
                    type="button"
                    className={"bf-chip bf-ws" + (on ? " on" : "")}
                    aria-pressed={on}
                    data-tip={`${on ? "Hide" : "Show only"} ${w.name}${filter.ws.length && !on ? " (add it to the selection)" : ""}${owner ? `\nOwner: ${owner.display_name || owner.username}` : ""}`}
                    style={{ "--c": c } as CSSProperties}
                    onClick={() => toggleWs(w.slug)}
                  >
                    {owner ? <Avatar user={owner} size="xs" /> : <span className="dot" style={{ background: c }} />}
                    {w.name}
                  </button>
                );
              })}
            </div>
          )}
          {projects.length > 1 && (
            <div className="bf-group" role="group" aria-label="Projects">
              <button
                type="button"
                className={"bf-chip" + (filter.p.length === 0 ? " on" : "")}
                aria-pressed={filter.p.length === 0}
                data-tip="Show every project of the workspaces shown"
                onClick={() => setFilter({ ...filter, p: [] })}
              >
                All projects
              </button>
              {projects.map(({ w, p }) => {
                const on = filter.p.includes(p.id);
                return (
                  <button
                    key={p.id}
                    type="button"
                    className={"bf-chip" + (on ? " on" : "")}
                    aria-pressed={on}
                    data-tip={`${w.name} / ${p.name} (${p.key})\nOne project shown: its own columns and manual order`}
                    onClick={() => toggleP(p.id)}
                  >
                    <span className="dot" style={{ background: colorFor(w.slug, w.color) }} />
                    {p.name}
                  </button>
                );
              })}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// TrashModal: the deleted tasks of the projects in view, newest first, each with Restore (for people
// who may delete tasks there).
function TrashModal({ scope: inScope, onClose, onRestored }: { scope: { ws: WorkspaceTree; project: { id: number; key: string; name: string } }[]; onClose: () => void; onRestored: (key: string) => void }) {
  const { me } = useApp();
  const [items, setItems] = useState<Task[] | null>(null);
  const [err, setErr] = useState("");
  const pids = inScope.map((x) => x.project.id).join(",");
  const load = useCallback(() => {
    const ids = new Set(pids.split(",").map(Number));
    const wss = [...new Map(inScope.map((x) => [x.ws.id, x.ws])).values()];
    Promise.allSettled(wss.map((w) => viewsApi.tasks(w.slug, { deleted: true, limit: 500 }))).then((res) => {
      const all = res.flatMap((r) => (r.status === "fulfilled" ? r.value.items : [])).filter((t) => ids.has(t.project_id));
      setItems(all.sort((a, b) => b.updated_at - a.updated_at));
      setErr(res.some((r) => r.status === "rejected") ? "Some workspaces couldn't be read." : "");
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pids]);
  useEffect(load, [load]);
  const wsOf = (t: Task) => inScope.find((x) => x.ws.id === t.workspace_id)?.ws;
  const restore = (t: Task) =>
    api
      .restoreTask(t.id)
      .then(() => {
        onRestored(t.key);
        load();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : `Couldn't restore ${t.key}.`));
  return (
    <Modal title="Trash" onClose={onClose} wide help={<HelpButton topic="board" label="Help: the board (archive and trash)" />}>
      <p className="muted small">Deleted tasks of the projects in view, most recently changed first. Restoring puts a task back where it was.</p>
      {err && (
        <p className="danger small" role="alert">
          {err}
        </p>
      )}
      {items === null && <p className="muted">Loading…</p>}
      {items && items.length === 0 && <p className="muted">The trash is empty.</p>}
      <div className="pick-list">
        {(items ?? []).map((t) => {
          const ws = wsOf(t);
          const may = !!ws && canWriteIn(ws) && hasCap(me, "delete_tasks", ws.id);
          return (
            <div key={t.id} className="pick-item trash-item">
              {ws && <WorkspaceBadge ws={ws} />}
              <span className="pill pill-key">{t.key}</span>
              <span className="grow">{t.title}</span>
              <span className="muted small" data-tip="Last changed (when it was deleted, unless edited since)">
                {fmtTime(t.updated_at)}
              </span>
              <button
                type="button"
                className="button secondary small"
                disabled={!may}
                data-tip={may ? `Restore ${t.key} to its column` : "Restoring needs the Delete tasks permission here"}
                onClick={() => restore(t)}
              >
                ↺ Restore
              </button>
            </div>
          );
        })}
      </div>
    </Modal>
  );
}

// PickModal: the project (or workspace) a board-wide action applies to, when several are in view.
function PickModal({ what, options, workspaces, onPick, onClose }: {
  what: ProjectAction | "schedules";
  options: { ws: WorkspaceTree; project: { id: number; key: string; name: string } }[];
  workspaces: WorkspaceTree[];
  onPick: (id: number) => void;
  onClose: () => void;
}) {
  const title = { new: "New task in…", recurring: "Recurring tasks of…", templates: "Templates for…", archive: "Archive done tasks in…", schedules: "Schedules of…" }[what];
  return (
    <Modal title={title} onClose={onClose}>
      <div className="pick-list">
        {what === "schedules"
          ? workspaces.map((w) => (
              <button key={w.id} type="button" className="pick-item" onClick={() => onPick(w.id)}>
                <WorkspaceBadge ws={w} />
              </button>
            ))
          : options.map(({ ws, project }) => (
              <button key={project.id} type="button" className="pick-item" onClick={() => onPick(project.id)}>
                <WorkspaceBadge ws={ws} />
                <span className="pill pill-key">{project.key}</span>
                <span>{project.name}</span>
              </button>
            ))}
        {what !== "schedules" && options.length === 0 && <p className="muted small">No project in view allows this.</p>}
      </div>
    </Modal>
  );
}

// ArchiveDoneModal: archive the done tasks of the projects ticked (all those in view to start with), one call
// per project; a failure stops there and says which projects were done.
function ArchiveDoneModal({ options, initial, onClose, onDone }: {
  options: { ws: WorkspaceTree; project: { id: number; key: string; name: string } }[];
  initial: number[];
  onClose: () => void;
  onDone: (n: number) => void;
}) {
  const [days, setDays] = useState(7);
  const [picked, setPicked] = useState<number[]>(initial);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const all = options.length > 0 && picked.length === options.length;
  const toggle = (id: number) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));
  const go = async () => {
    setBusy(true);
    setErr("");
    let total = 0;
    const done: string[] = [];
    for (const { project } of options.filter((o) => picked.includes(o.project.id))) {
      try {
        total += (await api.archiveDone(project.id, days)).archived;
        done.push(project.key);
      } catch (e: unknown) {
        const why = e instanceof ApiError ? e.message : "it failed";
        setErr(`Couldn't archive in ${project.key}: ${why}.${done.length ? ` Done in ${done.join(", ")} (${total} archived).` : ""}`);
        setBusy(false);
        if (total > 0) onDone(total);
        return;
      }
    }
    setBusy(false);
    onDone(total);
    onClose();
  };
  return (
    <Modal title="Archive done tasks" onClose={onClose}>
      <p className="small">Archived tasks leave the board and lists; they stay searchable and can be unarchived from the task.</p>
      {options.length === 0 ? (
        <p className="muted small">No project in view allows archiving.</p>
      ) : (
        options.length > 1 && (
          <fieldset className="archive-pick">
            <legend className="small muted">Projects</legend>
            <label className="archive-row archive-all">
              <input type="checkbox" checked={all} onChange={() => setPicked(all ? [] : options.map((o) => o.project.id))} />
              <span>All projects in view ({options.length})</span>
            </label>
            {options.map(({ ws, project }) => (
              <label key={project.id} className="archive-row">
                <input type="checkbox" checked={picked.includes(project.id)} onChange={() => toggle(project.id)} />
                <WorkspaceBadge ws={ws} />
                <span className="pill pill-key">{project.key}</span>
                <span>{project.name}</span>
              </label>
            ))}
          </fieldset>
        )
      )}
      <div className="archive-days" role="radiogroup" aria-label="Done for at least">
        {[0, 7, 30].map((d) => (
          <button key={d} type="button" role="radio" aria-checked={days === d} className={"assignee" + (days === d ? " on" : "")} onClick={() => setDays(d)}>
            {d === 0 ? "All done" : `Done ${d}+ days ago`}
          </button>
        ))}
      </div>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      <div className="row end">
        <button type="button" className="button secondary" onClick={onClose}>
          Cancel
        </button>
        <button type="button" className="button" onClick={() => void go()} disabled={busy || picked.length === 0}>
          <Icon name="archive" />
          {picked.length > 1 ? `Archive in ${picked.length} projects` : "Archive"}
        </button>
      </div>
    </Modal>
  );
}

// the icon of a card's agent state (running has its pulsing dot): a cue that doesn't depend on colour
const STATE_ICON = { running: "play", queued: "refresh", answer: "question", failed: "alert" } as const;

// useNow: the time, re-read every `every` ms while it is non-zero (a running card's clock).
function useNow(every: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!every) return;
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), every);
    return () => clearInterval(id);
  }, [every]);
  return every ? now : Date.now();
}

// StageIcon: a column's stage as a small ring in its colour: dashed (backlog), empty (to do),
// half full (doing), with a pause bar (waiting), filled with a check (done), crossed (cancelled).
function StageIcon({ category, color }: { category: Category; color: string }) {
  return (
    <svg className="stage-icon" viewBox="0 0 16 16" width="1em" height="1em" aria-hidden="true" style={{ color }}>
      {category === "backlog" && <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeWidth="1.6" strokeDasharray="2.4 2" />}
      {category === "todo" && <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeWidth="1.6" />}
      {category === "doing" && (
        <>
          <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeWidth="1.6" />
          <path d="M8 4a4 4 0 0 1 0 8z" fill="currentColor" />
        </>
      )}
      {category === "waiting" && (
        <>
          <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeWidth="1.6" />
          <path d="M6.6 5.6v4.8M9.4 5.6v4.8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
        </>
      )}
      {category === "done" && (
        <>
          <circle cx="8" cy="8" r="7" fill="currentColor" />
          <path d="m5 8.2 2 2 4-4.2" fill="none" stroke="var(--surface)" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
        </>
      )}
      {category === "cancelled" && (
        <>
          <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeWidth="1.6" />
          <path d="m5.8 5.8 4.4 4.4M10.2 5.8l-4.4 4.4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
        </>
      )}
    </svg>
  );
}

function Card({ task, statuses, ws, showWs, labels, canWrite, canReorder, statusLabel, dragging, first, last, onDragStart, onDragEnd, onReorder, onOpen, onChanged, fields }: {
  task: Task;
  statuses: Status[];
  ws: WorkspaceTree;
  showWs: boolean; // several workspaces in view: name the task's workspace
  labels: Tag[];
  canWrite: boolean;
  canReorder: boolean; // a manual order exists (one project in view)
  statusLabel?: string; // the task's own status, when the column (a category) is named otherwise
  dragging: boolean;
  first: boolean;
  last: boolean;
  onDragStart: () => void;
  onDragEnd: () => void;
  onReorder: (where: "top" | "up" | "down" | "bottom") => void;
  onOpen: () => void;
  onChanged: () => void;
  fields?: CustomField[];
}) {
  const { users } = useApp();
  const [menu, setMenu] = useState(false);
  const [err, setErr] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!menu) return;
    const close = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setMenu(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [menu]);
  const move = (body: MoveBody) => {
    setMenu(false);
    api
      .moveTask(task.id, body)
      .then(() => onChanged())
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Move failed."));
  };
  const reorder = (where: "top" | "up" | "down" | "bottom") => {
    setMenu(false);
    onReorder(where);
  };
  const overdue = task.due_at !== null && task.due_at < Date.now() && task.status_category !== "done";
  const creator = users.get(task.created_by);
  const labelFor = (l: Tag) => labels.find((x) => x.id === l.id) ?? l;
  const showDue = task.due_at && task.due_at !== task.not_after; // a window's end is shown by its own badge
  const assignees = task.assignees.map((id) => users.get(id)).filter((u): u is NonNullable<typeof u> => !!u);
  const nameOf = (u: { username: string; display_name: string }) => u.display_name || u.username;
  const shownLabels = task.labels.slice(0, 2);
  const moreLabels = task.labels.slice(2);
  // who: the assignees, stacked; the creator only when there are no assignees, or a person handed the task to an agent
  const handOff = !!creator && creator.kind !== "agent" && assignees.some((u) => u.kind === "agent");
  const createdBy = creator ? `Created by ${nameOf(creator)}` : "";
  const assigneeTip = (u: { username: string; display_name: string; kind: string }) => `Assigned to ${nameOf(u)}${u.kind === "agent" ? " (agent)" : ""}${createdBy ? `\n${createdBy}` : ""}`;
  const wsColor = colorFor(ws.slug, ws.color);
  const state = cardState(task);
  const run = task.last_run;
  const running = state?.kind === "running";
  const now = useNow(running ? 1000 : 0);
  const worker = run ? users.get(run.worker_user_id) : undefined;
  const asker = task.question ? users.get(task.question.author_id) : undefined;
  const [retrying, setRetrying] = useState(false);
  // the button's name replaces its contents for screen readers: say the agent state there too
  const cardLabel = [
    `${task.key} ${task.title}`,
    state && (running && run?.progress_note ? `${state.label}: ${run.progress_note}` : state.label), // no clock: the name would change every second
    state?.kind === "answer" && task.question ? plainText(task.question.text) : "",
  ]
    .filter(Boolean)
    .join(". ");
  const retry = () => {
    setRetrying(true);
    setErr("");
    api
      .resumeTask(task.id, "")
      .then(() => onChanged())
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Retry failed."))
      .finally(() => setRetrying(false));
  };
  return (
    <article
      className={"task-card" + (state ? " state-" + state.kind : "") + (dragging ? " dragging" : "") + (task.missed_at ? " missed" : "") + (task.archived_at ? " archived" : "") + (task.blocked_by ? " blocked" : "") + (task.color ? " colored" : "")}
      style={task.color ? ({ "--card-c": task.color } as CSSProperties) : undefined}
      data-task-id={task.id}
      draggable={canWrite}
      onDragStart={(e) => {
        e.dataTransfer.effectAllowed = "move";
        e.dataTransfer.setData("text/plain", task.key);
        setMenu(false);
        onDragStart();
      }}
      onDragEnd={onDragEnd}
    >
      <button type="button" className="card-open" onClick={onOpen} aria-label={cardLabel}>
        <div className="card-top">
          <PriorityIcon priority={task.priority} />
          {state && (
            <span className={"run-badge run-" + state.kind} data-tip={state.tip}>
              {running ? <span className="pulse-dot" aria-hidden="true" /> : <Icon name={STATE_ICON[state.kind]} />}
              {state.label}
              {running && run && <span className="mono">· {elapsed(now - run.started_at)}</span>}
            </span>
          )}
          {showWs && (
            <span className="card-ws" data-tip={`Workspace: ${ws.name}`}>
              <span className="dot" style={{ background: wsColor }} />
              {ws.name}
            </span>
          )}
          {statusLabel && (
            <span className="card-status" data-tip={`Its project's status: ${statusLabel}`}>
              {statusLabel}
            </span>
          )}
          {!!task.archived_at && (
            <span className="pill pill-muted" data-tip="Archived: hidden from the board and lists; unarchive it from the task">
              <Icon name="archive" /> archived
            </span>
          )}
          <span className="key card-key">{task.key}</span>
        </div>
        <div className="card-title">{task.title}</div>
        {(task.labels.length > 0 || showDue) && (
          <div className="chips">
            {showDue && (
              <span className={"fact-chip" + (overdue ? " fact-overdue" : "")} data-tip={overdue ? "Overdue" : "Due"}>
                <Icon name="calendar" /> {fmtDue(task.due_at)}
              </span>
            )}
            {shownLabels.map((l) => (
              <LabelChip key={l.id} label={labelFor(l)} />
            ))}
            {moreLabels.length > 0 && (
              <span className="chip-more" data-tip={moreLabels.map((l) => l.name).join("\n")}>
                +{moreLabels.length}
              </span>
            )}
          </div>
        )}
        {running && run && (
          <div className="card-run">
            <div className="card-run-head">
              {worker && <Avatar user={worker} size="xs" />}
              <span className="card-run-who">{worker ? nameOf(worker) : "Agent"}</span>
              {run.model && <span className="card-run-model mono">{run.model}</span>}
            </div>
            {run.progress_pct !== null && (
              <div className="card-run-bar" role="progressbar" aria-label="Progress" aria-valuemin={0} aria-valuemax={100} aria-valuenow={run.progress_pct}>
                <span style={{ width: `${run.progress_pct}%` }} />
              </div>
            )}
            {run.progress_note && <div className="card-run-note mono">› {run.progress_note}</div>}
          </div>
        )}
        {state?.kind === "answer" && task.question && (
          <div className="card-ask">
            <span className="card-ask-label">
              <Icon name="question" /> {task.question.kind === "question" ? (asker ? `${nameOf(asker)} asks` : "Question") : asker ? `${nameOf(asker)} reports` : "Report"}
            </span>
            <p>{plainText(task.question.text)}</p>
          </div>
        )}
        <div className="card-foot">
          <span className="meta">
            <TaskBadges task={task} items fields={fields} />
            {task.comment_count > 0 && (
              <span className="meta-item" data-tip={`${task.comment_count} comment${task.comment_count === 1 ? "" : "s"}`}>
                <Icon name="comment" /> {task.comment_count}
              </span>
            )}
            {task.session_id && !running && (
              <span className="meta-item" data-tip="An agent has worked on it: it has an agent session (see Runs in the task)">
                <Icon name="bot" />
              </span>
            )}
            {task.cost_usd_total > 0 && (
              <span className="meta-item mono" data-tip="What the agent's runs on it cost so far (API-equivalent USD, as reported by the agent)">
                ${task.cost_usd_total.toFixed(2)}
              </span>
            )}
          </span>
          {(assignees.length > 0 || creator) && (
            <span className="people">
              {(assignees.length === 0 || handOff) && creator && (
                <span className="creator">
                  <Avatar user={creator} size="xs" tip={`${createdBy}${assignees.length ? " → assigned to " + assignees.map(nameOf).join(", ") : " (unassigned)"}`} />
                  {assignees.length > 0 && <Icon name="chevron-right" strokeWidth={2.2} />}
                </span>
              )}
              {assignees.map((u) => (
                <Avatar key={u.id} user={u} size="xs" tip={assigneeTip(u)} />
              ))}
            </span>
          )}
        </div>
      </button>
      {state?.kind === "failed" && run && (
        <div className="card-fail">
          <div className="card-fail-text">
            <Icon name="alert" />
            <span>
              <b>{state.label}</b>
              {run.error_reason ? `: ${run.error_reason}` : ""}
            </span>
          </div>
          {canWrite && (
            <button
              type="button"
              className="button small"
              disabled={retrying}
              onClick={retry}
              aria-label={`Retry ${task.key}`}
              data-tip={
                task.session_id
                  ? "Resume the agent's session: the task goes back to To-Do and its runner continues on its next pass"
                  : "Back to To-Do: the agent's runner starts it again on its next pass"
              }
            >
              <Icon name="refresh" /> Retry
            </button>
          )}
        </div>
      )}
      {canWrite && (
        <div className="card-menu" ref={ref}>
          <button type="button" className="icon-button" aria-label={`Move ${task.key}`} aria-expanded={menu} onClick={() => setMenu(!menu)}>
            ⋯
          </button>
          {menu && (
            <div className="menu" role="menu">
              {canReorder && !(first && last) && <div className="menu-label">Order</div>}
              {canReorder && !first && (
                <>
                  <button type="button" role="menuitem" onClick={() => reorder("top")}>
                    ⤒ To top
                  </button>
                  <button type="button" role="menuitem" onClick={() => reorder("up")}>
                    ↑ Up
                  </button>
                </>
              )}
              {canReorder && !last && (
                <>
                  <button type="button" role="menuitem" onClick={() => reorder("down")}>
                    ↓ Down
                  </button>
                  <button type="button" role="menuitem" onClick={() => reorder("bottom")}>
                    ⤓ To bottom
                  </button>
                </>
              )}
              <div className="menu-label">Move to</div>
              {statuses
                .filter((s) => s.id !== task.status_id)
                .map((s) => (
                  <button key={s.id} type="button" role="menuitem" onClick={() => move({ status: s.id })}>
                    <span className="dot" style={{ background: s.color ?? CATEGORY_COLOR[s.category] }} /> {s.name}
                  </button>
                ))}
              {ws.projects.filter((p) => p.id !== task.project_id).length > 0 && <div className="menu-label">Move to project</div>}
              {ws.projects
                .filter((p) => p.id !== task.project_id)
                .map((p) => (
                  <button key={p.id} type="button" role="menuitem" onClick={() => move({ project: p.id })}>
                    <span className="pill pill-key">{p.key}</span> {p.name}
                  </button>
                ))}
            </div>
          )}
        </div>
      )}
      {err && <div className="danger small">{err}</div>}
    </article>
  );
}
