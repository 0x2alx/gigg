// Small shared pieces: colour-coded badges and a modal. Data colours (labels, statuses, priorities,
// users) come from theme.ts; their text colour is chosen for contrast so they read on any theme.
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useNarrow } from "./narrow";
import { avatarURL, priorityNames, type Category, type CustomField, type Tag, type Gig, type User } from "./api";
import { blockedWords, cardChips, progressWords } from "./items";
import { windowBadge } from "./recurrence";
import { CATEGORY_COLOR, PRIORITY_COLOR, ALWAYS_RUN_COLOR, SCHEDULE_COLOR, WINDOW_OPEN_COLOR, colorFor, readableOn } from "./theme";
import { Icon } from "./icons";
import { FLAG_FILTERS, parseFlag, type FlagFilter, type FlagKey } from "./boardModel";

// ClearFilters: the one "Clear filters" button of every filtered list. It shows only while something
// is filtered; otherwise it keeps its place invisibly (and out of the accessibility tree), so the bar
// doesn't jump when a filter is set.
export function ClearFilters({ active, onClear, className }: { active: boolean; onClear: () => void; className?: string }) {
  return (
    <button
      type="button"
      className={"link small clear-filters" + (className ? " " + className : "")}
      disabled={!active}
      aria-hidden={active ? undefined : true}
      onClick={onClear}
      aria-label="Clear filters"
      title={active ? "Show everything again" : undefined}
    >
      <Icon name="x" /> <span className="clear-label">Clear filters</span>
    </button>
  );
}

// Segmented: one choice among a few, as a segmented control (.seg). With selectWhenNarrow, a phone
// gets a select instead: five pills don't fit beside their label at phone width, and a row cut off
// at the edge hides options.
export function Segmented<T extends string>({ options, value, onChange, label, selectWhenNarrow }: {
  options: [T, string][];
  value: T;
  onChange: (v: T) => void;
  label: string;
  selectWhenNarrow?: boolean;
}) {
  const narrow = useNarrow();
  if (narrow && selectWhenNarrow) {
    return (
      <select className="seg-select" value={value} aria-label={label} onChange={(e) => onChange(e.target.value as T)}>
        {options.map(([v, l]) => (
          <option key={v} value={v}>
            {l}
          </option>
        ))}
      </select>
    );
  }
  return (
    <div className="seg" role="radiogroup" aria-label={label}>
      {options.map(([v, l]) => (
        <button key={v} type="button" role="radio" aria-checked={value === v} className={value === v ? "on" : ""} onClick={() => onChange(v)}>
          {l}
        </button>
      ))}
    </div>
  );
}

// FlagSelect: the one picker of a yes/no filter (recurring, always run) on the board and in the table:
// every gig, hide the gigs with the flag, or only those.
export function FlagSelect({ flag, value, onChange, className }: { flag: FlagKey; value: FlagFilter; onChange: (v: FlagFilter) => void; className?: string }) {
  return (
    <select className={className} aria-label={FLAG_FILTERS[flag].aria} value={value} onChange={(e) => onChange(parseFlag(e.target.value))}>
      {FLAG_FILTERS[flag].choices.map((c) => (
        <option key={c.value} value={c.value}>
          {c.label}
        </option>
      ))}
    </select>
  );
}

// The More filters pickers the board and the table share, so both read the same.
// AssigneeSelect: anyone, a person (agents marked); `special` adds "Assigned to me" and "Unassigned"
// (the board's values "me" and "none"). Values are strings: "" any, or a user id.
export function AssigneeSelect({ people, value, onChange, special, className }: {
  people: { id: number; username: string; display_name: string; kind?: string }[];
  value: string;
  onChange: (v: string) => void;
  special?: boolean;
  className?: string;
}) {
  return (
    <select className={className} aria-label="Assignee" value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">Anyone</option>
      {special && <option value="me">Assigned to me</option>}
      {special && <option value="none">Unassigned</option>}
      {people.map((u) => (
        <option key={u.id} value={String(u.id)}>
          {u.display_name || u.username}
          {u.kind === "agent" ? " (agent)" : ""}
        </option>
      ))}
    </select>
  );
}

// LabelSelect: any label, or one by name.
export function LabelSelect({ names, value, onChange, className }: { names: string[]; value: string; onChange: (v: string) => void; className?: string }) {
  return (
    <select className={className} aria-label="Label" value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">Any label</option>
      {names.map((l) => (
        <option key={l} value={l}>
          {l}
        </option>
      ))}
    </select>
  );
}

// PrioritySelect: a minimum priority (0 = any): "High or higher", ..., "Urgent".
export function PrioritySelect({ value, onChange, className }: { value: number; onChange: (v: number) => void; className?: string }) {
  return (
    <select className={className} aria-label="Priority" value={value} onChange={(e) => onChange(Number(e.target.value))}>
      <option value={0}>Any priority</option>
      {priorityNames.slice(1).map((n, i) => (
        <option key={n} value={i + 1}>
          {i + 1 === 4 ? n : `${n} or higher`}
        </option>
      ))}
    </select>
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

// Kbd: a key as a small keycap (the command palette's hints, the shortcuts overlay).
export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="kbd">{children}</kbd>;
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

// Focus return. A dialog opened from a menu loses its opener with the menu (the item leaves the page
// when the menu closes), and one that renders a beat late finds the focus already on <body>. So the
// last element that had focus is tracked here, with its ancestors, and on close the focus goes back
// to the opener or, if that left the page, to the button of the menu it was in (the nearest ancestor
// still on the page that has an aria-haspopup / aria-expanded button as a direct child).
// The ancestors are taken when the element gets the focus: by the time a dialog opens, a menu that
// closed has already left the page, and a detached item no longer knows which menu it was in.
const ancestors = (el: HTMLElement | null): HTMLElement[] => {
  const out: HTMLElement[] = [];
  for (let p = el?.parentElement; p && p !== document.body; p = p.parentElement) out.push(p);
  return out;
};
let lastFocus: { el: HTMLElement; chain: HTMLElement[] } | null = null;
if (typeof document !== "undefined")
  document.addEventListener("focusin", (e) => {
    if (e.target instanceof HTMLElement && e.target !== document.body) lastFocus = { el: e.target, chain: ancestors(e.target) };
  });

export function rememberOpener(): (inside?: HTMLElement | null) => void {
  const active = document.activeElement;
  const live = active instanceof HTMLElement && active !== document.body ? active : null;
  const el = live ?? lastFocus?.el ?? null;
  const chain = live ? (lastFocus?.el === live ? lastFocus.chain : ancestors(live)) : (lastFocus?.chain ?? []);
  return (inside) => {
    // leave the focus alone if something else took it on purpose (a dialog that opened the gig panel)
    const now = document.activeElement;
    if (now && now !== document.body && !inside?.contains(now)) return;
    let target = el?.isConnected ? el : null;
    if (!target) target = chain.find((p) => p.isConnected)?.querySelector<HTMLElement>(":scope > [aria-haspopup], :scope > [aria-expanded]") ?? null;
    target?.focus();
  };
}

// EXIT_MS: how long a dialog or the gig panel takes to animate out before it really closes
export const EXIT_MS = 150;

// expandable: a button in the header grows the dialog to the whole page (and back); the choice is
// remembered on this device for the next expandable dialog.
// tools: small controls of the dialog's own (a counter, a button) between the title and the help.
export function Modal({ title, onClose, children, wide, xl, help, tools, expandable }: { title: string; onClose: () => void; children: ReactNode; wide?: boolean; xl?: boolean; help?: ReactNode; tools?: ReactNode; expandable?: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const narrow = useNarrow();
  // the saved choice is read even when not expandable yet: a dialog that becomes expandable (the
  // viewer stepping from an image to a text file) opens it as the user left it
  const [full, setFull] = useState(readFull);
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
    const back = rememberOpener();
    const box = ref.current;
    (ref.current?.querySelector<HTMLElement>("[autofocus], [data-autofocus]") ??
      ref.current?.querySelector<HTMLElement>("input, textarea, select, button:not(.help-button)"))?.focus();
    // only the top dialog (the last one opened) answers Esc and keeps the focus
    const top = () => [...document.querySelectorAll('[role="dialog"][aria-modal="true"]')].pop() === ref.current;
    const esc = (e: KeyboardEvent) => {
      // Esc leaves fullscreen (a part of this dialog shown on the whole screen) first; a second Esc closes
      if (e.key === "Escape" && top() && leaveFullscreen(ref.current)) e.preventDefault();
      else if (e.key === "Escape" && top()) animatedClose.current();
      else if (e.key === "Tab" && top()) trapTab(e, ref.current);
    };
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("keydown", esc);
      back(box);
    };
  }, []);
  // Rendered into <body>, not where it's used: a dialog opened from a table cell inherited the cell's
  // text-align, white-space and weight. It carries the shell's "narrow" itself, for the phone rules.
  return createPortal(
    <div className={"modal-backdrop" + (narrow ? " narrow" : "") + (closing ? " closing" : "") + (reopen ? " reopen" : "")} inert={closing || undefined} onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div className={"modal" + (xl ? " modal-xl" : wide ? " modal-wide" : "") + (expandable && full ? " modal-full" : "")} role="dialog" aria-modal="true" aria-label={title} ref={ref}>
        <div className="modal-head">
          <h2>{title}</h2>
          {tools}
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

// leaveFullscreen: true when this Esc belongs to fullscreen, not to the dialog: something inside the
// dialog is fullscreen (leave it), or the browser itself just left fullscreen on this Esc and still
// passed the key on (some do). Then the dialog stays open.
let fsLeftAt = -1e9;
if (typeof document !== "undefined") {
  document.addEventListener("fullscreenchange", () => {
    if (!document.fullscreenElement) fsLeftAt = performance.now();
  });
}
function leaveFullscreen(dialog: HTMLElement | null): boolean {
  const fs = document.fullscreenElement;
  if (fs && dialog?.contains(fs)) {
    void document.exitFullscreen().catch(() => undefined);
    return true;
  }
  return !fs && performance.now() - fsLeftAt < 250;
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

export const ALWAYS_RUN_TIP = "Always run: the runner takes it any time, outside the night window and the budget";

// GigBadges: the schedule, time-window and missed badges (cards and the gig panel). With items
// (cards): also blocked, checklist and subgig progress, and up to two select/checkbox values. On a card
// the progress and window marks are quiet text (icon and numbers, no box); only "blocked" and "missed"
// keep a solid badge, since they mean something is wrong.
export function GigBadges({ gig, now = Date.now(), items, fields }: {
  gig: Pick<Gig, "recurrence_id" | "always_run" | "missed_at" | "start_at" | "not_after"> & Partial<ItemCounts>;
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
      {/* the numbers are for the eye; screen readers get the same in words */}
      {items && !!gig.blocked_by && (
        <span className="pill pill-danger" title={`${blockedWords(gig.blocked_by)}: agents won't pick it up`}>
          <Icon name="lock" strokeWidth={2.2} /> <span aria-hidden="true">{gig.blocked_by}</span>
          <span className="sr-only">{blockedWords(gig.blocked_by)}</span>
        </span>
      )}
      {items && ck > 0 && (
        <span className={quiet(gig.checklist_done === ck)} title="Checklist">
          <Icon name="checklist" /> <span aria-hidden="true">{gig.checklist_done ?? 0}/{ck}</span>
          <span className="sr-only">{progressWords(gig.checklist_done ?? 0, ck, "checklist item")}</span>
        </span>
      )}
      {items && st > 0 && (
        <span className={quiet(gig.subgig_done === st)} title="Subgigs done">
          <Icon name="subgigs" /> <span aria-hidden="true">{gig.subgig_done ?? 0}/{st}</span>
          <span className="sr-only">{progressWords(gig.subgig_done ?? 0, st, "subgig")}</span>
        </span>
      )}
      {items &&
        fields &&
        cardChips(gig, fields).map((c) => (
          <Pill key={c.field.id} color={colorFor(c.field.name + ":" + c.text)} title={c.field.name}>
            {c.text}
          </Pill>
        ))}
      {/* a card says "Recurring" on its top line instead (Board's rec-badge) */}
      {!!gig.recurrence_id && !items && (
        <Pill color={SCHEDULE_COLOR} title="Made by a recurring gig">
          <Icon name="refresh" />
        </Pill>
      )}
      {!!gig.always_run && !items && (
        <Pill color={ALWAYS_RUN_COLOR} title={ALWAYS_RUN_TIP}>
          <Icon name="bolt" />
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

// usePopover: a small panel under a button: open state, closed by a press outside it or Escape (which
// puts the focus back on the button, the wrapper's first), and kept inside the window (CSS places it
// under its button; one that would still run off an edge, at the right of a phone, is shifted back in).
// `ref` goes on the wrapper, `panelRef` on the panel.
export function usePopover() {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        setOpen(false);
        ref.current?.querySelector<HTMLButtonElement>("button")?.focus();
      }
    };
    document.addEventListener("pointerdown", down);
    document.addEventListener("keydown", key, true);
    return () => {
      document.removeEventListener("pointerdown", down);
      document.removeEventListener("keydown", key, true);
    };
  }, [open]);
  useLayoutEffect(() => {
    const m = panelRef.current;
    if (!open || !m) return;
    m.style.transform = "";
    const r = m.getBoundingClientRect();
    const pad = 8;
    const dx = r.right > window.innerWidth - pad ? window.innerWidth - pad - r.right : r.left < pad ? pad - r.left : 0;
    if (dx) m.style.transform = `translateX(${Math.round(Math.max(dx, pad - r.left))}px)`;
  }, [open]);
  return { open, setOpen, ref, panelRef };
}

// MoreMenu: a "⋯" button with a drop-down of actions (page heads). Closes on a choice, a tap outside
// or Esc. `icon` makes it a square "⋯" button named by the label (for the wide page head); `className`
// goes on the wrapper, `buttonClass` replaces the button's classes, `face` replaces the button's
// contents (the label then names the button, as with `icon`).
export function MoreMenu({ label, children, icon, className, buttonClass, face }: { label: string; children: ReactNode; icon?: boolean; className?: string; buttonClass?: string; face?: ReactNode }) {
  const { open, setOpen, ref, panelRef: menuRef } = usePopover();
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
        <div className="menu" role="menu" ref={menuRef} onClick={() => setOpen(false)}>
          {children}
        </div>
      )}
    </div>
  );
}

// MoreFilters: the one "More filters" button of a filter bar (the board and the table), with a count
// of the filters in it that are on; it opens a panel of labelled pickers (FilterField) that stays open
// while you pick. A press outside or Escape closes it.
export function MoreFilters({ count, children }: { count: number; children: ReactNode }) {
  const { open, setOpen, ref, panelRef } = usePopover();
  return (
    <div
      className="more-filters"
      ref={ref}
      onBlur={(e) => {
        // tabbing out of the panel closes it
        if (open && e.relatedTarget && !e.currentTarget.contains(e.relatedTarget as Node)) setOpen(false);
      }}
    >
      <button
        type="button"
        className={"mf-button" + (count ? " on" : "")}
        aria-haspopup="dialog"
        aria-expanded={open}
        data-tip={count ? `${count} more filter${count === 1 ? "" : "s"} on` : undefined}
        onClick={() => setOpen((o) => !o)}
      >
        <Icon name="settings" />
        More filters
        {count > 0 && <span className="count">{count}</span>}
        <Icon name={open ? "chevron-up" : "chevron-down"} />
      </button>
      {open && (
        <div className="mf-panel" role="dialog" aria-label="More filters" ref={panelRef}>
          {children}
        </div>
      )}
    </div>
  );
}

// FilterField: one row of the More filters panel, a label and its picker side by side.
export function FilterField({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="mf-field">
      <span className="mf-label">{label}</span>
      {children}
    </label>
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
