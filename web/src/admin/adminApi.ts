// Admin client: the instance-admin endpoints (workspaces, members, projects, statuses, users, keys,
// audit). Same transport rules as ../api.ts: session cookie, same-origin JSON, X-Gigg-CSRF on every
// call. PATCH bodies are JSON merge patch (null clears); If-Match carries the version we last saw.
import { request } from "../transport";
import { ApiError, noteStatus, type Category } from "../api";

export type Role = "viewer" | "consumer" | "member" | "manager";
export const ROLES: Role[] = ["viewer", "consumer", "member", "manager"];
export const CATEGORIES: Category[] = ["backlog", "todo", "doing", "waiting", "done", "cancelled"];

export interface StatusTemplate {
  name: string;
  category: Category;
  color?: string | null;
}

export interface AdminWorkspace {
  id: number;
  slug: string;
  name: string;
  color: string | null;
  owner_id: number | null; // the user it belongs to (display only; admins set it)
  description_md: string;
  status_template: StatusTemplate[];
  archived: boolean;
  created_at: number;
  version: number;
}

export interface AdminMembership {
  workspace_id: number;
  user_id: number;
  role: Role | ""; // the closest preset to what they can do in the whole workspace ("" = only some projects)
  preset?: Role | ""; // their own preset here ("" = access comes from elsewhere)
  source?: string; // where "see the workspace" comes from: user | user_all | group | group_all
  whole?: boolean; // sees the whole workspace (else only projects)
  projects?: ProjectMembership[]; // projects with a rule of their own, or (not whole) the ones they see
}

export interface ProjectMembership {
  project_id: number;
  role: ProjectRole | ""; // closest preset of what they can do in it ("" = can't see it)
  preset?: ProjectRole | ""; // their own preset for this project
  source?: string;
}

// The roles a rule for one project can give (workspace-level ones need the whole workspace).
export const PROJECT_ROLES = ["viewer", "consumer", "member"] as const;
export type ProjectRole = (typeof PROJECT_ROLES)[number];

export interface AdminProject {
  id: number;
  workspace_id: number;
  slug: string;
  name: string;
  key: string;
  color: string | null;
  description_md: string;
  position: number;
  archived: boolean;
  deleted: boolean;
  created_at: number;
  version: number;
}

export interface AdminStatus {
  id: number;
  workspace_id: number;
  project_id: number;
  name: string;
  category: Category;
  color: string | null;
  position: number;
  wip_limit: number | null;
  is_default: boolean;
  version: number;
}

export interface AdminUser {
  id: number;
  username: string;
  kind: "human" | "agent";
  display_name: string;
  description: string;
  color: string | null;
  has_avatar: boolean;
  is_admin: boolean;
  disabled: boolean;
  created_at: number;
  last_login_at?: number | null;
  version: number;
}

export interface ApiKey {
  id: number;
  name: string;
  prefix: string;
  user_id: number;
  bound: boolean; // true: only the places in reach; false: wherever its user's rules reach (an admin's: admin key)
  reach: KeyPlace[]; // a bound key's places, in order (the first one's workspace is its "current")
  scopes: string[];
  allowed_listeners: string[] | null; // null = any
  allowed_cidrs: string[] | null; // null = any
  expires_at: number;
  created_at: number;
  created_by_user: number | null;
  created_by_key: number | null;
  last_used_at: number | null;
  last_used_listener: string | null;
  last_used_ip: string | null;
  revoked: boolean;
  version: number;
}

// KeyPlace: a whole workspace, or one project of it.
export interface KeyPlace {
  workspace_id: number;
  project_id?: number;
}

export interface KeyCreate {
  name: string;
  user_id?: number;
  reach?: string[]; // "workspace" or "workspace/project" (ids or slugs); empty = follows its user
  scopes: string[];
  allowed_listeners?: string[];
  allowed_cidrs?: string[];
  expires_at?: number;
}

export interface KeyCreated {
  key: ApiKey;
  token: string;
}

export interface AuditItem {
  id: number;
  at: number;
  actor_user_id: number | null;
  key_id: number | null;
  listener: string;
  action: string;
  entity_type: string;
  entity_id: number | null;
  project_id: number | null;
  data: unknown;
}

// Scopes a key can carry, grouped for the create form (admin:* only on an admin user's unbound key).
export const SCOPE_GROUPS: { name: string; scopes: string[] }[] = [
  { name: "Read", scopes: ["workspace:read", "gigs:read", "comments:read", "errands:read", "errands:output"] },
  {
    name: "Write",
    scopes: ["workspace:write", "gigs:work", "gigs:write", "gigs:delete", "comments:write", "runs:write", "projects:write", "profile:write", "keys:self", "errands:write", "errands:run"],
  },
  {
    name: "Admin",
    scopes: ["admin:workspaces", "admin:members", "admin:users", "admin:keys", "admin:audit"],
  },
];

async function call<T>(method: string, path: string, body?: unknown, version?: number): Promise<T> {
  const r = await request(method, path, { body, headers: version ? { "If-Match": `"${version}"` } : undefined });
  if (r.status === 204) return undefined as T;
  if (!r.ok) {
    noteStatus(r.status, path);
    const d = (r.data ?? {}) as { code?: string; detail?: string; errors?: { message?: string; location?: string }[] };
    // huma's validation errors carry the useful part in errors[]
    const first = d.errors?.[0];
    const extra = first?.message ? ` (${first.location ? first.location.replace(/^body\./, "") + ": " : ""}${first.message})` : "";
    throw new ApiError(r.status, d.code ?? "error", (d.detail ?? r.statusText) + extra);
  }
  return r.data as T;
}

// upload sends a raw body (an image) with its own content type.
async function upload(method: string, path: string, body: Blob): Promise<void> {
  const r = await request(method, path, { raw: body.type ? body : new Blob([body], { type: "image/png" }) });
  if (!r.ok) {
    const d = (r.data ?? {}) as { code?: string; detail?: string };
    throw new ApiError(r.status, d.code ?? "error", d.detail ?? r.statusText);
  }
}

type Items<T> = { items: T[] };

export const adminApi = {
  // workspaces
  workspaces: () => call<Items<AdminWorkspace>>("GET", "/workspaces"),
  workspace: (ws: number) => call<AdminWorkspace>("GET", `/workspaces/${ws}`),
  createWorkspace: (body: { slug: string; name: string }) => call<AdminWorkspace>("POST", "/workspaces", body),
  patchWorkspace: (w: AdminWorkspace, body: Partial<{ name: string; color: string | null; owner_id: number | null; description_md: string; archived: boolean }>) =>
    call<AdminWorkspace>("PATCH", `/workspaces/${w.id}`, body, w.version),
  members: (ws: number) => call<Items<AdminMembership>>("GET", `/workspaces/${ws}/members`),
  setMember: (ws: number, user: number, role: Role) => call<void>("PUT", `/workspaces/${ws}/members/${user}`, { role }),
  removeMember: (ws: number, user: number) => call<void>("DELETE", `/workspaces/${ws}/members/${user}`),
  setProjectMember: (project: number, user: number, role: ProjectRole) => call<void>("PUT", `/projects/${project}/members/${user}`, { role }),
  removeProjectMember: (project: number, user: number) => call<void>("DELETE", `/projects/${project}/members/${user}`),

  // projects and statuses
  projects: (ws: number) => call<Items<AdminProject>>("GET", `/workspaces/${ws}/projects`),
  createProject: (ws: number, body: { slug: string; name: string; key: string }) => call<AdminProject>("POST", `/workspaces/${ws}/projects`, body),
  patchProject: (p: AdminProject, body: Partial<{ name: string; color: string | null; description_md: string; archived: boolean }>) =>
    call<AdminProject>("PATCH", `/projects/${p.id}`, body, p.version),
  deleteProject: (p: number) => call<void>("DELETE", `/projects/${p}`),
  statuses: (p: number) => call<Items<AdminStatus>>("GET", `/projects/${p}/statuses`),
  createStatus: (p: number, body: { name: string; category: Category }) => call<AdminStatus>("POST", `/projects/${p}/statuses`, body),
  patchStatus: (s: AdminStatus, body: Partial<{ name: string; category: Category; color: string | null; is_default: boolean }>) =>
    call<AdminStatus>("PATCH", `/statuses/${s.id}`, body, s.version),
  deleteStatus: (id: number) => call<void>("DELETE", `/statuses/${id}`),
  reorderStatuses: (p: number, ids: number[]) => call<void>("POST", `/projects/${p}/statuses/reorder`, { ids }),

  // users
  users: () => call<Items<AdminUser>>("GET", "/users"),
  createUser: (body: { username: string; kind: "human" | "agent"; display_name?: string; password?: string; is_admin?: boolean }) =>
    call<AdminUser>("POST", "/users", body),
  patchUser: (u: AdminUser, body: Partial<{ display_name: string; description: string; color: string | null; is_admin: boolean; disabled: boolean }>) =>
    call<AdminUser>("PATCH", `/users/${u.id}`, body, u.version),
  setPassword: (id: number, password: string) => call<void>("POST", `/users/${id}/password`, { password }),
  // an agent's picture (admins; the server refuses other people's): the raw image as the body
  putUserAvatar: (id: number, file: Blob) => upload("PUT", `/users/${id}/avatar`, file),
  deleteUserAvatar: (id: number) => call<void>("DELETE", `/users/${id}/avatar`),

  // keys
  keys: () => call<Items<ApiKey>>("GET", "/keys"),
  createKey: (body: KeyCreate) => call<KeyCreated>("POST", "/keys", body),
  rotateKey: (id: number) => call<KeyCreated>("POST", `/keys/${id}/rotate`),
  revokeKey: (id: number) => call<void>("DELETE", `/keys/${id}`),

  // audit
  audit: (beforeId?: number, limit = 100) =>
    call<Items<AuditItem>>("GET", `/audit?limit=${limit}${beforeId ? `&before_id=${beforeId}` : ""}`),
};

export function errMsg(e: unknown, fallback = "Something went wrong."): string {
  return e instanceof ApiError ? e.message : e instanceof Error ? e.message : fallback;
}
