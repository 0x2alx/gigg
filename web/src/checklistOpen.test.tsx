import { describe, expect, it } from "vitest";
import { renderToString } from "react-dom/server";
import { ChecklistDraft, ChecklistSection, checklistStartsOpen } from "./GigSections";
import type { Gig } from "./api";

// The Checklist section opens on its own when there are items, and stays folded when there are none.
// Rendered to HTML (no DOM needed): aria-expanded on the section's head button tells which.
const expanded = (html: string) => /aria-expanded="(true|false)"/.exec(html)?.[1];
const gig = (id: number, total: number) => ({ id, version: 1, checklist_total: total, checklist_done: 0 }) as unknown as Gig;
const section = (g: Gig) => renderToString(<ChecklistSection gig={g} canEdit onGig={() => undefined} />);

describe("checklist section open by default", () => {
  it("the rule: open with items, folded with none", () => {
    expect(checklistStartsOpen(0)).toBe(false);
    expect(checklistStartsOpen(undefined)).toBe(false);
    expect(checklistStartsOpen(null)).toBe(false);
    expect(checklistStartsOpen(3)).toBe(true);
  });

  it("a saved gig: folded with 0 items, open with items", () => {
    expect(expanded(section(gig(1, 0)))).toBe("false");
    expect(expanded(section(gig(2, 2)))).toBe("true");
  });

  it("switching gigs: each gig starts from its own checklist, not the previous one's", () => {
    // the panel keys the section on the gig id, so each gig mounts a fresh section; rendering them in
    // turn is what the panel does (with items, without, with again)
    expect([gig(2, 2), gig(1, 0), gig(2, 2)].map((g) => expanded(section(g)))).toEqual(["true", "false", "true"]);
  });

  it("a draft (new gig, template editor): folded when empty, open when it starts with items", () => {
    expect(expanded(renderToString(<ChecklistDraft items={[]} onChange={() => undefined} />))).toBe("false");
    expect(expanded(renderToString(<ChecklistDraft items={["Pack", "Ship"]} onChange={() => undefined} />))).toBe("true");
  });
});
