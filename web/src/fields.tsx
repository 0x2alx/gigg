// Inputs shared by the new-gig modal and the gig panel: labels, assignees, colour.
import { useEffect, useRef, useState } from "react";
import type { Tag, User } from "./api";
import { fillTime, joinDateTime, splitDateTime } from "./dates";

import { colorFor } from "./theme";
import { Avatar, LabelChip } from "./ui";

// assignable: the workspace's members plus the admins (who reach every workspace).
export function assignable(users: Map<number, User>, memberIds: number[]): User[] {
  const out = [...users.values()].filter((u) => memberIds.includes(u.id) || u.is_admin);
  return out.sort((a, b) => Number(a.kind === "agent") - Number(b.kind === "agent") || a.username.localeCompare(b.username));
}

// withCurrent: the candidates plus anyone already assigned who isn't one any more (a former member),
// so they stay visible and can be unassigned.
export function withCurrent(candidates: User[], ids: number[], users: Map<number, User>): User[] {
  const extra = ids.filter((id) => !candidates.some((u) => u.id === id)).map((id) => users.get(id)).filter((u): u is User => !!u);
  return extra.length ? [...candidates, ...extra] : candidates;
}

// autosave: each change saves at once (the gig panel), so Backspace in the empty box doesn't remove the last label.
export function LabelInput({ value, onChange, known, autosave }: { value: string[]; onChange: (v: string[]) => void; known: Tag[]; autosave?: boolean }) {
  const [text, setText] = useState("");
  const add = (name: string) => {
    const n = name.trim();
    if (n && !value.some((v) => v.toLowerCase() === n.toLowerCase())) onChange([...value, n]);
    setText("");
  };
  const suggestions = known.filter((k) => !value.some((v) => v.toLowerCase() === k.name.toLowerCase()) && k.name.toLowerCase().includes(text.toLowerCase()));
  const tagFor = (name: string): Tag => known.find((k) => k.name.toLowerCase() === name.toLowerCase()) ?? { id: 0, name, color: null };
  return (
    <div className="label-input">
      <div className="chips">
        {value.map((v) => (
          <LabelChip key={v} label={tagFor(v)} onRemove={() => onChange(value.filter((x) => x !== v))} />
        ))}
        <input
          className="chip-text"
          value={text}
          placeholder={value.length ? "" : "Add labels…"}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === ",") {
              e.preventDefault();
              add(text);
            } else if (e.key === "Backspace" && !text && value.length && !autosave) {
              onChange(value.slice(0, -1));
            }
          }}
          aria-label="Add a label"
        />
      </div>
      {suggestions.length > 0 && (
        <div className="suggest">
          {suggestions.slice(0, 8).map((s) => (
            <button key={s.id} type="button" onClick={() => add(s.name)}>
              <LabelChip label={s} />
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// AssigneePicker: people toggled on and off (assignees, and followers with label "Followers").
export function AssigneePicker({ value, onChange, candidates, label = "Assignees" }: { value: number[]; onChange: (v: number[]) => void; candidates: User[]; label?: string }) {
  const toggle = (id: number) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);
  if (candidates.length === 0) return <span className="muted small">No members to pick.</span>;
  return (
    <div className="assignees" role="group" aria-label={label}>
      {candidates.map((u) => (
        <button key={u.id} type="button" className={"assignee" + (value.includes(u.id) ? " on" : "")} aria-pressed={value.includes(u.id)} onClick={() => toggle(u.id)}>
          <Avatar user={u} />
          <span>{u.display_name || u.username}</span>
          {u.kind === "agent" && <span className="pill pill-agent">agent</span>}
        </button>
      ))}
    </div>
  );
}

const SWATCHES = ["#E5484D", "#F76B15", "#FFC53D", "#46A758", "#12A594", "#0090FF", "#3E63DD", "#8E4EC6", "#D6409F", "#A18072"];

export function ColorPicker({ value, onChange }: { value: string | null; onChange: (v: string | null) => void }) {
  return (
    <div className="swatches" role="radiogroup" aria-label="Colour">
      <button type="button" className={"swatch none" + (value === null ? " on" : "")} aria-checked={value === null} role="radio" aria-label="No colour" onClick={() => onChange(null)}>
        ∅
      </button>
      {SWATCHES.map((c) => (
        <button
          key={c}
          type="button"
          role="radio"
          aria-checked={value?.toUpperCase() === c}
          aria-label={`Colour ${c}`}
          className={"swatch" + (value?.toUpperCase() === c ? " on" : "")}
          style={{ background: colorFor("", c) }}
          onClick={() => onChange(c)}
        />
      ))}
    </div>
  );
}

export function toDateInput(ms: number | null): string {
  if (!ms) return "";
  const d = new Date(ms);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export function fromDateInput(v: string): number | null {
  return v ? new Date(v + "T12:00:00").getTime() : null;
}

// DateTimeInput: a local date plus a time (unix ms out). Picking only a date fills in defaultTime
// (shown, so the user sees it). live: report every valid change (forms); otherwise commit when focus
// leaves the pair or on Enter (the gig panel, which saves each commit).
export function DateTimeInput({ value, onChange, defaultTime, label, disabled, live }: {
  value: number | null;
  onChange: (v: number | null) => void;
  defaultTime: string;
  label: string;
  disabled?: boolean;
  live?: boolean;
}) {
  const [v, setV] = useState(() => splitDateTime(value));
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!box.current?.contains(document.activeElement)) setV(splitDateTime(value));
  }, [value]);
  const emit = (next: { date: string; time: string }) => {
    const ms = joinDateTime(next.date, next.time, defaultTime);
    if (ms !== (value ?? null) && (ms !== null || !next.date)) onChange(ms);
  };
  const set = (next: { date: string; time: string }) => {
    setV(next);
    if (live) emit(next);
  };
  return (
    <div
      className="dt-input"
      ref={box}
      role="group"
      aria-label={label}
      onBlur={(e) => {
        if (!live && !e.currentTarget.contains(e.relatedTarget as Node | null)) emit(v);
      }}
      onKeyDown={(e) => {
        if (!live && e.key === "Enter") {
          e.preventDefault();
          emit(v);
        }
      }}
    >
      <input type="date" value={v.date} disabled={disabled} aria-label={`${label} date`} onChange={(e) => set({ date: e.target.value, time: fillTime(e.target.value, v.time, defaultTime) })} />
      <input type="time" value={v.time} disabled={disabled || !v.date} aria-label={`${label} time`} onChange={(e) => set({ ...v, time: e.target.value })} />
      {!disabled && v.date && (
        <button type="button" className="icon-button dt-clear" aria-label={`Clear ${label.toLowerCase()}`} title="Clear" onClick={() => {
          const next = { date: "", time: "" };
          setV(next);
          if (value !== null) onChange(null);
        }}>
          ×
        </button>
      )}
    </div>
  );
}
