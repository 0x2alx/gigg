// Small shared pieces: colour-coded badges and a modal. Data colours (labels, statuses, priorities,
// users) come from theme.ts; their text colour is chosen for contrast so they read on any theme.
import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useNarrow } from "./narrow";
import { avatarURL, priorityNames, type Category, type CustomField, type Tag, type Gig, type User } from "./api";
import { cardChips } from "./items";
import { windowBadge } from "./recurrence";
import { CATEGORY_COLOR, PRIORITY_COLOR, SCHEDULE_COLOR, WINDOW_OPEN_COLOR, colorFor, readableOn } from "./theme";
import { Icon } from "./icons";

// ClearFilters: the one "Clear filters" button of every filtered list. Always there, so the bar
// doesn't jump; greyed out while nothing is filtered.
export function ClearFilters({ active, onClear, className }: { active: boolean; onClear: () => void; className?: string }) {
  return (
    <button type="button" className={"link small clear-filters" + (className ? " " + className : "")} disabled={!active} onClick={onClear} title={active ? "Show everything again" : "Nothing is filtered"}>
      <Icon name="x" /> Clear filters
    </button>
  );
}

type ItemCounts = Pick<Gig, "checklist_total" | "checklist_done" | "subgig_count" | "subgig_done" | "blocked_by" | "custom">;

// Pill: a tinted badge. The data colour goes in --c; theme.css turns it into a 14% tint with text of
// the same hue pulled toward the theme's text colour (so it reads on light and dark). `outline` is
// kept for old callers and looks the same.
export function Pill({ color, children, title }: { color: string; children: ReactNode; title?: string; outline?: boolean }) {
  return (
    <span className="pill" style={{ "--c": color } as CSSProperties} title={title}>
      {children}
    </span>
  );
}

export function StatusBadge({ name, category, color }: { name: string; category: Category; color?: string | null }) {
  return <Pill color={color ?? CATEGORY_COLOR[category] ?? "#8B8D98"}>{name}</Pill>;
}

// StatusDot: a status as a dot in its colour and the name in plain text (tables and lists, where a
// column of tinted badges would be loud).
export function StatusDot({ name, category, color }: { name: string; category: Category; color?: string | null }) {
  return (
    <span className="status-dot">
      <span className="dot" style={{ background: color ?? CATEGORY_COLOR[category] ?? "#8B8D98" }} aria-hidden="true" />
      {name}
    </span>
  );
}

export function PriorityBadge({ priority, showNone }: { priority: number; showNone?: boolean }) {
  if (priority <= 0) return showNone ? <span className="pill pill-muted">No priority</span> : null;
  const arrows = ["", "▾", "▸", "▴", "⏶"][priority];
  return (
    <Pill color={PRIORITY_COLOR[priority]!} title={`Priority: ${priorityNames[priority]}`}>
      {arrows} {priorityNames[priority]}
    </Pill>
  );
}

// PriorityIcon: the card's priority mark, three rising bars (Low 1, Medium 2, High 3) or a square with a
// "!" (Urgent). Low and Medium stay in the neutral text colour; High and Urgent take their level's colour.
export function PriorityIcon({ priority }: { priority: number }) {
  if (priority <= 0) return null;
  const name = priorityNames[priority];
  return (
    <span className={"prio-icon prio-icon-" + priority} style={{ "--c": PRIORITY_COLOR[priority] } as CSSProperties} title={`Priority: ${name}`} role="img" aria-label={`Priority: ${name}`}>
      <svg viewBox="0 0 16 16" width="1em" height="1em" aria-hidden="true">
        {priority >= 4 ? (
          <>
            <rect x="1.5" y="1.5" width="13" height="13" rx="3.5" fill="currentColor" />
            <rect x="7.1" y="4.2" width="1.8" height="5.2" rx="0.9" fill="var(--surface)" />
            <circle cx="8" cy="11.7" r="1.05" fill="var(--surface)" />
          </>
        ) : (
          [0, 1, 2].map((i) => <rect key={i} x={2 + i * 4.5} y={13 - (4 + i * 3.5)} width="3" height={4 + i * 3.5} rx="1" fill="currentColor" opacity={i < priority ? 1 : 0.28} />)
        )}
      </svg>
    </span>
  );
}

export function LabelChip({ label, onRemove }: { label: Tag; onRemove?: () => void }) {
  const c = colorFor(label.name, label.color);
  return (
    <span className="chip-label" style={{ "--c": c } as CSSProperties}>
      {label.name}
      {onRemove && (
        <button type="button" className="chip-x" onClick={onRemove} aria-label={`Remove label ${label.name}`}>
          ×
        </button>
      )}
    </span>
  );
}

// WorkspaceChip: a workspace as a pill tinted in its colour, led by its owner's picture (or a dot in its colour).
export function WorkspaceChip({ ws, owner }: { ws: { slug: string; name: string; color?: string | null }; owner?: User }) {
  const c = colorFor(ws.slug, ws.color);
  return (
    <span className="pill pill-wsc ws-chip" style={{ "--c": c } as CSSProperties} title={`Workspace: ${ws.name}`}>
      {owner ? <Avatar user={owner} size="xs" tip={`Workspace: ${ws.name}`} /> : <span className="dot" style={{ background: c }} aria-hidden="true" />}
      <span className="ws-chip-name">{ws.name}</span>
    </span>
  );
}

// Avatar sizes (at the 16px root): xs (about 31px) for cards and dense rows, s (default, about 34px) for lists, l (about 43px) for comments, m (about 67px) for profiles
// and the gig header, m (48px) for profile pages. People are round, agents rounded squares; the agent
// mark (a small robot head on the lower right) shows from s up, the shape says it at xs.
export function Avatar({ user, size, tip }: { user: Pick<User, "username" | "display_name" | "color" | "kind"> & { id?: number; has_avatar?: boolean; version?: number }; size?: "xs" | "s" | "l" | "m"; tip?: string }) {
  const c = colorFor(user.username, user.color);
  const sz = (size && size !== "s" ? " avatar-" + size : "") + (user.kind === "agent" ? " avatar-agent" : "");
  if (user.has_avatar && user.id) {
    return (
      <span
        className={"avatar avatar-img" + sz + (user.color ? " avatar-ring" : "")}
        style={user.color ? ({ "--ring": user.color } as CSSProperties) : undefined}
        title={tip ?? `${user.display_name || user.username}${user.kind === "agent" ? " (agent)" : ""}`}
      >
        <img src={avatarURL(user.id, user.version)} alt="" />
        {user.kind === "agent" && <AgentMark />}
      </span>
    );
  }
  const name = user.display_name || user.username;
  const initials = name
    .split(/[\s_-]+/)
    .map((p) => p[0] ?? "")
    .join("")
    .slice(0, 2)
    .toUpperCase();
  return (
    <span className={"avatar" + sz} style={{ background: c, color: readableOn(c) }} title={tip ?? `${name}${user.kind === "agent" ? " (agent)" : ""}`}>
      {initials}
      {user.kind === "agent" && <AgentMark />}
    </span>
  );
}

function AgentMark() {
  return (
    <span className="avatar-bot" aria-hidden="true">
      <Icon name="bot" strokeWidth={2.6} />
    </span>
  );
}

export function UserBadge({ user }: { user: Pick<User, "username" | "display_name" | "color" | "kind"> | undefined }) {
  if (!user) return null;
  return (
    <span className="user-badge">
      <Avatar user={user} />
      <span>{user.display_name || user.username}</span>
      {user.kind === "agent" && <span className="pill pill-agent">agent</span>}
    </span>
  );
}

// reducedMotion: the user asked the system for less motion (exit animations then close at once)
export const reducedMotion = (): boolean => typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

// trapTab: Tab and Shift+Tab cycle inside the dialog (it's aria-modal: the page behind is out of reach),
// also when the focus has slipped out of it.
const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
const shown = (el: HTMLElement) => (typeof el.checkVisibility === "function" ? el.checkVisibility({ visibilityProperty: true, contentVisibilityAuto: true }) : el.getClientRects().length > 0);
export function trapTab(e: KeyboardEvent, box: HTMLElement | null) {
  if (!box || e.defaultPrevented) return; // a field that uses Tab itself (mentions, list indent) wins
  const all = [...box.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => el === document.activeElement || shown(el));
  if (all.length === 0) return;
  const first = all[0]!;
  const last = all[all.length - 1]!;
  const at = document.activeElement;
  if (!box.contains(at)) {
    e.preventDefault();
    (e.shiftKey ? last : first).focus();
  } else if (e.shiftKey && at === first) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && at === last) {
    e.preventDefault();
    first.focus();
  }
}

// EXIT_MS: how long a dialog or the gig panel takes to animate out before it really closes
export const EXIT_MS = 150;

// expandable: a button in the header grows the dialog to the whole page (and back); the choice is
// remembered on this device for the next expandable dialog.
export function Modal({ title, onClose, children, wide, xl, help, expandable }: { title: string; onClose: () => void; children: ReactNode; wide?: boolean; xl?: boolean; help?: ReactNode; expandable?: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const narrow = useNarrow();
  const [full, setFull] = useState(() => !!expandable && readFull());
  const toggleFull = () => {
    writeFull(!full);
    setFull(!full);
  };
  // onClose through a ref: the focus setup runs once per dialog, not on every render of a caller that
  // passes a new function each time (that sent the focus back to the first button at each click)
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  // the dialog's own ways out (×, Esc, the backdrop) fade it out first; a caller that refuses to close
  // (busy) leaves it mounted, and it comes back
  const [closing, setClosing] = useState(false);
  const [reopen, setReopen] = useState(false);
  const alive = useRef(true);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const close = useCallback(() => {
    if (reducedMotion()) return closeRef.current();
    if (timer.current) return;
    setReopen(false);
    setClosing(true);
    timer.current = setTimeout(() => {
      closeRef.current();
      setTimeout(() => {
        timer.current = undefined;
        if (alive.current) {
          setReopen(true);
          setClosing(false);
          // inert dropped the focus: put it back in the dialog
          requestAnimationFrame(() => ref.current?.querySelector<HTMLElement>("input, textarea, select, button:not(.help-button)")?.focus());
        }
      }, 30);
    }, EXIT_MS);
  }, []);
  const animatedClose = useRef(close);
  animatedClose.current = close;
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      clearTimeout(timer.current);
    };
  }, []);
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    (ref.current?.querySelector<HTMLElement>("[autofocus], [data-autofocus]") ??
      ref.current?.querySelector<HTMLElement>("input, textarea, select, button:not(.help-button)"))?.focus();
    // only the top dialog (the last one opened) answers Esc and keeps the focus
    const top = () => [...document.querySelectorAll('[role="dialog"][aria-modal="true"]')].pop() === ref.current;
    const esc = (e: KeyboardEvent) => {
      if (e.key === "Escape" && top()) animatedClose.current();
      else if (e.key === "Tab" && top()) trapTab(e, ref.current);
    };
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("keydown", esc);
      prev?.focus();
    };
  }, []);
  // Rendered into <body>, not where it's used: a dialog opened from a table cell inherited the cell's
  // text-align, white-space and weight. It carries the shell's "narrow" itself, for the phone rules.
  return createPortal(
    <div className={"modal-backdrop" + (narrow ? " narrow" : "") + (closing ? " closing" : "") + (reopen ? " reopen" : "")} inert={closing || undefined} onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className={"modal" + (xl ? " modal-xl" : wide ? " modal-wide" : "") + (full ? " modal-full" : "")} role="dialog" aria-modal="true" aria-label={title} ref={ref}>
        <div className="modal-head">
          <h2>{title}</h2>
          {help}
          {expandable && (
            <button type="button" className="icon-button modal-expand" onClick={toggleFull} aria-label={full ? "Back to a dialog" : "Expand to the whole page"} title={full ? "Back to a dialog" : "Expand to the whole page"}>
              <Icon name={full ? "shrink" : "expand"} />
            </button>
          )}
          <button type="button" className="icon-button" onClick={close} aria-label="Close">
            <Icon name="x" />
          </button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  );
}

const FULL_KEY = "gigg:modal-full";
function readFull(): boolean {
  try {
    return localStorage.getItem(FULL_KEY) === "1";
  } catch {
    return false;
  }
}
function writeFull(on: boolean) {
  try {
    if (on) localStorage.setItem(FULL_KEY, "1");
    else localStorage.removeItem(FULL_KEY);
  } catch {
    // storage blocked: the choice lasts for this dialog only
  }
}

// GigBadges: the schedule, time-window and missed badges (cards and the gig panel). With items
// (cards): also blocked, checklist and subgig progress, and up to two select/checkbox values. On a card
// the progress and window marks are quiet text (icon and numbers, no box); only "blocked" and "missed"
// keep a solid badge, since they mean something is wrong.
export function GigBadges({ gig, now = Date.now(), items, fields }: {
  gig: Pick<Gig, "recurrence_id" | "missed_at" | "start_at" | "not_after"> & Partial<ItemCounts>;
  now?: number;
  items?: boolean;
  fields?: CustomField[];
}) {
  const w = windowBadge(gig, now);
  const ck = gig.checklist_total ?? 0;
  const st = gig.subgig_count ?? 0;
  const quiet = (done: boolean) => "meta-item" + (done ? " meta-done" : "");
  return (
    <>
      {items && !!gig.blocked_by && (
        <span className="pill pill-danger" title={`Blocked by ${gig.blocked_by} open gig${gig.blocked_by === 1 ? "" : "s"}: agents won't pick it up`}>
          <Icon name="lock" strokeWidth={2.2} /> {gig.blocked_by}
        </span>
      )}
      {items && ck > 0 && (
        <span className={quiet(gig.checklist_done === ck)} title="Checklist">
          <Icon name="checklist" /> {gig.checklist_done ?? 0}/{ck}
        </span>
      )}
      {items && st > 0 && (
        <span className={quiet(gig.subgig_done === st)} title="Subgigs done">
          <Icon name="subgigs" /> {gig.subgig_done ?? 0}/{st}
        </span>
      )}
      {items &&
        fields &&
        cardChips(gig, fields).map((c) => (
          <Pill key={c.field.id} color={colorFor(c.field.name + ":" + c.text)} title={c.field.name}>
            {c.text}
          </Pill>
        ))}
      {!!gig.recurrence_id && (
        <Pill color={SCHEDULE_COLOR} title="Made by a recurring gig">
          <Icon name="refresh" />
        </Pill>
      )}
      {!!gig.missed_at && (
        <span className="pill pill-danger" title="The window closed before anyone started it">
          missed
        </span>
      )}
      {w &&
        (w.state === "open" ? (
          <Pill color={WINDOW_OPEN_COLOR} title="Agents can claim it until the window closes">
            <Icon name="clock" /> {w.text}
          </Pill>
        ) : (
          <span className={items ? "meta-item" : "pill pill-muted"}>
            <Icon name="clock" /> {w.text}
          </span>
        ))}
    </>
  );
}

// MoreMenu: a "⋯" button with a drop-down of actions (page heads). Closes on a choice, a tap outside
// or Esc. `icon` makes it a square "⋯" button named by the label (for the wide page head); `className`
// goes on the wrapper, `buttonClass` replaces the button's classes, `face` replaces the button's
// contents (the label then names the button, as with `icon`).
export function MoreMenu({ label, children, icon, className, buttonClass, face }: { label: string; children: ReactNode; icon?: boolean; className?: string; buttonClass?: string; face?: ReactNode }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        setOpen(false);
      }
    };
    document.addEventListener("pointerdown", down);
    document.addEventListener("keydown", key, true);
    return () => {
      document.removeEventListener("pointerdown", down);
      document.removeEventListener("keydown", key, true);
    };
  }, [open]);
  return (
    <div className={"more-menu" + (className ? " " + className : "")} ref={ref}>
      <button
        type="button"
        className={buttonClass ?? "button secondary" + (icon ? " button-icon" : "")}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={icon || face ? label : undefined}
        data-tip={icon || face ? label : undefined}
        onClick={() => setOpen((o) => !o)}
      >
        {face ?? (icon ? "⋯" : buttonClass ? `${label} ▾` : `⋯ ${label}`)}
      </button>
      {open && (
        <div className="menu" role="menu" onClick={() => setOpen(false)}>
          {children}
        </div>
      )}
    </div>
  );
}

// ConfirmModal: a yes/no question on top of another dialog (Cancel keeps the dialog below as it was).
export function ConfirmModal({ title, children, confirmLabel, onConfirm, onCancel }: { title: string; children: ReactNode; confirmLabel: string; onConfirm: () => void; onCancel: () => void }) {
  return (
    <Modal title={title} onClose={onCancel}>
      <div className="confirm-body">{children}</div>
      <div className="row end">
        <button type="button" className="button secondary" onClick={onCancel}>
          Cancel
        </button>
        <button type="button" className="button" onClick={onConfirm} data-autofocus>
          {confirmLabel}
        </button>
      </div>
    </Modal>
  );
}
