// Thin client for gigg's REST API. The browser authenticates with the session cookie; every write
// is same-origin JSON, which is what the server's CSRF rules expect.

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
  role: string; // the closest preset to what you may do here (display only)
  permissions?: string[]; // what you may do here (see GET /permissions)
}

export interface Me {
  user: User;
  admin: boolean;
  workspaces: WorkspaceAccess[];
  capabilities?: string[]; // permissions held in at least one workspace
}

// hasCap: whether this account may use a feature, in workspace ws when given (permissions are per
// workspace), else anywhere. An older server without the lists allows all.
export function hasCap(me: Pick<Me, "capabilities" | "workspaces" | "admin"> | null | undefined, cap: string, ws?: number): boolean {
  if (!me) return true;
  if (me.admin) return true;
  if (ws !== undefined) {
    const w = me.workspaces?.find((x) => x.id === ws);
    if (w?.permissions) return w.permissions.includes(cap);
  }
  return !me.capabilities || me.capabilities.includes(cap);
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

export interface Task {
  model?: string; // claude -p --model for the agent ("" = its default)
  effort?: string; // claude -p --effort ("" = its default)
  always_run?: boolean; // worked whatever the usage budget
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
  parent_id?: number | null;
  subtask_count?: number;
  subtask_done?: number;
  checklist_total?: number;
  checklist_done?: number;
  blocked_by?: number; // open tasks blocking this one
  custom?: Record<string, unknown> | null; // custom field values by field id
  last_run?: RunBrief; // the latest run, in brief
  question?: QuestionBrief; // waiting tasks on the board and in lists: the latest question
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
  from_task_id: number;
  from_key: string;
  to_task_id: number;
  to_key: string;
  kind: "blocks" | "relates";
  open: boolean; // the other task is not done or cancelled
  title: string; // the other task's title
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

export interface TemplateBody {
  model?: string;
  effort?: string;
  always_run?: boolean;
  allowed_tools?: string[]; // runner tool allowlist (permission rules)
  name: string;
  project_id?: number;
  title: string;
  description_md?: string;
  priority?: number;
  color?: string;
  labels?: string[];
  assignees?: number[];
  checklist?: string[];
  custom?: Record<string, unknown>;
}

export interface Template {
  model?: string;
  effort?: string;
  always_run?: boolean;
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
  checklist: string[] | null;
  custom: Record<string, unknown> | null;
  version: number;
  recurring?: number; // live recurring tasks that create their tasks from it
}

export interface TaskCreateBody {
  model?: string;
  effort?: string;
  always_run?: boolean;
  allowed_tools?: string[]; // runner tool allowlist (permission rules)
  title: string;
  description_md?: string;
  priority?: number;
  status?: number;
  category?: Category;
  labels?: string[];
  assignees?: number[];
  color?: string;
  start_at?: number;
  due_at?: number;
  parent_id?: number;
  checklist?: string[];
  custom?: Record<string, unknown>;
}

// A schedule that creates a task per occurrence (the "recurrences" API).
export interface RecurrenceBody {
  template_id?: number; // make each task from this template (content fields ignored); on update, switch to it
  schedule_id?: number; // use this schedule (timing fields ignored); on update, switch to it
  checklist?: string[]; // omit = keep the template's
  custom?: Record<string, unknown>; // omit = keep the template's
  model?: string;
  effort?: string;
  always_run?: boolean;
  allowed_tools?: string[]; // runner tool allowlist (permission rules)
  title?: string;
  description_md?: string;
  priority?: number;
  color?: string;
  labels?: string[];
  assignees?: number[];
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
  last_task_id: number | null;
  paused: boolean;
  created_at: number;
  version: number;
  upcoming: number[];
  // the task content comes from this template, the timing from this schedule
  template_id: number;
  template_name: string;
  template_created_by: number;
  template_uses: number; // live recurring tasks using it, this one included
  checklist: string[] | null;
  custom: Record<string, unknown> | null;
  schedule_id: number;
  schedule_name: string;
  schedule_uses: number; // live recurring tasks on it, this one included
}

// Schedule: named timing of a workspace (rule, window, create-early); recurring tasks pair one with a template.
export interface Schedule {
  id: number;
  workspace_id: number;
  name: string;
  freq: "daily" | "weekly" | "monthly";
  interval: number;
  weekdays: number[] | null;
  month_day: number | null;
  tz: string;
  window_start: string;
  window_minutes: number;
  lead_minutes: number;
  version: number;
  uses: number; // live recurring tasks on it
}

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

// recurrenceBody: a PUT body that keeps the recurring task's template and schedule and its own
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
  statuses: (Status & { tasks: Task[] })[];
  labels: Tag[];
}

export interface InboxItem {
  task: Task;
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

async function call<T>(method: string, path: string, body?: unknown, headers?: Record<string, string>): Promise<T> {
  // X-Gigg-CSRF on every call: body-less writes (logout, restore) carry no JSON content type, and a
  // cross-site page can't set a custom header without a CORS preflight, which gigg never grants.
  const init: RequestInit = { method, credentials: "same-origin", headers: { ...headers, "X-Gigg-CSRF": "1" } };
  if (body !== undefined) {
    (init.headers as Record<string, string>)["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  const r = await fetch("/api/v1" + path, init);
  if (r.status === 204) return undefined as T;
  const ct = r.headers.get("Content-Type") ?? "";
  const data = ct.includes("json") ? await r.json() : await r.text();
  if (!r.ok) {
    noteStatus(r.status, path);
    const d = data as { code?: string; detail?: string };
    throw new ApiError(r.status, d.code ?? "error", d.detail ?? r.statusText);
  }
  return data as T;
}

export const api = {
  me: () => call<Me>("GET", "/auth/me"),
  login: (username: string, password: string) => call<Me>("POST", "/auth/login", { username, password }),
  logout: () => call<void>("POST", "/auth/logout"),
  projects: (ws: string | number) => call<{ items: Project[] }>("GET", `/workspaces/${ws}/projects`),
  board: (p: number) => call<Board>("GET", `/projects/${p}/board`),
  inbox: () => call<{ items: InboxItem[] }>("GET", "/inbox"),
  task: (id: string | number) => call<Task>("GET", `/tasks/${id}`),
  createTask: (p: number, body: TaskCreateBody) =>
    call<Task>("POST", `/projects/${p}/tasks`, body),
  patchTask: (id: number, body: Record<string, unknown>) => call<Task>("PATCH", `/tasks/${id}`, body),
  moveTask: (id: number, body: { status?: number; category?: Category; project?: number; position?: string; task?: number }) =>
    call<Task>("POST", `/tasks/${id}/move`, body),
  cloneTask: (id: number, project?: number) => call<Task>("POST", `/tasks/${id}/clone`, project ? { project } : {}),
  deleteTask: (id: number) => call<Task>("DELETE", `/tasks/${id}`),
  restoreTask: (id: number) => call<Task>("POST", `/tasks/${id}/restore`),
  resumeTask: (id: number, comment_md: string) => call<Task>("POST", `/tasks/${id}/resume`, { comment_md }),
  comments: (id: number) => call<{ items: Comment[] }>("GET", `/tasks/${id}/comments`),
  addComment: (id: number, body_md: string) => call<Comment>("POST", `/tasks/${id}/comments`, { body_md }),
  runs: (id: number) => call<{ items: Run[] }>("GET", `/tasks/${id}/runs`),
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
    const r = await fetch("/api/v1/me/avatar", { method: "PUT", credentials: "same-origin", headers: { "X-Gigg-CSRF": "1", "Content-Type": file.type || "image/png" }, body: file });
    if (!r.ok) {
      const d = (await r.json().catch(() => ({}))) as { code?: string; detail?: string };
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
  deleteRecurrence: (id: number) => call<void>("DELETE", `/recurrences/${id}`),
  schedules: (ws: string | number) => call<{ items: Schedule[] }>("GET", `/workspaces/${ws}/schedules`),
  createSchedule: (ws: string | number, body: ScheduleBody) => call<Schedule>("POST", `/workspaces/${ws}/schedules`, body),
  putSchedule: (id: number, body: ScheduleBody, version?: number) =>
    call<Schedule>("PUT", `/schedules/${id}`, body, version ? { "If-Match": `"${version}"` } : undefined),
  deleteSchedule: (id: number) => call<void>("DELETE", `/schedules/${id}`),
  wsTasks: (ws: string | number, params: Record<string, string | number>) =>
    call<{ items: Task[]; next_cursor?: string }>("GET", `/workspaces/${ws}/tasks?${new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)]))}`),
  projectTasks: (p: number, params: Record<string, string | number>) =>
    call<{ items: Task[]; next_cursor?: string }>("GET", `/projects/${p}/tasks?${new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)]))}`),
  relations: (id: number) => call<{ items: Relation[] }>("GET", `/tasks/${id}/relations`),
  addRelation: (id: number, task: string, direction: RelationDirection) => call<Task>("POST", `/tasks/${id}/relations`, { task, direction }),
  removeRelation: (id: number, task: number | string, direction: RelationDirection) =>
    call<Task>("DELETE", `/tasks/${id}/relations?${new URLSearchParams({ task: String(task), direction })}`),
  checklist: (id: number) => call<{ items: ChecklistItem[] }>("GET", `/tasks/${id}/checklist`),
  addChecklist: (id: number, items: string[]) => call<Task>("POST", `/tasks/${id}/checklist`, { items }),
  patchChecklist: (id: number, item: number, body: { text?: string; done?: boolean; after?: number }) => call<Task>("PATCH", `/tasks/${id}/checklist/${item}`, body),
  deleteChecklist: (id: number, item: number) => call<Task>("DELETE", `/tasks/${id}/checklist/${item}`),
  archiveTask: (id: number) => call<Task>("POST", `/tasks/${id}/archive`),
  unarchiveTask: (id: number) => call<Task>("POST", `/tasks/${id}/unarchive`),
  archiveDone: (p: number, older_than_days: number, category?: "done" | "cancelled") =>
    call<{ archived: number }>("POST", `/projects/${p}/archive-done`, category ? { older_than_days, category } : { older_than_days }),
  fields: (ws: string | number) => call<{ items: CustomField[] }>("GET", `/workspaces/${ws}/fields`),
  setCustom: (id: number, values: Record<string, unknown>) => call<Task>("PATCH", `/tasks/${id}/custom`, { values }),
  templates: (ws: string | number, project?: number) => call<{ items: Template[] }>("GET", `/workspaces/${ws}/templates${project ? `?project=${project}` : ""}`),
  createTemplate: (ws: string | number, body: TemplateBody) => call<Template>("POST", `/workspaces/${ws}/templates`, body),
  putTemplate: (id: number, body: TemplateBody) => call<Template>("PUT", `/templates/${id}`, body),
  deleteTemplate: (id: number) => call<void>("DELETE", `/templates/${id}`),
  useTemplate: (id: number, project: number, title?: string) => call<Task>("POST", `/templates/${id}/use`, title ? { project, title } : { project }),
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
