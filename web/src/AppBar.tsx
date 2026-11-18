// The app bar: one row across the top that stays put while the page scrolls. The logo, the two pages
// (Gigs, Errands); on the right New gig and New errand, the live pip, the inbox, the bell, and your
// picture, which opens the account menu (the board's tools, Dashboard, Admin, Profile, Themes, Log out).
// On a phone the pages are icons and the two New buttons fold into one "+" menu.
import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";
import { Link, useLocation, useSearch } from "wouter";
import { api, ApiError, canWriteIn, errandMakers, gigChoices, hasCap } from "./api";
import { useApp } from "./App";
import { TopBell } from "./Bell";
import { badgeCount } from "./notifications";
import { ADMIN_TABS } from "./admin/common";
import { viewHref } from "./views/savedViews";
import { CATEGORY_COLOR, PRIORITY_COLOR } from "./theme";
import { LogoMark } from "./Logo";
import { useNarrow } from "./narrow";
import { Avatar, MoreMenu } from "./ui";
import { Icon } from "./icons";
import type { LiveState } from "./live";

export function AppBar({ inboxCount, onLogout }: { inboxCount: number; onLogout: () => void }) {
  const { me, tree, live } = useApp();
  const narrow = useNarrow();
  const [loc] = useLocation();
  const search = useSearch();
  const canGig = gigChoices(me, tree).length > 0;
  const canErrand = errandMakers(me, tree).length > 0;
  // on the board, New gig keeps its filters (the new gig starts in the project in view)
  const onBoard = loc === "/board" || loc.startsWith("/board/");
  const newGig = onBoard ? withParam(loc, search, "new", "1") : "/board?new=1";
  const newErrand = "/errands?new=1";
  return (
    <header className="appbar">
      <Link href="/board" className="brand appbar-brand" aria-label="gigg: the gigs" title="gigg">
        <LogoMark className="brand-mark" compact />
        {!narrow && <span className="brand-word">gigg</span>}
      </Link>
      <nav className="appbar-nav" aria-label="Pages">
        <PageTab href="/board" also={["/table", "/w/"]} icon="board" label="Gigs" iconOnly={narrow} />
        <PageTab href="/errands" icon="clock" label="Errands" iconOnly={narrow} />
      </nav>
      <span className="grow" />
      {narrow
        ? (canGig || canErrand) && (
            <MoreMenu label="New" buttonClass="icon-button topbar-icon appbar-new" face={<Icon name="plus" strokeWidth={2.2} />}>
              {canGig && (
                <Link href={newGig} role="menuitem">
                  <Icon name="board" /> New gig
                </Link>
              )}
              {canErrand && (
                <Link href={newErrand} role="menuitem">
                  <Icon name="clock" /> New errand
                </Link>
              )}
            </MoreMenu>
          )
        : (
            <>
              {canGig && (
                <Link href={newGig} className="button appbar-button">
                  <Icon name="plus" strokeWidth={2.2} /> New gig
                </Link>
              )}
              {canErrand && (
                <Link href={newErrand} className="button secondary appbar-button">
                  <Icon name="plus" strokeWidth={2.2} /> New errand
                </Link>
              )}
            </>
          )}
      <span className={"live-dot live-" + live} style={live === "off" ? undefined : ({ "--live": live === "live" ? CATEGORY_COLOR.done : PRIORITY_COLOR[2] } as CSSProperties)} role="status" title={LIVE_TITLE[live]} aria-label={LIVE_TITLE[live]}>
        <span className="live-pip" aria-hidden="true" />
      </span>
      <InboxLink count={inboxCount} />
      <TopBell />
      <AccountMenu onLogout={onLogout} />
    </header>
  );
}

const LIVE_TITLE: Record<LiveState, string> = {
  live: "Live updates: connected",
  connecting: "Live updates: connecting…",
  off: "Live updates: offline (retrying)",
};

// withParam: path?search with one more parameter.
function withParam(path: string, search: string, key: string, value: string): string {
  const s = new URLSearchParams(search);
  s.set(key, value);
  return path + "?" + s.toString();
}

// PageTab: one of the two pages. Gigs is on for the board and the table views too.
function PageTab({ href, also = [], icon, label, iconOnly }: { href: string; also?: string[]; icon: string; label: string; iconOnly: boolean }) {
  const [loc] = useLocation();
  const active = loc === href || loc.startsWith(href + "/") || also.some((a) => loc === a || loc.startsWith(a));
  return (
    <Link href={href} className={"appbar-tab" + (active ? " on" : "")} aria-current={active ? "page" : undefined} aria-label={iconOnly ? label : undefined} title={iconOnly ? label : undefined}>
      <Icon name={icon} />
      {!iconOnly && <span>{label}</span>}
    </Link>
  );
}

// InboxLink: the envelope, to the inbox (gigs waiting for a human) with their count.
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

// AccountMenu: your picture; the menu opens while the mouse rests on it (after a short pause, so passing
// over doesn't flash it) and folds a little after it leaves. A click, a tap or Enter opens it too; Esc,
// a click outside or choosing an entry closes it.
function AccountMenu({ onLogout }: { onLogout: () => void }) {
  const { me, tree, savedViews } = useApp();
  const [loc] = useLocation();
  const search = useSearch();
  const [open, setOpen] = useState(false);
  const [adminOpen, setAdminOpen] = useState(() => loc.startsWith("/admin"));
  const [logoutFailed, setLogoutFailed] = useState(false);
  const self = useRef<HTMLDivElement>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const later = (v: boolean, ms: number) => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setOpen(v), ms);
  };
  useEffect(() => setOpen(false), [loc, search]);
  useEffect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      if (!self.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", down);
    return () => document.removeEventListener("pointerdown", down);
  }, [open]);
  const onKey = (e: ReactKeyboardEvent) => {
    if (e.key === "Escape" && open) {
      e.preventDefault();
      e.stopPropagation(); // the gig panel closes on an Esc nobody else handled
      setOpen(false);
      btn.current?.focus();
    } else if ((e.key === "ArrowDown" || e.key === "ArrowUp") && open) {
      const items = [...(self.current?.querySelectorAll<HTMLElement>(".account-menu [role=menuitem]") ?? [])];
      if (!items.length) return;
      e.preventDefault();
      const i = items.indexOf(document.activeElement as HTMLElement);
      const next = e.key === "ArrowDown" ? (i + 1) % items.length : (i <= 0 ? items.length : i) - 1;
      items[next]?.focus();
    }
  };
  const logout = () => {
    setLogoutFailed(false);
    api.logout().then(onLogout, (e: unknown) => {
      if (e instanceof ApiError && e.status === 401) onLogout();
      else setLogoutFailed(true);
    });
  };

  // the board's tools open on the board (the projects in view, or every project from another page)
  const onBoard = loc === "/board" || loc.startsWith("/board/");
  const tool = (t: string) => (onBoard ? withParam(loc, search, "tool", t) : `/board?tool=${t}`);
  const anyTemplates = tree.some((w) => hasCap(me, "templates", w.id));
  const anyArchive = tree.some((w) => w.projects.some((p) => canWriteIn(w, p.id) && hasCap(me, "archive_gigs", w.id, p.id)));
  const name = me.user.display_name || me.user.username;

  return (
    <div
      className="account"
      ref={self}
      onKeyDown={onKey}
      onPointerEnter={(e) => e.pointerType === "mouse" && later(true, 120)}
      onPointerLeave={(e) => e.pointerType === "mouse" && !self.current?.contains(document.activeElement) && later(false, 350)}
    >
      <button
        ref={btn}
        type="button"
        className={"account-button" + (open ? " on" : "")}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`${name}: account menu`}
        onClick={() => {
          clearTimeout(timer.current);
          setOpen((o) => !o);
        }}
      >
        <Avatar user={me.user} size="s" />
      </button>
      {open && (
        <div className="menu account-menu" role="menu" aria-label="Account">
          <div className="account-who" aria-hidden="true">
            <Avatar user={me.user} size="s" />
            <span className="account-name">
              <strong>{name}</strong>
              <span className="muted small">@{me.user.username}</span>
            </span>
          </div>
          <MenuSection>
            <MenuLink href={tool("recurring")} icon="refresh" label="Recurring" tip="Gigs that come back on a schedule (a template + a schedule)" />
            <MenuLink href={tool("schedules")} icon="clock" label="Schedules" tip="Timings (every day 9:00, every Friday…) that recurring gigs share" />
            {anyTemplates && <MenuLink href={tool("templates")} icon="template" label="Templates" tip="Reusable gig contents: start a gig or a recurring gig from one" />}
            {anyArchive && <MenuLink href={tool("archive")} icon="archive" label="Archive done" tip="Archive the gigs in Done and Cancelled: they leave the board but stay searchable" />}
            <MenuLink href={tool("trash")} icon="trash" label="Trash" tip="Deleted gigs: restore them from here" />
          </MenuSection>
          {savedViews.length > 0 && (
            <MenuSection label="Saved views">
              {savedViews.map((v) => (
                <MenuLink key={v.id} href={viewHref(tree, v, me.user.id)} icon="star" label={v.name} />
              ))}
            </MenuSection>
          )}
          <MenuSection>
            <MenuLink href="/dashboard" icon="dashboard" label="Dashboard" />
            {me.admin && (
              <>
                <button type="button" role="menuitem" aria-expanded={adminOpen} className={"account-item" + (loc.startsWith("/admin") ? " on" : "")} onClick={(e) => (e.stopPropagation(), setAdminOpen((o) => !o))}>
                  <Icon name="settings" />
                  <span className="grow">Admin</span>
                  <Icon name={adminOpen ? "chevron-down" : "chevron-right"} />
                </button>
                {adminOpen && (
                  <div className="account-sub" role="group" aria-label="Admin sections">
                    {ADMIN_TABS.map((t) => (
                      <MenuLink key={t.href} href={"/admin" + t.href} icon={t.icon} label={t.label} />
                    ))}
                  </div>
                )}
              </>
            )}
          </MenuSection>
          <MenuSection>
            <MenuLink href="/profile" icon="users" label="Profile" />
            <MenuLink href="/themes" icon="theme" label="Themes" />
            <button type="button" role="menuitem" className={"account-item" + (logoutFailed ? " danger" : "")} onClick={logout}>
              <Icon name="logout" />
              <span role={logoutFailed ? "alert" : undefined}>{logoutFailed ? "Couldn't log out: try again" : "Log out"}</span>
            </button>
          </MenuSection>
        </div>
      )}
    </div>
  );
}

function MenuSection({ label, children }: { label?: string; children: ReactNode }) {
  return (
    <div className="account-section" role="group" aria-label={label}>
      {label && <div className="account-label" aria-hidden="true">{label}</div>}
      {children}
    </div>
  );
}

function MenuLink({ href, icon, label, tip }: { href: string; icon: string; label: string; tip?: string }) {
  const [loc] = useLocation();
  const path = href.split("?")[0];
  const on = !href.includes("?") && (loc === path || loc.startsWith(path + "/"));
  return (
    <Link href={href} role="menuitem" className={"account-item" + (on ? " on" : "")} aria-current={on ? "page" : undefined} data-tip={tip}>
      <Icon name={icon} />
      <span>{label}</span>
    </Link>
  );
}
