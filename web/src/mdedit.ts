// Pure Markdown editing transforms for the MarkdownEditor toolbar and shortcuts. Each takes the text
// and the selection and returns the new text and selection (or null: nothing to do, let the browser act).

export interface Edit {
  text: string;
  start: number;
  end: number;
}

export type LineKind = "ul" | "ol" | "gig" | "quote" | "heading";

const LIST_RE = /^(\s*)([-*+]\s\[[ xX]\]\s|[-*+]\s|\d+[.)]\s)/;
const PREFIX_RE: Record<LineKind, RegExp> = {
  ul: /^(\s*)[-*+]\s(?!\[[ xX]\]\s)/,
  ol: /^(\s*)\d+[.)]\s/,
  gig: /^(\s*)[-*+]\s\[[ xX]\]\s/,
  quote: /^(\s*)>\s?/,
  heading: /^(\s*)#{1,6}\s/,
};

// toggleWrap: wraps the selection in marker (bold "**", italic "_", code "`"), or unwraps it when
// it is already wrapped. An empty selection inserts the markers around a selected placeholder.
// Code on a multi-line selection becomes a fenced block.
export function toggleWrap(e: Edit, marker: string, placeholder: string): Edit {
  const { text, start, end } = e;
  const sel = text.slice(start, end);
  const m = marker.length;
  if (marker === "`" && sel.includes("\n")) {
    const before = start > 0 && text[start - 1] !== "\n" ? "\n" : "";
    const block = `${before}\`\`\`\n${sel}\n\`\`\`\n`;
    const inner = start + before.length + 4;
    return { text: text.slice(0, start) + block + text.slice(end), start: inner, end: inner + sel.length };
  }
  if (start >= m && text.slice(start - m, start) === marker && text.slice(end, end + m) === marker) {
    return { text: text.slice(0, start - m) + sel + text.slice(end + m), start: start - m, end: end - m };
  }
  if (sel.length >= 2 * m && sel.startsWith(marker) && sel.endsWith(marker)) {
    const inner = sel.slice(m, sel.length - m);
    return { text: text.slice(0, start) + inner + text.slice(end), start, end: start + inner.length };
  }
  // keep surrounding spaces outside the markers ("**word** " not "**word **")
  const lead = sel.length - sel.trimStart().length;
  const trail = sel.length - sel.trimEnd().length;
  const core = sel.trim() || placeholder;
  const s = start + lead;
  const out = text.slice(0, s) + marker + core + marker + text.slice(end - trail);
  return { text: out, start: s + m, end: s + m + core.length };
}

// insertLink: [selection](https://) with the URL selected; an empty selection gets a placeholder text.
// A selection that is itself a URL becomes the target, with the link text selected.
export function insertLink(e: Edit): Edit {
  const { text, start, end } = e;
  const sel = text.slice(start, end);
  if (/^https?:\/\/\S+$/.test(sel)) {
    const out = text.slice(0, start) + `[link text](${sel})` + text.slice(end);
    return { text: out, start: start + 1, end: start + 10 };
  }
  const label = sel || "link text";
  const url = "https://";
  const out = text.slice(0, start) + `[${label}](${url})` + text.slice(end);
  const u = start + label.length + 3;
  return sel ? { text: out, start: u, end: u + url.length } : { text: out, start: start + 1, end: start + 1 + label.length };
}

// lineRange: the start of the first and the end of the last line the selection touches.
function lineRange(text: string, start: number, end: number): [number, number] {
  const a = text.lastIndexOf("\n", start - 1) + 1;
  // a selection ending right after a newline does not include the next line
  const e2 = end > start && text[end - 1] === "\n" ? end - 1 : end;
  const nl = text.indexOf("\n", e2);
  return [a, nl === -1 ? text.length : nl];
}

// toggleLines: adds a line prefix (list, task list, quote, heading) to every selected line, or
// removes it when every non-empty line already has it. Switching between list kinds replaces the marker.
export function toggleLines(e: Edit, kind: LineKind): Edit {
  const [a, b] = lineRange(e.text, e.start, e.end);
  const lines = e.text.slice(a, b).split("\n");
  const has = PREFIX_RE[kind];
  const filled = lines.filter((l) => l.trim() !== "");
  const remove = filled.length > 0 && filled.every((l) => has.test(l));
  let n = 0;
  const out = lines.map((l) => {
    if (remove) return l.replace(has, "$1");
    if (l.trim() === "" && lines.length > 1) return l;
    const indent = /^\s*/.exec(l)![0];
    let body = l.slice(indent.length);
    if (kind === "ul" || kind === "ol" || kind === "gig") body = body.replace(/^([-*+]\s\[[ xX]\]\s|[-*+]\s|\d+[.)]\s)/, "");
    else if (kind === "heading") body = body.replace(/^#{1,6}\s/, "");
    n++;
    const prefix = kind === "ul" ? "- " : kind === "ol" ? `${n}. ` : kind === "gig" ? "- [ ] " : kind === "quote" ? "> " : "## ";
    return indent + prefix + body;
  });
  const joined = out.join("\n");
  const text = e.text.slice(0, a) + joined + e.text.slice(b);
  if (e.start === e.end && lines.length === 1) {
    const caret = Math.max(a, Math.min(a + joined.length, e.start + joined.length - lines[0]!.length));
    return { text, start: caret, end: caret };
  }
  return { text, start: a, end: a + joined.length };
}

// indentList: Tab / Shift-Tab on list lines indents or outdents them by two spaces. Returns null when a
// selected line is not a list item, so Tab keeps moving focus outside lists.
export function indentList(e: Edit, outdent: boolean): Edit | null {
  const [a, b] = lineRange(e.text, e.start, e.end);
  const lines = e.text.slice(a, b).split("\n");
  if (!lines.every((l) => LIST_RE.test(l))) return null;
  let first = 0;
  let total = 0;
  const out = lines.map((l, i) => {
    if (!outdent) {
      if (i === 0) first = 2;
      total += 2;
      return "  " + l;
    }
    const cut = Math.min(2, /^ */.exec(l)![0].length);
    if (i === 0) first = -cut;
    total -= cut;
    return l.slice(cut);
  });
  if (total === 0) return outdent ? { ...e } : null;
  const text = e.text.slice(0, a) + out.join("\n") + e.text.slice(b);
  const start = Math.max(a, e.start + first);
  return { text, start, end: e.start === e.end ? start : Math.max(start, e.end + total) };
}

// continueList: Enter at the end of a list item starts the next item (numbered lists count on, gig
// items start unchecked); Enter on an empty item ends the list. Null when the caret is not in a list item.
export function continueList(e: Edit): Edit | null {
  if (e.start !== e.end) return null;
  const { text, start } = e;
  const a = text.lastIndexOf("\n", start - 1) + 1;
  const line = text.slice(a, start);
  const m = LIST_RE.exec(line);
  if (!m) return null;
  const [whole, indent, marker] = m;
  const lineEnd = text.indexOf("\n", start);
  const rest = text.slice(start, lineEnd === -1 ? text.length : lineEnd);
  if (line.slice(whole.length).trim() === "" && rest.trim() === "") {
    const out = text.slice(0, a) + text.slice(start);
    return { text: out, start: a, end: a };
  }
  let next = marker!;
  const num = /^(\d+)([.)])/.exec(marker!);
  if (num) next = `${Number(num[1]) + 1}${num[2]} `;
  else if (/\[[ xX]\]/.test(marker!)) next = marker!.replace(/\[[ xX]\]/, "[ ]");
  const ins = "\n" + indent + next;
  return { text: text.slice(0, start) + ins + text.slice(start), start: start + ins.length, end: start + ins.length };
}

// Pasted and dropped files: each is attached, and its line goes in at the caret: an image as
// ![name](url), anything else as a link. While it uploads a placeholder stands in its place.
// mdText: a file name as literal link text: every character that could start inline Markdown (emphasis,
// code, a nested link or image, HTML, an entity, strikethrough, a table cell) is escaped and line
// breaks become spaces, so a name like "x](y) *z*" stays one plain link label.
const mdText = (s: string) => s.replace(/[\r\n]+/g, " ").replace(/[\\`*_[\]<>!&~|]/g, "\\$&");

export const isImageFile = (f: { type: string }) => f.type.startsWith("image/");

export const uploadingLine = (name: string) => `![Uploading ${mdText(name)}…]()`;

export const fileLine = (f: { name: string; type: string }, url: string) => `${isImageFile(f) ? "!" : ""}[${mdText(f.name)}](${url})`;

// insertText: text in place of the selection, the caret after it.
export function insertText(e: Edit, s: string): Edit {
  const text = e.text.slice(0, e.start) + s + e.text.slice(e.end);
  const at = e.start + s.length;
  return { text, start: at, end: at };
}

// swapText: the first `from` replaced by `to` (nothing to do: null), the selection kept where it was
// in the text around it. An empty `to` on a line of its own also takes that line's newline.
export function swapText(e: Edit, from: string, to: string): Edit | null {
  const i = e.text.indexOf(from);
  if (i < 0) return null;
  let a = i;
  let b = i + from.length;
  if (!to && e.text[b] === "\n" && (a === 0 || e.text[a - 1] === "\n")) b++;
  else if (!to && b === e.text.length && a > 0 && e.text[a - 1] === "\n") a--;
  const text = e.text.slice(0, a) + to + e.text.slice(b);
  const d = to.length - (b - a);
  const move = (p: number) => (p >= b ? p + d : p > a ? a + to.length : p);
  return { text, start: move(e.start), end: move(e.end) };
}
