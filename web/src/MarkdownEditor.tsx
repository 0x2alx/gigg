// MarkdownEditor: a Markdown textarea with a compact formatting toolbar, Ctrl/Cmd+B / I / K, Write and
// Preview tabs, an auto-growing height, list continuation on Enter and Tab / Shift-Tab indenting in
// lists. The @mention autocomplete of MentionTextarea keeps working inside it.
// Given `attach`, files pasted or dropped into it are attached one by one (a placeholder line stands
// in while each uploads) and linked at the caret: images as ![name](url), other files as a link. The
// description editors and the comment box all get this from here.
import { useEffect, useId, useLayoutEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent, type ReactNode, type TextareaHTMLAttributes, useReducer } from "react";
import type { User } from "./api";
import { MentionTextarea } from "./Mention";
import { continueList, fileLine, isImageFile, indentList, insertLink, insertText, swapText, toggleLines, toggleWrap, uploadingLine, type Edit, type LineKind } from "./mdedit";
import { RichText, richTools, type RichHandle } from "./RichText";
import { roundTrips } from "./richmd";
import { Icon } from "./icons";

export type MarkdownEditorProps = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "value" | "onChange"> & {
  value: string;
  onChange: (v: string) => void;
  users: User[];
  minRows?: number;
  dirty?: boolean; // marks the editor as holding unsaved text (the gig panel flashes it on close)
  // attaches a pasted or dropped file and returns the URL to link it with (rejects with the reason)
  attach?: (file: File) => Promise<string>;
};

type Tool = { id: string; label: string; icon: ReactNode; key?: string; run: (e: Edit) => Edit };

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
  { id: "ul", label: "Bulleted list", icon: <Icon name="list" />, run: lines("ul") },
  { id: "ol", label: "Numbered list", icon: "1.", run: lines("ol") },
  { id: "gig", label: "Checkbox list", icon: "☑", run: lines("gig") },
];

const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
const mod = isMac ? "⌘" : "Ctrl+";

export function MarkdownEditor({ value, onChange, users, minRows = 4, dirty, attach, onKeyDown, className, ...rest }: MarkdownEditorProps) {
  const ref = useRef<HTMLTextAreaElement | null>(null);
  const box = useRef<HTMLDivElement | null>(null);
  // Visual (as it reads) by default; Markdown (the source) when the text wouldn't come back the same
  // from the visual editor, so saving never changes what wasn't touched
  const [tab, setTab] = useState<"visual" | "markdown">(() => (roundTrips(value) ? "visual" : "markdown"));
  const rich = useRef<RichHandle>(null);
  // the visual editor's caret moved or its document changed: the toolbar shows what is on there
  const [, redraw] = useReducer((n: number) => n + 1, 0);
  const fileInput = useRef<HTMLInputElement>(null);
  const [uploading, setUploading] = useState<string[]>([]);
  const [attachErr, setAttachErr] = useState("");
  const id = useId();
  // the text as last set (an upload finishing changes it after the render that started it)
  const latest = useRef(value);
  latest.current = value;

  // auto-grow: fit the content, up to 60% of the viewport (then it scrolls)
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || tab !== "markdown") return;
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
    if (tab === "visual") {
      // each file is uploaded, then put in at the caret (the line under the editor says what's on its way)
      setUploading(files.map((f) => f.name));
      const problems: string[] = [];
      for (const f of files) {
        try {
          const url = await up(f);
          rich.current?.insertFile(f.name, url, isImageFile(f));
        } catch (e) {
          problems.push(`${f.name}: ${e instanceof Error && e.message ? e.message : "Couldn't attach it."}`);
        }
        setUploading((u) => u.filter((n) => n !== f.name));
      }
      setAttachErr(problems.join(" "));
      return;
    }
    const holders = files.map((f) => uploadingLine(f.name));
    const cur = current();
    // an image starts a line of its own (a link to another file may sit inside a sentence)
    const own = isImageFile(files[0]!) && cur && cur.start > 0 && cur.text[cur.start - 1] !== "\n" ? "\n" : "";
    const block = own + holders.join("\n");
    if (cur && tab === "markdown") {
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

  const askLink = (): string | null => {
    const u = window.prompt("Link to (https://…)")?.trim();
    return u && /^(https?:\/\/|mailto:)\S+$/i.test(u) ? u : null;
  };
  const visualEditor = tab === "visual" ? (rich.current?.editor ?? null) : null;
  const tools =
    tab === "visual"
      ? richTools(visualEditor, () => rich.current?.link())
      : TOOLS.map((x) => (x === "|" ? x : { ...x, active: false, run: () => run(x) }));

  return (
    <div ref={box} className={"mde" + (className ? " " + className : "")} data-dirty={dirty ? "" : undefined}>
      <div className="mde-bar">
        <div className="mde-tabs" role="tablist" aria-label="Editor mode">
          <button type="button" role="tab" id={id + "w"} aria-selected={tab === "visual"} aria-controls={id + "p"} className={tab === "visual" ? "on" : undefined} onClick={() => setTab("visual")} title="Edit it as it reads">
            Visual
          </button>
          <button type="button" role="tab" id={id + "v"} aria-selected={tab === "markdown"} aria-controls={id + "p"} className={tab === "markdown" ? "on" : undefined} onClick={() => setTab("markdown")} title="Edit the Markdown source">
            Markdown
          </button>
        </div>
        <div className="mde-tools" role="toolbar" aria-label="Formatting">
          {tools.map((x, i) =>
            x === "|" ? (
              <span key={"s" + i} className="mde-sep" aria-hidden="true" />
            ) : (
              <button
                key={x.id}
                type="button"
                className={"mde-tool mde-" + x.id + (x.active ? " on" : "")}
                title={x.label + (x.key ? ` (${mod}${x.key.toUpperCase()})` : "")}
                aria-label={x.label}
                aria-pressed={tab === "visual" ? x.active : undefined}
                onMouseDown={(e) => e.preventDefault()}
                disabled={tab === "visual" && !visualEditor}
                onClick={() => x.run()}
              >
                {x.icon}
              </button>
            ),
          )}
        </div>
        {attach && (
          <>
            <button type="button" className="mde-tool mde-file" title="Insert a file (images show in place)" aria-label="Insert file" onMouseDown={(e) => e.preventDefault()} onClick={() => fileInput.current?.click()}>
                <Icon name="paperclip" />
              </button>
              <input
                ref={fileInput}
                type="file"
                multiple
                hidden
                tabIndex={-1}
                onChange={(e) => {
                  const fs = [...(e.target.files ?? [])];
                  e.target.value = "";
                  void addFiles(fs);
                }}
              />
          </>
        )}
      </div>
      <div id={id + "p"} role="tabpanel" aria-labelledby={id + (tab === "visual" ? "w" : "v")}>
        {tab === "markdown" ? (
          <MentionTextarea {...rest} inputRef={ref} value={value} onChange={onChange} users={users} rows={minRows} onKeyDown={keys} onPaste={attach ? paste : rest.onPaste} />
        ) : (
          <RichText
            ref={rich}
            value={value}
            onChange={onChange}
            users={users}
            minRows={minRows}
            placeholder={typeof rest.placeholder === "string" ? rest.placeholder.replace("(Markdown) ", "") : undefined}
            ariaLabel={rest["aria-label"]}
            autoFocus={rest.autoFocus}
            autofocusMark={"data-autofocus" in rest}
            onKeyDown={onKeyDown ? (k) => onKeyDown(k as unknown as KeyboardEvent<HTMLTextAreaElement>) : undefined}
            onFiles={attach ? (fs) => void addFiles(fs) : undefined}
            onLink={askLink}
            onState={redraw}
          />
        )}
      </div>
      {uploading.length > 0 && (
        <p className="muted small mde-uploading" role="status">
          Uploading {uploading.join(", ")}…
        </p>
      )}
      {attachErr && (
        <p className="danger small mde-err" role="alert">
          {attachErr}
        </p>
      )}
    </div>
  );
}
