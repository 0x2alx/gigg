import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { setMentionUsers } from "./markdown";
import { Redirect, Route, Switch, useLocation } from "wouter";
import { api, ApiError, SIGNED_OUT, type Me, type Project, type ProjectAccess, type ThemeState, type User } from "./api";
import { NotificationsPage } from "./Bell";
import { AppBar } from "./AppBar";
import { BoardPage } from "./Board";
import { ErrandsPage } from "./Errands";
import { DashboardPage } from "./Dashboard";
import { InboxPage } from "./Inbox";
import { countsChange, onActivity, treeChange, useLive, type LiveState } from "./live";
import { ProfilePage } from "./Profile";
import { ThemesPage } from "./Themes";
import { AdminRoutes } from "./admin/AdminRoutes";
import { CommandPalette } from "./views/CommandPalette";
import { TableView } from "./views/TableView";
import { savedViewsApi, type SavedFilter } from "./views/savedViews";
import { BUILTINS, applyTheme, findTheme, initialTheme, rememberTheme, sane, type Theme } from "./theme";
import { useNarrow } from "./narrow";
import { setPreferredTZ } from "./recurrence";
import { FONTS_ALLOWED_EVENT, setRemoteFonts } from "./fonts";
import { LiveBanner } from "./LiveBanner";
import { LaunchOverlay } from "./Launch";

export interface WorkspaceTree {
  id: number;
  slug: string;
  name: string;
  role: string;
  color?: string | null;
  owner_id?: number | null; // the user it belongs to (display only)
  projects: Project[];
  whole?: boolean; // you see the whole workspace (false: only its projects listed here)
  access?: ProjectAccess[]; // what you may do in each of those projects (from /auth/me)
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
  // only a 401 means "not logged in"; anything else (the server restarting, a database blip, no
  // network) keeps the page waiting and asks again, 2 s doubling up to 30 s
  const [unreachable, setUnreachable] = useState(false);
  const retry = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const load = useCallback(() => {
    let attempt = 0;
    const once = () => {
      api.me().then(
        (m) => {
          setUnreachable(false);
          setMe(m);
        },
        (e: unknown) => {
          if (e instanceof ApiError && e.status === 401) {
            setUnreachable(false);
            setMe(null);
            return;
          }
          setUnreachable(true);
          retry.current = setTimeout(once, Math.min(30_000, 2000 * 2 ** attempt++));
        },
      );
    };
    clearTimeout(retry.current);
    once();
  }, []);
  useEffect(() => {
    load();
    return () => clearTimeout(retry.current);
  }, [load]);
  // a call answered 401: check the session; gone, back to the login page with a word why
  const [ended, setEnded] = useState(false);
  const signedIn = useRef(false); // calls still failing after a logout aren't a session that ended
  signedIn.current = !!me;
  useEffect(() => {
    // one check at a time: a page whose session died fails many calls at once
    let checking: Promise<void> | null = null;
    const out = () => {
      if (!signedIn.current || checking) return;
      checking = api
        .me()
        .then(setMe, (e: unknown) => {
          if (e instanceof ApiError && e.status === 401) {
            setEnded(true);
            setMe(null);
          }
        })
        .finally(() => (checking = null));
    };
    window.addEventListener(SIGNED_OUT, out);
    return () => window.removeEventListener(SIGNED_OUT, out);
  }, []);

  if (me === undefined)
    return (
      <div className="center muted" role="status">
        {unreachable ? "Can't reach the server. Trying again…" : "Loading…"}
      </div>
    );
  if (me === null) return <Login onDone={() => (setEnded(false), load())} ended={ended} />;
  return <Shell key={me.user.id} me={me} onLogout={() => setMe(null)} />;
}

function Login({ onDone, ended }: { onDone: () => void; ended?: boolean }) {
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
        {ended && !error && (
          <p className="muted small" role="status">
            Your session ended. Log in again.
          </p>
        )}
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
  const narrow = useNarrow();
  const prefSaves = useRef(new Map<string, Promise<unknown>>());
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

  // the account's theme wins over the local first-paint cache; applied again once Google Fonts are
  // allowed (a theme with a font gigg doesn't bundle then gets it)
  const [fontsAllowed, setFontsAllowed] = useState(0);
  useEffect(() => {
    const on = () => setFontsAllowed((n) => n + 1);
    window.addEventListener(FONTS_ALLOWED_EVENT, on);
    return () => window.removeEventListener(FONTS_ALLOWED_EVENT, on);
  }, []);
  const remoteFonts = me.remote_fonts !== false;
  // set while rendering, not in an effect: the pages below read it as they render, before effects run
  setRemoteFonts(remoteFonts);
  useEffect(() => {
    applyTheme(themes.active);
    rememberTheme(themes.active);
  }, [themes.active, fontsAllowed, remoteFonts]);

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
    // one failed list keeps that workspace as it was (a blip must not empty the board and close what is
    // open on it); a workspace that is gone for this user (403/404) drops out
    void Promise.allSettled(me.workspaces.map((w) => api.projects(w.slug).then((r) => ({ ...w, access: w.projects, projects: r.items })))).then((res) =>
      setTree((prev) =>
        res.flatMap((r, i) => {
          if (r.status === "fulfilled") return [r.value];
          const e = r.reason;
          if (e instanceof ApiError && (e.status === 403 || e.status === 404)) return [];
          const old = prev?.find((w) => w.id === me.workspaces[i]!.id);
          return old ? [old] : [];
        }),
      ),
    );
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
  return (
    <Ctx.Provider value={ctx}>
      <div className={"shell" + (narrow ? " narrow" : "")}>
        {/* one bar across the top (pages, New, messages, your account menu); the page scrolls under it */}
        <AppBar inboxCount={inboxCount} onLogout={onLogout} />
        <LiveBanner live={live} onBack={refresh} />
        <LaunchOverlay />
        <main className="content">
          {/* a page change fades the new page in (keyed by the section, so a gig opening on the board or a
              tab inside Admin doesn't replay it) */}
          <div className="page-anim" key={loc.split("/")[1] ?? ""}>
          <Switch>
            <Route path="/dashboard">
              <DashboardPage />
            </Route>
            <Route path="/inbox">
              <InboxPage />
            </Route>
            {/* one route, so opening an errand's panel keeps the page (and its filters) mounted */}
            <Route path="/errands/:id?">{(params) => <ErrandsPage open={Number(params.id) || undefined} />}</Route>
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
            {/* "My gigs" of before: the board's "Assigned to me" */}
            <Route path="/my">
              <Redirect replace to="/board?a=me" />
            </Route>
            <Route path="/w/:ws/p/:pid/table">{(params) => <TableView ws={params.ws} pid={Number(params.pid)} />}</Route>
            <Route path="/w/:ws/table">{(params) => <TableView ws={params.ws} />}</Route>
            <Route path="/table">
              <TableView />
            </Route>
            <Route path="/board/p/:pid/g/:key">{(params) => <BoardPage gigPid={Number(params.pid)} gigKey={params.key} />}</Route>
            {/* links from before gigs and errands (tasks: /t/, crons: /crons) */}
            <Route path="/board/p/:pid/t/:key">{(params) => <Redirect replace to={`/board/p/${params.pid}/g/${params.key}`} />}</Route>
            <Route path="/crons/:id?">{(params) => <Redirect replace to={params.id ? `/errands/${params.id}` : "/errands"} />}</Route>
            <Route path="/board">
              <BoardPage />
            </Route>
            {/* the per-project boards of before: their links still work */}
            <Route path="/w/:ws/p/:pid/t/:key">{(params) => <Redirect replace to={`/board/p/${params.pid}/g/${params.key}`} />}</Route>
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
