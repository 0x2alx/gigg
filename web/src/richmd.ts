// richmd: the visual editor's document model and its Markdown, in one place. The editor (RichEditor)
// and the tests use the same extensions, so what is tested is what is edited. Markdown stays the
// stored format: the editor loads it, and every change goes back out as Markdown.
//
// Same dialect as the renderer (markdown.tsx: markdown-it, GFM tables and task lists, breaks: a
// single newline is a line break).
import { type AnyExtension, type JSONContent } from "@tiptap/core";
import { MarkdownManager } from "@tiptap/markdown";
import StarterKit from "@tiptap/starter-kit";
import Image from "@tiptap/extension-image";
import { TableKit } from "@tiptap/extension-table";
import { TaskItem, TaskList } from "@tiptap/extension-list";
import HardBreak from "@tiptap/extension-hard-break";
import { attachmentHrefOK, imageSrcOK } from "./imgsrc";
import { mdit } from "./mdit";
import { previewOf } from "./previews";

// a line break is a plain newline: the renderer treats one as a break (breaks: true)
const LineBreak = HardBreak.extend({
  renderMarkdown: () => "\n",
});

// images show only when this server serves them (as in the rendered Markdown), or, for a file waiting
// for its gig to be created, the file itself; any other keeps its Markdown but loads nothing
const ServerImage = Image.extend({
  renderHTML({ HTMLAttributes }) {
    const src = String(HTMLAttributes.src ?? "");
    if (imageSrcOK(src)) return ["img", HTMLAttributes];
    const local = previewOf(src);
    if (local) return ["img", { ...HTMLAttributes, src: local }];
    return ["img", { ...HTMLAttributes, src: null, "data-blocked": "", title: "Only images on this server show" }];
  },
});

// the links the editor keeps live: what the renderer links (http(s), mailto, an attachment), plus a
// file waiting for its gig to be created
export const linkOK = (href: string): boolean => /^(?:https?:\/\/|mailto:)\S+$/i.test(href) || attachmentHrefOK(href) || /^pending-upload:\d+$/.test(href);

export function richExtensions(): AnyExtension[] {
  return [
    StarterKit.configure({
      hardBreak: false,
      underline: false, // no Markdown for it
      link: { openOnClick: false, autolink: true, linkOnPaste: true, isAllowedUri: (url) => linkOK(url) },
      // trailingNode (on): there's always a paragraph after a last image or table to type into
    }),
    LineBreak,
    ServerImage.configure({ inline: false, allowBase64: false }),
    TableKit.configure({ table: { resizable: false } }),
    TaskList,
    TaskItem.configure({ nested: true }),
  ];
}

let manager: MarkdownManager | null = null;
export function mdManager(): MarkdownManager {
  manager ??= new MarkdownManager({ markedOptions: { gfm: true, breaks: true }, extensions: richExtensions() });
  return manager;
}

export const parseMd = (md: string): JSONContent => mdManager().parse(md);
// toMd: the document as Markdown, written the way people and agents write it: a bare URL stays bare
// (the renderer links it), an underscore inside a word isn't escaped (it never makes emphasis), no
// spaces at line ends, at most one blank line in a row. Code blocks are left exactly as they are.
export function toMd(doc: JSONContent): string {
  const out: string[] = [];
  let fence = "";
  for (const line of mdManager().serialize(doc).split("\n")) {
    const f = /^\s*(`{3,}|~{3,})/.exec(line);
    if (fence) {
      out.push(line);
      if (f && f[1]!.startsWith(fence)) fence = "";
      continue;
    }
    if (f) {
      fence = f[1]!;
      out.push(line);
      continue;
    }
    if (line.trim() === "" && out.length && out[out.length - 1] === "") continue;
    out.push(
      line
        .replace(/\s+$/, "")
        .replace(/\[((?:https?:\/\/|mailto:)[^\]\s]+)\]\(\1\)/g, "$1")
        .replace(/(?<=\w)\\_(?=\w)/g, "_"),
    );
  }
  return out.join("\n").replace(/^\n+/, "").replace(/\n+$/, "");
}

// roundTrips: loading md into the visual editor and saving it gives Markdown that renders exactly the
// same (the board's own renderer, mdit). When it doesn't, the editor opens on the Markdown source
// instead, so nothing is lost on save.
export function roundTrips(md: string): boolean {
  if (!md.trim()) return true;
  try {
    return mdit.render(toMd(parseMd(md))) === mdit.render(md);
  } catch {
    return false;
  }
}
