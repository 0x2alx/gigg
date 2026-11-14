import { describe, expect, it } from "vitest";
import { continueList, indentList, insertLink, toggleLines, toggleWrap, type Edit } from "./mdedit";

// sel: builds an Edit from text with "[" and "]" marking the selection ("|" for a caret).
function sel(s: string): Edit {
  if (s.includes("|")) {
    const i = s.indexOf("|");
    return { text: s.replace("|", ""), start: i, end: i };
  }
  const a = s.indexOf("[");
  const b = s.indexOf("]") - 1;
  return { text: s.replace("[", "").replace("]", ""), start: a, end: b };
}
const show = (e: Edit | null) => (e ? (e.start === e.end ? e.text.slice(0, e.start) + "|" + e.text.slice(e.start) : e.text.slice(0, e.start) + "[" + e.text.slice(e.start, e.end) + "]" + e.text.slice(e.end)) : null);

describe("toggleWrap", () => {
  it("wraps the selection and keeps it selected", () => {
    expect(show(toggleWrap(sel("say [hello] there"), "**", "bold"))).toBe("say **[hello]** there");
  });
  it("unwraps when the markers surround the selection", () => {
    expect(show(toggleWrap(sel("say **[hello]** there"), "**", "bold"))).toBe("say [hello] there");
  });
  it("unwraps a selection that includes the markers", () => {
    expect(show(toggleWrap(sel("say [_hi_] x"), "_", "italic"))).toBe("say [hi] x");
  });
  it("inserts a selected placeholder at a caret", () => {
    expect(show(toggleWrap(sel("a | b"), "`", "code"))).toBe("a `[code]` b");
  });
  it("keeps surrounding spaces outside the markers", () => {
    expect(show(toggleWrap(sel("x[ word ]y"), "**", "bold"))).toBe("x **[word]** y");
  });
  it("fences multi-line code", () => {
    expect(toggleWrap(sel("[a\nb]"), "`", "code").text).toBe("```\na\nb\n```\n");
  });
});

describe("insertLink", () => {
  it("links the selection and selects the URL", () => {
    expect(show(insertLink(sel("see [docs] now")))).toBe("see [docs]([https://]) now");
  });
  it("uses a selected URL as the target", () => {
    expect(show(insertLink(sel("[https://x.io]")))).toBe("[[link text]](https://x.io)");
  });
  it("inserts a placeholder at a caret", () => {
    expect(show(insertLink(sel("|")))).toBe("[[link text]](https://)");
  });
});

describe("toggleLines", () => {
  it("bullets every selected line", () => {
    expect(toggleLines(sel("[a\nb]"), "ul").text).toBe("- a\n- b");
  });
  it("numbers lines and switches list kinds", () => {
    expect(toggleLines(sel("[- a\n- b]"), "ol").text).toBe("1. a\n2. b");
    expect(toggleLines(sel("[1. a\n2. b]"), "task").text).toBe("- [ ] a\n- [ ] b");
  });
  it("removes the prefix when every line has it", () => {
    expect(toggleLines(sel("[> a\n> b]"), "quote").text).toBe("a\nb");
    expect(toggleLines(sel("- [ ] a|"), "task").text).toBe("a");
  });
  it("keeps the caret on its text for a single line", () => {
    expect(show(toggleLines(sel("x\nhel|lo"), "heading"))).toBe("x\n## hel|lo");
  });
  it("does not treat a task item as a plain bullet", () => {
    expect(toggleLines(sel("- [ ] a|"), "ul").text).toBe("- a");
  });
});

describe("indentList", () => {
  it("indents list lines and moves the caret", () => {
    expect(show(indentList(sel("- a\n- b|"), false))).toBe("- a\n  - b|");
  });
  it("outdents", () => {
    expect(show(indentList(sel("  1. a|"), true))).toBe("1. a|");
  });
  it("leaves non-list lines to the browser", () => {
    expect(indentList(sel("plain|"), false)).toBeNull();
  });
});

describe("continueList", () => {
  it("continues bullets, numbers and tasks", () => {
    expect(show(continueList(sel("- a|")))).toBe("- a\n- |");
    expect(show(continueList(sel("  9. a|")))).toBe("  9. a\n  10. |");
    expect(show(continueList(sel("- [x] done|")))).toBe("- [x] done\n- [ ] |");
  });
  it("ends the list on an empty item", () => {
    expect(show(continueList(sel("- a\n- |")))).toBe("- a\n|");
  });
  it("ignores plain lines", () => {
    expect(continueList(sel("text|"))).toBeNull();
  });
});

import MarkdownIt from "markdown-it";
import { taskLists } from "./mdtasks";
const renderRaw = (src: string) => new MarkdownIt({ html: false, breaks: true }).use(taskLists).render(src);
describe("task lists", () => {
  it("renders read-only checkboxes", () => {
    const html = renderRaw("- [ ] todo\n- [x] done\n- plain [ ] text");
    expect(html).toContain('<input type="checkbox" disabled> todo');
    expect(html).toContain('<input type="checkbox" disabled checked> done');
    expect(html).toContain("plain [ ] text");
  });
});
