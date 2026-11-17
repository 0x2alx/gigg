import { describe, expect, it } from "vitest";
import { helpPage, type HelpTopic } from "./help";

const TOPICS: HelpTopic[] = ["board", "gigs", "agents", "recurring", "dashboard", "inbox", "views", "themes", "profile", "admin"];

describe("help pages", () => {
  it("every section has its guide page, titled by its first heading, without screenshots or file links", () => {
    for (const t of TOPICS) {
      const p = helpPage(t);
      expect(p.title, t).not.toBe("Help");
      expect(p.body.length, t).toBeGreaterThan(200);
      expect(p.body, t).not.toMatch(/!\[[^\]]*\]\(/);
      expect(p.body, t).not.toMatch(/\]\((?!https?:)[^)]*\.md\)/);
    }
  });
});
