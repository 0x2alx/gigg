// historyText: a gig's activity entries as short readable phrases (the History section).
import { fmtTime, priorityNames, type User } from "./api";
import type { ActivityItem } from "./live";

type Data = Record<string, unknown> | null | undefined;

const RUN_FIELDS = ["model", "effort", "always_run", "allowed_tools"];
const DATE_LABEL: Record<string, string> = { start_at: "start", due_at: "due date", not_after: "window close" };

function str(v: unknown): string {
  return typeof v === "string" ? v : typeof v === "number" ? String(v) : "";
}

function when(v: unknown): string {
  return typeof v === "number" ? fmtTime(v) : "";
}

function quote(s: string, max = 60): string {
  const t = s.length > max ? s.slice(0, max - 1) + "…" : s;
  return `“${t}”`;
}

function names(ids: unknown, users: Map<number, User>): string {
  if (!Array.isArray(ids) || ids.length === 0) return "";
  return ids.map((id) => users.get(Number(id))?.display_name || users.get(Number(id))?.username || "someone").join(", ");
}

// changes: the readable parts of a gig.updated entry, one per field that changed.
export function changes(d: Data, users: Map<number, User>): string[] {
  if (!d) return ["edited the gig"];
  const out: string[] = [];
  let runDone = false;
  for (const [k, v] of Object.entries(d)) {
    const ov = v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : null;
    if (k === "title") out.push(`renamed it to ${quote(str(ov?.new))}`);
    else if (k === "description_md") out.push("edited the description");
    else if (k === "priority") out.push(`set priority to ${priorityNames[Number(ov?.new)] ?? str(ov?.new)}${ov && "old" in ov ? ` (was ${priorityNames[Number(ov.old)] ?? str(ov.old)})` : ""}`);
    else if (k === "color") out.push(v ? "changed the colour" : "removed the colour");
    else if (RUN_FIELDS.includes(k)) {
      if (!runDone) out.push("changed the agent run settings");
      runDone = true;
    } else if (k in DATE_LABEL) out.push(v ? `set the ${DATE_LABEL[k]} to ${when(v)}` : `cleared the ${DATE_LABEL[k]}`);
    else if (k === "parent_id") out.push(v ? "set a parent gig" : "removed the parent gig");
    else if (k === "labels") out.push(Array.isArray(v) && v.length ? `set labels to ${v.join(", ")}` : "removed the labels");
    else if (k === "assignees") out.push(Array.isArray(v) && v.length ? `assigned ${names(v, users)}` : "removed the assignees");
    else out.push(`changed ${k.replace(/_/g, " ")}`);
  }
  return out.length ? out : ["edited the gig"];
}

const RELATION: Record<string, string> = { blocks: "blocks", relates: "relates to" };

// describe: one entry as a short phrase (without the actor).
export function describe(it: Pick<ActivityItem, "action" | "data">, users: Map<number, User>): string {
  const d = it.data as Data;
  switch (it.action) {
    case "gig.created":
      return d?.cloned_from ? "created this gig as a copy" : "created this gig";
    case "gig.from_template":
      return `filled it from the template ${quote(str(d?.name))}`;
    case "gig.updated":
      return changes(d, users).join(", ");
    case "gig.moved": {
      const parts: string[] = [];
      if (d?.new_key) parts.push(`moved it to another project (now ${str(d.new_key)})`);
      if (d?.from_status !== d?.to_status && d?.to_status) parts.push(`moved it from ${str(d?.from_status)} to ${str(d.to_status)}`);
      return parts.length ? parts.join(", ") : "reordered it";
    }
    case "gig.deleted":
      return "moved it to the trash";
    case "gig.restored":
      return "restored it from the trash";
    case "gig.archived":
      return "archived it";
    case "gig.unarchived":
      return "took it out of the archive";
    case "gig.custom": {
      const fields = d ? Object.keys(d) : [];
      if (fields.length === 0) return "changed custom fields";
      return fields.length === 1 && d![fields[0]!] === null ? `cleared ${fields[0]}` : `set ${fields.join(", ")}`;
    }
    case "gig.resume_requested":
      return "asked the agent to continue";
    case "relation.added":
      return `linked it: ${RELATION[str(d?.kind)] ?? str(d?.kind).replace(/_/g, " ")} ${str(d?.to)}`;
    case "relation.removed":
      return `removed the link to ${str(d?.to)}`;
    case "checklist.added": {
      const n = Array.isArray(d?.items) ? d.items.length : 1;
      return n === 1 ? "added a checklist item" : `added ${n} checklist items`;
    }
    case "checklist.updated":
      return "updated the checklist";
    case "checklist.removed":
      return "removed a checklist item";
    case "comment.created":
      return "commented";
    case "comment.edited":
      return "edited a comment";
    case "comment.deleted":
      return "deleted a comment";
    case "run.started":
      return "started an agent run";
    case "run.finished":
      return `finished an agent run${d?.status ? ` (${str(d.status).replace(/_/g, " ")})` : ""}`;
    case "run.stale":
      return "an agent run stopped reporting";
    default:
      return it.action.replace(/[._]/g, " ");
  }
}

