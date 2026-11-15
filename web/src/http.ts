// Shared JSON call for the newer endpoints (capabilities, groups, fields, notifications, saved
// filters). Same transport rules as api.ts: session cookie, same-origin JSON, X-Gigg-CSRF on every
// call, If-Match when a version is given; huma's validation details are folded into the message.
import { ApiError, noteStatus } from "./api";
import { request } from "./transport";

export async function call<T>(method: string, path: string, body?: unknown, version?: number): Promise<T> {
  const r = await request(method, path, { body, headers: version ? { "If-Match": `"${version}"` } : undefined });
  if (r.status === 204) return undefined as T;
  if (!r.ok) {
    noteStatus(r.status, path);
    const d = (r.data ?? {}) as { code?: string; detail?: string; errors?: { message?: string; location?: string }[] };
    const first = d.errors?.[0];
    const extra = first?.message ? ` (${first.location ? first.location.replace(/^body\./, "") + ": " : ""}${first.message})` : "";
    throw new ApiError(r.status, d.code ?? "error", (d.detail ?? r.statusText) + extra);
  }
  return r.data as T;
}

// compact drops undefined and null top-level fields: huma rejects explicit nulls where a field is optional.
export function compact<T extends Record<string, unknown>>(o: T): Partial<T> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(o)) if (v !== undefined && v !== null) out[k] = v;
  return out as Partial<T>;
}
