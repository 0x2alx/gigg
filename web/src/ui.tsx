// Small shared pieces: colour-coded badges and a modal. Data colours (labels, statuses, priorities,
// users) come from theme.ts; their text colour is chosen for contrast so they read on any theme.
import { useEffect, useRef, useState, type ReactNode } from "react";
import { avatarURL, priorityNames, type Category, type CustomField, type Tag, type Task, type User } from "./api";
import { cardChips } from "./items";
import { windowBadge } from "./recurrence";
import { CATEGORY_COLOR, PRIORITY_COLOR, SCHEDULE_COLOR, WINDOW_OPEN_COLOR, colorFor, readableOn } from "./theme";

type ItemCounts = Pick<Task, "checklist_total" | "checklist_done" | "subtask_count" | "subtask_done" | "blocked_by" | "custom">;

export function Pill({ color, children, title, outline }: { color: string; children: ReactNode; title?: string; outline?: boolean }) {
  const style = outline ? { borderColor: color, color } : { background: color, color: readableOn(color), borderColor: color };
  return (
    <span className="pill" style={style} title={title}>
      {children}
    </span>
  );
}

export function StatusBadge({ name, category, color }: { name: string; category: Category; color?: string | null }) {
  return <Pill color={color ?? CATEGORY_COLOR[category] ?? "#8B8D98"}>{name}</Pill>;
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

export function LabelChip({ label, onRemove }: { label: Tag; onRemove?: () => void }) {
  const c = colorFor(label.name, label.color);
  return (
    <span className="chip-label" style={{ background: c, color: readableOn(c) }}>
      {label.name}
      {onRemove && (
        <button type="button" className="chip-x" onClick={onRemove} aria-label={`Remove label ${label.name}`} style={{ color: readableOn(c) }}>
          ×
        </button>
      )}
    </span>
  );
}

// Avatar sizes: xs for dense rows, s (default) for cards and lists, l for comments and the task
// header, m for profile pages.
export function Avatar({ user, size }: { user: Pick<User, "username" | "display_name" | "color" | "kind"> & { id?: number; has_avatar?: boolean; version?: number }; size?: "xs" | "s" | "l" | "m" }) {
  const c = colorFor(user.username, user.color);
  const sz = size && size !== "s" ? " avatar-" + size : "";
  if (user.has_avatar && user.id) {
    return (
      <span className={"avatar avatar-img" + sz} title={`${user.display_name || user.username}${user.kind === "agent" ? " (agent)" : ""}`}>
        <img src={avatarURL(user.id, user.version)} alt="" />
        {user.kind === "agent" && <span className="avatar-bot" aria-hidden="true">⚙</span>}
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
    <span className={"avatar" + sz} style={{ background: c, color: readableOn(c) }} title={`${name}${user.kind === "agent" ? " (agent)" : ""}`}>
      {initials}
      {user.kind === "agent" && <span className="avatar-bot" aria-hidden="true">⚙</span>}
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
            ✕
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

// TaskBadges: the schedule, time-window and missed badges (cards and the task panel). With items
// (cards): also blocked, checklist and subtask progress, and up to two select/checkbox values.
export function TaskBadges({ task, now = Date.now(), items, fields }: {
  task: Pick<Task, "recurrence_id" | "missed_at" | "start_at" | "not_after"> & Partial<ItemCounts>;
  now?: number;
  items?: boolean;
  fields?: CustomField[];
}) {
  const w = windowBadge(task, now);
  const ck = task.checklist_total ?? 0;
  const st = task.subtask_count ?? 0;
  return (
    <>
      {items && !!task.blocked_by && (
        <span className="pill pill-danger" title={`Blocked by ${task.blocked_by} open task${task.blocked_by === 1 ? "" : "s"}: agents won't pick it up`}>
          🔒 {task.blocked_by}
        </span>
      )}
      {items && ck > 0 && (
        <span className={"pill " + (task.checklist_done === ck ? "pill-done" : "pill-muted")} title="Checklist">
          ☑ {task.checklist_done ?? 0}/{ck}
        </span>
      )}
      {items && st > 0 && (
        <span className={"pill " + (task.subtask_done === st ? "pill-done" : "pill-muted")} title="Subtasks done">
          ⑂ {task.subtask_done ?? 0}/{st}
        </span>
      )}
      {items &&
        fields &&
        cardChips(task, fields).map((c) => (
          <Pill key={c.field.id} color={colorFor(c.field.name + ":" + c.text)} outline title={c.field.name}>
            {c.text}
          </Pill>
        ))}
      {!!task.recurrence_id && (
        <Pill color={SCHEDULE_COLOR} title="Made by a schedule">
          ⟳
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
            ◷ {w.text}
          </Pill>
        ) : (
          <span className="pill pill-muted">◷ {w.text}</span>
        ))}
    </>
  );
}

// MoreMenu: a "⋯" button with a drop-down of actions (phone page heads). Closes on a choice, a tap
// outside or Esc.
export function MoreMenu({ label, children }: { label: string; children: ReactNode }) {
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
    <div className="more-menu" ref={ref}>
      <button type="button" className="button secondary" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        ⋯ {label}
      </button>
      {open && (
        <div className="menu" role="menu" onClick={() => setOpen(false)}>
          {children}
        </div>
      )}
    </div>
  );
}
