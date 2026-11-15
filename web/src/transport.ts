// transport: the one place every API call goes through (api.ts, http.ts, the views and admin helpers).
//
// - Reads are shared: identical GETs in flight become one request, and an answer is reused for a
//   few seconds. Any change empties the cache: this tab's own writes when they return, and every
//   live event (another tab, another person, an agent over MCP or the API) except the echoes of
//   this tab's own writes, whose effects it has already fetched.
// - Retries: a network failure or a 502/503/504 is retried with growing gaps (0.5 s, 1 s, 2 s).
//   Reads always; PUT, PATCH and DELETE (repeating them changes nothing); POST only with an
//   Idempotency-Key, which every POST here carries, so a repeat gets the first answer back instead
//   of running twice. Routes that handle credentials are never repeated (the server doesn't keep
//   their answers); other methods on those routes are retried like any other, which is safe because
//   repeating them changes nothing. 4xx answers are final, except "the first try is still running".

export interface Reply {
  status: number;
  ok: boolean;
  statusText: string;
  data: unknown;
}

export interface RequestOptions {
  body?: unknown; // sent as JSON
  raw?: Blob; // sent as is, with its own type
  headers?: Record<string, string>;
}

const FRESH_MS = 3000;
const RETRY_DELAYS = [500, 1000, 2000];
const RETRY_STATUS = new Set([502, 503, 504]);
const IN_PROGRESS_TRIES = 15; // about 30 s of waiting for a slow first try to finish

let gen = 0; // bumped by every change; answers from an older generation are neither cached nor shared
const cache = new Map<string, { gen: number; at: number; reply: Reply }>();
const inflight = new Map<string, { gen: number; p: Promise<Reply> }>();

// this tab's own writes (their X-Request-Id), so their echoes on the event stream are recognised
const own = new Set<string>();
const OWN_MAX = 500;

function remember(id: string | null) {
  if (!id) return;
  own.add(id);
  if (own.size > OWN_MAX) own.delete(own.values().next().value!);
}

// invalidate forgets every cached answer (and keeps in-flight ones from being cached or shared).
export function invalidate(): void {
  gen++;
  cache.clear();
  inflight.clear();
}

// noteEvent: called for every live event before the app reacts to it. Another source's change
// empties the cache; an echo of this tab's own write leaves it alone.
export function noteEvent(requestId: string | undefined): void {
  if (requestId && own.has(requestId)) return;
  invalidate();
}

// cacheable: plain reads of app data. The login check and anything streaming always go out.
function cacheable(method: string, path: string): boolean {
  return method === "GET" && !path.startsWith("/auth/") && !path.includes("/events");
}

// secretBearing mirrors the server's list of routes whose answers aren't kept for replay.
export function secretBearing(path: string): boolean {
  const p = path.split("?")[0]!;
  return ["/auth/", "/keys", "/me/keys", "/users"].some((x) => p.startsWith(x)) || p.includes("/password") || p.endsWith("/rotate");
}

function retryable(method: string, path: string): boolean {
  if (method === "GET" || method === "HEAD" || method === "PUT" || method === "PATCH" || method === "DELETE") return true;
  return method === "POST" && !secretBearing(path);
}

let keySeq = 0;
function idemKey(): string {
  const rnd = typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;
  return `web-${rnd}-${++keySeq}`;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

function copy(r: Reply): Reply {
  // every caller gets its own copy: one screen sorting a list in place can't disturb another
  return { ...r, data: r.data === undefined ? undefined : structuredClone(r.data) };
}

async function once(path: string, init: RequestInit): Promise<Reply & { requestId: string | null; retryAfter: number }> {
  const r = await fetch("/api/v1" + path, init);
  const requestId = r.headers.get("X-Request-Id");
  const retryAfter = Number(r.headers.get("Retry-After")) || 0;
  if (r.status === 204) return { status: 204, ok: true, statusText: r.statusText, data: undefined, requestId, retryAfter };
  const ct = r.headers.get("Content-Type") ?? "";
  const data = ct.includes("json") ? await r.json().catch(() => ({})) : await r.text();
  return { status: r.status, ok: r.ok, statusText: r.statusText, data, requestId, retryAfter };
}

async function send(method: string, path: string, o: RequestOptions): Promise<Reply> {
  const headers: Record<string, string> = { ...o.headers, "X-Gigg-CSRF": "1" };
  const init: RequestInit = { method, credentials: "same-origin", headers };
  if (o.raw) {
    headers["Content-Type"] = o.raw.type || "application/octet-stream";
    init.body = o.raw;
  } else if (o.body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(o.body);
  }
  const write = method !== "GET" && method !== "HEAD";
  if (method === "POST" && !secretBearing(path) && !headers["Idempotency-Key"]) headers["Idempotency-Key"] = idemKey();
  const canRetry = retryable(method, path);
  // a conditional write (If-Match) whose first try got through would fail its repeat with a false
  // conflict: repeat it only when the server said it didn't run it (503), never after a lost answer
  const conditional = !!headers["If-Match"];
  for (let attempt = 0; ; attempt++) {
    let res: Awaited<ReturnType<typeof once>> | null = null;
    let failure: unknown = null;
    try {
      res = await once(path, init);
    } catch (e) {
      failure = e; // network error: the request may or may not have reached the server
    }
    if (write) {
      // a write may have changed something even when its answer got lost
      invalidate();
      remember(res?.requestId ?? null);
    }
    // the first try is still running on the server: wait for it longer than for a failure (its
    // answer is replayed once it's done), rather than report an error for a write that will succeed
    const running = res !== null && res.status === 409 && (res.data as { code?: string })?.code === "idempotency_in_progress";
    const again =
      canRetry &&
      attempt < (running ? IN_PROGRESS_TRIES : RETRY_DELAYS.length) &&
      (conditional ? res !== null && res.status === 503 : failure !== null || running || (res !== null && RETRY_STATUS.has(res.status)));
    if (!again) {
      if (failure !== null) throw failure;
      // a repeated DELETE whose first try got through finds nothing left: that's the success it wanted
      if (attempt > 0 && method === "DELETE" && res!.status === 404) return { status: 204, ok: true, statusText: "No Content", data: undefined };
      const { requestId: _r, retryAfter: _a, ...reply } = res!;
      return reply;
    }
    const wait = Math.min(5000, Math.max(RETRY_DELAYS[Math.min(attempt, RETRY_DELAYS.length - 1)]!, (res?.retryAfter ?? 0) * 1000));
    await sleep(wait);
  }
}

// request sends one API call (path after /api/v1) and resolves with the answer, whatever its status;
// it rejects only when the network failed on every try.
export function request(method: string, path: string, o: RequestOptions = {}): Promise<Reply> {
  if (!cacheable(method, path) || o.raw || o.body !== undefined) return send(method, path, o);
  const key = path;
  const now = Date.now();
  const hit = cache.get(key);
  if (hit && hit.gen === gen && now - hit.at < FRESH_MS) return Promise.resolve(copy(hit.reply));
  const live = inflight.get(key);
  if (live && live.gen === gen) return live.p.then(copy);
  const g = gen;
  const p = send(method, path, o).then(
    (reply) => {
      if (inflight.get(key)?.p === p) inflight.delete(key);
      if (reply.ok && g === gen) cache.set(key, { gen: g, at: Date.now(), reply });
      return reply;
    },
    (e: unknown) => {
      if (inflight.get(key)?.p === p) inflight.delete(key);
      throw e;
    },
  );
  inflight.set(key, { gen: g, p });
  return p.then(copy);
}
