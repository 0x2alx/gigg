import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { invalidate, noteEvent, request, secretBearing } from "./transport";

type Handler = (url: string, init: RequestInit) => Response | Promise<Response>;

function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } });
}

let calls: { url: string; init: RequestInit }[] = [];
function serve(h: Handler) {
  calls = [];
  vi.stubGlobal("fetch", (url: string, init: RequestInit) => {
    calls.push({ url, init });
    return Promise.resolve(h(url, init));
  });
}

beforeEach(() => {
  invalidate();
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("shared reads", () => {
  it("merges identical GETs in flight and gives each caller its own copy", async () => {
    serve(() => json({ items: [3, 1, 2] }));
    const [a, b] = await Promise.all([request("GET", "/projects/1/board"), request("GET", "/projects/1/board")]);
    expect(calls).toHaveLength(1);
    (a.data as { items: number[] }).items.sort();
    expect((b.data as { items: number[] }).items).toEqual([3, 1, 2]);
  });

  it("reuses an answer for a few seconds, then asks again", async () => {
    serve(() => json({ n: 1 }));
    await request("GET", "/gigs/9");
    await request("GET", "/gigs/9");
    expect(calls).toHaveLength(1);
    vi.advanceTimersByTime(3100);
    await request("GET", "/gigs/9");
    expect(calls).toHaveLength(2);
  });

  it("forgets everything after this tab's own write", async () => {
    serve((_u, init) => (init.method === "GET" ? json({ n: 1 }) : json({}, 200, { "X-Request-Id": "r1" })));
    await request("GET", "/gigs/9");
    await request("PATCH", "/gigs/9", { body: { title: "x" } });
    await request("GET", "/gigs/9");
    expect(calls.filter((c) => c.init.method === "GET")).toHaveLength(2);
  });

  it("keeps the cache on its own echo, drops it on anyone else's event", async () => {
    serve((_u, init) => (init.method === "GET" ? json({ n: 1 }) : json({}, 200, { "X-Request-Id": "mine" })));
    await request("POST", "/gigs/9/move", { body: {} });
    await request("GET", "/gigs/9");
    noteEvent("mine");
    await request("GET", "/gigs/9");
    expect(calls.filter((c) => c.init.method === "GET")).toHaveLength(1);
    noteEvent("someone-else");
    await request("GET", "/gigs/9");
    expect(calls.filter((c) => c.init.method === "GET")).toHaveLength(2);
    noteEvent(undefined); // an agent's change: no request id at all
    await request("GET", "/gigs/9");
    expect(calls.filter((c) => c.init.method === "GET")).toHaveLength(3);
  });

  it("never caches the login check or failed answers", async () => {
    serve((u) => (u.endsWith("/auth/me") ? json({}) : json({ detail: "no" }, 404)));
    await request("GET", "/auth/me");
    await request("GET", "/auth/me");
    await request("GET", "/gigs/404");
    await request("GET", "/gigs/404");
    expect(calls).toHaveLength(4);
  });

  it("doesn't cache an answer that started before a change", async () => {
    let resolve!: (r: Response) => void;
    serve(() => new Promise<Response>((r) => (resolve = r)));
    const p = request("GET", "/gigs/9");
    noteEvent("other");
    resolve(json({ n: 1 }));
    await p;
    serve(() => json({ n: 2 }));
    const r = await request("GET", "/gigs/9");
    expect((r.data as { n: number }).n).toBe(2);
  });
});

describe("retries", () => {
  it("retries a read after a network error and a 503", async () => {
    let n = 0;
    serve(() => {
      n++;
      if (n === 1) throw new TypeError("network");
      if (n === 2) return json({ code: "unavailable" }, 503);
      return json({ ok: true });
    });
    const p = request("GET", "/inbox");
    await vi.runAllTimersAsync();
    const r = await p;
    expect(r.status).toBe(200);
    expect(calls).toHaveLength(3);
  });

  it("gives up after three retries and reports the last answer", async () => {
    serve(() => json({ code: "unavailable" }, 503));
    const p = request("GET", "/inbox");
    await vi.runAllTimersAsync();
    expect((await p).status).toBe(503);
    expect(calls).toHaveLength(4);
  });

  it("never retries a 4xx", async () => {
    serve(() => json({ detail: "bad" }, 422));
    expect((await request("PATCH", "/gigs/1", { body: {} })).status).toBe(422);
    expect(calls).toHaveLength(1);
  });

  it("repeats a POST with the same Idempotency-Key", async () => {
    let n = 0;
    serve(() => (++n === 1 ? json({}, 502) : json({ id: 5 }, 201)));
    const p = request("POST", "/projects/1/gigs", { body: { title: "t" } });
    await vi.runAllTimersAsync();
    await p;
    const keys = calls.map((c) => (c.init.headers as Record<string, string>)["Idempotency-Key"]);
    expect(keys).toHaveLength(2);
    expect(keys[0]).toBeTruthy();
    expect(keys[1]).toBe(keys[0]);
  });

  it("waits for a slow first try instead of reporting it as an error", async () => {
    let n = 0;
    serve(() => (++n <= 6 ? json({ code: "idempotency_in_progress" }, 409) : json({ id: 5 }, 201)));
    const p = request("POST", "/projects/1/gigs", { body: { title: "t" } });
    await vi.runAllTimersAsync();
    expect((await p).status).toBe(201);
    expect(calls).toHaveLength(7);
  });

  it("never repeats a POST that carries credentials", async () => {
    serve(() => json({}, 503));
    const p = request("POST", "/auth/login", { body: { username: "a", password: "b" } });
    await vi.runAllTimersAsync();
    await p;
    expect(calls).toHaveLength(1);
    expect((calls[0]!.init.headers as Record<string, string>)["Idempotency-Key"]).toBeUndefined();
  });

  it("repeats a conditional write only when the server said it didn't run it", async () => {
    serve(() => {
      throw new TypeError("network");
    });
    await expect(request("PATCH", "/gigs/1", { body: {}, headers: { "If-Match": '"3"' } })).rejects.toThrow("network");
    expect(calls).toHaveLength(1);
    let n = 0;
    serve(() => (++n === 1 ? json({ code: "unavailable" }, 503) : json({ version: 4 })));
    const busy = request("PATCH", "/gigs/1", { body: {}, headers: { "If-Match": '"3"' } });
    await vi.runAllTimersAsync();
    expect((await busy).status).toBe(200);
    expect(calls).toHaveLength(2);
  });

  it("takes a 404 on a repeated DELETE as done", async () => {
    let n = 0;
    serve(() => {
      if (++n === 1) throw new TypeError("network");
      return json({ code: "not_found" }, 404);
    });
    const p = request("DELETE", "/gigs/3");
    await vi.runAllTimersAsync();
    expect((await p).ok).toBe(true);
  });
});

describe("secretBearing", () => {
  it("matches the server's list", () => {
    expect(secretBearing("/auth/login")).toBe(true);
    expect(secretBearing("/keys/3/rotate")).toBe(true);
    expect(secretBearing("/users/2/password")).toBe(true);
    expect(secretBearing("/me/keys")).toBe(true);
    expect(secretBearing("/projects/1/gigs")).toBe(false);
  });
});
