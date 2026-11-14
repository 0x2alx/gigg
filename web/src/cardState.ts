// What a board card says about the agent side of a task: the badge at its top left and the block
// under its title (a running run, a question waiting for an answer, a run that failed).
import type { Task } from "./api";

export type CardStateKind = "running" | "queued" | "answer" | "failed";

export interface CardState {
  kind: CardStateKind;
  label: string; // the badge text
  tip: string;
}

const FAILED: Record<string, string> = { error: "Failed", timeout: "Timed out", interrupted: "Interrupted" };

type CardTask = Pick<Task, "status_category" | "resume_requested_at" | "last_run" | "question" | "archived_at">;

export function cardState(t: CardTask): CardState | null {
  if (t.archived_at) return null; // archived: out of the agents' way, nothing to act on
  const r = t.last_run;
  const closed = t.status_category === "done" || t.status_category === "cancelled";
  if (r?.status === "running") return { kind: "running", label: "Running", tip: "An agent is working on it now" };
  if (t.resume_requested_at && t.status_category === "todo")
    return { kind: "queued", label: "Resume queued", tip: "Resume requested: the agent's runner continues its previous session on its next pass" };
  if (t.status_category === "waiting" && t.question) return { kind: "answer", label: "Needs answer", tip: "The agent asked a question and waits for a person" };
  if (r && !closed && FAILED[r.status]) return { kind: "failed", label: FAILED[r.status]!, tip: "The agent's last run didn't finish" };
  return null;
}

// elapsed: a duration as a card shows it ("42s", "4m 12s", "1h 05m").
export function elapsed(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${String(s % 60).padStart(2, "0")}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${String(m % 60).padStart(2, "0")}m`;
}

// plainText: Markdown flattened to one line of plain text for a short preview.
export function plainText(md: string): string {
  return md
    .replace(/```[\s\S]*?(```|$)/g, " ")
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/`([^`]*)`/g, "$1")
    // no lookbehind here: Safari before 16.4 can't parse it, and that would break the whole bundle
    .replace(/(\*\*|~~)(.+?)\1/g, "$2")
    .replace(/(^|[^\w*])\*([^*\s](?:[^*]*[^*\s])?)\*(?!\w)/g, "$1$2")
    .replace(/(^|\W)__([^_]+?)__(?!\w)/g, "$1$2")
    .replace(/(^|\W)_([^_\s](?:[^_]*[^_\s])?)_(?!\w)/g, "$1$2")
    .replace(/^\s{0,3}(#{1,6}\s+|>\s?|[-*+]\s+(\[[ xX]\]\s+)?|\d+[.)]\s+)/gm, "")
    .replace(/\s+/g, " ")
    .trim();
}
