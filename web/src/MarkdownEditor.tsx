// MarkdownEditor: a Markdown textarea with a compact formatting toolbar, Ctrl/Cmd+B / I / K, Write and
// Preview tabs, an auto-growing height, list continuation on Enter and Tab / Shift-Tab indenting in
// lists. The @mention autocomplete of MentionTextarea keeps working inside it.
// Given `attach`, files pasted or dropped into it are attached one by one (a placeholder line stands
// in while each uploads) and linked at the caret: images as ![name](url), other files as a link. The
// description editors and the comment box all get this from here.
import { useEffect, useId, useLayoutEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent, type TextareaHTMLAttributes } from "react";
import type { User } from "./api";
import { Markdown } from "./markdown";
import { MentionTextarea } from "./Mention";
import { continueList, fileLine, isImageFile, indentList, insertLink, insertText, swapText, toggleLines, toggleWrap, uploadingLine, type Edit, type LineKind } from "./mdedit";

type Props = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "value" | "onChange"> & {
  value: string;
  onChange: (v: string) => void;
  users: User[];
  minRows?: number;
  dirty?: boolean; // marks the editor as holding unsaved text (the gig panel flashes it on close)
  // attaches a pasted or dropped file and returns the URL to link it with (rejects with the reason)
  attach?: (file: File) => Promise<string>;
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
  { id: "gig", label: "Checkbox list", icon: "☑", run: lines("gig") },
];

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
const mod = isMac ? "⌘" : "Ctrl+";

export function MarkdownEditor({ value, onChange, users, minRows = 4, dirty, attach, onKeyDown, className, ...rest }: Props) {
  const ref = useRef<HTMLTextAreaElement | null>(null);
  const box = useRef<HTMLDivElement | null>(null);
  const [tab, setTab] = useState<"write" | "preview">("write");
  const [attachErr, setAttachErr] = useState("");
  const id = useId();
  // the text as last set (an upload finishing changes it after the render that started it)
  const latest = useRef(value);
  latest.current = value;

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
  // set: the whole text at once (an upload's placeholder swapped), keeping the caret where it was
  const set = (next: Edit) => {
    latest.current = next.text;
    onChange(next.text);
    const el = ref.current;
    if (el && document.activeElement === el)
      requestAnimationFrame(() => {
        if (ref.current === el) el.setSelectionRange(next.start, next.end);
      });
  };
  const attachRef = useRef(attach);
  attachRef.current = attach;
  const addFiles = async (files: File[]) => {
    const up = attachRef.current;
    if (!up || !files.length) return;
    setAttachErr("");
    const holders = files.map((f) => uploadingLine(f.name));
    const cur = current();
    // an image starts a line of its own (a link to another file may sit inside a sentence)
    const own = isImageFile(files[0]!) && cur && cur.start > 0 && cur.text[cur.start - 1] !== "\n" ? "\n" : "";
    const block = own + holders.join("\n");
    if (cur && tab === "write") {
      const next = insertText(cur, block);
      apply(next);
      latest.current = next.text;
    } else set(insertText({ text: latest.current, start: latest.current.length, end: latest.current.length }, (latest.current && !latest.current.endsWith("\n") ? "\n" : "") + block));
    const problems: string[] = [];
    for (let i = 0; i < files.length; i++) {
      const f = files[i]!;
      let line = "";
      try {
        line = fileLine(f, await up(f));
      } catch (e) {
        problems.push(`${f.name}: ${e instanceof Error && e.message ? e.message : "Couldn't attach it."}`);
      }
      const el = ref.current;
      const end = latest.current.length;
      const here: Edit = { text: latest.current, start: el?.selectionStart ?? end, end: el?.selectionEnd ?? end };
      const next = swapText(here, holders[i]!, line);
      if (next) set(next);
    }
    setAttachErr(problems.join(" "));
  };
  const addRef = useRef(addFiles);
  addRef.current = addFiles;

  // files dropped on the editor are its own: stopped here (natively, before the gig panel's drop zone
  // sees them), so they are not also attached a second time by the panel
  const canAttach = !!attach;
  useEffect(() => {
    const el = box.current;
    if (!el || !canAttach) return;
    const hasFiles = (e: DragEvent) => !!e.dataTransfer && [...e.dataTransfer.types].includes("Files");
    const over = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      e.dataTransfer!.dropEffect = "copy";
      el.classList.add("mde-drop");
    };
    const leave = (e: DragEvent) => {
      if (!el.contains(e.relatedTarget as Node | null)) el.classList.remove("mde-drop");
    };
    const drop = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      e.stopPropagation();
      el.classList.remove("mde-drop");
      void addRef.current([...(e.dataTransfer?.files ?? [])]);
    };
    el.addEventListener("dragover", over);
    el.addEventListener("dragleave", leave);
    el.addEventListener("drop", drop);
    return () => {
      el.classList.remove("mde-drop");
      el.removeEventListener("dragover", over);
      el.removeEventListener("dragleave", leave);
      el.removeEventListener("drop", drop);
    };
  }, [canAttach]);
  // pasted files (a screenshot); text pasted along with them (from an office app) wins
  const paste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = [...e.clipboardData.files];
    if (!attach || !files.length || e.clipboardData.getData("text/plain")) return;
    e.preventDefault();
    void addFiles(files);
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
    <div ref={box} className={"mde" + (className ? " " + className : "")} data-dirty={dirty ? "" : undefined}>
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
          <MentionTextarea {...rest} inputRef={ref} value={value} onChange={onChange} users={users} rows={minRows} onKeyDown={keys} onPaste={attach ? paste : rest.onPaste} />
        ) : value.trim() ? (
          <Markdown source={value} className="mde-preview" />
        ) : (
          <p className="mde-preview muted">Nothing to preview.</p>
        )}
      </div>
      {attachErr && (
        <p className="danger small mde-err" role="alert">
          {attachErr}
        </p>
      )}
    </div>
  );
}
