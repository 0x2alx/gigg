// A textarea with @mention autocomplete: typing "@" lists workspace members and agents; picking one
// writes "@username". Arrow keys move, Enter/Tab pick, Escape closes the list.
import { useRef, useState, type RefObject, type TextareaHTMLAttributes } from "react";
import type { User } from "./api";
import { activeMention, insertMention, matchUsers } from "./items";
import { Avatar } from "./ui";

type Props = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "value" | "onChange"> & {
  value: string;
  onChange: (v: string) => void;
  users: User[];
  inputRef?: RefObject<HTMLTextAreaElement | null>; // the textarea, for editors wrapping this one
};

export function MentionTextarea({ value, onChange, users, onKeyDown, inputRef, ...rest }: Props) {
  const own = useRef<HTMLTextAreaElement>(null);
  const ref = inputRef ?? own;
  const [m, setM] = useState<{ start: number; query: string; caret: number } | null>(null);
  const [sel, setSel] = useState(0);
  const matches = m ? matchUsers(users, m.query) : [];
  const open = !!m && matches.length > 0;

  const track = (el: HTMLTextAreaElement) => {
    const caret = el.selectionStart ?? el.value.length;
    const a = el.selectionEnd === caret ? activeMention(el.value, caret) : null;
    setM(a ? { ...a, caret } : null);
    if (!a || a.query !== m?.query) setSel(0);
  };
  const pick = (u: User) => {
    if (!m) return;
    const r = insertMention(value, m.start, m.caret, u.username);
    onChange(r.text);
    setM(null);
    requestAnimationFrame(() => {
      const el = ref.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(r.caret, r.caret);
    });
  };

  return (
    <div className="mention-box">
      <textarea
        {...rest}
        ref={ref}
        value={value}
        role="combobox"
        aria-expanded={open}
        aria-autocomplete="list"
        onChange={(e) => {
          onChange(e.target.value);
          track(e.target);
        }}
        onClick={(e) => track(e.currentTarget)}
        onBlur={() => setTimeout(() => setM(null), 150)}
        onKeyDown={(e) => {
          if (open) {
            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault();
              setSel((s) => (s + (e.key === "ArrowDown" ? 1 : matches.length - 1)) % matches.length);
              return;
            }
            if (e.key === "Enter" || e.key === "Tab") {
              e.preventDefault();
              pick(matches[Math.min(sel, matches.length - 1)]!);
              return;
            }
            if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              setM(null);
              return;
            }
          }
          onKeyDown?.(e);
        }}
      />
      {open && (
        <ul className="mention-list" role="listbox" aria-label="Mention someone">
          {matches.map((u, i) => (
            <li key={u.id} role="option" aria-selected={i === sel}>
              <button
                type="button"
                className={i === sel ? "on" : undefined}
                tabIndex={-1}
                onMouseDown={(e) => {
                  e.preventDefault();
                  pick(u);
                }}
              >
                <Avatar user={u} size="xs" />
                <strong>@{u.username}</strong>
                {u.display_name && u.display_name !== u.username && <span className="muted small">{u.display_name}</span>}
                {u.kind === "agent" && <span className="pill pill-agent">agent</span>}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
