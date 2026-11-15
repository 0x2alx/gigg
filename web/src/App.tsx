import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type CSSProperties, type FocusEvent as ReactFocusEvent, type FormEvent, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { setMentionUsers } from "./markdown";
import { Link, Redirect, Route, Switch, useLocation, useSearch } from "wouter";
import { api, ApiError, type Me, type Project, type ThemeState, type User } from "./api";
import { NotificationsPage, TopBell } from "./Bell";
import { badgeCount } from "./notifications";
import { BoardPage } from "./Board";
import { DashboardPage } from "./Dashboard";
import { InboxPage } from "./Inbox";
import { countsChange, onActivity, treeChange, useLive, type LiveState } from "./live";
import { ProfilePage } from "./Profile";
import { ThemesPage } from "./Themes";
import { AdminRoutes } from "./admin/AdminRoutes";
import { ADMIN_TABS } from "./admin/common";
import { CommandPalette } from "./views/CommandPalette";
import { MyTasks } from "./views/MyTasks";
import { TableView } from "./views/TableView";
import { savedViewsApi, viewHref, type SavedFilter } from "./views/savedViews";
import { BUILTINS, CATEGORY_COLOR, PRIORITY_COLOR, applyTheme, findTheme, initialTheme, rememberTheme, sane, type Theme } from "./theme";
import { LogoMark } from "./Logo";
import { useNarrow } from "./narrow";
import { Avatar } from "./ui";
import { Icon } from "./icons";
import { setPreferredTZ } from "./recurrence";

export interface WorkspaceTree {
  id: number;
  slug: string;
  name: string;
  role: string;
  color?: string | null;
  owner_id?: number | null; // the user it belongs to (display only)
  projects: Project[];
}

export interface AppCtx {
  me: Me;
  tree: WorkspaceTree[];
  users: Map<number, User>;
  members: Map<number, number[]>; // workspace id -> user ids
  refresh: () => void;
  prefs: Record<string, unknown>;
  setPref: (key: string, value: unknown) => void;
  themes: Themes;
  savedViews: SavedFilter[];
  reloadSavedViews: () => void;
  live: LiveState;
}

export interface Themes {
  custom: Theme[];
  selectedId: string;
  matchSystem: boolean;
  active: Theme;
  select: (id: string, matchSystem?: boolean) => Promise<void>;
  setMatchSystem: (on: boolean) => Promise<void>;
  reload: () => Promise<void>;
}

// toTheme: a stored custom theme (already sanitised by the server) as a Theme.
// The base fills fields the stored theme lacks; the gigg-only extras are not filled in (absent means
// "follow the corners", "auto" and "none"), so Nightwalker's pill shape doesn't leak into other themes.
export function toTheme(id: string, o: Record<string, unknown>): Theme {
  const { controlShape: _s, laneColor: _l, laneBorder: _b, logoColor: _g, ...base } = BUILTINS[1] as Theme;
  return sane({ ...base, ...(o as Partial<Theme>), secondary: (o.secondary as string | undefined) ?? null, id, description: "" });
}

const Ctx = createContext<AppCtx | null>(null);
export function useApp(): AppCtx {
  const c = useContext(Ctx);
  if (!c) throw new Error("no app context");
  return c;
}

export function App() {
  const [me, setMe] = useState<Me | null | undefined>(undefined);
  const load = useCallback(() => {
    api
      .me()
      .then(setMe)
      .catch(() => setMe(null));
  }, []);
  useEffect(load, [load]);

  if (me === undefined) return <div className="center muted">Loading…</div>;
  if (me === null) return <Login onDone={load} />;
  return <Shell me={me} onLogout={() => setMe(null)} />;
}

function Login({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    api
      .login(username, password)
      .then(onDone)
      .catch((err: unknown) => setError(err instanceof ApiError ? err.message : "Login failed."))
      .finally(() => setBusy(false));
  };
  return (
    <div className="center">
      <form className="card login" onSubmit={submit} aria-label="Log in">
        <div className="login-brand">
          <img className="login-mark" src="/logo-mark.svg" alt="" width="96" height="96" />
          <h1 className="brand-big">gigg</h1>
        </div>
        <label>
          Username
          <input autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required autoFocus />
        </label>
        <label>
          Password
          <input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </label>
        {error && (
          <p className="danger" role="alert">
            {error}
          </p>
        )}
        <button className="button" type="submit" disabled={busy}>
          Log in
        </button>
      </form>
    </div>
  );
}

function Shell({ me: initialMe, onLogout }: { me: Me; onLogout: () => void }) {
  const [loc] = useLocation();
  // phones: the sidebar is a drawer behind a top bar; it closes on navigation, a tap outside or Esc
  const narrow = useNarrow();
  const [drawer, setDrawer] = useState(false);
  const prefSaves = useRef(new Map<string, Promise<unknown>>());
  useEffect(() => setDrawer(false), [loc, narrow]);
  useEffect(() => {
    if (!drawer) return;
    const on = (e: KeyboardEvent) => {
      if (e.key === "Escape") setDrawer(false);
    };
    window.addEventListener("keydown", on);
    return () => window.removeEventListener("keydown", on);
  }, [drawer]);
  const [me, setMe] = useState<Me>(initialMe);
  const [tree, setTree] = useState<WorkspaceTree[] | null>(null);
  const [users, setUsers] = useState<Map<number, User>>(new Map());
  useEffect(() => setMentionUsers(users), [users]);
  const [members, setMembers] = useState<Map<number, number[]>>(new Map());
  const [inboxCount, setInboxCount] = useState(0);
  const [prefs, setPrefs] = useState<Record<string, unknown>>(() => (me.user.preferences as Record<string, unknown>) ?? {});
  setPreferredTZ(prefs.timezone);
  const [themeState, setThemeState] = useState<ThemeState | null>(null);
  const [systemDark, setSystemDark] = useState(() => matchMedia("(prefers-color-scheme: dark)").matches);
  useEffect(() => {
    const mq = matchMedia("(prefers-color-scheme: dark)");
    const on = () => setSystemDark(mq.matches);
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, []);
  const reloadThemes = useCallback(
    () =>
      api
        .myThemes()
        .then(setThemeState)
        .catch(() => setThemeState({ custom: [], selected_id: null, match_system: false })),
    [],
  );
  useEffect(() => {
    void reloadThemes();
  }, [reloadThemes]);

  const themes = useMemo<Themes>(() => {
    const custom = (themeState?.custom ?? []).map((c) => toTheme(c.id, c.theme));
    const fallback = initialTheme();
    const selectedId = themeState?.selected_id ?? (prefs.theme as string | undefined) ?? fallback.id;
    const selected = custom.find((c) => c.id === selectedId) ?? findTheme(selectedId) ?? fallback;
    const matchSystem = themeState?.match_system ?? false;
    // "Match the system": Daywalker while the system is light, the selected theme while it's dark
    const active = matchSystem && !systemDark ? BUILTINS[2]! : selected;
    const select = async (id: string, ms = matchSystem) => {
      await api.selectTheme(id, ms);
      await reloadThemes();
    };
    return { custom, selectedId: selected.id, matchSystem, active, select, setMatchSystem: (on: boolean) => select(selected.id, on), reload: reloadThemes };
  }, [themeState, prefs.theme, systemDark, reloadThemes]);

  // the account's theme wins over the local first-paint cache
  useEffect(() => {
    applyTheme(themes.active);
    rememberTheme(themes.active);
  }, [themes.active]);

  const reloadMe = useCallback(() => {
    api
      .me()
      .then((m) => setMe((cur) => (JSON.stringify(cur) === JSON.stringify(m) ? cur : m)))
      .catch(() => undefined);
  }, []);
  const [savedViews, setSavedViews] = useState<SavedFilter[]>([]);
  const reloadSavedViews = useCallback(() => {
    savedViewsApi
      .list()
      .then((r) => setSavedViews(r.items))
      .catch(() => setSavedViews([]));
  }, []);
  useEffect(reloadSavedViews, [reloadSavedViews]);
  const reloadInbox = useCallback(() => {
    api
      .inbox()
      .then((r) => setInboxCount(r.items.length))
      .catch(() => undefined);
  }, []);
  const refresh = useCallback(() => {
    reloadMe();
    Promise.all(me.workspaces.map((w) => api.projects(w.slug).then((r) => ({ ...w, projects: r.items }))))
      .then(setTree)
      .catch(() => setTree([]));
    api
      .users()
      .then((r) => setUsers(new Map(r.items.map((u) => [u.id, u]))))
      .catch(() => undefined);
    Promise.all(me.workspaces.map((w) => api.members(w.slug).then((r) => [w.id, r.items.map((m) => m.user_id)] as [number, number[]])))
      .then((pairs) => setMembers(new Map(pairs)))
      .catch(() => undefined);
    api
      .inbox()
      .then((r) => setInboxCount(r.items.length))
      .catch(() => setInboxCount(0));
  }, [me]);
  useEffect(refresh, [refresh]);

  // live updates: one stream per workspace; counts and the tree follow relevant activity (debounced)
  const live = useLive(me.workspaces);
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;
  useEffect(() => {
    let counts: ReturnType<typeof setTimeout> | undefined;
    let whole: ReturnType<typeof setTimeout> | undefined;
    const off = onActivity(({ item }) => {
      if (treeChange(item.action)) {
        clearTimeout(whole);
        whole = setTimeout(() => refreshRef.current(), 2000);
      } else if (countsChange(item.action)) {
        clearTimeout(counts);
        counts = setTimeout(reloadInbox, 2000);
      }
    });
    return () => {
      off();
      clearTimeout(counts);
      clearTimeout(whole);
    };
  }, [reloadInbox]);

  // setPref: one key at a time; the server merges it into the stored preferences, so another tab saving
  // its own key can't put back an old value of this one (a stale preference from another tab did exactly that)
  // (undefined or null removes the key: sent as null, since JSON drops undefined)
  const setPref = useCallback((key: string, value: unknown) => {
    const gone = value === undefined || value === null;
    setPrefs((p) => {
      const next = { ...p };
      if (gone) delete next[key];
      else next[key] = value;
      return next;
    });
    // one key's saves go out one after another, so the last one made is the one the server keeps
    const prev = prefSaves.current.get(key) ?? Promise.resolve();
    const next = prev.then(() => api.patchMe({ preferences: { [key]: gone ? null : value } })).catch(() => undefined);
    prefSaves.current.set(key, next);
  }, []);

  const ctx = useMemo<AppCtx | null>(
    () => (tree ? { me, tree, users, members, refresh, prefs, setPref, themes, savedViews, reloadSavedViews, live } : null),
    [me, tree, users, members, refresh, prefs, setPref, themes, savedViews, reloadSavedViews, live],
  );
  if (!ctx) return <div className="center muted">Loading…</div>;

  const first = ctx.tree.find((w) => w.projects.length > 0);
  const logout = () => {
    api.logout().finally(onLogout);
  };

  const inAdmin = loc === "/admin" || loc.startsWith("/admin/");
  return (
    <Ctx.Provider value={ctx}>
      <div className={"shell" + (narrow ? " narrow" : "") + (drawer ? " drawer-open" : "")}>
        {narrow && drawer && <div className="drawer-backdrop" onClick={() => setDrawer(false)} aria-hidden="true" />}
        {/* wide screens: a rail of icons; each of its two panels opens over the page on its own while the
            mouse (or the keyboard focus) is on it. Phones: a drawer with every label. */}
        <aside id="sidebar" className="sidebar" aria-label="Navigation" inert={narrow && !drawer ? true : undefined}>
          <div className="side-top">
            <Link href="/" className="brand" aria-label="gigg home" title="gigg">
              <LogoMark className="brand-mark" compact />
              {narrow && <span className="brand-word">gigg</span>}
            </Link>
            {narrow && (
              <button type="button" className="icon-button collapse" onClick={() => setDrawer(false)} aria-label="Close the menu">
                <Icon name="x" />
              </button>
            )}
          </div>
          <SidePanel as="nav" label="Pages" hover={!narrow}>
            <NavLink href="/board" prefix icon={<Icon name="board" />} label="Board" className="nav-primary" />
            <NavLink href="/dashboard" icon={<Icon name="dashboard" />} label="Dashboard" />
            <NavLink href="/my" icon={<Icon name="check" />} label="My tasks" />
            {savedViews.length > 0 && (
              <div className="nav-sub" role="group" aria-label="Saved views">
                {savedViews.map((v) => (
                  <SavedViewLink key={v.id} href={viewHref(ctx.tree, v, me.user.id)} label={v.name} title={savedViewTitle(v, ctx.tree)} />
                ))}
              </div>
            )}
          </SidePanel>
          <SidePanel as="div" label="Settings and account" hover={!narrow} className="side-bottom">
            {me.admin && <NavLink href="/admin" icon={<Icon name="settings" />} label="Admin" />}
            {me.admin && inAdmin && (
              <div className="nav-sub" role="group" aria-label="Admin sections">
                {ADMIN_TABS.map((t) => (
                  <NavLink key={t.href} href={"/admin" + t.href} prefix icon={<Icon name={t.icon} />} label={t.label} />
                ))}
              </div>
            )}
            <NavLink href="/themes" icon={<Icon name="theme" />} label="Themes" />
            <NavLink href="/profile" icon={<Avatar user={me.user} size="xs" />} label={me.user.display_name || me.user.username} />
            <button type="button" className="nav-item" onClick={logout}>
              <span className="nav-icon" aria-hidden="true">
                <Icon name="logout" />
              </span>
              <span className="nav-label">Log out</span>
            </button>
          </SidePanel>
        </aside>
        <main className="content">
          {/* the top bar: messages in the top-right corner (inbox, notifications, the live pip); on a phone
              also the menu button, the brand and you */}
          <header className="topbar">
            {narrow && (
              <>
                <button type="button" className="icon-button topbar-menu" onClick={() => setDrawer(true)} aria-label="Open the menu" aria-expanded={drawer} aria-controls="sidebar">
                  <Icon name="menu" />
                </button>
                <Link href="/" className="brand topbar-brand" aria-label="gigg home">
                  <LogoMark className="brand-mark" compact />
                  <span className="brand-word">gigg</span>
                </Link>
              </>
            )}
            <span className="grow" />
            <span className={"live-dot live-" + live} style={live === "off" ? undefined : ({ "--live": live === "live" ? CATEGORY_COLOR.done : PRIORITY_COLOR[2] } as CSSProperties)} role="status" title={LIVE_TITLE[live]} aria-label={LIVE_TITLE[live]}>
              <span className="live-pip" aria-hidden="true" />
            </span>
            <InboxLink count={inboxCount} />
            <TopBell />
            {narrow && (
              <Link href="/profile" className="topbar-me" aria-label="Profile" title="Profile">
                <Avatar user={me.user} size="xs" />
              </Link>
            )}
          </header>
          {/* a page change fades the new page in (keyed by the section, so a task opening on the board or a
              tab inside Admin doesn't replay it) */}
          <div className="page-anim" key={loc.split("/")[1] ?? ""}>
          <Switch>
            <Route path="/dashboard">
              <DashboardPage />
            </Route>
            <Route path="/inbox">
              <InboxPage />
            </Route>
            <Route path="/notifications">
              <NotificationsPage />
            </Route>
            <Route path="/profile">
              <ProfilePage onLogout={onLogout} />
            </Route>
            <Route path="/themes">
              <ThemesPage />
            </Route>
            <Route path="/admin" nest>
              <AdminRoutes />
            </Route>
            <Route path="/my">
              <MyTasks />
            </Route>
            <Route path="/w/:ws/p/:pid/table">{(params) => <TableView ws={params.ws} pid={Number(params.pid)} />}</Route>
            <Route path="/w/:ws/table">{(params) => <TableView ws={params.ws} />}</Route>
            <Route path="/table">
              <TableView />
            </Route>
            <Route path="/board/p/:pid/t/:key">{(params) => <BoardPage taskPid={Number(params.pid)} taskKey={params.key} />}</Route>
            <Route path="/board">
              <BoardPage />
            </Route>
            {/* the per-project boards of before: their links still work */}
            <Route path="/w/:ws/p/:pid/t/:key">{(params) => <Redirect replace to={`/board/p/${params.pid}/t/${params.key}`} />}</Route>
            <Route path="/w/:ws/p/:pid">{(params) => <Redirect replace to={`/board?p=${params.pid}`} />}</Route>
            <Route>{first ? <Redirect to="/board" /> : <Redirect to="/inbox" />}</Route>
          </Switch>
          </div>
        </main>
        <CommandPalette />
      </div>
    </Ctx.Provider>
  );
}

const LIVE_TITLE: Record<LiveState, string> = {
  live: "Live updates: connected",
  connecting: "Live updates: connecting…",
  off: "Live updates: offline (retrying)",
};

function savedViewTitle(v: SavedFilter, tree: WorkspaceTree[]): string {
  const ws = tree.find((w) => w.id === v.workspace_id);
  return `Saved view: ${v.name}${ws && tree.length > 1 ? ` (${ws.name})` : ""}`;
}

// SavedViewLink: like NavLink, but active only when the search matches too.
function SavedViewLink({ href, label, title }: { href: string; label: string; title: string }) {
  const [loc] = useLocation();
  const search = useSearch();
  const active = loc + (search ? "?" + search : "") === href;
  return (
    <Link href={href} className={"nav-item" + (active ? " selected" : "")} aria-current={active ? "page" : undefined} title={title}>
      <span className="nav-icon" aria-hidden="true">
        <Icon name="star" />
      </span>
      <span className="nav-label">{label}</span>
    </Link>
  );
}

function NavLink({ href, children, prefix, icon, label, title, className }: {
  href: string; children?: ReactNode; prefix?: boolean; icon: ReactNode; label: string; title?: string; className?: string;
}) {
  const [loc] = useLocation();
  const active = prefix ? loc === href || loc.startsWith(href + "/") : loc === href;
  return (
    <Link href={href} className={"nav-item" + (className ? " " + className : "") + (active ? " selected" : "")} aria-current={active ? "page" : undefined} title={title}>
      <span className="nav-icon" aria-hidden="true">{icon}</span>
      <span className="nav-label">{label}</span>
      {children}
    </Link>
  );
}

// InboxLink: the top bar's envelope, to the inbox (tasks waiting for a human) with their count.
function InboxLink({ count }: { count: number }) {
  const [loc] = useLocation();
  const label = count ? `Inbox, ${count} waiting` : "Inbox";
  return (
    <Link href="/inbox" className={"icon-button topbar-icon" + (loc === "/inbox" ? " on" : "")} aria-label={label} title={label} aria-current={loc === "/inbox" ? "page" : undefined}>
      <Icon name="mail" />
      {count > 0 && <span className="count count-alert">{badgeCount(count)}</span>}
    </Link>
  );
}

// SidePanel: one of the rail's two panels. With `hover` (wide screens) it folds to its icons and opens
// over the page while the mouse is on it (after a short pause, so passing over doesn't flash it) or
// while the keyboard works in it; it folds back a little after the mouse leaves. Each panel on its own.
// On a touch screen (a tablet: wide, but no hover) the first tap opens it instead of following the
// icon, a tap on an entry then goes there and folds it, and a tap outside folds it.
function SidePanel({ as: Tag, label, hover, className, children }: { as: "nav" | "div"; label: string; hover: boolean; className?: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  useEffect(() => {
    if (!hover) {
      clearTimeout(timer.current);
      setOpen(false);
    }
  }, [hover]);
  const later = (v: boolean, ms: number) => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setOpen(v), ms);
  };
  const self = useRef<HTMLElement>(null);
  const touch = useRef(false); // the last press on it was a finger or a pen
  useEffect(() => {
    if (!open || !hover) return;
    const out = (e: PointerEvent) => {
      if (e.pointerType !== "mouse" && !self.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", out);
    return () => document.removeEventListener("pointerdown", out);
  }, [open, hover]);
  const handlers = hover
    ? {
        // only a real mouse opens it (a tap on a touch screen just goes to the page)
        onPointerEnter: (e: ReactPointerEvent<HTMLElement>) => {
          if (e.pointerType === "mouse") later(true, OPEN_MS);
        },
        // the mouse leaving doesn't fold it while the keyboard is working in it
        onPointerLeave: (e: ReactPointerEvent<HTMLElement>) => {
          if (e.pointerType !== "mouse") return; // a finger lifting "leaves" too; taps fold it (see above)
          const f = document.activeElement;
          if (f && e.currentTarget.contains(f) && f.matches(":focus-visible")) return;
          later(false, CLOSE_MS);
        },
        // keyboard focus opens it (focus put back by a dialog or a popover isn't someone reaching for the menu)
        onFocus: (e: ReactFocusEvent<HTMLElement>) => {
          if (e.target instanceof HTMLElement && e.target.matches(":focus-visible") && !modalOpen()) later(true, 0);
        },
        onBlur: (e: ReactFocusEvent<HTMLElement>) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node | null) && !e.currentTarget.matches(":hover")) later(false, 150);
        },
        onPointerDown: (e: ReactPointerEvent<HTMLElement>) => {
          touch.current = e.pointerType !== "mouse";
        },
        onClickCapture: (e: ReactMouseEvent<HTMLElement>) => {
          if (!touch.current) return;
          if (!open) {
            e.preventDefault(); // the first tap opens the panel, it doesn't follow the icon
            e.stopPropagation();
            clearTimeout(timer.current);
            setOpen(true);
          } else if ((e.target as Element).closest("a, button")) later(false, 0);
        },
        onKeyDown: (e: ReactKeyboardEvent<HTMLElement>) => {
          if (e.key === "Escape" && open) {
            e.preventDefault(); // the task panel closes on an Esc nobody else handled
            e.stopPropagation();
            clearTimeout(timer.current);
            setOpen(false);
          }
        },
      }
    : {};
  return (
    <Tag ref={self as never} className={"side-panel" + (className ? " " + className : "") + (hover ? (open ? " open" : " folded") : "")} aria-label={label} {...(Tag === "div" ? { role: "group" } : {})} {...handlers}>
      {children}
    </Tag>
  );
}
const OPEN_MS = 120;
const CLOSE_MS = 450;

// modalOpen: a dialog is up (focus moving behind it, by Tab, isn't someone reaching for the menu)
function modalOpen(): boolean {
  return document.querySelector('[role="dialog"][aria-modal="true"]') !== null;
}
