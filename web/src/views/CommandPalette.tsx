// Command palette (Ctrl+K / Cmd+K): jump to a task by key, to a project or workspace, run a
// navigation command, or search task text across every workspace.
import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";
import { useLocation } from "wouter";
import { api, type Task } from "../api";
import { useApp, type WorkspaceTree } from "../App";
import { colorFor } from "../theme";
import { Pill, PriorityBadge, StatusBadge } from "../ui";
import { summary, viewHref } from "./savedViews";
import { errMsg, fuzzy, taskHref, tasksAcross } from "./viewsApi";
import "./views.css";

interface Item {
  id: string;
  group: string;
  icon: ReactNode;
  label: ReactNode;
  hint?: ReactNode;
  run: () => void | Promise<void>;
  keep?: boolean; // don't close the palette after running
}

const KEY_RE = /^[A-Za-z][A-Za-z0-9]*-\d+$/;

// CommandPalette installs the Ctrl/Cmd+K listener on window and renders the dialog when open.
// Mount it once inside the app context.
export function CommandPalette() {
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  return open ? <Palette onClose={() => setOpen(false)} /> : null;
}

// where the user is: the workspace and project in the URL, if any
function context(loc: string, tree: WorkspaceTree[]) {
  const m = /^\/w\/([^/]+)(?:\/p\/(\d+))?/.exec(loc);
  const ws = m ? tree.find((w) => w.slug === m[1]) : undefined;
  const pid = m?.[2] ? Number(m[2]) : undefined;
  return { ws, pid: pid && ws?.projects.some((p) => p.id === pid) ? pid : undefined };
}

function Palette({ onClose }: { onClose: () => void }) {
  const { me, tree, savedViews } = useApp();
  const [loc, navigate] = useLocation();
  const [text, setText] = useState("");
  const [active, setActive] = useState(0);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [results, setResults] = useState<{ q: string; tasks: Task[]; failed: string[] } | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLUListElement>(null);

  // focus the input on open, give focus back on close
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    inputRef.current?.focus();
    return () => prev?.focus();
  }, []);

  const go = useCallback(
    (href: string) => {
      navigate(href);
      onClose();
    },
    [navigate, onClose],
  );

  const items = useMemo<Item[]>(() => {
    const q = text.trim();
    const out: Item[] = [];
    const { ws: here, pid } = context(loc, tree);
    const firstBoard = tree.find((w) => w.projects.length > 0);
    const boardHref = here && pid ? `/w/${here.slug}/p/${pid}` : here?.projects[0] ? `/w/${here.slug}/p/${here.projects[0].id}` : firstBoard ? `/w/${firstBoard.slug}/p/${firstBoard.projects[0]!.id}` : null;
    const tableWs = here ?? tree[0];

    // 1. task key
    if (KEY_RE.test(q)) {
      const key = q.toUpperCase();
      out.push({
        id: "key",
        group: "Go to task",
        icon: "↪",
        label: (
          <>
            Open <span className="pill pill-key">{key}</span>
          </>
        ),
        keep: true,
        run: async () => {
          setBusy("key");
          setError("");
          try {
            const t = await api.task(key);
            go(taskHref(tree, t));
          } catch (e) {
            setError(`${key}: ${errMsg(e)}`);
          } finally {
            setBusy("");
          }
        },
      });
    }

    // 2. search results (shown while they still match the query)
    if (results && results.q === q) {
      for (const t of results.tasks) {
        const w = tree.find((x) => x.id === t.workspace_id);
        out.push({
          id: `task-${t.id}`,
          group: `Tasks matching “${results.q}”`,
          icon: <span className="pill pill-key">{t.key}</span>,
          label: t.title,
          hint: (
            <>
              {w && tree.length > 1 && <span className="pill pill-ws">{w.name}</span>}
              <StatusBadge name={t.status_name} category={t.status_category} />
              <PriorityBadge priority={t.priority} />
            </>
          ),
          run: () => go(taskHref(tree, t)),
        });
      }
    }

    // 3. projects and workspaces
    const places: { score: number; item: Item }[] = [];
    for (const w of tree) {
      const ws = fuzzy(q, `${w.name} ${w.slug}`);
      if (ws !== null) {
        const href = w.projects[0] ? `/w/${w.slug}/p/${w.projects[0].id}` : `/w/${w.slug}/table`;
        places.push({
          score: ws,
          item: { id: `ws-${w.id}`, group: "Workspaces", icon: "▦", label: w.name, hint: <span className="muted small">{w.projects.length} project{w.projects.length === 1 ? "" : "s"}</span>, run: () => go(href) },
        });
      }
      for (const p of w.projects) {
        const s = fuzzy(q, `${p.key} ${p.name} ${w.name}`);
        if (s === null) continue;
        places.push({
          score: s,
          item: {
            id: `p-${p.id}`,
            group: "Projects",
            icon: (
              <Pill color={colorFor(p.key, p.color)} title={p.name}>
                {p.key}
              </Pill>
            ),
            label: p.name,
            hint: tree.length > 1 ? <span className="pill pill-ws">{w.name}</span> : undefined,
            run: () => go(`/w/${w.slug}/p/${p.id}`),
          },
        });
      }
    }
    places.sort((a, b) => b.score - a.score);
    const placeItems = places.slice(0, q ? 8 : 6).map((x) => x.item);

    // 4. commands
    const cmds: Item[] = [];
    if (boardHref) cmds.push({ id: "c-new", group: "Commands", icon: "+", label: "New task", hint: <Kbd>board</Kbd>, run: () => go(`${boardHref}?new=1`) });
    cmds.push({ id: "c-my", group: "Commands", icon: "☑", label: "My tasks", run: () => go("/my") });
    cmds.push({ id: "c-inbox", group: "Commands", icon: "✉", label: "Inbox", run: () => go("/inbox") });
    cmds.push({ id: "c-notif", group: "Commands", icon: "🔔", label: "Notifications", run: () => go("/notifications") });
    if (tableWs)
      cmds.push({
        id: "c-table",
        group: "Commands",
        icon: "☰",
        label: here && pid ? "Table view (this project)" : `Table view (${tableWs.name})`,
        run: () => go(here && pid ? `/w/${here.slug}/p/${pid}/table` : `/w/${tableWs.slug}/table`),
      });
    cmds.push({ id: "c-themes", group: "Commands", icon: "◐", label: "Themes", run: () => go("/themes") });
    cmds.push({ id: "c-profile", group: "Commands", icon: "☺", label: "Profile", run: () => go("/profile") });
    if (me.admin) cmds.push({ id: "c-admin", group: "Commands", icon: "⚙", label: "Admin", run: () => go("/admin") });
    if (me.admin) cmds.push({ id: "c-groups", group: "Commands", icon: "⛭", label: "Groups", hint: <span className="muted small">admin</span>, run: () => go("/admin/groups") });
    const cmdItems = cmds
      .map((c) => ({ c, s: fuzzy(q, String(c.label)) }))
      .filter((x) => x.s !== null)
      .sort((a, b) => (q ? b.s! - a.s! : 0))
      .map((x) => x.c);

    // saved views
    const viewItems = savedViews
      .map((v) => ({ v, s: fuzzy(q, `${v.name} saved view`) }))
      .filter((x) => x.s !== null)
      .sort((a, b) => (q ? b.s! - a.s! : 0))
      .slice(0, q ? 4 : 6)
      .map(({ v }): Item => {
        const w = tree.find((x) => x.id === v.workspace_id);
        const chips = summary(v.query).slice(0, 3);
        return {
          id: `view-${v.id}`,
          group: "Saved views",
          icon: "★",
          label: v.name,
          hint: (
            <>
              {w && tree.length > 1 && <span className="pill pill-ws">{w.name}</span>}
              {chips.map((c) => (
                <span key={c} className="pill pill-muted">
                  {c}
                </span>
              ))}
            </>
          ),
          run: () => go(viewHref(tree, v, me.user.id)),
        };
      });

    if (q) out.push(...cmdItems.slice(0, 4), ...viewItems, ...placeItems);
    else out.push(...cmdItems, ...viewItems, ...placeItems);

    // 5. full-text search
    if (q.length >= 2 && !(results && results.q === q)) {
      out.push({
        id: "search",
        group: "Search",
        icon: "⌕",
        label: (
          <>
            Search tasks for <strong>“{q}”</strong>
          </>
        ),
        hint: tree.length > 1 ? <span className="muted small">all workspaces</span> : undefined,
        keep: true,
        run: async () => {
          setBusy("search");
          setError("");
          try {
            const r = await tasksAcross(tree, { q, limit: 15, sort: "-updated" });
            r.items.sort((a, b) => b.updated_at - a.updated_at);
            setResults({ q, tasks: r.items.slice(0, 30), failed: r.failed });
            setActive(0);
            if (r.items.length === 0) setError(`No tasks match “${q}”.`);
          } catch (e) {
            setError(errMsg(e));
          } finally {
            setBusy("");
          }
        },
      });
    }
    return out;
  }, [text, loc, tree, me.admin, me.user.id, savedViews, results, go]);

  // keep the highlight in range and visible
  useEffect(() => {
    if (active >= items.length) setActive(Math.max(0, items.length - 1));
  }, [items.length, active]);
  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-idx="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [active]);

  const runItem = (it: Item | undefined) => {
    if (!it || busy) return;
    void it.run();
    if (!it.keep) onClose();
  };

  const onKeyDown = (e: ReactKeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((a) => (items.length ? (a + 1) % items.length : 0));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((a) => (items.length ? (a - 1 + items.length) % items.length : 0));
    } else if (e.key === "Home" && e.ctrlKey) {
      e.preventDefault();
      setActive(0);
    } else if (e.key === "End" && e.ctrlKey) {
      e.preventDefault();
      setActive(Math.max(0, items.length - 1));
    } else if (e.key === "Enter") {
      e.preventDefault();
      runItem(items[active]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      onClose();
    } else if (e.key === "Tab") {
      e.preventDefault(); // keep focus inside the dialog
    }
  };

  let lastGroup = "";
  const optId = (i: number) => `cp-opt-${i}`;
  return (
    <div className="modal-backdrop cp-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="cp" role="dialog" aria-modal="true" aria-label="Command palette">
        <div className="cp-input-row">
          <span className="cp-glyph" aria-hidden="true">
            ⌘
          </span>
          <input
            ref={inputRef}
            className="cp-input"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setActive(0);
              setError("");
            }}
            onKeyDown={onKeyDown}
            placeholder="Type a command, a project, a task key like GIG-12, or text to search…"
            role="combobox"
            aria-expanded="true"
            aria-controls="cp-list"
            aria-autocomplete="list"
            aria-activedescendant={items.length ? optId(active) : undefined}
            aria-label="Command"
            spellCheck={false}
            autoComplete="off"
          />
          {busy && <span className="cp-spin" aria-label="Working" />}
          <Kbd>Esc</Kbd>
        </div>
        {error && (
          <div className="cp-error" role="alert">
            {error}
          </div>
        )}
        {results && results.failed.length > 0 && results.q === text.trim() && <div className="cp-error">Couldn't search: {results.failed.join(", ")}</div>}
        <ul className="cp-list" id="cp-list" role="listbox" aria-label="Results" ref={listRef}>
          {items.map((it, i) => {
            const head = it.group !== lastGroup ? it.group : "";
            lastGroup = it.group;
            return (
              <li key={it.id} role="presentation">
                {head && (
                  <div className="cp-group" role="presentation">
                    {head}
                  </div>
                )}
                <div
                  id={optId(i)}
                  data-idx={i}
                  role="option"
                  aria-selected={i === active}
                  className={"cp-opt" + (i === active ? " on" : "")}
                  onMouseMove={() => i !== active && setActive(i)}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => runItem(it)}
                >
                  <span className="cp-icon">{it.icon}</span>
                  <span className="cp-label">{it.label}</span>
                  {it.hint && <span className="cp-hint">{it.hint}</span>}
                  {i === active && <span className="cp-enter" aria-hidden="true">↵</span>}
                </div>
              </li>
            );
          })}
          {items.length === 0 && <li className="cp-none">No matches. Type at least two characters to search task text.</li>}
        </ul>
        <div className="cp-foot muted small">
          <span>
            <Kbd>↑</Kbd> <Kbd>↓</Kbd> move
          </span>
          <span>
            <Kbd>↵</Kbd> run
          </span>
          <span>
            <Kbd>Esc</Kbd> close
          </span>
          <span className="cp-foot-right">
            <Kbd>Ctrl</Kbd>+<Kbd>K</Kbd> anywhere
          </span>
        </div>
      </div>
    </div>
  );
}

function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="cp-kbd">{children}</kbd>;
}
