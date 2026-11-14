// The bell: unread notifications (mentions, assignments, questions, missed windows, comments).
// A sidebar item with a count; on wide screens it opens a popover, on narrow ones the
// /notifications page. Polls the count every 60 s and refreshes (debounced) on live activity.
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { Link, useLocation } from "wouter";
import { api } from "./api";
import { useApp } from "./App";
import { countsChange, onActivity } from "./live";
import { badgeCount, kindInfo, notificationsApi, notificationText, placePopover, type Notification, type Placement } from "./notifications";
import { Avatar } from "./ui";
import { Icon } from "./icons";
import { errMsg, relTime, taskHref } from "./views/viewsApi";
import { HelpButton } from "./help";

const POLL_MS = 60_000;
export const BELL_REFRESH = "gigg:bell";

// useUnread: the unread count, polled and refreshed on live events or after the list changes it.
export function useUnread(): number {
  const [n, setN] = useState(0);
  const load = useCallback(() => {
    notificationsApi
      .list({ unread: true, limit: 1 })
      .then((r) => setN(r.unread))
      .catch(() => undefined);
  }, []);
  useEffect(() => {
    load();
    const t = setInterval(load, POLL_MS);
    let deb: ReturnType<typeof setTimeout> | undefined;
    const off = onActivity((d) => {
      if (!countsChange(d.item.action)) return;
      clearTimeout(deb);
      deb = setTimeout(load, 1500);
    });
    window.addEventListener(BELL_REFRESH, load);
    return () => {
      clearInterval(t);
      clearTimeout(deb);
      off();
      window.removeEventListener(BELL_REFRESH, load);
    };
  }, [load]);
  return n;
}

const narrow = () => matchMedia("(max-width: 720px)").matches;

// TopBell: the phone top bar's bell, a link to the notifications page with the unread count.
export function TopBell() {
  const unread = useUnread();
  const label = unread ? `Notifications, ${unread} unread` : "Notifications";
  return (
    <Link href="/notifications" className="icon-button topbar-bell" aria-label={label} title={label}>
      <Icon name="bell" />
      {unread > 0 && <span className="count count-alert">{badgeCount(unread)}</span>}
    </Link>
  );
}

export function BellButton({ collapsed }: { collapsed: boolean }) {
  const unread = useUnread();
  const [loc, navigate] = useLocation();
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  const active = loc === "/notifications";
  const label = unread ? `Notifications, ${unread} unread` : "Notifications";
  const click = () => {
    if (narrow()) navigate("/notifications");
    else setOpen((o) => !o);
  };
  return (
    <>
      <button
        ref={btn}
        type="button"
        className={"nav-item bell" + (active || open ? " selected" : "")}
        onClick={click}
        aria-haspopup="dialog"
        aria-expanded={open}
        title={label}
        aria-label={collapsed ? label : undefined}
      >
        <span className="nav-icon" aria-hidden="true">
          <Icon name="bell" />
        </span>
        {!collapsed && <span className="nav-label">Notifications</span>}
        {unread > 0 && <span className="count count-alert">{badgeCount(unread)}</span>}
      </button>
      {open && <BellPopover anchor={btn.current} onClose={() => setOpen(false)} />}
    </>
  );
}

// BellPopover: rendered into <body> (a portal) so no sidebar overflow or page stacking context can
// clip it or paint over it; it sits beside the sidebar edge with a notch pointing at the bell.
function BellPopover({ anchor, onClose }: { anchor: HTMLElement | null; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<Placement | null>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!anchor || !el) return;
    const place = () => {
      const r = anchor.getBoundingClientRect();
      const edge = anchor.closest(".sidebar")?.getBoundingClientRect().right ?? r.right;
      setPos(placePopover({ top: r.top, height: r.height }, edge, { width: el.offsetWidth, height: el.offsetHeight }, { width: window.innerWidth, height: window.innerHeight }));
    };
    place();
    const ro = new ResizeObserver(place);
    ro.observe(el);
    const side = anchor.closest(".sidebar");
    if (side) ro.observe(side); // a peeking sidebar widens and folds back: the popover follows its edge
    window.addEventListener("resize", place);
    side?.addEventListener("scroll", place);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", place);
      side?.removeEventListener("scroll", place);
    };
  }, [anchor]);
  // focus moves into the popover once it is placed (a hidden element can't take focus)
  const placed = pos !== null;
  useEffect(() => {
    if (placed) ref.current?.focus({ preventScroll: true });
  }, [placed]);
  useEffect(() => {
    const down = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!ref.current?.contains(t) && !anchor?.contains(t)) onClose();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      onClose();
      anchor?.focus();
    };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
    };
  }, [anchor, onClose]);
  const style = { top: pos?.top ?? 8, left: pos?.left ?? 8, visibility: pos ? undefined : "hidden", "--notch-y": `${pos?.arrowY ?? 0}px` } as CSSProperties;
  return createPortal(
    <div ref={ref} tabIndex={-1} className={"bell-pop" + (pos?.arrowY != null ? " has-notch" : "")} role="dialog" aria-label="Notifications" style={style}>
      <div className="bell-pop-body">
        <NotificationList compact onNavigate={onClose} />
      </div>
    </div>,
    document.body,
  );
}

// NotificationsPage: the full list (narrow screens, the command palette).
export function NotificationsPage() {
  return (
    <div className="notif-page">
      <NotificationList />
    </div>
  );
}

function NotificationList({ compact, onNavigate }: { compact?: boolean; onNavigate?: () => void }) {
  const { tree, users } = useApp();
  const [, navigate] = useLocation();
  const [items, setItems] = useState<Notification[] | null>(null);
  const [unread, setUnread] = useState(0);
  const [onlyUnread, setOnlyUnread] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    notificationsApi
      .list({ unread: onlyUnread, limit: 50 })
      .then((r) => {
        setItems(r.items);
        setUnread(r.unread);
        setError("");
      })
      .catch((e: unknown) => setError(errMsg(e)));
  }, [onlyUnread]);
  useEffect(load, [load]);
  useEffect(() => {
    let deb: ReturnType<typeof setTimeout> | undefined;
    const off = onActivity((d) => {
      if (!countsChange(d.item.action)) return;
      clearTimeout(deb);
      deb = setTimeout(load, 1500);
    });
    return () => {
      clearTimeout(deb);
      off();
    };
  }, [load]);

  const changed = () => window.dispatchEvent(new Event(BELL_REFRESH));
  const markAll = () =>
    notificationsApi
      .readAll()
      .then(() => {
        load();
        changed();
      })
      .catch((e: unknown) => setError(errMsg(e)));

  const open = async (n: Notification) => {
    if (!n.read_at) {
      setItems((cur) => cur?.map((x) => (x.id === n.id ? { ...x, read_at: Date.now() } : x)) ?? cur);
      setUnread((u) => Math.max(0, u - 1));
      notificationsApi.read([n.id]).then(changed, () => undefined);
    }
    const ref = n.task_key ?? n.task_id;
    if (ref === null) return;
    try {
      const t = await api.task(ref);
      onNavigate?.();
      navigate(taskHref(tree, t));
    } catch (e) {
      setError(`${n.task_key ?? "Task"}: ${errMsg(e)}`);
    }
  };

  return (
    <div className={"notif" + (compact ? " notif-compact" : "")}>
      <header className="notif-head">
        {compact ? <h2>Notifications</h2> : <h1>Notifications<HelpButton topic="inbox" label="Help: inbox and notifications" /></h1>}
        {unread > 0 && <span className="count count-alert">{badgeCount(unread)}</span>}
        <span className="grow" />
        <label className="vw-toggle">
          <input type="checkbox" checked={onlyUnread} onChange={(e) => setOnlyUnread(e.target.checked)} />
          Unread
        </label>
        <button type="button" className="button secondary small" onClick={markAll} disabled={unread === 0}>
          ✓ Mark all read
        </button>
      </header>
      {error && (
        <p className="danger small" role="alert">
          {error}
        </p>
      )}
      {items === null && !error && <p className="muted small">Loading…</p>}
      {items && items.length === 0 && <p className="muted small notif-empty">{onlyUnread ? "All caught up." : "Nothing yet."}</p>}
      <ul className="notif-list">
        {(items ?? []).map((n) => {
          const k = kindInfo(n.kind);
          const actor = n.actor_user_id ? users.get(n.actor_user_id) : undefined;
          const name = actor ? actor.display_name || actor.username : null;
          return (
            <li key={n.id}>
              <button type="button" className={"notif-item" + (n.read_at ? "" : " unread")} onClick={() => void open(n)}>
                <span className="notif-who">{actor ? <Avatar user={actor} /> : <span className="avatar notif-sys" aria-hidden="true">{k.icon}</span>}</span>
                <span className="notif-body">
                  <span className="notif-line">
                    <span className="pill" style={{ "--c": k.color } as CSSProperties}>
                      {k.icon} {k.label}
                    </span>
                    {n.task_key && <span className="pill pill-key">{n.task_key}</span>}
                    <time className="muted small notif-time" dateTime={new Date(n.created_at).toISOString()} title={new Date(n.created_at).toLocaleString()}>
                      {relTime(n.created_at)}
                    </time>
                  </span>
                  <span className="notif-title">{n.task_title ?? notificationText(n, name)}</span>
                  {n.task_title && <span className="muted small notif-text">{notificationText(n, name)}</span>}
                </span>
                {!n.read_at && <span className="notif-dot" aria-label="unread" />}
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
