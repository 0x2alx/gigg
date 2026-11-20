// Thin client for gigg's REST API. The browser authenticates with the session cookie; every write
// is same-origin JSON, which is what the server's CSRF rules expect.
import { request } from "./transport";

export type Category = "backlog" | "todo" | "doing" | "waiting" | "done" | "cancelled";

export interface User {
  id: number;
  username: string;
  kind: "human" | "agent";
  display_name: string;
  description: string;
  has_avatar: boolean;
  is_admin: boolean;
  color: string | null;
  preferences?: Record<string, unknown>;
  version: number;
}

export interface Membership {
  workspace_id: number;
  user_id: number;
  role: string;
}

export interface Session {
  id: number;
  created_at: number;
  last_seen_at: number;
  ip: string;
  user_agent: string;
}

export interface WorkspaceAccess {
  id: number;
  slug: string;
  name: string;
  color?: string | null; // #RRGGBB; null = the default colour from the slug (colorFor)
  owner_id?: number | null; // the user it belongs to (display only)
  role: string; // the closest preset to what you may do in the whole workspace ("" = only some projects; display only)
  permissions?: string[]; // what you may do in the whole workspace (see GET /permissions)
  whole?: boolean; // you see the whole workspace (false: only `projects`, none of its workspace-level things)
  projects?: ProjectAccess[]; // every project you see here, with what you may do in it
}

// ProjectAccess: what you may do in one project (the gig permissions: view, comment, work_gigs, ...).
export interface ProjectAccess {
  id: number;
  role: string;
  permissions: string[];
}

export interface Me {
  user: User;
  admin: boolean;
  workspaces: WorkspaceAccess[];
  capabilities?: string[]; // permissions held in at least one workspace
  all_workspaces?: string[]; // permissions held on the all-workspaces level (what the shared library asks)
  remote_fonts?: boolean; // the server allows Google Fonts (GIGG_REMOTE_FONTS)
  version?: string;
  source_url?: string; // this server's source code (AGPL-3.0 §13; GIGG_SOURCE_URL)
}

// hasCap: whether this account may use a feature: in project `project` of workspace ws when both are
// given (a project can have rules of its own), in workspace ws when given (the whole workspace), else
// anywhere. An older server without the lists allows all.
export function hasCap(me: Pick<Me, "capabilities" | "workspaces" | "admin"> | null | undefined, cap: string, ws?: number, project?: number): boolean {
  if (!me) return true;
  if (me.admin) return true;
  if (ws !== undefined) {
    const w = me.workspaces?.find((x) => x.id === ws);
    const p = project !== undefined ? w?.projects?.find((x) => x.id === project) : undefined;
    if (p && PROJECT_PERMISSIONS.has(cap)) return p.permissions.includes(cap);
    if (w?.permissions) return w.permissions.includes(cap);
  }
  return !me.capabilities || me.capabilities.includes(cap);
}

// hasAllCap: a permission on the all-workspaces level, which is what the shared library (shared
// templates, schedules, errand templates) asks for. An older server without the list: admins only.
export function hasAllCap(me: Pick<Me, "admin" | "all_workspaces"> | null | undefined, cap: string): boolean {
  if (!me) return false;
  return me.admin || !!me.all_workspaces?.includes(cap);
}

// canShare: may make, change and delete shared templates, schedules and errand templates.
export const canShare = (me: Pick<Me, "admin" | "all_workspaces"> | null | undefined) => hasAllCap(me, "shared_library");

// The permissions a project's rules can decide; the rest are the whole workspace's.
export const PROJECT_PERMISSIONS = new Set(["view", "comment", "work_gigs", "edit_gigs", "create_gigs", "delete_gigs", "archive_gigs", "errands"]);

// canWriteIn: whether you may edit gigs (Edit gigs: member and up), which creating gigs, templates
// and recurring gigs need besides their own permission: in one project when given (its own rules
// count), else in the whole workspace. One place for every screen.
export const canWriteIn = (ws: { role?: string; whole?: boolean; access?: ProjectAccess[] }, project?: number) => {
  const p = project !== undefined ? ws.access?.find((x) => x.id === project) : undefined;
  if (p) return p.permissions.includes("edit_gigs");
  if (ws.whole === false) return false;
  return ws.role !== "viewer" && ws.role !== "consumer";
};

// The places you can make things in, for every screen that offers a "New" button (one rule each).
type Place = { id: number; role?: string; whole?: boolean; access?: ProjectAccess[]; projects: { id: number }[] };
type Caps = Pick<Me, "capabilities" | "workspaces" | "admin" | "user">;

// canCreateGigIn: new gigs need Create gigs and Edit gigs there.
export const canCreateGigIn = (me: Caps, w: Place, project: number) => canWriteIn(w, project) && hasCap(me, "create_gigs", w.id, project);

// gigChoices: every workspace you can add a gig to, with only the projects you can add to.
export function gigChoices<W extends Place>(me: Caps, tree: W[]): W[] {
  return tree.map((w) => ({ ...w, projects: w.projects.filter((p) => canCreateGigIn(me, w, p.id)) })).filter((w) => w.projects.length > 0);
}

// errandWritable: workspaces where you may change errands (Errands for the whole workspace or one of its
// projects); errandMakers: those where you may also make one. An errand needs a worker: an agent makes
// errands for itself; giving one to an agent takes Manage the workspace.
export function errandWritable<W extends Place>(me: Caps, tree: W[]): W[] {
  return tree.filter((w) => hasCap(me, "errands", w.id) || w.projects.some((p) => hasCap(me, "errands", w.id, p.id)));
}
export function errandMakers<W extends Place>(me: Caps, tree: W[]): W[] {
  return errandWritable(me, tree).filter((w) => me.user.kind === "agent" || hasCap(me, "manage_workspace", w.id));
}

export interface Project {
  id: number;
  workspace_id: number;
  slug: string;
  name: string;
  key: string;
  color: string | null;
  archived: boolean;
}

export interface Status {
  id: number;
  project_id: number;
  name: string;
  category: Category;
  color: string | null;
  position: number;
}

export interface Tag {
  id: number;
  name: string;
  color: string | null;
}

export interface Gig {
  model?: string; // claude -p --model for the agent ("" = its default)
  effort?: string; // claude -p --effort ("" = its default)
  always_run?: boolean; // worked whatever the usage budget
  run_timeout_minutes?: number | null; // null: the agent's default
  auto_archive_minutes?: number | null; // archived this long after done or cancelled; null: never
  allowed_tools?: string[]; // the runner runs it with exactly these tools (claude -p --allowedTools)
  id: number;
  key: string;
  workspace_id: number;
  project_id: number;
  title: string;
  description_md: string;
  status_id: number;
  status_name: string;
  status_category: Category;
  priority: number;
  created_by: number;
  assignees: number[];
  followers: number[]; // they get its bells (as their bell settings allow), like the assignees
  color: string | null;
  start_at: number | null;
  due_at: number | null;
  created_by_username: string;
  created_by_admin: boolean;
  created_at: number;
  updated_at: number;
  session_id: string | null;
  last_run_id: number | null;
  resume_requested_at: number | null;
  cost_usd_total: number;
  comment_count: number;
  deleted: boolean;
  labels: Tag[];
  aliases: string[];
  version: number;
  recurrence_id?: number | null; // made by a schedule
  occurrence_at?: number | null;
  not_after?: number | null; // the window closes: agents can't claim it after this
  missed_at?: number | null; // the window closed while nobody had touched it
  archived_at?: number | null;
  done_at?: number | null; // when it entered a done status (null when it isn't done)
  schedule?: { name: string; text: string }; // made by a recurring gig: its schedule, in words
  parent_id?: number | null;
  subgig_count?: number;
  subgig_done?: number;
  checklist_total?: number;
  checklist_done?: number;
  blocked_by?: number; // open gigs blocking this one (dependencies and subgigs that block it)
  blocked_by_subgigs?: number; // how many of blocked_by are its own subgigs
  blocks_parent?: boolean; // a subgig its parent waits for
  custom?: Record<string, unknown> | null; // custom field values by field id
  last_run?: RunBrief; // the latest run, in brief
  question?: QuestionBrief; // waiting gigs on the board and in lists: the latest question
}

export interface RunBrief {
  id: number;
  status: "running" | "done" | "needs_human" | "error" | "timeout" | "interrupted" | "parked";
  worker_user_id: number;
  started_at: number;
  heartbeat_at: number;
  ended_at: number | null;
  model: string | null;
  progress_pct: number | null;
  progress_note: string | null;
  error_reason: string | null;
}

export interface QuestionBrief {
  comment_id: number;
  author_id: number;
  created_at: number;
  kind: "question" | "report";
  text: string; // Markdown, at most 280 characters
}

export interface Relation {
  from_gig_id: number;
  from_key: string;
  to_gig_id: number;
  to_key: string;
  kind: "blocks" | "relates";
  open: boolean; // the other gig is not done or cancelled
  title: string; // the other gig's title
  created_at: number;
}

export type RelationDirection = "blocks" | "blocked_by" | "relates";

export interface ChecklistItem {
  id: number;
  position: number;
  text: string;
  done_at: number | null;
  done_by: number | null;
}

export type FieldKind = "text" | "number" | "date" | "select" | "checkbox" | "url";

export interface CustomField {
  id: number;
  workspace_id: number;
  name: string;
  kind: FieldKind;
  options: string[] | null;
  position: number;
}

// A subgig a template (or a new gig) creates; blocks_parent: the parent waits for it.
export interface TemplateSubgig {
  title: string;
  blocks_parent?: boolean;
}

export interface TemplateBody {
  model?: string;
  effort?: string;
  always_run?: boolean;
  run_timeout_minutes?: number | null;
  auto_archive_minutes?: number | null;
  allowed_tools?: string[]; // runner tool allowlist (permission rules)
  name: string;
  project_id?: number;
  title: string;
  description_md?: string;
  priority?: number;
  color?: string;
  labels?: string[];
  assignees?: number[];
  followers?: number[];
  checklist?: string[];
  subgigs?: TemplateSubgig[];
  custom?: Record<string, unknown>;
}

export interface Template {
  model?: string;
  effort?: string;
  always_run?: boolean;
  run_timeout_minutes?: number | null;
  auto_archive_minutes?: number | null;
  allowed_tools?: string[]; // runner tool allowlist (permission rules)
  id: number;
  workspace_id: number;
  project_id: number | null; // null: any project of the workspace
  name: string;
  title: string;
  description_md: string;
  priority: number;
  color: string | null;
  labels: string[] | null;
  assignees: number[] | null;
  followers?: number[] | null;
  checklist: string[] | null;
  subgigs?: TemplateSubgig[] | null;
  custom: Record<string, unknown> | null;
  version: number;
  recurring?: number; // live recurring gigs that create their gigs from it
  shared?: boolean; // every workspace's (workspace_id 0): admins change it; any project, no assignees or field values
}

export interface GigCreateBody {
  model?: string;
  effort?: string;
  always_run?: boolean;
  run_timeout_minutes?: number | null;
  auto_archive_minutes?: number | null;
  allowed_tools?: string[]; // runner tool allowlist (permission rules)
  title: string;
  description_md?: string;
  priority?: number;
  status?: number;
  category?: Category;
  labels?: string[];
  assignees?: number[];
  followers?: number[];
  color?: string;
  start_at?: number;
  due_at?: number;
  not_after?: number;
  parent_id?: number;
  blocks_parent?: boolean;
  checklist?: string[];
  subgigs?: TemplateSubgig[]; // created with it, made like it
  relations?: { gig: string; direction: RelationDirection }[]; // links it starts with
  custom?: Record<string, unknown>;
}

// A schedule that creates a gig per occurrence (the "recurrences" API).
export interface RecurrenceBody {
  template_id?: number; // make each gig from this template (content fields ignored); on update, switch to it
  schedule_id?: number; // use this schedule (timing fields ignored); on update, switch to it
  checklist?: string[]; // omit = keep the template's
  subgigs?: TemplateSubgig[]; // omit = keep the template's
  custom?: Record<string, unknown>; // omit = keep the template's
  model?: string;
  effort?: string;
  always_run?: boolean;
  run_timeout_minutes?: number | null;
  auto_archive_minutes?: number | null;
  allowed_tools?: string[]; // runner tool allowlist (permission rules)
  title?: string;
  description_md?: string;
  priority?: number;
  color?: string;
  labels?: string[];
  assignees?: number[];
  followers?: number[]; // kept in its template
  freq?: "daily" | "weekly" | "monthly";
  interval?: number;
  weekdays?: number[];
  month_day?: number;
  tz?: string;
  window_start?: string;
  window_minutes?: number;
  lead_minutes?: number;
  starts_on?: number;
  ends_on?: number;
  max_count?: number;
  overlap?: "skip" | "create";
  paused?: boolean;
}

export interface Recurrence {
  model?: string;
  effort?: string;
  always_run?: boolean;
  run_timeout_minutes?: number | null;
  auto_archive_minutes?: number | null;
  allowed_tools?: string[]; // runner tool allowlist (permission rules)
  id: number;
  workspace_id: number;
  project_id: number;
  created_by: number;
  title: string;
  description_md: string;
  priority: number;
  color: string | null;
  labels: string[] | null;
  assignees: number[] | null;
  followers?: number[] | null; // its template's
  freq: "daily" | "weekly" | "monthly";
  interval: number;
  weekdays: number[] | null;
  month_day: number | null;
  tz: string;
  window_start: string;
  window_minutes: number;
  lead_minutes: number;
  starts_on: number;
  ends_on: number | null;
  max_count: number | null;
  overlap: "skip" | "create";
  next_at: number | null; // null: finished
  created_count: number;
  skipped_count: number;
  last_gig_id: number | null;
  paused: boolean;
  created_at: number;
  version: number;
  upcoming: number[];
  // the gig content comes from this template, the timing from this schedule
  template_id: number;
  template_name: string;
  template_created_by: number;
  template_uses: number; // live recurring gigs using it, this one included
  template_shared?: boolean; // a shared template: editing the content here gives this recurring gig its own copy
  assignees_own?: boolean; // its own assignees (with a shared template)
  checklist: string[] | null;
  subgigs?: TemplateSubgig[] | null;
  custom: Record<string, unknown> | null;
  schedule_id: number;
  schedule_name: string;
  schedule_uses: number; // live recurring gigs on it, this one included
  schedule_shared?: boolean; // a shared schedule: new timing moves it to a schedule of its workspace
}

// Schedule: named timing of a workspace (rule, window, create-early); recurring gigs pair one with a
// template, errands run on one. minutes/hours timings are for errands only.
export interface Schedule {
  id: number;
  workspace_id: number;
  name: string;
  freq: "daily" | "weekly" | "monthly" | "minutes" | "hours";
  interval: number;
  weekdays: number[] | null;
  month_day: number | null;
  tz: string;
  window_start: string;
  window_minutes: number;
  lead_minutes: number;
  version: number;
  uses: number; // live recurring gigs on it
  errand_uses?: number; // live errands on it
  shared?: boolean; // every workspace's (workspace_id 0): admins change it
}

export type ShareKind = "schedule" | "template" | "errand_template";
const SHARE_PATH: Record<ShareKind, string> = { schedule: "/schedules", template: "/templates", errand_template: "/errand-templates" };
export interface ShareItem { id: number; workspace_id: number; workspace: string; name: string; recurring_gigs: number; errands: number; reason?: string }
export interface ShareUser { kind: "recurring_gig" | "errand"; id: number; workspace_id: number; workspace: string; label: string; from: number }
export interface SharePlan {
  kind: ShareKind; dry_run: boolean; source: ShareItem; shared?: ShareItem; copies: ShareItem[]; skipped: ShareItem[]; users: ShareUser[];
  moved: number; deleted: number; new_name: string;
}

// GigSchedule: a schedule recurring gigs can use (not a minutes/hours one).
export type GigSchedule = Schedule & { freq: "daily" | "weekly" | "monthly" };
export const isGigSchedule = (s: Schedule): s is GigSchedule => s.freq === "daily" || s.freq === "weekly" || s.freq === "monthly";

export interface ScheduleBody {
  name?: string;
  freq: "daily" | "weekly" | "monthly";
  interval?: number;
  weekdays?: number[];
  month_day?: number;
  tz?: string;
  window_start?: string;
  window_minutes?: number;
  lead_minutes?: number;
}

// recurrenceBody: a PUT body that keeps the recurring gig's template and schedule and its own
// settings as they are (nulls omitted), with overrides.
export function recurrenceBody(r: Recurrence, over: Partial<RecurrenceBody> = {}): RecurrenceBody {
  const b: RecurrenceBody = {
    template_id: r.template_id,
    schedule_id: r.schedule_id,
    starts_on: r.starts_on,
    overlap: r.overlap,
    paused: r.paused,
  };
  if (r.ends_on !== null) b.ends_on = r.ends_on;
  if (r.max_count !== null) b.max_count = r.max_count;
  return { ...b, ...over };
}

export interface Comment {
  id: number;
  author_id: number;
  author_username: string;
  author_kind: string;
  author_is_admin: boolean;
  kind: "comment" | "report" | "question" | "resume_request" | "system";
  body_md: string;
  created_at: number;
  edited_at: number | null;
  deleted: boolean;
}

export interface Run {
  id: number;
  worker_username: string;
  session_id: string | null;
  mode: string;
  attempt: number;
  status: string;
  started_at: number;
  ended_at: number | null;
  duration_ms: number | null;
  cost_usd: number | null;
  num_turns: number | null;
  model: string | null;
  transcript_path: string | null;
  log_path: string | null;
  progress_pct: number | null;
  progress_note: string | null;
}

export interface Board {
  project: Project;
  statuses: (Status & { gigs: Gig[] })[];
  labels: Tag[];
  truncated?: boolean; // more unarchived gigs than a board shows: the rest are only in Table and search
}

export interface InboxItem {
  gig: Gig;
  question: Comment | null;
  project: string;
  workspace: string;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

// signedOut: a 401 outside the login calls means the session ended (expired, logged out elsewhere, the
// account disabled): the app goes back to the login page (App listens for this event).
export const SIGNED_OUT = "gigg:signed-out";
export function noteStatus(status: number, path: string) {
  if (status === 401 && path !== "/auth/login" && path !== "/auth/me") window.dispatchEvent(new Event(SIGNED_OUT));
}

// call: one API call through the shared transport (session cookie, CSRF header, shared reads,
// retries; see transport.ts).
async function call<T>(method: string, path: string, body?: unknown, headers?: Record<string, string>): Promise<T> {
  const r = await request(method, path, { body, headers });
  if (r.status === 204) return undefined as T;
  if (!r.ok) {
    noteStatus(r.status, path);
    const d = (r.data ?? {}) as { code?: string; detail?: string; errors?: { message?: string; location?: string }[] };
    // a refused body: say which field (huma's detail alone is just "validation failed")
    const first = d.errors?.[0];
    const extra = first?.message ? ` (${first.location ? first.location.replace(/^body\./, "") + ": " : ""}${first.message})` : "";
    throw new ApiError(r.status, d.code ?? "error", (d.detail ?? r.statusText) + extra);
  }
  return r.data as T;
}

export const api = {
  me: () => call<Me>("GET", "/auth/me"),
  login: (username: string, password: string) => call<Me>("POST", "/auth/login", { username, password }),
  logout: () => call<void>("POST", "/auth/logout"),
  projects: (ws: string | number) => call<{ items: Project[] }>("GET", `/workspaces/${ws}/projects`),
  board: (p: number) => call<Board>("GET", `/projects/${p}/board`),
  inbox: () => call<{ items: InboxItem[] }>("GET", "/inbox"),
  gig: (id: string | number) => call<Gig>("GET", `/gigs/${id}`),
  createGig: (p: number, body: GigCreateBody) =>
    call<Gig>("POST", `/projects/${p}/gigs`, body),
  // version: refuse (412) when the gig changed since it was read (text edits, so nobody's words are overwritten)
  patchGig: (id: number, body: Record<string, unknown>, version?: number) =>
    call<Gig>("PATCH", `/gigs/${id}`, body, version ? { "If-Match": `"${version}"` } : undefined),
  moveGig: (id: number, body: { status?: number; category?: Category; project?: number; position?: string; gig?: number }) =>
    call<Gig>("POST", `/gigs/${id}/move`, body),
  cloneGig: (id: number, project?: number) => call<Gig>("POST", `/gigs/${id}/clone`, project ? { project } : {}),
  deleteGig: (id: number) => call<Gig>("DELETE", `/gigs/${id}`),
  restoreGig: (id: number) => call<Gig>("POST", `/gigs/${id}/restore`),
  resumeGig: (id: number, comment_md: string) => call<Gig>("POST", `/gigs/${id}/resume`, { comment_md }),
  comments: (id: number) => call<{ items: Comment[] }>("GET", `/gigs/${id}/comments`),
  addComment: (id: number, body_md: string) => call<Comment>("POST", `/gigs/${id}/comments`, { body_md }),
  runs: (id: number) => call<{ items: Run[] }>("GET", `/gigs/${id}/runs`),
  statuses: (p: number) => call<{ items: Status[] }>("GET", `/projects/${p}/statuses`),
  members: (ws: string | number) => call<{ items: Membership[] }>("GET", `/workspaces/${ws}/members`),
  users: () => call<{ items: User[] }>("GET", "/users"),
  labels: (ws: string | number) => call<{ items: Tag[] }>("GET", `/workspaces/${ws}/labels`),
  patchLabel: (id: number, body: { name?: string; color?: string | null }) => call<Tag>("PATCH", `/labels/${id}`, body),
  getMe: () => call<User>("GET", "/me"),
  patchMe: (body: Record<string, unknown>) => call<User>("PATCH", "/me", body),
  changePassword: (current: string, next: string) => call<void>("POST", "/auth/password", { current, new: next }),
  sessions: () => call<{ items: Session[] }>("GET", "/auth/sessions"),
  revokeSession: (id: number) => call<void>("DELETE", `/auth/sessions/${id}`),
  myThemes: () => call<ThemeState>("GET", "/me/themes"),
  putTheme: (id: string, theme: unknown) => call<{ id: string; theme: Record<string, unknown>; dropped_oldest: boolean }>("PUT", `/me/themes/${encodeURIComponent(id)}`, { theme }),
  deleteTheme: (id: string) => call<void>("DELETE", `/me/themes/${encodeURIComponent(id)}`),
  selectTheme: (selected_id: string | null, match_system: boolean) =>
    call<void>("PUT", "/me/themes-selection", selected_id ? { selected_id, match_system } : { match_system }),
  putAvatar: async (file: Blob) => {
    const r = await request("PUT", "/me/avatar", { raw: file.type ? file : new Blob([file], { type: "image/png" }) });
    if (!r.ok) {
      const d = (r.data ?? {}) as { code?: string; detail?: string };
      throw new ApiError(r.status, d.code ?? "error", d.detail ?? r.statusText);
    }
  },
  deleteAvatar: () => call<void>("DELETE", "/me/avatar"),
  recurrences: (ws: string | number, project?: number) =>
    call<{ items: Recurrence[] }>("GET", `/workspaces/${ws}/recurrences${project ? `?project=${project}` : ""}`),
  recurrence: (id: number) => call<Recurrence>("GET", `/recurrences/${id}`),
  createRecurrence: (p: number, body: RecurrenceBody) => call<Recurrence>("POST", `/projects/${p}/recurrences`, body),
  putRecurrence: (id: number, body: RecurrenceBody, version?: number) =>
    call<Recurrence>("PUT", `/recurrences/${id}`, body, version ? { "If-Match": `"${version}"` } : undefined),
  // also: its template and/or schedule too (refused while another recurring gig uses them)
  deleteRecurrence: (id: number, also: { template?: boolean; schedule?: boolean } = {}) =>
    call<void>("DELETE", `/recurrences/${id}${also.template || also.schedule ? "?" + [also.template && "template=true", also.schedule && "schedule=true"].filter(Boolean).join("&") : ""}`),
  schedules: (ws: string | number) => call<{ items: Schedule[] }>("GET", `/workspaces/${ws}/schedules`),
  createSchedule: (ws: string | number, body: ScheduleBody) => call<Schedule>("POST", `/workspaces/${ws}/schedules`, body),
  createSharedSchedule: (body: ScheduleBody) => call<Schedule>("POST", `/shared/schedules`, body),
  putSchedule: (id: number, body: ScheduleBody, version?: number) =>
    call<Schedule>("PUT", `/schedules/${id}`, body, version ? { "If-Match": `"${version}"` } : undefined),
  deleteSchedule: (id: number) => call<void>("DELETE", `/schedules/${id}`),
  // Make shared: dry_run returns the plan (nothing changes); without it, the move is done.
  makeShared: (kind: ShareKind, id: number, dry_run: boolean) => call<SharePlan>("POST", `${SHARE_PATH[kind]}/${id}/make-shared`, { dry_run }),
  wsGigs: (ws: string | number, params: Record<string, string | number>) =>
    call<{ items: Gig[]; next_cursor?: string }>("GET", `/workspaces/${ws}/gigs?${new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)]))}`),
  projectGigs: (p: number, params: Record<string, string | number>) =>
    call<{ items: Gig[]; next_cursor?: string }>("GET", `/projects/${p}/gigs?${new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)]))}`),
  relations: (id: number) => call<{ items: Relation[] }>("GET", `/gigs/${id}/relations`),
  addRelation: (id: number, gig: string, direction: RelationDirection) => call<Gig>("POST", `/gigs/${id}/relations`, { gig, direction }),
  removeRelation: (id: number, gig: number | string, direction: RelationDirection) =>
    call<Gig | undefined>("DELETE", `/gigs/${id}/relations?${new URLSearchParams({ gig: String(gig), direction })}`).then((t) => t ?? call<Gig>("GET", `/gigs/${id}`)),
  checklist: (id: number) => call<{ items: ChecklistItem[] }>("GET", `/gigs/${id}/checklist`),
  addChecklist: (id: number, items: string[]) => call<Gig>("POST", `/gigs/${id}/checklist`, { items }),
  patchChecklist: (id: number, item: number, body: { text?: string; done?: boolean; after?: number }) => call<Gig>("PATCH", `/gigs/${id}/checklist/${item}`, body),
  // a repeated delete whose first try got through comes back empty (see transport.ts): fetch the gig
  deleteChecklist: (id: number, item: number) =>
    call<Gig | undefined>("DELETE", `/gigs/${id}/checklist/${item}`).then((t) => t ?? call<Gig>("GET", `/gigs/${id}`)),
  archiveGig: (id: number) => call<Gig>("POST", `/gigs/${id}/archive`),
  followGig: (id: number, on: boolean) => call<Gig>(on ? "PUT" : "DELETE", `/gigs/${id}/follow`),
  unarchiveGig: (id: number) => call<Gig>("POST", `/gigs/${id}/unarchive`),
  archiveDone: (p: number, older_than_days: number, category?: "done" | "cancelled") =>
    call<{ archived: number }>("POST", `/projects/${p}/archive-done`, category ? { older_than_days, category } : { older_than_days }),
  fields: (ws: string | number) => call<{ items: CustomField[] }>("GET", `/workspaces/${ws}/fields`),
  setCustom: (id: number, values: Record<string, unknown>) => call<Gig>("PATCH", `/gigs/${id}/custom`, { values }),
  templates: (ws: string | number, project?: number) => call<{ items: Template[] }>("GET", `/workspaces/${ws}/templates${project ? `?project=${project}` : ""}`),
  createTemplate: (ws: string | number, body: TemplateBody) => call<Template>("POST", `/workspaces/${ws}/templates`, body),
  createSharedTemplate: (body: TemplateBody) => call<Template>("POST", `/shared/templates`, body),
  putTemplate: (id: number, body: TemplateBody, version?: number) =>
    call<Template>("PUT", `/templates/${id}`, body, version ? { "If-Match": `"${version}"` } : undefined),
  deleteTemplate: (id: number) => call<void>("DELETE", `/templates/${id}`),
  useTemplate: (id: number, project: number, title?: string) => call<Gig>("POST", `/templates/${id}/use`, title ? { project, title } : { project }),
};

export interface ThemeState {
  custom: { id: string; theme: Record<string, unknown>; updated_at: number }[];
  selected_id: string | null;
  match_system: boolean;
}

export const avatarURL = (userId: number, bust?: number) => `/api/v1/users/${userId}/avatar${bust ? `?v=${bust}` : ""}`;

export const priorityNames = ["None", "Low", "Medium", "High", "Urgent"];

export function fmtTime(ms: number | null): string {
  if (!ms) return "";
  return new Date(ms).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function fmtDate(ms: number | null): string {
  if (!ms) return "";
  return new Date(ms).toLocaleDateString(undefined, { dateStyle: "medium" });
}

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

// resumeCommand shows the command only for a well-formed session id (never anything else).
export function resumeCommand(sessionId: string | null): string | null {
  return sessionId && uuid.test(sessionId) ? `claude --resume ${sessionId}` : null;
}
