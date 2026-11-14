import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type CSSProperties, type FormEvent, type ReactNode } from "react";
import { setMentionUsers } from "./markdown";
import { Link, Redirect, Route, Switch, useLocation, useSearch } from "wouter";
import { api, ApiError, type Me, type Project, type ThemeState, type User } from "./api";
import { BellButton, NotificationsPage, TopBell } from "./Bell";
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
export function toTheme(id: string, o: Record<string, unknown>): Theme {
  return sane({ ...(BUILTINS[1] as Theme), ...(o as Partial<Theme>), secondary: (o.secondary as string | undefined) ?? null, id, description: "" });
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

  const setPref = useCallback((key: string, value: unknown) => {
    setPrefs((p) => {
      const next = { ...p, [key]: value };
      api.patchMe({ preferences: next }).catch(() => undefined);
      return next;
    });
  }, []);

  const ctx = useMemo<AppCtx | null>(
    () => (tree ? { me, tree, users, members, refresh, prefs, setPref, themes, savedViews, reloadSavedViews, live } : null),
    [me, tree, users, members, refresh, prefs, setPref, themes, savedViews, reloadSavedViews, live],
  );
  if (!ctx) return <div className="center muted">Loading…</div>;

  const collapsed = !narrow && prefs.sidebar === "collapsed"; // the drawer always shows labels
  const first = ctx.tree.find((w) => w.projects.length > 0);
  const logout = () => {
    api.logout().finally(onLogout);
  };

  const inAdmin = loc === "/admin" || loc.startsWith("/admin/");
  return (
    <Ctx.Provider value={ctx}>
      <div className={"shell" + (collapsed ? " collapsed" : "") + (narrow ? " narrow" : "") + (drawer ? " drawer-open" : "")}>
        {narrow && drawer && <div className="drawer-backdrop" onClick={() => setDrawer(false)} aria-hidden="true" />}
        <aside id="sidebar" className="sidebar" aria-label="Navigation" inert={narrow && !drawer ? true : undefined}>
          <div className="side-top">
            <Link href="/" className="brand" aria-label={collapsed ? "gigg home" : undefined} title={collapsed ? "gigg" : undefined}>
              <LogoMark className="brand-mark" compact />
              {!collapsed && <span className="brand-word">gigg</span>}
            </Link>
            {narrow ? (
              <button type="button" className="icon-button collapse" onClick={() => setDrawer(false)} aria-label="Close the menu">
                <Icon name="x" />
              </button>
            ) : (
              <button
                type="button"
                className="icon-button collapse"
                onClick={() => setPref("sidebar", collapsed ? "open" : "collapsed")}
                aria-label={collapsed ? "Open the menu" : "Close the menu"}
                aria-expanded={!collapsed}
              >
                <Icon name="sidebar" />
              </button>
            )}
          </div>
          <nav>
            <NavLink href="/board" icon={<Icon name="board" />} label="Board" collapsed={collapsed} />
            <NavLink href="/dashboard" icon={<Icon name="dashboard" />} label="Dashboard" collapsed={collapsed} />
            <NavLink href="/inbox" icon={<Icon name="inbox" />} label="Inbox" collapsed={collapsed}>
              {inboxCount > 0 && <span className="count count-alert">{inboxCount}</span>}
            </NavLink>
            <BellButton collapsed={collapsed} />
            <NavLink href="/my" icon={<Icon name="check" />} label="My tasks" collapsed={collapsed} />
            {savedViews.length > 0 && (
              <div className="nav-sub" role="group" aria-label="Saved views">
                {savedViews.map((v) => (
                  <SavedViewLink key={v.id} href={viewHref(ctx.tree, v, me.user.id)} label={v.name} collapsed={collapsed} title={savedViewTitle(v, ctx.tree)} />
                ))}
              </div>
            )}
          </nav>
          <div className="side-bottom">
            {me.admin && <NavLink href="/admin" icon={<Icon name="settings" />} label="Admin" collapsed={collapsed} />}
            {me.admin && inAdmin && (
              <div className="nav-sub" role="group" aria-label="Admin sections">
                {ADMIN_TABS.map((t) => (
                  <NavLink key={t.href} href={"/admin" + t.href} prefix icon={<Icon name={t.icon} />} label={t.label} collapsed={collapsed} />
                ))}
              </div>
            )}
            <NavLink href="/themes" icon={<Icon name="theme" />} label="Themes" collapsed={collapsed} />
            <NavLink href="/profile" icon={<Avatar user={me.user} size="xs" />} label={me.user.display_name || me.user.username} collapsed={collapsed} />
            <div className="side-foot">
              <span className={"live-dot live-" + live} style={live === "off" ? undefined : ({ "--live": live === "live" ? CATEGORY_COLOR.done : PRIORITY_COLOR[2] } as CSSProperties)} role="status" title={LIVE_TITLE[live]} aria-label={LIVE_TITLE[live]}>
                <span className="live-pip" aria-hidden="true" />
                {!collapsed && <span className="live-word">{live === "live" ? "Live" : live === "connecting" ? "Connecting" : "Offline"}</span>}
              </span>
              {!collapsed && (
                <button type="button" className="link logout" onClick={logout}>
                  <Icon name="logout" />
                  Log out
                </button>
              )}
            </div>
          </div>
        </aside>
        <main className="content">
          {narrow && (
            <header className="topbar">
              <button type="button" className="icon-button topbar-menu" onClick={() => setDrawer(true)} aria-label="Open the menu" aria-expanded={drawer} aria-controls="sidebar">
                <Icon name="menu" />
              </button>
              <Link href="/" className="brand topbar-brand" aria-label="gigg home">
                <LogoMark className="brand-mark" compact />
                <span className="brand-word">gigg</span>
              </Link>
              <span className="grow" />
              <TopBell />
              <Link href="/profile" className="topbar-me" aria-label="Profile" title="Profile">
                <Avatar user={me.user} size="xs" />
              </Link>
            </header>
          )}
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
            <Route path="/board/p/:pid/t/:key">{(params) => <BoardPage taskPid={Number(params.pid)} taskKey={params.key} />}</Route>
            <Route path="/board">
              <BoardPage />
            </Route>
            {/* the per-project boards of before: their links still work */}
            <Route path="/w/:ws/p/:pid/t/:key">{(params) => <Redirect replace to={`/board/p/${params.pid}/t/${params.key}`} />}</Route>
            <Route path="/w/:ws/p/:pid">{(params) => <Redirect replace to={`/board?p=${params.pid}`} />}</Route>
            <Route>{first ? <Redirect to="/board" /> : <Redirect to="/inbox" />}</Route>
          </Switch>
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
function SavedViewLink({ href, label, collapsed, title }: { href: string; label: string; collapsed: boolean; title: string }) {
  const [loc] = useLocation();
  const search = useSearch();
  const active = loc + (search ? "?" + search : "") === href;
  return (
    <Link href={href} className={"nav-item" + (active ? " selected" : "")} aria-current={active ? "page" : undefined} title={title} aria-label={collapsed ? title : undefined}>
      <span className="nav-icon" aria-hidden="true">
        <Icon name="star" />
      </span>
      {!collapsed && <span className="nav-label">{label}</span>}
    </Link>
  );
}

function NavLink({ href, children, prefix, icon, label, collapsed, title }: {
  href: string; children?: ReactNode; prefix?: boolean; icon: ReactNode; label: string; collapsed: boolean; title?: string;
}) {
  const [loc] = useLocation();
  const active = prefix ? loc === href || loc.startsWith(href + "/") : loc === href;
  return (
    <Link href={href} className={"nav-item" + (active ? " selected" : "")} aria-current={active ? "page" : undefined} title={title ?? label} aria-label={collapsed ? label : undefined}>
      <span className="nav-icon" aria-hidden="true">{icon}</span>
      {!collapsed && <span className="nav-label">{label}</span>}
      {children}
    </Link>
  );
}
