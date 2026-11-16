// The bell: unread notifications (mentions, assignments, questions, missed windows, comments).
// A top-bar button with a count; on wide screens it opens a popover, on narrow ones the
// /notifications page. Polls the count every 60 s and refreshes (debounced) on live activity.
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useLocation } from "wouter";
import { api } from "./api";
import { useApp } from "./App";
import { countsChange, onActivity } from "./live";
import { badgeCount, kindInfo, notificationsApi, notificationText, placeBelow, type Notification, type Placement } from "./notifications";
import { Avatar } from "./ui";
import { Icon } from "./icons";
import { errMsg, fromHere, relTime, taskHref } from "./views/viewsApi";
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

// TopBell: the top bar's bell with the unread count. Wide screens: a popover under it; phones: the
// notifications page.
export function TopBell() {
  const unread = useUnread();
  const [loc, navigate] = useLocation();
  const [open, setOpen] = useState(false);
  const btn = useRef<HTMLButtonElement>(null);
  const label = unread ? `Notifications, ${unread} unread` : "Notifications";
  const close = useCallback(() => setOpen(false), []);
  return (
    <>
      <button
        ref={btn}
        type="button"
        className={"icon-button topbar-icon" + (open || loc === "/notifications" ? " on" : "")}
        onClick={() => (narrow() ? navigate("/notifications") : setOpen((o) => !o))}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={label}
        title={label}
      >
        <Icon name="bell" />
        {unread > 0 && <span className="count count-alert">{badgeCount(unread)}</span>}
      </button>
      {open && <BellPopover anchor={btn.current} onClose={close} />}
    </>
  );
}

// BellPopover: rendered into <body> (a portal) so no page stacking context can clip it or paint over it;
// it hangs under the bell, its right edge in line with the bell's, inside the viewport.
function BellPopover({ anchor, onClose }: { anchor: HTMLElement | null; onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<Placement | null>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!anchor || !el) return;
    const place = () => {
      const r = anchor.getBoundingClientRect();
      setPos(placeBelow({ left: r.left, right: r.right, bottom: r.bottom }, { width: el.offsetWidth, height: el.offsetHeight }, { width: window.innerWidth, height: window.innerHeight }));
    };
    place();
    const ro = new ResizeObserver(place);
    ro.observe(el);
    window.addEventListener("resize", place);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", place);
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
    // capture phase, and stopped: Esc closes only the popover, not the task panel (or dialog) under it
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      e.preventDefault();
      e.stopPropagation();
      onClose();
      anchor?.focus();
    };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key, true);
    return () => {
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key, true);
    };
  }, [anchor, onClose]);
  const style = { top: pos?.top ?? 8, left: pos?.left ?? 8, visibility: pos ? undefined : "hidden", "--notch-x": `${pos?.arrowX ?? 0}px` } as CSSProperties;
  return createPortal(
    <div ref={ref} tabIndex={-1} className={"bell-pop" + (pos?.arrowX != null ? " has-notch" : "")} role="dialog" aria-label="Notifications" style={style}>
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
      navigate(taskHref(tree, t), fromHere());
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
