import { describe, expect, it } from "vitest";
import { mdit } from "./mdit";
import { linkOK, parseMd, roundTrips, toMd } from "./richmd";

// what people and agents write in gigs: each comes back from the visual editor rendering exactly the
// same, so opening a gig in it and saving never changes what wasn't touched
const corpus: string[] = [
  "Plain line one\nline two right under it",
  "Para one\n\nPara two with **bold**, *italic*, `code` and ~~strike~~.",
  "- one\n- two\n  - nested\n- three",
  "1. first\n2. second",
  "- [ ] todo\n- [x] done",
  "# Head\n\n## Sub\n\ntext",
  "> quoted\n> more",
  "```sh\nmake release VERSION=1.4.0\n```",
  "| Check | Result |\n| --- | --- |\n| Unit tests | 284 passed |",
  "See [the PR](https://example.com/pr/1) and https://example.com/raw",
  "![shot.png](/api/v1/attachments/12/file)\n\nafter",
  "Hi @scout, please check.",
  "---\n\nafter rule",
  "Text with snake_case_names and #hash",
  // a real gig description (GIGG-33's shape: bold lead-ins, nested lists, code)
  "Spec agreed on 11-20.\n\n1. **A `last_progress_at` column on gigs,** kept current by the write paths. It's the latest of:\n   - any comment (comment, report, question);\n   - a checklist tick or untick;\n\n   It counts whoever does it.\n2. **`GET /api/v1/workspaces/{ws}/stale?hours=N`** (default 24):\n   - each item is `[{key, title, status, last_progress_at, hours}]`, at most 20, oldest first.",
];

describe("richmd", () => {
  it.each(corpus)("round-trips %j", (md) => {
    expect(mdit.render(toMd(parseMd(md)))).toBe(mdit.render(md));
    expect(roundTrips(md)).toBe(true);
  });

  it("keeps bare URLs and snake_case as written", () => {
    expect(toMd(parseMd("go to https://example.com/x now"))).toBe("go to https://example.com/x now");
    expect(toMd(parseMd("a_b_c"))).toBe("a_b_c");
  });

  it("tidies spaces and blank lines, but never inside code", () => {
    const doc = parseMd("text\n\n| a |\n| --- |\n| 1 |");
    expect(toMd(doc)).toBe("text\n\n| a   |\n| --- |\n| 1   |");
    expect(toMd(parseMd("```\nkeep  \n\n\nthis\n```"))).toBe("```\nkeep  \n\n\nthis\n```");
    expect(toMd({ type: "doc", content: [{ type: "paragraph", content: [{ type: "text", text: "ping @scout " }] }] })).toBe("ping @scout");
  });

  it("writes a line break as a newline (the renderer's breaks)", () => {
    expect(toMd(parseMd("one\ntwo"))).toBe("one\ntwo");
  });

  it("empty is empty", () => {
    expect(roundTrips("")).toBe(true);
    expect(roundTrips("   ")).toBe(true);
  });

  it("catches text that would change", () => {
    // a loose list (blank lines between items) comes back tight: the editor opens on the source
    expect(roundTrips("- a\n\n- b")).toBe(false);
    // raw HTML renders as text either way, so it's safe to edit visually
    expect(roundTrips("a <b>bold</b> word")).toBe(true);
  });
});

describe("linkOK", () => {
  it("allows what the renderer links", () => {
    for (const u of ["https://example.com", "mailto:a@b.c", "/api/v1/attachments/3/file", "/api/v1/attachments/3/file?download=true", "pending-upload:2"]) expect(linkOK(u)).toBe(true);
  });
  it("refuses the rest", () => {
    for (const u of ["javascript:alert(1)", "ftp://x", "/settings", "//evil.example/x", "data:text/html,x", "tel:123"]) expect(linkOK(u)).toBe(false);
  });
  it("keeps attachment and pending links through a round trip", () => {
    expect(toMd(parseMd("[notes.txt](/api/v1/attachments/4/file)"))).toBe("[notes.txt](/api/v1/attachments/4/file)");
    expect(toMd(parseMd("![shot.png](pending-upload:1)"))).toBe("![shot.png](pending-upload:1)");
  });
});
