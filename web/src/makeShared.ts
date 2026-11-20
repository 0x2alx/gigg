// Make shared: the plan a dry run returns, in words. Pure helpers (the dialog is MakeSharedDialog).
import type { ShareKind, SharePlan, ShareUser } from "./api";

export const KIND_NAME: Record<ShareKind, string> = { schedule: "schedule", template: "template", errand_template: "errand template" };

const plural = (n: number, one: string, many = one + "s") => `${n} ${n === 1 ? one : many}`;

// usersText: "2 recurring gigs and 1 errand", or "nobody uses them".
export function usersText(users: Pick<ShareUser, "kind">[]): string {
  const gigs = users.filter((u) => u.kind === "recurring_gig").length;
  const errands = users.length - gigs;
  const parts = [gigs > 0 && plural(gigs, "recurring gig"), errands > 0 && plural(errands, "errand")].filter(Boolean);
  return parts.length ? parts.join(" and ") : "";
}

// planSummary: one sentence over the plan: what is merged, what moves.
export function planSummary(plan: Pick<SharePlan, "kind" | "copies" | "users">): string {
  const kind = KIND_NAME[plan.kind];
  const merged = plan.copies.length === 0 ? `No identical ${kind} to merge.` : `Merges ${plural(plan.copies.length, "identical copy", "identical copies")} into it.`;
  const moved = usersText(plan.users);
  return `${merged} ${moved ? `${moved} ${plan.users.length === 1 ? "moves" : "move"} to the shared ${kind}.` : `No recurring gig or errand uses it yet.`}`;
}

// byWorkspace groups the users by workspace name (in order of first appearance).
export function byWorkspace(users: ShareUser[]): { workspace: string; users: ShareUser[] }[] {
  const out: { workspace: string; users: ShareUser[] }[] = [];
  for (const u of users) {
    const g = out.find((x) => x.workspace === u.workspace);
    if (g) g.users.push(u);
    else out.push({ workspace: u.workspace, users: [u] });
  }
  return out;
}

// doneMessage: the toast after the move.
export function doneMessage(plan: Pick<SharePlan, "kind" | "copies" | "users" | "new_name">): string {
  const moved = usersText(plan.users);
  return `Shared ${KIND_NAME[plan.kind]} “${plan.new_name}” made${plan.copies.length ? `, ${plural(plan.copies.length, "copy", "copies")} merged` : ""}${moved ? `, ${moved} moved` : ""}`;
}
