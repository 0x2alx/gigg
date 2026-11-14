import { describe, expect, it } from "vitest";
import { cardState, elapsed, plainText } from "./cardState";
import type { RunBrief } from "./api";

const run = (status: RunBrief["status"]): RunBrief => ({
  id: 1, status, worker_user_id: 2, started_at: 0, heartbeat_at: 0, ended_at: null, model: null, progress_pct: null, progress_note: null, error_reason: null,
});
const q = { comment_id: 1, author_id: 2, created_at: 0, text: "Siren or **chime**?" };

describe("cardState", () => {
  it("running wins over everything", () => {
    expect(cardState({ status_category: "doing", resume_requested_at: 5, last_run: run("running") })?.kind).toBe("running");
  });
  it("resume queued only in To-Do", () => {
    expect(cardState({ status_category: "todo", resume_requested_at: 5, last_run: run("error") })?.kind).toBe("queued");
    expect(cardState({ status_category: "doing", resume_requested_at: 5 })).toBeNull();
  });
  it("a waiting task with a question needs an answer", () => {
    expect(cardState({ status_category: "waiting", resume_requested_at: null, last_run: run("needs_human"), question: q })?.label).toBe("Needs answer");
    expect(cardState({ status_category: "waiting", resume_requested_at: null })).toBeNull();
  });
  it("archived tasks show no state", () => {
    expect(cardState({ status_category: "doing", resume_requested_at: null, last_run: run("error"), archived_at: 5 })).toBeNull();
  });
  it("a failed last run shows until the task is closed", () => {
    expect(cardState({ status_category: "doing", resume_requested_at: null, last_run: run("timeout") })?.label).toBe("Timed out");
    expect(cardState({ status_category: "done", resume_requested_at: null, last_run: run("error") })).toBeNull();
    expect(cardState({ status_category: "doing", resume_requested_at: null, last_run: run("done") })).toBeNull();
  });
});

describe("elapsed", () => {
  it("formats", () => {
    expect(elapsed(42_000)).toBe("42s");
    expect(elapsed(252_000)).toBe("4m 12s");
    expect(elapsed(3_900_000)).toBe("1h 05m");
    expect(elapsed(-5)).toBe("0s");
  });
});

describe("plainText", () => {
  it("flattens Markdown", () => {
    expect(plainText("Siren or **chime**?")).toBe("Siren or chime?");
    expect(plainText("## Which one\n- [x] `a`\n- see [docs](https://x.y)")).toBe("Which one a see docs");
    expect(plainText("snake_case_name stays")).toBe("snake_case_name stays");
    expect(plainText("*one* and _two_ and __three__, 2 * 3 * 4")).toBe("one and two and three, 2 * 3 * 4");
  });
});
