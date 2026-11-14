// Task lists for markdown-it: a list item starting with "[ ]" or "[x]" gets a read-only checkbox. The
// checkbox is an html_inline token built here (user HTML stays off); DOMPurify still filters the output.
import type MarkdownIt from "markdown-it";

type MD = ReturnType<typeof MarkdownIt>;

export const taskLists = (md: MD): void => {
  md.core.ruler.after("inline", "task-lists", (state) => {
    const toks = state.tokens;
    for (let i = 2; i < toks.length; i++) {
      const t = toks[i]!;
      if (t.type !== "inline" || toks[i - 1]!.type !== "paragraph_open" || toks[i - 2]!.type !== "list_item_open") continue;
      const first = t.children?.[0];
      const m = first && first.type === "text" ? /^\[([ xX])\]\s+/.exec(first.content) : null;
      if (!m || !first) continue;
      first.content = first.content.slice(m[0].length);
      const box = new state.Token("html_inline", "", 0);
      box.content = `<input type="checkbox" disabled${m[1] === " " ? "" : " checked"}> `;
      t.children!.unshift(box);
    }
  });
};
