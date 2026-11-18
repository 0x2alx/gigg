import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type DragEvent, type ReactNode } from "react";
import { Link, useLocation, useSearch } from "wouter";
import { api, ApiError, fmtTime, canCreateGigIn, canWriteIn, gigChoices, hasCap, priorityNames, type Board, type Category, type CustomField, type Status, type Tag, type Gig } from "./api";
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
import { loadSorts, saveSorts, sortGigs, sortsFor, SORT_LABEL, type ColumnSort } from "./columnSort";
import { applyMove, reorderIndex, type MoveBody } from "./dnd";
import { useNarrow } from "./narrow";
import { setMentionUsers } from "./markdown";
import { RecurringModal } from "./Recurring";
import { SchedulesModal } from "./Schedules";
import { TemplatesModal } from "./Templates";
import { GigModal, lastProject } from "./GigModal";
import { GigPanel } from "./GigPanel";
import { backTo, relTime, viewsApi } from "./views/viewsApi";
import { CATEGORY_COLOR, colorFor, readableOn } from "./theme";
import { ClearFilters, Avatar, LabelChip, Modal, MoreMenu, PriorityIcon, GigBadges, WorkspaceChip, reducedMotion } from "./ui";
import { WorkspaceBadge } from "./WorkspaceBadge";
import { HelpButton } from "./help";
import { Icon } from "./icons";
import { cardState, elapsed, plainText } from "./cardState";



const openStatuses = (b: Board): Status[] => b.statuses.filter((s) => s.category !== "cancelled");

type BoardAction = "new" | "recurring" | "templates" | "schedules" | "archive";

// BoardPage: one board over every workspace and project in reach, narrowed by the filter in the URL
// (/board?ws=..&p=..&a=..&l=..&pr=..&q=..&arch=1). One project in view: its own status columns and
// manual order. Several: one column per category, ordered by priority and due date.
export function BoardPage({ gigPid, gigKey }: { gigPid?: number; gigKey?: string }) {
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
  // a visit without a filter brings back the last one used (the URL stays shareable); arriving with one
  // (a link, a dashboard card) makes it the last one used
  const restored = useRef(false);
  useEffect(() => {
    if (restored.current) return;
    restored.current = true;
    const saved = typeof prefs.board_filter === "string" ? prefs.board_filter : "";
    if (!search && saved.startsWith("?")) navigate(loc + saved, { replace: true });
    else if (search && qs !== saved) setPref("board_filter", qs);
  }, [search, qs, prefs.board_filter, loc, navigate, setPref]);

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
  const [archived, setArchived] = useState<Gig[]>([]);
  const [fields, setFields] = useState<Map<number, CustomField[]>>(new Map());
  // the toast: a short message, with an Undo when the change can be taken back in one click
  const [toast, setToast] = useState<{ text: string; undo?: () => void } | null>(null);
  const [moveErr, setMoveErr] = useState("");
  const [drag, setDrag] = useState<number | null>(null);
  const [drop, setDrop] = useState<{ col: string; index: number } | null>(null);
  const [activeCol, setActiveCol] = useState<string | null>(null);
  const [adding, setAdding] = useState<number | null>(null);
  // a column's "+": the new gig starts in that column (its status, or its stage's first status in the picked project)
  const [newIn, setNewIn] = useState<{ category: Category; statusId?: number } | null>(null);
  // the lists that span every workspace (each with its own optional filter)
  const [recurring, setRecurring] = useState<{ highlight?: number } | null>(null);
  const [templates, setTemplates] = useState(false);
  // the archive dialog: the projects ticked to start with, and which finished gigs (both when unset)
  const [archiveDone, setArchiveDone] = useState<{ ids: number[]; category?: "done" | "cancelled" } | null>(null);
  const [timings, setTimings] = useState(false);
  const [trash, setTrash] = useState(false);
  const narrow = useNarrow();
  // the layout (wide screens; a phone shows one column at a time): statuses as columns, or as rows
  const rows = !narrow && prefs.board_layout === "rows";

  // the boards to load: the projects in view, plus the open gig's project
  const wantKey = [...scopeIds, ...(gigPid && !scopeIds.includes(gigPid) ? [gigPid] : [])].join(",");
  const loadSeq = useRef(0);
  const load = useCallback(() => {
    const ids = wantKey ? wantKey.split(",").map(Number) : [];
    const seq = ++loadSeq.current;
    Promise.allSettled(ids.map((id) => api.board(id))).then((res) => {
      if (seq !== loadSeq.current) return; // a newer load (another filter) is on its way
      const bad: string[] = [];
      // a board that failed to reload keeps its last answer (dropping it would close an open gig panel
      // and the text in it); one never loaded stays out
      setBoards((prev) => {
        const m = new Map<number, Board>();
        res.forEach((r, i) => {
          if (r.status === "fulfilled") m.set(ids[i]!, r.value);
          else if (prev.has(ids[i]!)) m.set(ids[i]!, prev.get(ids[i]!)!);
        });
        return m;
      });
      res.forEach((r, i) => {
        if (r.status === "rejected") bad.push(projOf(ids[i]!)?.project.name ?? `project ${ids[i]}`);
      });
      setFailed(bad);
      setLoaded(true);
    });
  }, [wantKey, projOf]);
  useEffect(load, [load]);
  // archived gigs: the board endpoint hides them, so the toggle lists them separately
  const archKey = filter.arch ? scopeIds.join(",") : "";
  useEffect(() => {
    if (!archKey) {
      setArchived([]);
      return;
    }
    let live = true;
    Promise.allSettled(archKey.split(",").map((id) => api.projectGigs(Number(id), { archived: "only", limit: 500 }))).then((res) => {
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
  // patchGig: put a freshly fetched gig in place on the board (same status: same column, same order)
  const patchRef = useRef((t: Gig) => {
    void t;
  });
  patchRef.current = (t: Gig) =>
    setBoards((cur) => {
      const b = cur.get(t.project_id);
      if (!b || !b.statuses.some((s) => s.gigs.some((x) => x.id === t.id && x.status_id === t.status_id))) return cur;
      const nb = { ...b, statuses: b.statuses.map((s) => ({ ...s, gigs: s.gigs.map((x) => (x.id === t.id ? t : x)) })) };
      return new Map(cur).set(t.project_id, nb);
    });
  useEffect(() => {
    const ids = new Set(wsKey ? wsKey.split(",").map(Number) : []);
    if (gigPid) {
      const w = projOf(gigPid)?.ws.id;
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
    // progress of a running agent changes one card: fetch that gig alone (debounced per gig)
    const progress = new Map<number, ReturnType<typeof setTimeout>>();
    const on = (e: Event) => {
      const d = (e as CustomEvent<{ workspaceId?: number; item?: { action?: string; data?: unknown } }>).detail;
      if (d?.workspaceId === undefined || !ids.has(d.workspaceId)) return;
      const tid = (d.item?.data as { gig_id?: unknown } | undefined)?.gig_id;
      if (d.item?.action === "run.progress" && typeof tid === "number") {
        clearTimeout(progress.get(tid));
        progress.set(
          tid,
          setTimeout(() => {
            progress.delete(tid);
            api.gig(tid).then((t) => patchRef.current(t), () => undefined);
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
  }, [wsKey, gigPid, projOf]);

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), toast.undo ? 6000 : 4000);
    return () => clearTimeout(t);
  }, [toast]);
  const changed = useCallback(
    (message?: string, undo?: () => void) => {
      load();
      refresh();
      if (message) setToast({ text: message, undo });
    },
    [load, refresh],
  );

  // a new gig opens straight in the form, which offers every project you can add to: it starts in the
  // project in view, else the one last used in view, else the first in view; with none of those
  // writable, the last used anywhere, else the first project of a workspace you can add to
  // the projects in view you can add gigs to (the role, and the Create gigs permission)
  const writable = inScope.filter((x) => canCreateGigIn(me, x.ws, x.project.id));
  // every workspace you can add to, with only the projects you can add to (the form may move the gig there)
  const newChoices = gigChoices(me, tree);
  const openNew = useCallback(() => {
    const last = lastProject();
    const at = (single && writable.includes(single) ? single : undefined) ?? writable.find((x) => x.project.id === last) ?? writable[0];
    if (at) return setAdding(at.project.id);
    const anywhere = newChoices.flatMap((w) => w.projects);
    const p = anywhere.find((x) => x.id === last) ?? anywhere[0];
    if (p) setAdding(p.id);
  }, [single, writable, newChoices]);
  const archivable = inScope.filter((x) => canWriteIn(x.ws, x.project.id) && hasCap(me, "archive_gigs", x.ws.id, x.project.id));
  const action = (what: BoardAction, category?: "done" | "cancelled") => {
    if (what === "archive") {
      // one dialog for any number of projects: up to three in view start ticked; more start unticked, so
      // one stray click can't archive across every workspace ("All projects in view" is one tick away)
      setArchiveDone({ ids: archivable.length <= 3 ? archivable.map((x) => x.project.id) : [], category });
    } else if (what === "schedules") setTimings(true);
    else if (what === "recurring") setRecurring({});
    else if (what === "templates") setTemplates(true);
    else {
      setNewIn(null);
      openNew();
    }
  };
  // ?new=1 (the top bar's New gig, the command palette) opens the new-gig flow; ?tool=… (the account
  // menu: recurring, schedules, templates, archive, trash) opens that dialog for the projects in view
  useEffect(() => {
    const s = new URLSearchParams(search);
    const isNew = s.get("new") === "1";
    const tool = s.get("tool");
    if (!isNew && !tool) return;
    s.delete("new");
    s.delete("tool");
    const rest = s.toString();
    navigate(loc + (rest ? "?" + rest : ""), { replace: true });
    if (tool === "trash") setTrash(true);
    else if (tool === "archive" || tool === "recurring" || tool === "schedules" || tool === "templates") action(tool);
    else if (isNew) {
      setNewIn(null);
      openNew();
    }
  }, [search, loc, navigate, openNew]); // eslint-disable-line react-hooks/exhaustive-deps

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

  // per-column sort, remembered per board like the collapsed columns
  const [sorts, setSorts] = useState<Record<string, ColumnSort>>(() => loadSorts(collapseKey));
  useEffect(() => setSorts(loadSorts(collapseKey)), [collapseKey]);
  const setSort = (col: string, sort: ColumnSort) =>
    setSorts((cur) => {
      const next = { ...cur, [col]: sort };
      saveSorts(collapseKey, next);
      return next;
    });
  const sortOf = (c: { key: string }): ColumnSort => sorts[c.key] ?? "manual";

  const allGigs = useMemo(() => [...boards.values()].flatMap((b) => b.statuses.flatMap((s) => s.gigs)), [boards]);
  const gigById = (id: number) => allGigs.find((t) => t.id === id);

  // moves: optimistic, then the server places the gig; a failure reloads the boards (putting back the
  // snapshot from before this move would also undo later moves and reloads)
  const sendMove = useCallback(
    (gigId: number, body: MoveBody, next: Map<number, Board>) => {
      const key = allGigs.find((t) => t.id === gigId)?.key ?? "the gig";
      setBoards(next);
      setMoveErr("");
      movesInFlight.current++;
      loadSeq.current++; // a load already on its way predates the move: drop its answer
      api
        .moveGig(gigId, body)
        .finally(() => movesInFlight.current--)
        .then(() => changed())
        .catch((e: unknown) => {
          const conflict = e instanceof ApiError && (e.status === 409 || e.status === 412);
          setMoveErr(conflict ? `${key} changed meanwhile; the board is reloaded.` : e instanceof ApiError ? `Couldn't move ${key}: ${e.message}` : `Couldn't move ${key}.`);
          load();
        });
    },
    [allGigs, changed, load],
  );
  const moveInProject = (gigId: number, statusId: number, index: number) => {
    const t = gigById(gigId);
    const b = t && boards.get(t.project_id);
    if (!t || !b) return;
    const m = applyMove(b, gigId, statusId, index);
    if (!m) return;
    sendMove(gigId, m.body, new Map(boards).set(t.project_id, m.board));
  };
  const moveToCat = (gigId: number, category: Category) => {
    const t = gigById(gigId);
    if (!t || t.status_category === category) return;
    const next = moveToCategory(boards, gigId, category);
    if (!next) {
      setMoveErr(`${t.key}'s project has no ${CATEGORY_LABEL[category]} column.`);
      return;
    }
    sendMove(gigId, { category }, next);
  };

  // cards glide to a new place (a drop, a move another user or an agent made): a card that changed column,
  // or changed position in a column whose number of cards stayed the same (a reorder), animates from where
  // it was. Cards that only shift because another one came or went (a filter, a new gig) don't move.
  // Positions are relative to the board (scrolling isn't a move) and re-read when the board resizes; the
  // layout is only read when the placements changed, and not while a card is being dragged.
  const boardRef = useRef<HTMLDivElement>(null);
  type Spot = { col: string; i: number; n: number; x: number; y: number };
  const placed = useRef(new Map<string, Spot>());
  const placedSig = useRef("");
  const placedCols = useRef("");
  const lastDragged = useRef<number | null>(null);
  if (drag !== null) lastDragged.current = drag;
  const measure = useCallback((animate: boolean, force = false) => {
    const root = boardRef.current;
    if (!root) return;
    const slots = [...root.querySelectorAll<HTMLElement>(".card-slot[data-slot]")];
    const sig = slots.map((el) => el.dataset.slot + "@" + el.dataset.place).join("|");
    if (!force && sig === placedSig.current) {
      lastDragged.current = null;
      return;
    }
    placedSig.current = sig;
    // another set of columns (one project in view instead of several, a column collapsed): a new layout,
    // not a move
    const cols = [...root.querySelectorAll<HTMLElement>(".column[data-col]")].map((el) => el.dataset.col).join("|");
    if (cols !== placedCols.current) animate = false;
    placedCols.current = cols;
    // offsets, not rects: they ignore transforms, so a card mid-glide (or a column rising in) still
    // reports its real place and nothing has to be cancelled
    const at = (el: HTMLElement | null) => {
      let x = 0;
      let y = 0;
      for (let e = el; e; e = e.offsetParent as HTMLElement | null) {
        x += e.offsetLeft;
        y += e.offsetTop;
      }
      return { x, y };
    };
    const origin = at(root);
    const next = new Map<string, Spot>();
    for (const el of slots) {
      const parts = (el.dataset.place ?? "").split(":"); // the column key may hold ":" itself
      const n = parts.pop() ?? "0";
      const i = parts.pop() ?? "0";
      const col = parts.join(":");
      const p = at(el);
      const now: Spot = { col, i: Number(i), n: Number(n), x: p.x - origin.x, y: p.y - origin.y };
      const was = placed.current.get(el.dataset.slot!);
      next.set(el.dataset.slot!, now);
      if (!animate || !was || String(lastDragged.current) === el.dataset.slot) continue;
      if (was.col === now.col && (was.i === now.i || was.n !== now.n)) continue;
      const dx = was.x - now.x;
      const dy = was.y - now.y;
      if (Math.abs(dx) < 2 && Math.abs(dy) < 2) continue;
      el.animate([{ transform: `translate(${dx}px, ${dy}px)` }, { transform: "none" }], { duration: 280, easing: "cubic-bezier(0.2, 0.8, 0.2, 1)" });
    }
    placed.current = next;
    lastDragged.current = null;
  }, []);
  useLayoutEffect(() => {
    if (drag === null) measure(!reducedMotion());
  });
  useEffect(() => {
    const root = boardRef.current;
    if (!root || typeof ResizeObserver === "undefined") return;
    // only a change of width moves cards sideways (a card moving changes the board's height: not a resize)
    let frame = 0;
    let width = root.offsetWidth;
    const ro = new ResizeObserver(() => {
      if (root.offsetWidth === width) return;
      width = root.offsetWidth;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => measure(false, true));
    });
    ro.observe(root);
    return () => {
      ro.disconnect();
      cancelAnimationFrame(frame);
    };
  }, [measure, loaded]);
  if (tree.every((w) => w.projects.length === 0)) return <p className="muted">No projects yet.</p>;
  if (!loaded) return <p className="muted">Loading…</p>;

  const me_ = me.user.id;
  // a manual order only makes sense on the whole column: with a gig filter on, drops go last
  const manual = !!single && !(filter.a || filter.l || filter.pr || filter.q);
  const columns: BoardColumn[] = (
    single
      ? boards.get(single.project.id)
        ? statusColumns(boards.get(single.project.id)!, filter, me_)
        : []
      : categoryColumns(scopeIds.map((id) => boards.get(id)).filter((b): b is Board => !!b), filter, me_)
  ).map((c) => ({ ...c, gigs: sortGigs(c.gigs, sortOf(c)) }));
  // drag-to-reorder and its drop line only where the column shows its manual order
  const manualIn = (c: BoardColumn) => manual && sortOf(c) === "manual";
  const countOpen = (cols: BoardColumn[]) => cols.filter((c) => c.category !== "done").reduce((n, c) => n + c.gigs.length, 0);
  const open = countOpen(columns);
  // with a gig filter (assignee, label, priority, search) on, the count says how many of all match
  const gigFiltered = !!(filter.a || filter.l || filter.pr || filter.q);
  const allOpen = !gigFiltered
    ? open
    : countOpen(
        (() => {
          const f: BoardFilter = { ...filter, a: "", l: "", pr: 0, q: "" };
          return single
            ? boards.get(single.project.id)
              ? statusColumns(boards.get(single.project.id)!, f, me_)
              : []
            : categoryColumns(scopeIds.map((id) => boards.get(id)).filter((b): b is Board => !!b), f, me_);
        })(),
      );
  const active = columns.find((c) => c.key === activeCol) ?? columns.find((c) => c.category === "todo") ?? columns[0];
  const shown = narrow && active ? [active] : columns;
  // collapsed columns leave the grid and become small chips above it (wide screens; a phone shows one column)
  const openCols = narrow ? shown : shown.filter((c) => !collapsed.includes(c.id));
  // which open column carries the "Archive done" footer: the last done-stage column that has gigs (one
  // project can have several; one footer is enough)
  let archiveCol: string | undefined;
  for (const c of openCols) if (c.category === "done" && c.gigs.length > 0) archiveCol = c.key;
  const closedCols = narrow ? [] : shown.filter((c) => collapsed.includes(c.id));
  // the agent side of the gigs in view, in one line above the columns
  const pulse = { running: 0, answer: 0, failed: 0, queued: 0, total: 0 };
  for (const c of columns)
    for (const t of c.gigs) {
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
  // the table of what's in view: the one project, the one workspace, else every workspace
  const tableHref = single ? `/w/${single.ws.slug}/p/${single.project.id}/table` : wsInScope.length === 1 ? `/w/${wsInScope[0]!.slug}/table` : "/table";

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
    if (manualIn(c) && !atEnd) {
      const cards = [...e.currentTarget.querySelectorAll<HTMLElement>("[data-gig-id]")].filter((el) => Number(el.dataset.gigId) !== drag);
      index = cards.findIndex((el) => {
        const r = el.getBoundingClientRect();
        return rows ? e.clientX < r.left + r.width / 2 : e.clientY < r.top + r.height / 2; // rows: cards run left to right
      });
      if (index < 0) index = cards.length;
    }
    if (drop?.col !== c.key || drop.index !== index) setDrop({ col: c.key, index });
  };
  const dropOn = (e: DragEvent<HTMLElement>, c: BoardColumn) => {
    if (drag === null) return;
    e.preventDefault();
    if (c.statusId !== undefined) {
      const all = boards.get(single!.project.id)?.statuses.find((s) => s.id === c.statusId)?.gigs ?? c.gigs; // unfiltered
      const index = manualIn(c) && drop?.col === c.key && drop.index >= 0 ? drop.index : all.filter((t) => t.id !== drag).length;
      moveInProject(drag, c.statusId, index);
    } else moveToCat(drag, c.category);
    endDrag();
  };
  const dragLeave = (e: DragEvent<HTMLElement>, c: BoardColumn) => {
    if (drop?.col === c.key && !e.currentTarget.contains(e.relatedTarget as Node | null)) setDrop(null);
  };
  const colColor = (c: BoardColumn): string => c.color ?? CATEGORY_COLOR[c.category] ?? "var(--muted)";
  // the column name sits on a filled label of its stage colour (a hex colour, so the text colour can be picked)
  const stageBg = (c: BoardColumn): string => c.color ?? CATEGORY_COLOR[c.category] ?? CATEGORY_COLOR.backlog!;
  // a column's "+": the form starts in that column's status (or its stage, in another project)
  const canAddIn = (c: BoardColumn) => writable.length > 0 && c.category !== "cancelled";
  const addIn = (c: BoardColumn) => {
    setNewIn({ category: c.category, statusId: c.statusId });
    openNew();
  };
  const cardFor = (t: Gig, c: BoardColumn, i: number, archivedCard: boolean) => {
    const p = projOf(t.project_id);
    const b = boards.get(t.project_id);
    if (!p || !b) return null;
    const write = !archivedCard && canWriteIn(p.ws, p.project.id);
    // done gigs get a one-click archive (the Done column's footer archives them all)
    const archive = write && t.status_category === "done" && hasCap(me, "archive_gigs", p.ws.id, p.project.id);
    return (
      <Card
        gig={t}
        statuses={openStatuses(b)}
        ws={p.ws}
        showWs={wsInScope.length > 1}
        labels={b.labels}
        canWrite={write}
        canArchive={archive}
        canReorder={write && c.statusId !== undefined && manualIn(c)}
        statusLabel={c.statusId === undefined && t.status_name !== c.name ? t.status_name : undefined}
        dragging={t.id === drag}
        onDragStart={() => {
          dragTimer.current = setTimeout(() => setDrag(t.id), 0);
        }}
        onDragEnd={endDrag}
        onReorder={(where) => c.statusId !== undefined && moveInProject(t.id, c.statusId, reorderIndex(where, i, c.gigs.length))}
        first={i === 0}
        last={i === c.gigs.length - 1}
        onOpen={() => navigate(`/board/p/${t.project_id}/g/${t.key}${qs}`)}
        onChanged={changed}
        fields={fields.get(p.ws.id)}
      />
    );
  };

  const panel = gigPid && gigKey ? projOf(gigPid) : null;
  const panelBoard = gigPid ? boards.get(gigPid) : undefined;
  // the new gig's start project: its board when in view (statuses, labels), else the form fetches them
  const addAt = adding === null ? null : projOf(adding);
  const addBoard = adding === null ? undefined : boards.get(adding);

  // the far end of the filters row: the count, help, Board | Table and Columns | Rows
  const viewTools = (
    <>
      <span className="muted bf-count">
        {gigFiltered ? `${open} of ${allOpen} open` : `${open} open gig${open === 1 ? "" : "s"}`}
      </span>
      <HelpButton topic="board" label="Help: the board" />
      <div className="view-switch" role="group" aria-label="View">
        <span className="vs-item on" aria-current="page">
          Board
        </span>
        <Link href={tableHref} className="vs-item">
          Table
        </Link>
      </div>
      {!narrow && (
        <div className="view-switch layout-switch" role="group" aria-label="Board layout">
          <button type="button" className={"vs-item" + (rows ? "" : " on")} aria-pressed={!rows} data-tip="Statuses as columns" onClick={() => setPref("board_layout", "columns")}>
            <Icon name="columns" />
            <span className="sr-only">Columns</span>
          </button>
          <button type="button" className={"vs-item" + (rows ? " on" : "")} aria-pressed={rows} data-tip="Statuses as rows: cards run left to right" onClick={() => setPref("board_layout", "rows")}>
            <Icon name="rows" />
            <span className="sr-only">Rows</span>
          </button>
        </div>
      )}
    </>
  );

  return (
    <div className="board-page">
      <h1 className="sr-only">{single ? single.project.name : "Gigs"}</h1>
      <BoardFilters filter={filter} setFilter={setFilter} tree={tree} labelNames={labelNames} people={people} narrow={narrow} end={viewTools} />
      {failed.length > 0 && (
        <p className="danger small" role="alert">
          Couldn't load {failed.join(", ")}.
        </p>
      )}
      {[...boards.values()].some((b) => b.truncated) && (
        <p className="muted small" role="status">
          {[...boards.values()].filter((b) => b.truncated).map((b) => b.project.name).join(", ")}: more gigs than a board shows. Archive finished ones, or use Table to see them all.
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
              <span className="count">{c.gigs.length}</span>
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
            <Link href="/inbox" className="pulse-item pulse-answer" data-tip="Gigs whose agent asked a question: answer them from the Inbox or the gig">
              <Icon name="question" />
              {pulse.answer} need{pulse.answer === 1 ? "s" : ""} an answer
            </Link>
          )}
          {pulse.failed > 0 && (
            <span className="pulse-item pulse-failed" tabIndex={0} data-tip="Gigs whose agent's last run failed, timed out or was interrupted: Retry on the card resumes it">
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
              <span className="count-pill">{c.gigs.length}</span>
            </button>
          ))}
        </div>
      )}
      {openCols.length === 0 && closedCols.length > 0 && <p className="muted">Every column is collapsed: click one above to show it.</p>}
      <div className={"board" + (rows ? " board-rows" : "")} ref={boardRef} style={rows ? undefined : { gridTemplateColumns: `repeat(${Math.max(1, openCols.length)}, minmax(0, 1fr))` }}>
        {openCols.map((c) => {
          const others = c.gigs.filter((t) => t.id !== drag);
          const line = drop?.col === c.key ? drop.index : -1;
          let k = -1;
          const addHere = canAddIn(c);
          return (
            <section
              key={c.key}
              className={"column" + (drop?.col === c.key ? " drag-over" : "")}
              data-col={c.key}
              aria-label={c.name}
              role={narrow ? "tabpanel" : undefined}
              onDragOver={(e) => dragOver(e, c)}
              onDrop={(e) => dropOn(e, c)}
              onDragLeave={(e) => dragLeave(e, c)}
            >
              <div className="column-head">
                <h2 className="column-title" tabIndex={-1} data-col-focus={c.id}>
                  <span className="stage-label" style={{ "--stage": stageBg(c), color: readableOn(stageBg(c)) } as CSSProperties}>
                    <StageIcon category={c.category} color="currentColor" />
                    <span className="column-name">{c.name}</span>
                  </span>
                  <span className="count-pill" aria-label={`${c.gigs.length} gig${c.gigs.length === 1 ? "" : "s"}`}>
                    {c.gigs.length}
                  </span>
                </h2>
                {sortOf(c) !== "manual" && <SortMenu column={c.name} sorts={sortsFor(c.category)} value={sortOf(c)} onChange={(s) => setSort(c.key, s)} />}
                <span className="column-tools">
                  {sortOf(c) === "manual" && <SortMenu column={c.name} sorts={sortsFor(c.category)} value={sortOf(c)} onChange={(s) => setSort(c.key, s)} />}
                  {addHere && (
                    <button type="button" className="icon-button" aria-label={`New gig in ${c.name}`} onClick={() => addIn(c)}>
                      <Icon name="plus" strokeWidth={2} />
                    </button>
                  )}
                  {!narrow && (
                    <button type="button" className="icon-button col-collapse" aria-expanded={true} aria-label={`Collapse ${c.name}: it becomes a small button above the board`} onClick={() => toggleCollapsed(c.id)}>
                      <Icon name="shrink" />
                    </button>
                  )}
                </span>
              </div>
              <div className="cards">
                {c.gigs.map((t, i) => {
                  if (t.id !== drag) k++;
                  const showLine = manualIn(c) && c.statusId !== undefined && t.id !== drag && k === line;
                  return (
                    <div key={t.id} className="card-slot" data-slot={t.id} data-place={`${c.key}:${i}:${c.gigs.length}`}>
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
                {manualIn(c) && c.statusId !== undefined && line === others.length && <div className="drop-line" aria-hidden="true" />}
                {c.gigs.length === 0 && (drag !== null || archivedIn(c).length === 0) && <div className="empty">{drag !== null ? "Drop here" : "Nothing here"}</div>}
                {addHere && c.category !== "done" && (
                  <button type="button" className="column-add" onClick={() => addIn(c)}>
                    <Icon name="plus" strokeWidth={2} /> New gig
                  </button>
                )}
                {c.category === "done" &&
                  // Done takes no new gigs: its footer archives the done gigs instead (once: one project can
                  // have several done columns), or stays as an empty slot of the same height so the columns
                  // line up. The board never shows cancelled gigs, so they have no column or footer.
                  (archiveCol === c.key && archivable.length > 0 ? (
                    <button type="button" className="column-add" onClick={() => action("archive", "done")}>
                      <Icon name="archive" /> Archive done
                    </button>
                  ) : (
                    writable.length > 0 && <div className="column-add column-add-empty" aria-hidden="true">{"\u00a0"}</div>
                  ))}
              </div>
            </section>
          );
        })}
      </div>
      {addAt && (
        <GigModal
          ws={addAt.ws}
          projectId={addAt.project.id}
          statuses={addBoard ? openStatuses(addBoard) : []}
          labels={addBoard?.labels ?? []}
          choices={newChoices}
          initialStatus={newIn && addBoard ? (newIn.statusId ?? openStatuses(addBoard).find((s) => s.category === newIn.category)?.id) : undefined}
          initialCategory={newIn?.category}
          onClose={() => {
            setAdding(null);
            setNewIn(null);
          }}
          onCreated={changed}
        />
      )}
      {recurring && <RecurringModal highlight={recurring.highlight} onClose={() => setRecurring(null)} onChanged={changed} />}
      {timings && <SchedulesModal onClose={() => setTimings(false)} onChanged={changed} />}
      {templates && <TemplatesModal onClose={() => setTemplates(false)} onChanged={changed} />}
      {archiveDone !== null && (
        <ArchiveDoneModal
          options={archivable}
          initial={archiveDone.ids}
          category={archiveDone.category}
          onClose={() => setArchiveDone(null)}
          onDone={(n) => changed(n === 0 ? "Nothing to archive" : `Archived ${n} gig${n === 1 ? "" : "s"}`)}
        />
      )}
      {trash && <TrashModal scope={inScope} onClose={() => setTrash(false)} onRestored={(key) => changed(`Restored ${key}`)} />}
      {panel && gigKey && panelBoard && (
        <GigPanel
          gigKey={gigKey}
          ws={panel.ws}
          statuses={openStatuses(panelBoard)}
          labels={panelBoard.labels}
          canWrite={canWriteIn(panel.ws, panel.project.id)}
          // opened from the table, My gigs or the bell: closing goes back there
          onClose={() => {
            // opened from a list (pushed from there): step back to it, so Back afterwards doesn't
            // reopen the panel; a gig moved meanwhile replaced its entry, so one step is enough
            if (backTo()) history.back();
            else navigate(boardURL);
          }}
          onChanged={changed}
          onMoved={(t) => navigate(`/board/p/${t.project_id}/g/${t.key}${qs}`, backTo() ? { state: history.state, replace: true } : undefined)}
          onOpenSchedule={(id) => setRecurring({ highlight: id })}
        />
      )}
      {toast && (
        <div className="toast" role="status">
          {toast.text}
          {toast.undo && (
            <button
              type="button"
              className="toast-undo"
              onClick={() => {
                const undo = toast.undo;
                setToast(null);
                undo?.();
              }}
            >
              Undo
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// BoardFilters: one toolbar row (search, assignee, label, priority, the archived toggle, Clear filters) and under it
// the workspaces and projects as compact toggle chips (none on = all).
function BoardFilters({ filter, setFilter, tree, labelNames, people, narrow, end }: {
  filter: BoardFilter;
  setFilter: (f: BoardFilter) => void;
  tree: WorkspaceTree[];
  labelNames: string[];
  people: { id: number; username: string; display_name: string }[];
  narrow: boolean;
  end?: ReactNode; // the row's far end (counts and view switches)
}) {
  const { users, prefs, setPref } = useApp();
  // folded: one line with the filters that are on (removable), or just the Filters button. The row starts
  // folded; on wide screens opening it is saved, on a phone it starts folded each time.
  const [narrowOpen, setNarrowOpen] = useState(false);
  const shut = narrow ? !narrowOpen : prefs.board_filters !== "open";
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
  // the workspace and project chips; open, the view tools end their row (the fields row is full)
  const scopeRow = tree.length > 1 || projects.length > 1;
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
    project: (id) => {
      const w = tree.find((x) => x.projects.some((p) => p.id === id));
      const p = w?.projects.find((x) => x.id === id);
      return p && w && { name: p.name, color: colorFor(w.slug, w.color) };
    },
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
      <Icon name="filter" />
      Filters
      {shut && active.length > 0 && <span className="count">{active.length}</span>}
      <Icon name={shut ? "chevron-right" : "chevron-down"} />
    </button>
  );
  if (shut) {
    return (
      <div className="board-filters bf-folded" id="board-filters" role="group" aria-label="Board filters (folded)">
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
          {end && <div className="bf-end">{end}</div>}
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
          <input type="search" aria-label="Search gigs" placeholder="Search key, title, description" value={text} onChange={(e) => setText(e.target.value)} />
        </span>
        <label className="bf-pick">
          <span className="bf-pick-label" aria-hidden="true">Assignee</span>
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
        </label>
        <label className="bf-pick">
          <span className="bf-pick-label" aria-hidden="true">Label</span>
          <select className={sel(filter.l)} aria-label="Label" value={filter.l} onChange={(e) => setFilter({ ...filter, l: e.target.value })}>
            <option value="">Any label</option>
            {labelNames.map((l) => (
              <option key={l} value={l}>
                {l}
              </option>
            ))}
          </select>
        </label>
        <label className="bf-pick">
          <span className="bf-pick-label" aria-hidden="true">Priority</span>
          <select className={sel(filter.pr)} aria-label="Priority" value={filter.pr} onChange={(e) => setFilter({ ...filter, pr: Number(e.target.value) })}>
            <option value={0}>Any priority</option>
            {priorityNames.slice(1).map((n, i) => (
              <option key={n} value={i + 1}>
                {i + 1 === 4 ? n : `${n} or higher`}
              </option>
            ))}
          </select>
        </label>
        <button
          type="button"
          className={"bf-toggle" + (filter.arch ? " on" : "")}
          aria-pressed={filter.arch}
          data-tip="Show archived gigs too, greyed out, in their columns"
          onClick={() => setFilter({ ...filter, arch: !filter.arch })}
        >
          <Icon name="archive" />
          Archived
        </button>
        <ClearFilters className="bf-clear" active={isFiltered(filter)} onClear={() => setFilter({ ...EMPTY_FILTER, arch: filter.arch })} />
        {end && !scopeRow && <div className="bf-end">{end}</div>}
      </div>
      {scopeRow && (
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
                    className={"bf-chip bf-ws" + (on ? " on" : "")}
                    aria-pressed={on}
                    style={{ "--c": colorFor(w.slug, w.color) } as CSSProperties}
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
          {end && <div className="bf-end">{end}</div>}
        </div>
      )}
    </div>
  );
}

// TrashModal: the deleted gigs of the projects in view, newest first, each with Restore (for people
// who may delete gigs there).
function TrashModal({ scope: inScope, onClose, onRestored }: { scope: { ws: WorkspaceTree; project: { id: number; key: string; name: string } }[]; onClose: () => void; onRestored: (key: string) => void }) {
  const { me } = useApp();
  const [items, setItems] = useState<Gig[] | null>(null);
  const [err, setErr] = useState("");
  const pids = inScope.map((x) => x.project.id).join(",");
  const load = useCallback(() => {
    const ids = new Set(pids.split(",").map(Number));
    const wss = [...new Map(inScope.map((x) => [x.ws.id, x.ws])).values()];
    Promise.allSettled(wss.map((w) => viewsApi.gigs(w.slug, { deleted: true, limit: 500 }))).then((res) => {
      const all = res.flatMap((r) => (r.status === "fulfilled" ? r.value.items : [])).filter((t) => ids.has(t.project_id));
      setItems(all.sort((a, b) => b.updated_at - a.updated_at));
      setErr(res.some((r) => r.status === "rejected") ? "Some workspaces couldn't be read." : "");
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pids]);
  useEffect(load, [load]);
  const wsOf = (t: Gig) => inScope.find((x) => x.ws.id === t.workspace_id)?.ws;
  const restore = (t: Gig) =>
    api
      .restoreGig(t.id)
      .then(() => {
        onRestored(t.key);
        load();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : `Couldn't restore ${t.key}.`));
  return (
    <Modal title="Trash" onClose={onClose} wide help={<HelpButton topic="board" label="Help: the board (archive and trash)" />}>
      <p className="muted small">Deleted gigs of the projects in view, most recently changed first. Restoring puts a gig back where it was.</p>
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
          const may = !!ws && canWriteIn(ws, t.project_id) && hasCap(me, "delete_gigs", ws.id, t.project_id);
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
                data-tip={may ? `Restore ${t.key} to its column` : "Restoring needs the Delete gigs permission here"}
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

// ArchiveDoneModal: archive the finished gigs (done, cancelled, or both when `category` is unset) of the
// projects ticked, one call per project; a failure stops there and says which projects were done.
function ArchiveDoneModal({ options, initial, category, onClose, onDone }: {
  options: { ws: WorkspaceTree; project: { id: number; key: string; name: string } }[];
  initial: number[];
  category?: "done" | "cancelled";
  onClose: () => void;
  onDone: (n: number) => void;
}) {
  const what = category ?? "done and cancelled";
  const verb = category === "cancelled" ? "Cancelled" : category === "done" ? "Done" : "Finished";
  const [days, setDays] = useState(7);
  const [ticked, setTicked] = useState<number[]>(initial);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const total = useRef(0); // archived so far, over a failed attempt and its retry
  // only projects still on offer count (the board's projects can change while the dialog is open)
  const picked = options.map((o) => o.project.id).filter((id) => ticked.includes(id));
  const all = options.length > 0 && picked.length === options.length;
  const allBox = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (allBox.current) allBox.current.indeterminate = picked.length > 0 && !all;
  });
  const toggle = (id: number) => setTicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]));
  const close = () => {
    if (busy) return; // the loop finishes first
    if (total.current > 0) onDone(total.current); // a partial run that was given up: still refresh and say so
    onClose();
  };
  const go = async () => {
    setBusy(true);
    setErr("");
    for (const { project } of options.filter((o) => picked.includes(o.project.id))) {
      try {
        total.current += (await api.archiveDone(project.id, days, category)).archived;
        setTicked((p) => p.filter((x) => x !== project.id)); // done: a retry skips it
      } catch (e: unknown) {
        const why = e instanceof ApiError ? e.message : "it failed";
        setErr(`Couldn't archive in ${project.key}: ${why}.${total.current ? ` ${total.current} archived so far; the projects done are unticked.` : ""}`);
        setBusy(false);
        return;
      }
    }
    setBusy(false);
    onDone(total.current);
    onClose();
  };
  return (
    <Modal title={`Archive ${what} gigs`} onClose={close}>
      <p className="small">
        Archived gigs leave the board and lists; they stay searchable and can be unarchived from the gig. Every {what} gig of the projects ticked is
        archived: the assignee, label, priority and search filters don't apply here.
      </p>
      {options.length === 0 ? (
        <p className="muted small">No project in view allows archiving.</p>
      ) : (
        options.length > 1 && (
          <fieldset className="archive-pick">
            <legend className="small muted">Projects</legend>
            <label className="archive-row archive-all">
              <input ref={allBox} type="checkbox" checked={all} onChange={() => setTicked(all ? [] : options.map((o) => o.project.id))} />
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
      <div className="archive-days" role="radiogroup" aria-label={`${verb} at least this long ago`}>
        {[0, 7, 30].map((d) => (
          <button key={d} type="button" role="radio" aria-checked={days === d} className={"assignee" + (days === d ? " on" : "")} onClick={() => setDays(d)}>
            {d === 0 ? `All ${what}` : `${verb} ${d}+ days ago`}
          </button>
        ))}
      </div>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      <div className="row end">
        <button type="button" className="button secondary" onClick={close} disabled={busy}>
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
// SortMenu: a column's sort, as an icon button with a drop-down of the orders it offers. Sorted columns
// show it lit, beside the title.
function SortMenu({ column, sorts, value, onChange }: { column: string; sorts: ColumnSort[]; value: ColumnSort; onChange: (s: ColumnSort) => void }) {
  const on = value !== "manual";
  return (
    <MoreMenu
      label={on ? `${column}: sorted by ${SORT_LABEL[value].toLowerCase()}` : `Sort ${column}`}
      className={"col-sort" + (on ? " on" : "")}
      buttonClass={"icon-button" + (on ? " on" : "")}
      face={<Icon name="sort" />}
    >
      <div className="menu-label">Sort</div>
      {sorts.map((s) => (
        <button key={s} type="button" role="menuitemradio" aria-checked={s === value} onClick={() => onChange(s)}>
          <span className="menu-check" aria-hidden="true">{s === value ? "✓" : ""}</span> {SORT_LABEL[s]}
        </button>
      ))}
    </MoreMenu>
  );
}

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
          <path d="m5 8.2 2 2 4-4.2" fill="none" stroke="var(--stage-check, var(--surface))" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
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

function Card({ gig, statuses, ws, showWs, labels, canWrite, canArchive, canReorder, statusLabel, dragging, first, last, onDragStart, onDragEnd, onReorder, onOpen, onChanged, fields }: {
  gig: Gig;
  statuses: Status[];
  ws: WorkspaceTree;
  showWs: boolean; // several workspaces in view: name the gig's workspace
  labels: Tag[];
  canWrite: boolean;
  canArchive: boolean; // a done gig you may archive: the card shows a one-click Archive
  canReorder: boolean; // a manual order exists (one project in view)
  statusLabel?: string; // the gig's own status, when the column (a category) is named otherwise
  dragging: boolean;
  first: boolean;
  last: boolean;
  onDragStart: () => void;
  onDragEnd: () => void;
  onReorder: (where: "top" | "up" | "down" | "bottom") => void;
  onOpen: () => void;
  onChanged: (message?: string, undo?: () => void) => void;
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
    // Esc closes it (and only it), focus back on its button
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.preventDefault();
      e.stopPropagation();
      setMenu(false);
      ref.current?.querySelector<HTMLButtonElement>("button")?.focus();
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", key, true);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", key, true);
    };
  }, [menu]);
  // near the bottom of the screen the menu opens upwards, so all of it stays in view
  const [up, setUp] = useState(false);
  useLayoutEffect(() => {
    if (!menu) return setUp(false);
    const m = ref.current?.querySelector<HTMLElement>(".menu");
    const b = ref.current?.getBoundingClientRect();
    if (!m || !b) return;
    const h = m.getBoundingClientRect().height;
    setUp(b.bottom + h + 8 > window.innerHeight && b.top - h - 8 > 0);
  }, [menu]);
  const move = (body: MoveBody) => {
    setMenu(false);
    api
      .moveGig(gig.id, body)
      .then(() => onChanged())
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Move failed."));
  };
  const reorder = (where: "top" | "up" | "down" | "bottom") => {
    setMenu(false);
    onReorder(where);
  };
  const overdue = gig.due_at !== null && gig.due_at < Date.now() && gig.status_category !== "done";
  const creator = users.get(gig.created_by);
  const labelFor = (l: Tag) => labels.find((x) => x.id === l.id) ?? l;
  const showDue = gig.due_at && gig.due_at !== gig.not_after; // a window's end is shown by its own badge
  const assignees = gig.assignees.map((id) => users.get(id)).filter((u): u is NonNullable<typeof u> => !!u);
  const nameOf = (u: { username: string; display_name: string }) => u.display_name || u.username;
  const shownLabels = gig.labels.slice(0, 2);
  const moreLabels = gig.labels.slice(2);
  // who: the assignees, stacked; the creator only when there are no assignees, or a person handed the gig to an agent
  const handOff = !!creator && creator.kind !== "agent" && assignees.some((u) => u.kind === "agent");
  const createdBy = creator ? `Created by ${nameOf(creator)}` : "";
  const assigneeTip = (u: { username: string; display_name: string; kind: string }) => `Assigned to ${nameOf(u)}${u.kind === "agent" ? " (agent)" : ""}${createdBy ? `\n${createdBy}` : ""}`;
  const done = gig.status_category === "done" ? gig.done_at : null;
  const state = cardState(gig);
  const run = gig.last_run;
  const running = state?.kind === "running";
  const now = useNow(running ? 1000 : 0);
  const worker = run ? users.get(run.worker_user_id) : undefined;
  const asker = gig.question ? users.get(gig.question.author_id) : undefined;
  const [retrying, setRetrying] = useState(false);
  const [archiving, setArchiving] = useState(false);
  const archive = () => {
    setArchiving(true);
    setErr("");
    api
      .archiveGig(gig.id)
      .then(() =>
        onChanged(`Archived ${gig.key}`, () =>
          api
            .unarchiveGig(gig.id)
            .then(() => onChanged(`${gig.key} is back on the board`))
            .catch((e: unknown) => onChanged(`Couldn't unarchive ${gig.key}: ${e instanceof ApiError ? e.message : "try again from the gig"}`)),
        ),
      )
      .catch((e: unknown) => {
        setErr(e instanceof ApiError ? e.message : "Archive failed.");
        setArchiving(false);
      });
  };
  // the button's name replaces its contents for screen readers: say the agent state there too
  const cardLabel = [
    `${gig.key} ${gig.title}`,
    state && (running && run?.progress_note ? `${state.label}: ${run.progress_note}` : state.label), // no clock: the name would change every second
    state?.kind === "answer" && gig.question ? plainText(gig.question.text) : "",
  ]
    .filter(Boolean)
    .join(". ");
  const retry = () => {
    setRetrying(true);
    setErr("");
    api
      .resumeGig(gig.id, "")
      .then(() => onChanged())
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Retry failed."))
      .finally(() => setRetrying(false));
  };
  return (
    <article
      className={"gig-card" + (state ? " state-" + state.kind : "") + (dragging ? " dragging" : "") + (gig.missed_at ? " missed" : "") + (gig.archived_at ? " archived" : "") + (gig.blocked_by ? " blocked" : "") + (gig.color ? " colored" : "")}
      style={gig.color ? ({ "--card-c": gig.color } as CSSProperties) : undefined}
      data-gig-id={gig.id}
      draggable={canWrite}
      onDragStart={(e) => {
        e.dataTransfer.effectAllowed = "move";
        e.dataTransfer.setData("text/plain", gig.key);
        setMenu(false);
        onDragStart();
      }}
      onDragEnd={onDragEnd}
    >
      <button type="button" className="card-open" onClick={onOpen} aria-label={cardLabel}>
        <div className="card-top">
          <PriorityIcon priority={gig.priority} />
          {state && (
            <span className={"run-badge run-" + state.kind} data-tip={state.tip}>
              {running ? <span className="pulse-dot" aria-hidden="true" /> : <Icon name={STATE_ICON[state.kind]} />}
              {state.label}
              {running && run && <span className="mono">· {elapsed(now - run.started_at)}</span>}
            </span>
          )}
          {/* with a run state, the workspace and the key always start the next line (never just the key) */}
          {state && showWs && <span className="card-break" aria-hidden="true" />}
          {showWs && <WorkspaceChip ws={ws} />}
          {statusLabel && (
            <span className="card-status" data-tip={`Its project's status: ${statusLabel}`}>
              {statusLabel}
            </span>
          )}
          {!!gig.archived_at && (
            <span className="pill pill-muted" data-tip="Archived: hidden from the board and lists; unarchive it from the gig">
              <Icon name="archive" /> archived
            </span>
          )}
          <span className="pill pill-key card-key">{gig.key}</span>
        </div>
        <div className="card-title">{gig.title}</div>
        {(gig.labels.length > 0 || showDue) && (
          <div className="chips">
            {showDue && (
              <span className={"fact-chip" + (overdue ? " fact-overdue" : "")} data-tip={overdue ? "Overdue" : "Due"}>
                <Icon name="calendar" /> {fmtDue(gig.due_at)}
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
        {state?.kind === "answer" && gig.question && (
          <div className="card-ask">
            <span className="card-ask-label">
              <Icon name="question" /> {gig.question.kind === "question" ? (asker ? `${nameOf(asker)} asks` : "Question") : asker ? `${nameOf(asker)} reports` : "Report"}
            </span>
            <p>{plainText(gig.question.text)}</p>
          </div>
        )}
        <div className="card-foot">
          <span className="meta">
            <GigBadges gig={gig} items fields={fields} />
            {gig.comment_count > 0 && (
              <span className="meta-item" data-tip={`${gig.comment_count} comment${gig.comment_count === 1 ? "" : "s"}`}>
                <Icon name="comment" /> {gig.comment_count}
              </span>
            )}
            {gig.session_id && !running && (
              <span className="meta-item" data-tip="An agent has worked on it: it has an agent session (see Runs in the gig)">
                <Icon name="bot" />
              </span>
            )}
            {gig.cost_usd_total > 0 && (
              <span className="meta-item mono" data-tip="What the agent's runs on it cost so far (API-equivalent USD, as reported by the agent)">
                ${gig.cost_usd_total.toFixed(2)}
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
        {(gig.schedule || done) && (
          <div className="card-when">
            {gig.schedule && (
              <span className="meta-item" data-tip={`Schedule: ${gig.schedule.name}`}>
                <Icon name="refresh" /> {gig.schedule.text}
              </span>
            )}
            {done && (
              <span className="meta-item card-done-at" data-tip={`Done ${fmtTime(done)}`}>
                <Icon name="check" /> Done {relTime(done)}
              </span>
            )}
          </div>
        )}
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
              aria-label={`Retry ${gig.key}`}
              data-tip={
                gig.session_id
                  ? "Resume the agent's session: the gig goes back to To-Do and its runner continues on its next pass"
                  : "Back to To-Do: the agent's runner starts it again on its next pass"
              }
            >
              <Icon name="refresh" /> Retry
            </button>
          )}
        </div>
      )}
      {canArchive && !gig.archived_at && (
        <button
          type="button"
          className="icon-button card-archive"
          disabled={archiving}
          onClick={archive}
          aria-label={`Archive ${gig.key}`}
          data-tip="Archive: off the board and lists, still searchable (Undo in the message that follows)"
        >
          <Icon name="archive" />
        </button>
      )}
      {canWrite && (
        <div className="card-menu" ref={ref}>
          <button type="button" className="icon-button" aria-label={`Move ${gig.key}`} aria-expanded={menu} onClick={() => setMenu(!menu)}>
            ⋯
          </button>
          {menu && (
            <div className={"menu" + (up ? " menu-up" : "")} role="menu">
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
                .filter((s) => s.id !== gig.status_id)
                .map((s) => (
                  <button key={s.id} type="button" role="menuitem" onClick={() => move({ status: s.id })}>
                    <span className="dot" style={{ background: s.color ?? CATEGORY_COLOR[s.category] }} /> {s.name}
                  </button>
                ))}
              {ws.projects.filter((p) => p.id !== gig.project_id).length > 0 && <div className="menu-label">Move to project</div>}
              {ws.projects
                .filter((p) => p.id !== gig.project_id)
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
