// Small shared pieces: colour-coded badges and a modal. Data colours (labels, statuses, priorities,
// users) come from theme.ts; their text colour is chosen for contrast so they read on any theme.
import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { avatarURL, priorityNames, type Category, type CustomField, type Tag, type Task, type User } from "./api";
import { cardChips } from "./items";
import { windowBadge } from "./recurrence";
import { CATEGORY_COLOR, PRIORITY_COLOR, SCHEDULE_COLOR, WINDOW_OPEN_COLOR, colorFor, readableOn } from "./theme";
import { Icon } from "./icons";

type ItemCounts = Pick<Task, "checklist_total" | "checklist_done" | "subtask_count" | "subtask_done" | "blocked_by" | "custom">;

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

// Avatar sizes: xs (20px) for cards and dense rows, s (default, 24px) for lists, l (32px) for comments
// and the task header, m (48px) for profile pages. People are round, agents rounded squares; the agent
// mark (a small robot head on the lower right) shows from s up, the shape says it at xs.
export function Avatar({ user, size, tip }: { user: Pick<User, "username" | "display_name" | "color" | "kind"> & { id?: number; has_avatar?: boolean; version?: number }; size?: "xs" | "s" | "l" | "m"; tip?: string }) {
  const c = colorFor(user.username, user.color);
  const sz = (size && size !== "s" ? " avatar-" + size : "") + (user.kind === "agent" ? " avatar-agent" : "");
  if (user.has_avatar && user.id) {
    return (
      <span className={"avatar avatar-img" + sz} title={tip ?? `${user.display_name || user.username}${user.kind === "agent" ? " (agent)" : ""}`}>
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

export function Modal({ title, onClose, children, wide, xl, help }: { title: string; onClose: () => void; children: ReactNode; wide?: boolean; xl?: boolean; help?: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    (ref.current?.querySelector<HTMLElement>("[autofocus], [data-autofocus]") ??
      ref.current?.querySelector<HTMLElement>("input, textarea, select, button:not(.help-button)"))?.focus();
    const esc = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("keydown", esc);
      prev?.focus();
    };
  }, [onClose]);
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={"modal" + (xl ? " modal-xl" : wide ? " modal-wide" : "")} role="dialog" aria-modal="true" aria-label={title} ref={ref}>
        <div className="modal-head">
          <h2>{title}</h2>
          {help}
          <button type="button" className="icon-button" onClick={onClose} aria-label="Close">
            <Icon name="x" />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

// TaskBadges: the schedule, time-window and missed badges (cards and the task panel). With items
// (cards): also blocked, checklist and subtask progress, and up to two select/checkbox values. On a card
// the progress and window marks are quiet text (icon and numbers, no box); only "blocked" and "missed"
// keep a solid badge, since they mean something is wrong.
export function TaskBadges({ task, now = Date.now(), items, fields }: {
  task: Pick<Task, "recurrence_id" | "missed_at" | "start_at" | "not_after"> & Partial<ItemCounts>;
  now?: number;
  items?: boolean;
  fields?: CustomField[];
}) {
  const w = windowBadge(task, now);
  const ck = task.checklist_total ?? 0;
  const st = task.subtask_count ?? 0;
  const quiet = (done: boolean) => "meta-item" + (done ? " meta-done" : "");
  return (
    <>
      {items && !!task.blocked_by && (
        <span className="pill pill-danger" title={`Blocked by ${task.blocked_by} open task${task.blocked_by === 1 ? "" : "s"}: agents won't pick it up`}>
          <Icon name="lock" strokeWidth={2.2} /> {task.blocked_by}
        </span>
      )}
      {items && ck > 0 && (
        <span className={quiet(task.checklist_done === ck)} title="Checklist">
          <Icon name="checklist" /> {task.checklist_done ?? 0}/{ck}
        </span>
      )}
      {items && st > 0 && (
        <span className={quiet(task.subtask_done === st)} title="Subtasks done">
          <Icon name="subtasks" /> {task.subtask_done ?? 0}/{st}
        </span>
      )}
      {items &&
        fields &&
        cardChips(task, fields).map((c) => (
          <Pill key={c.field.id} color={colorFor(c.field.name + ":" + c.text)} title={c.field.name}>
            {c.text}
          </Pill>
        ))}
      {!!task.recurrence_id && (
        <Pill color={SCHEDULE_COLOR} title="Made by a recurring task">
          <Icon name="refresh" />
        </Pill>
      )}
      {!!task.missed_at && (
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
// goes on the wrapper, `buttonClass` replaces the button's classes.
export function MoreMenu({ label, children, icon, className, buttonClass }: { label: string; children: ReactNode; icon?: boolean; className?: string; buttonClass?: string }) {
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
        aria-label={icon ? label : undefined}
        data-tip={icon ? label : undefined}
        onClick={() => setOpen((o) => !o)}
      >
        {icon ? "⋯" : buttonClass ? `${label} ▾` : `⋯ ${label}`}
      </button>
      {open && (
        <div className="menu" role="menu" onClick={() => setOpen(false)}>
          {children}
        </div>
      )}
    </div>
  );
}
