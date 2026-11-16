// Markdown is stored as source and rendered here: markdown-it without HTML, then DOMPurify with a
// small allowlist. Links are http(s)/mailto only and open in a new tab; images only from this server (imgsrc.ts).
import MarkdownIt from "markdown-it";
import DOMPurify from "dompurify";
import type { User } from "./api";
import { splitMentions } from "./items";
import { taskLists } from "./mdtasks";
import { imageSrcOK } from "./imgsrc";

const md = new MarkdownIt({ html: false, linkify: true, breaks: true });

md.use(taskLists);

DOMPurify.addHook("afterSanitizeAttributes", (node) => {
  if (node.tagName === "A") {
    const href = node.getAttribute("href") ?? "";
    if (!/^(https?:|mailto:)/i.test(href)) node.removeAttribute("href");
    node.setAttribute("target", "_blank");
    node.setAttribute("rel", "noopener noreferrer");
  }
  if (node.tagName === "IMG") {
    // only images this server serves: avatars and static image files. Any other same-origin URL would
    // be fetched with the viewer's cookie (an API call, or an event stream held open per image).
    const src = node.getAttribute("src") ?? "";
    if (!imageSrcOK(src)) node.remove();
  }
});

const allowed = {
  ALLOWED_TAGS: ["p", "br", "strong", "em", "s", "del", "code", "pre", "blockquote", "ul", "ol", "li", "h1", "h2", "h3", "h4", "h5", "h6",
    "a", "hr", "table", "thead", "tbody", "tr", "th", "td", "img", "input"],
  ALLOWED_ATTR: ["href", "target", "rel", "src", "alt", "title", "type", "checked", "disabled", "align"],
};

// Known users for @mention pills (lower-cased username -> user). Set by the views that know them.
let known = new Map<string, Pick<User, "username" | "color" | "display_name"> & { kind?: User["kind"] }>();
let knownFrom: unknown = null;

export function setMentionUsers(users: Map<number, User> | User[]): void {
  if (users === knownFrom) return;
  knownFrom = users;
  known = new Map([...users.values()].map((u) => [u.username.toLowerCase(), u]));
}

// highlightMentions: wraps @username of known users in a coloured pill, outside code, pre and links.
// It runs on the already-sanitised HTML and only adds spans it builds itself.
function highlightMentions(html: string): string {
  if (!known.size || !html.includes("@") || typeof document === "undefined") return html;
  const root = document.createElement("template");
  root.innerHTML = html;
  const walker = document.createTreeWalker(root.content, NodeFilter.SHOW_TEXT);
  const texts: Text[] = [];
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    if (n.nodeValue?.includes("@") && !n.parentElement?.closest("code, pre, a")) texts.push(n as Text);
  }
  let changed = false;
  for (const t of texts) {
    const parts = splitMentions(t.nodeValue ?? "", (name) => known.has(name.toLowerCase()));
    if (!parts.some((p) => p.user)) continue;
    changed = true;
    const frag = document.createDocumentFragment();
    for (const p of parts) {
      if (!p.user) {
        frag.append(p.text);
        continue;
      }
      const u = known.get(p.user.toLowerCase())!;
      // No inline style: this HTML goes through innerHTML, where the CSP blocks style attributes.
      const span = document.createElement("span");
      span.className = "mention" + (u.kind === "agent" ? " mention-agent" : "");
      span.title = u.display_name || u.username;
      span.textContent = "@" + u.username;
      frag.append(span);
    }
    t.replaceWith(frag);
  }
  return changed ? root.innerHTML : html;
}

export function renderMarkdown(src: string): string {
  return highlightMentions(DOMPurify.sanitize(md.render(src), allowed) as string);
}

export function Markdown({ source, className }: { source: string; className?: string }) {
  return <div className={"md " + (className ?? "")} dangerouslySetInnerHTML={{ __html: renderMarkdown(source) }} />;
}
