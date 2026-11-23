// RichText: the visual side of MarkdownEditor. A ProseMirror editor (TipTap) on the same document model
// as richmd.ts: it loads the Markdown it is given, shows it as it reads (images, lists, tables, task
// lists), and sends every change back out as Markdown. It holds no state of its own beyond the
// editor: the Markdown in `value` stays the truth, and a new value from outside (a reset after
// saving, an upload finishing in Markdown mode) replaces the document.
//
// @ opens the same people list as the Markdown box; a pick inserts "@username" as plain text, which
// is what the server reads mentions from. Images show only when they come from this server, as in the
// rendered Markdown (imgsrc.ts).
import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";
import { Extension, type JSONContent } from "@tiptap/core";
import Suggestion, { type SuggestionKeyDownProps, type SuggestionProps } from "@tiptap/suggestion";
import { Placeholder } from "@tiptap/extensions";
import type { User } from "./api";
import { parseMd, richExtensions, toMd } from "./richmd";

// the keys the callers' onKeyDown reads (Ctrl/Cmd+Enter submits the form around the editor)
export interface EditorKey {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  defaultPrevented: boolean;
  preventDefault: () => void;
  stopPropagation: () => void;
  currentTarget: { form: HTMLFormElement | null };
}

export interface RichHandle {
  editor: Editor | null;
  focus: () => void;
  link: () => void; // Ctrl/Cmd+K and the toolbar: removes the link at the caret, or asks for one
  // inserts at the caret: an image (from this server) as its own block, any other file as a link
  insertFile: (name: string, url: string, image: boolean) => void;
}

const people = (users: () => User[]) =>
  Extension.create({
    name: "mentionPick",
    addProseMirrorPlugins() {
      return [
        Suggestion<User>({
          editor: this.editor,
          char: "@",
          items: ({ query }) => {
            const q = query.toLowerCase();
            return users()
              .filter((u) => (u.username.toLowerCase().startsWith(q) || (u.display_name ?? "").toLowerCase().includes(q)))
              .slice(0, 6);
          },
          command: ({ editor, range, props }) => {
            editor.chain().focus().insertContentAt(range, "@" + props.username + " ").run();
          },
          render: () => {
            let list: HTMLUListElement | null = null;
            let items: User[] = [];
            let sel = 0;
            let pick: ((u: User) => void) | null = null;
            const draw = (p: SuggestionProps<User>) => {
              items = p.items;
              pick = (u) => p.command(u);
              if (!list) {
                list = document.createElement("ul");
                list.className = "mention-list rt-mentions";
                list.setAttribute("role", "listbox");
                list.setAttribute("aria-label", "Mention someone");
                document.body.appendChild(list);
              }
              list.replaceChildren(
                ...items.map((u, i) => {
                  const li = document.createElement("li");
                  const b = document.createElement("button");
                  b.type = "button";
                  b.className = i === sel ? "on" : "";
                  b.setAttribute("role", "option");
                  b.setAttribute("aria-selected", String(i === sel));
                  b.textContent = "@" + u.username;
                  if (u.display_name && u.display_name !== u.username) {
                    const n = document.createElement("span");
                    n.className = "muted small";
                    n.textContent = u.display_name;
                    b.appendChild(n);
                  }
                  b.onmousedown = (e) => {
                    e.preventDefault();
                    pick?.(u);
                  };
                  li.appendChild(b);
                  return li;
                }),
              );
              list.hidden = items.length === 0;
              const r = p.clientRect?.();
              if (r) {
                list.style.position = "fixed";
                list.style.left = `${Math.max(8, Math.min(r.left, window.innerWidth - 330))}px`;
                list.style.top = `${r.bottom + 4}px`;
              }
            };
            return {
              onStart: (p) => {
                sel = 0;
                draw(p);
              },
              onUpdate: (p) => {
                sel = Math.min(sel, Math.max(0, p.items.length - 1));
                draw(p);
              },
              onKeyDown: ({ event }: SuggestionKeyDownProps) => {
                if (!items.length) return false;
                if (event.key === "ArrowDown" || event.key === "ArrowUp") {
                  sel = (sel + (event.key === "ArrowDown" ? 1 : items.length - 1)) % items.length;
                  list?.querySelectorAll("button").forEach((b, i) => {
                    b.className = i === sel ? "on" : "";
                    b.setAttribute("aria-selected", String(i === sel));
                  });
                  return true;
                }
                if (event.key === "Enter" || event.key === "Tab") {
                  pick?.(items[sel]!);
                  return true;
                }
                if (event.key === "Escape") {
                  list?.remove();
                  list = null;
                  return true;
                }
                return false;
              },
              onExit: () => {
                list?.remove();
                list = null;
              },
            };
          },
        }),
      ];
    },
  });

export const RichText = forwardRef<RichHandle, {
  value: string;
  onChange: (md: string) => void;
  users: User[];
  placeholder?: string;
  ariaLabel?: string;
  autoFocus?: boolean;
  autofocusMark?: boolean; // data-autofocus: a dialog focuses it first
  minRows: number;
  onKeyDown?: (e: EditorKey) => void;
  onLink?: () => string | null; // asks for a link's URL (Ctrl/Cmd+K)
  onFiles?: (files: File[]) => void; // pasted or dropped files
  onState?: () => void; // the document or the caret moved (the toolbar shows what is on there)
}>(function RichText(p, ref) {
  // the Markdown this editor last sent (or loaded): a different value from outside replaces the document
  const sent = useRef(p.value);
  const usersRef = useRef(p.users);
  usersRef.current = p.users;
  const cb = useRef(p);
  cb.current = p;
  const editor = useEditor({
    extensions: [...richExtensions(), Placeholder.configure({ placeholder: p.placeholder ?? "" }), people(() => usersRef.current)],
    content: p.value.trim() ? parseMd(p.value) : "",
    autofocus: p.autoFocus ? "end" : false,
    injectCSS: false, // the page's CSP allows no inline style: the editor's CSS is in theme.css
    onCreate: () => cb.current.onState?.(),
    onTransaction: () => cb.current.onState?.(),
    editorProps: {
      attributes: {
        class: "md rt-content" + (p.minRows >= 6 ? " rt-tall" : ""),
        role: "textbox",
        "aria-multiline": "true",
        ...(p.ariaLabel ? { "aria-label": p.ariaLabel } : {}),
        ...(p.autofocusMark ? { "data-autofocus": "" } : {}),
      },
      handleKeyDown: (view, event) => {
        const k: EditorKey = {
          key: event.key, ctrlKey: event.ctrlKey, metaKey: event.metaKey, shiftKey: event.shiftKey, altKey: event.altKey,
          get defaultPrevented() {
            return event.defaultPrevented;
          },
          preventDefault: () => event.preventDefault(),
          stopPropagation: () => event.stopPropagation(),
          currentTarget: { form: (view.dom as HTMLElement).closest("form") },
        };
        cb.current.onKeyDown?.(k);
        if (event.defaultPrevented) return true;
        if ((event.ctrlKey || event.metaKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === "k") {
          event.preventDefault();
          event.stopPropagation(); // the link, not the command palette
          link();
          return true;
        }
        return false;
      },
      handlePaste: (_view, event) => {
        const files = [...(event.clipboardData?.files ?? [])];
        if (!files.length || event.clipboardData?.getData("text/plain") || !cb.current.onFiles) return false;
        event.preventDefault();
        cb.current.onFiles(files);
        return true;
      },
    },
    onUpdate: ({ editor }) => {
      const md = editor.isEmpty ? "" : toMd(editor.getJSON());
      sent.current = md;
      cb.current.onChange(md);
    },
  });

  const link = () => {
    if (!editor) return;
    if (editor.isActive("link")) {
      editor.chain().focus().extendMarkRange("link").unsetLink().run();
      return;
    }
    const url = cb.current.onLink?.();
    if (!url) return;
    const { empty } = editor.state.selection;
    if (empty) editor.chain().focus().insertContent({ type: "text", text: url, marks: [{ type: "link", attrs: { href: url } }] }).run();
    else editor.chain().focus().extendMarkRange("link").setLink({ href: url }).run();
  };

  // a new value from outside (cleared after sending, a reset) replaces the document
  useEffect(() => {
    if (!editor || p.value === sent.current) return;
    sent.current = p.value;
    editor.commands.setContent(p.value.trim() ? (parseMd(p.value) as JSONContent) : "", { emitUpdate: false });
  }, [editor, p.value]);

  useImperativeHandle(
    ref,
    () => ({
      editor,
      focus: () => editor?.commands.focus(),
      link,
      insertFile: (name, url, image) => {
        if (!editor) return;
        if (image) editor.chain().focus().insertContent({ type: "image", attrs: { src: url, alt: name } }).run();
        else editor.chain().focus().insertContent([{ type: "text", text: name, marks: [{ type: "link", attrs: { href: url } }] }, { type: "text", text: " " }]).run();
      },
    }),
    [editor],
  );

  return <EditorContent editor={editor} className="rt" />;
});

// what the visual toolbar can do, for the editor at hand
export function richTools(editor: Editor | null, link: () => void) {
  const on = (name: string, attrs?: Record<string, unknown>) => !!editor?.isActive(name, attrs);
  const c = () => editor!.chain().focus();
  return [
    { id: "bold", label: "Bold", icon: "B", key: "b", active: on("bold"), run: () => c().toggleBold().run() },
    { id: "italic", label: "Italic", icon: "I", key: "i", active: on("italic"), run: () => c().toggleItalic().run() },
    { id: "code", label: "Code", icon: "</>", active: on("code"), run: () => c().toggleCode().run() },
    { id: "link", label: "Link", icon: "🔗", key: "k", active: on("link"), run: link },
    "|" as const,
    { id: "heading", label: "Heading", icon: "H", active: on("heading"), run: () => c().toggleHeading({ level: 2 }).run() },
    { id: "quote", label: "Quote", icon: "❝", active: on("blockquote"), run: () => c().toggleBlockquote().run() },
    "|" as const,
    { id: "ul", label: "Bulleted list", icon: "•", active: on("bulletList"), run: () => c().toggleBulletList().run() },
    { id: "ol", label: "Numbered list", icon: "1.", active: on("orderedList"), run: () => c().toggleOrderedList().run() },
    { id: "gig", label: "Checkbox list", icon: "☑", active: on("taskList"), run: () => c().toggleTaskList().run() },
  ];
}
