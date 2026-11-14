// MarkdownEditor: a Markdown textarea with a compact formatting toolbar, Ctrl/Cmd+B / I / K, Write and
// Preview tabs, an auto-growing height, list continuation on Enter and Tab / Shift-Tab indenting in
// lists. The @mention autocomplete of MentionTextarea keeps working inside it.
import { useId, useLayoutEffect, useRef, useState, type KeyboardEvent, type TextareaHTMLAttributes } from "react";
import type { User } from "./api";
import { Markdown } from "./markdown";
import { MentionTextarea } from "./Mention";
import { continueList, indentList, insertLink, toggleLines, toggleWrap, type Edit, type LineKind } from "./mdedit";

type Props = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "value" | "onChange"> & {
  value: string;
  onChange: (v: string) => void;
  users: User[];
  minRows?: number;
  dirty?: boolean; // marks the editor as holding unsaved text (the task panel flashes it on close)
};

type Tool = { id: string; label: string; icon: string; key?: string; run: (e: Edit) => Edit };

const wrap = (m: string, ph: string) => (e: Edit) => toggleWrap(e, m, ph);
const lines = (k: LineKind) => (e: Edit) => toggleLines(e, k);

const TOOLS: (Tool | "|")[] = [
  { id: "bold", label: "Bold", icon: "B", key: "b", run: wrap("**", "bold text") },
  { id: "italic", label: "Italic", icon: "I", key: "i", run: wrap("_", "italic text") },
  { id: "code", label: "Code", icon: "</>", run: wrap("`", "code") },
  { id: "link", label: "Link", icon: "🔗", key: "k", run: insertLink },
  "|",
  { id: "heading", label: "Heading", icon: "H", run: lines("heading") },
  { id: "quote", label: "Quote", icon: "❝", run: lines("quote") },
  "|",
  { id: "ul", label: "Bulleted list", icon: "•", run: lines("ul") },
  { id: "ol", label: "Numbered list", icon: "1.", run: lines("ol") },
  { id: "task", label: "Checkbox list", icon: "☑", run: lines("task") },
];

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
const mod = isMac ? "⌘" : "Ctrl+";

export function MarkdownEditor({ value, onChange, users, minRows = 4, dirty, onKeyDown, className, ...rest }: Props) {
  const ref = useRef<HTMLTextAreaElement | null>(null);
  const [tab, setTab] = useState<"write" | "preview">("write");
  const id = useId();

  // auto-grow: fit the content, up to 60% of the viewport (then it scrolls)
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || tab !== "write") return;
    el.style.height = "auto";
    const max = Math.max(160, window.innerHeight * 0.6);
    el.style.height = `${Math.min(el.scrollHeight + 2, max)}px`;
    el.style.overflowY = el.scrollHeight + 2 > max ? "auto" : "hidden";
  }, [value, tab]);

  // apply: replaces the changed span through the browser's editing (so Ctrl+Z undoes it) when it
  // can, otherwise sets the value; then selects the result.
  const apply = (next: Edit) => {
    const el = ref.current;
    if (!el) return;
    const old = el.value;
    let p = 0;
    while (p < old.length && p < next.text.length && old[p] === next.text[p]) p++;
    let s = 0;
    while (s < old.length - p && s < next.text.length - p && old[old.length - 1 - s] === next.text[next.text.length - 1 - s]) s++;
    el.focus();
    el.setSelectionRange(p, old.length - s);
    let ok = false;
    try {
      ok = document.execCommand("insertText", false, next.text.slice(p, next.text.length - s));
    } catch {
      ok = false;
    }
    if (!ok || el.value !== next.text) onChange(next.text);
    requestAnimationFrame(() => {
      const t = ref.current;
      if (!t) return;
      t.focus();
      t.setSelectionRange(next.start, next.end);
    });
  };
  const current = (): Edit | null => {
    const el = ref.current;
    return el ? { text: el.value, start: el.selectionStart, end: el.selectionEnd } : null;
  };
  const run = (t: Tool) => {
    const cur = current();
    if (cur) apply(t.run(cur));
  };

  const keys = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    onKeyDown?.(e);
    if (e.defaultPrevented) return;
    const cur = current();
    if (!cur) return;
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey) {
      const t = TOOLS.find((x): x is Tool => x !== "|" && x.key === e.key.toLowerCase());
      if (t) {
        e.preventDefault();
        e.stopPropagation(); // Ctrl/Cmd+K here is "link", not the command palette
        apply(t.run(cur));
      }
      return;
    }
    if (e.key === "Tab" && !e.ctrlKey && !e.metaKey && !e.altKey) {
      const r = indentList(cur, e.shiftKey);
      if (r) {
        e.preventDefault();
        apply(r);
      }
      return;
    }
    if (e.key === "Enter" && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey && !e.nativeEvent.isComposing) {
      const r = continueList(cur);
      if (r) {
        e.preventDefault();
        apply(r);
      }
    }
  };

  return (
    <div className={"mde" + (className ? " " + className : "")} data-dirty={dirty ? "" : undefined}>
      <div className="mde-bar">
        <div className="mde-tabs" role="tablist" aria-label="Editor mode">
          <button type="button" role="tab" id={id + "w"} aria-selected={tab === "write"} aria-controls={id + "p"} className={tab === "write" ? "on" : undefined} onClick={() => setTab("write")}>
            Write
          </button>
          <button type="button" role="tab" id={id + "v"} aria-selected={tab === "preview"} aria-controls={id + "p"} className={tab === "preview" ? "on" : undefined} onClick={() => setTab("preview")}>
            Preview
          </button>
        </div>
        {tab === "write" && (
          <div className="mde-tools" role="toolbar" aria-label="Formatting">
            {TOOLS.map((t, i) =>
              t === "|" ? (
                <span key={"s" + i} className="mde-sep" aria-hidden="true" />
              ) : (
                <button
                  key={t.id}
                  type="button"
                  className={"mde-tool mde-" + t.id}
                  title={t.label + (t.key ? ` (${mod}${t.key.toUpperCase()})` : "")}
                  aria-label={t.label}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => run(t)}
                >
                  {t.icon}
                </button>
              ),
            )}
          </div>
        )}
      </div>
      <div id={id + "p"} role="tabpanel" aria-labelledby={id + (tab === "write" ? "w" : "v")}>
        {tab === "write" ? (
          <MentionTextarea {...rest} inputRef={ref} value={value} onChange={onChange} users={users} rows={minRows} onKeyDown={keys} />
        ) : value.trim() ? (
          <Markdown source={value} className="mde-preview" />
        ) : (
          <p className="mde-preview muted">Nothing to preview.</p>
        )}
      </div>
    </div>
  );
}
