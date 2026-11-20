// Import JSON: paste JSON or choose a file (one object or a list), see what will be made, make it. One
// dialog for every page that lists these objects. Each object goes through the page's normal create call,
// one by one, so validation and permissions are exactly the forms' (and the API's); the preview only
// catches the gaps it can see (kind, missing fields, a name that resolves to nothing).
// Also here: copyJson, the "Copy as JSON" of every item.
import { useEffect, useMemo, useRef, useState } from "react";
import { api, ApiError, canShare, canWriteIn, errandMakers, gigChoices, hasCap, type GigCreateBody, type RecurrenceBody, type Schedule, type ScheduleBody, type Template, type TemplateBody, type User } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { errandApi, type ErrandBody, type ErrandTemplateBody } from "./errands";
import { CREATION_ORDER_NOTE, creationOrder, describeRow, exportObject, finishBody, findInBatch, KIND_LABEL, MAX_BYTES, needsOrder, parseImport, toBody, type After, type ImportCtx, type Kind, type Obj, type Parsed } from "./importJson";
import { Modal } from "./ui";

// ---- Copy as JSON

// copyJson: pretty JSON of one or more items to the clipboard; resolves to the message for the page's toast.
export async function copyJson(kind: Kind, src: unknown, users: Map<number, User>): Promise<string> {
  const list = Array.isArray(src) ? src : [src];
  const objs = list.map((s) => exportObject(kind, s as Obj, { userName: (id) => users.get(id)?.username }));
  const text = JSON.stringify(objs.length === 1 ? objs[0] : objs, null, 2);
  if (!navigator.clipboard?.writeText) throw new Error("This browser doesn't allow copying here.");
  await navigator.clipboard.writeText(text);
  return objs.length === 1 ? `Copied ${KIND_LABEL[kind].toLowerCase()} as JSON` : `Copied ${objs.length} as JSON`;
}

// ---- Where an object goes

export interface Target {
  value: string; // "w:<workspace id>", "p:<project id>" or "shared"
  label: string;
  ws: number; // 0: shared
  project: number; // 0: none
}

const wsTarget = (w: WorkspaceTree, label = w.name): Target => ({ value: `w:${w.id}`, label, ws: w.id, project: 0 });
const projectTargets = (ws: WorkspaceTree[], projects: (w: WorkspaceTree) => number[]): Target[] =>
  ws.flatMap((w) =>
    w.projects.filter((p) => projects(w).includes(p.id)).map((p) => ({ value: `p:${p.id}`, label: `${w.name} · ${p.key} ${p.name}`, ws: w.id, project: p.id })),
  );
const SHARED: Target = { value: "shared", label: "Shared (every workspace)", ws: 0, project: 0 };

// importTargets: where the signed-in account may make this kind (the same rules as the "New" buttons;
// the server decides in the end).
export function importTargets(kind: Kind, tree: WorkspaceTree[], me: ReturnType<typeof useApp>["me"]): Target[] {
  switch (kind) {
    case "schedule": {
      const ws = tree.filter((w) => canWriteIn(w) && hasCap(me, "recurring_gigs", w.id));
      return [...ws.map((w) => wsTarget(w)), ...(canShare(me) ? [SHARED] : [])];
    }
    case "gig_template": {
      const ws = tree.filter((w) => canWriteIn(w) && hasCap(me, "templates", w.id));
      return [
        ...ws.flatMap((w) => [wsTarget(w, `${w.name} (any project)`), ...projectTargets([w], () => w.projects.map((p) => p.id))]),
        ...(canShare(me) ? [SHARED] : []),
      ];
    }
    case "errand_template": {
      const ws = tree.filter((w) => hasCap(me, "errands", w.id));
      return [...ws.map((w) => wsTarget(w)), ...(canShare(me) ? [SHARED] : [])];
    }
    case "gig":
      return projectTargets(gigChoices(me, tree), (w) => w.projects.map((p) => p.id));
    case "errand":
      return projectTargets(errandMakers(me, tree), (w) => w.projects.filter((p) => hasCap(me, "errands", w.id, p.id)).map((p) => p.id));
    case "recurring_gig":
      return projectTargets(
        tree.filter((w) => canWriteIn(w) && hasCap(me, "recurring_gigs", w.id)),
        (w) => w.projects.map((p) => p.id),
      );
  }
}

// importKinds: every kind the signed-in account may make somewhere (the account menu's Import JSON).
export const IMPORT_KINDS: Kind[] = ["gig", "recurring_gig", "gig_template", "schedule", "errand", "errand_template"];
export function importKinds(tree: WorkspaceTree[], me: ReturnType<typeof useApp>["me"]): Kind[] {
  return IMPORT_KINDS.filter((k) => importTargets(k, tree, me).length > 0);
}

// createOne: the page's own create call for this kind.
function createOne(kind: Kind, t: Target, body: Obj): Promise<unknown> {
  switch (kind) {
    case "schedule":
      return t.ws === 0 ? api.createSharedSchedule(body as unknown as ScheduleBody) : api.createSchedule(t.ws, body as unknown as ScheduleBody);
    case "gig_template": {
      const b = body as unknown as TemplateBody;
      return t.ws === 0 ? api.createSharedTemplate(b) : api.createTemplate(t.ws, t.project ? { ...b, project_id: t.project } : b);
    }
    case "errand_template":
      return t.ws === 0 ? errandApi.createSharedTemplate(body as unknown as ErrandTemplateBody) : errandApi.createTemplate(t.ws, body as unknown as ErrandTemplateBody);
    case "gig":
      return api.createGig(t.project, body as unknown as GigCreateBody);
    case "errand":
      return errandApi.create(t.ws, { ...(body as unknown as ErrandBody), project: t.project });
    case "recurring_gig":
      return api.createRecurrence(t.project, body as unknown as RecurrenceBody);
  }
}

type Outcome = { ok: true } | { ok: false; message: string };

export function ImportJsonModal({ kinds, initialPlace = "", onClose, onDone }: {
  kinds: Kind[]; // the kinds this page takes (one: a row without "kind" is taken for it)
  initialPlace?: string; // "", "w:<id>" or "p:<id>": where rows start
  onClose: () => void;
  onDone: (message?: string) => void; // after a create: the page reloads its list and may toast
}) {
  const { tree, me, users } = useApp();
  const [text, setText] = useState("");
  const [fileNote, setFileNote] = useState("");
  const [picked, setPicked] = useState<Record<number, string>>({});
  const [results, setResults] = useState<Record<number, Outcome> | null>(null);
  const [busy, setBusy] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const parsed = useMemo(() => (text.trim() ? parseImport(text, kinds, kinds.length === 1 ? kinds[0] : undefined) : null), [text, kinds]);
  const rows = parsed?.rows ?? [];
  const targetsOf = useMemo(() => {
    const m = new Map<Kind, Target[]>();
    for (const k of kinds) m.set(k, importTargets(k, tree, me));
    return m;
  }, [kinds, tree, me]);
  // a row starts where the page is looking (its place), else in the first place it may make that kind
  const wsInView = initialPlace.startsWith("p:") ? tree.find((w) => w.projects.some((p) => `p:${p.id}` === initialPlace))?.id : initialPlace.startsWith("w:") ? Number(initialPlace.slice(2)) : undefined;
  const targetOf = (r: Parsed): Target | undefined => {
    const list = r.kind ? (targetsOf.get(r.kind) ?? []) : [];
    return list.find((t) => t.value === picked[r.index]) ?? list.find((t) => t.value === initialPlace) ?? list.find((t) => t.ws !== 0 && t.ws === wsInView) ?? list[0];
  };

  // templates and schedules in reach of the target workspaces, for recurring gigs' references
  const [lib, setLib] = useState<Record<number, { templates: Template[]; schedules: Schedule[] }>>({});
  const needWs = useMemo(() => {
    const ids = new Set<number>();
    for (const r of rows) if (r.kind === "recurring_gig") ids.add(targetOf(r)?.ws ?? 0);
    ids.delete(0);
    return [...ids].filter((id) => !lib[id]);
  }, [rows, picked, lib]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    for (const id of needWs)
      Promise.all([api.templates(id), api.schedules(id)])
        .then(([t, s]) => setLib((l) => ({ ...l, [id]: { templates: t.items, schedules: s.items } })))
        .catch(() => setLib((l) => ({ ...l, [id]: { templates: [], schedules: [] } })));
  }, [needWs]);

  const ctxFor = (ws: number): ImportCtx => ({
    userId: (v) => {
      if (typeof v === "number") return users.has(v) ? v : undefined;
      const n = v.trim().toLowerCase();
      for (const u of users.values()) if (u.username.toLowerCase() === n) return u.id;
      return undefined;
    },
    // a name first meets the templates and schedules of this same list (created first), then those in reach
    ref: (what, name, id) => {
      const items = (what === "template" ? lib[ws]?.templates : lib[ws]?.schedules) ?? [];
      if (name !== undefined) return findInBatch(entries, ws, what, name) ?? items.find((x) => x.name.toLowerCase() === name.toLowerCase())?.id;
      return items.find((x) => x.id === id)?.id;
    },
  });
  const waiting = needWs.length > 0;
  const entries = rows.map((r) => ({ row: r, ws: targetOf(r)?.ws ?? -1 }));
  // each row's create body and its problems (its own, plus what doesn't resolve)
  const plan = rows.map((r) => {
    const t = targetOf(r);
    const none: After[] = [];
    if (r.problems.length > 0 || !r.kind) return { r, t, body: {} as Obj, problems: r.problems, after: none };
    if (!t) return { r, t, body: {} as Obj, problems: ["You can't make this here."], after: none };
    const x = toBody(r, ctxFor(t.ws));
    return { r, t, body: x.body, problems: waiting && r.kind === "recurring_gig" ? [] : x.problems, after: x.after };
  });
  const good = plan.filter((p) => p.problems.length === 0);

  const readFile = (f: File | undefined) => {
    if (!f) return;
    if (f.size > MAX_BYTES) return setFileNote(`${f.name} is over 1 MB.`);
    void f.text().then((t) => {
      setText(t);
      setFileNote(`Loaded ${f.name}`);
      setResults(null);
      setPicked({});
    });
  };

  const run = async () => {
    setBusy(true);
    const out: Record<number, Outcome> = {};
    const created = new Map<number, number>(); // row -> the id it got, for the rows that name it
    for (const p of creationOrder(good.map((g) => ({ ...g, kind: g.r.kind })))) {
      const f = finishBody(p.body, p.after, created);
      if ("problem" in f) {
        out[p.r.index] = { ok: false, message: f.problem };
        setResults({ ...out });
        continue;
      }
      try {
        const made = (await createOne(p.r.kind!, p.t!, f.body)) as { id?: number };
        if (typeof made?.id === "number") created.set(p.r.index, made.id);
        out[p.r.index] = { ok: true };
      } catch (e) {
        out[p.r.index] = { ok: false, message: e instanceof ApiError ? e.message : "Couldn't create it." };
      }
      setResults({ ...out });
    }
    setBusy(false);
    const made = Object.values(out).filter((o) => o.ok).length;
    onDone(made > 0 ? `Imported ${made} object${made === 1 ? "" : "s"}` : undefined);
  };

  const done = results !== null && !busy;
  const made = results ? Object.values(results).filter((o) => o.ok).length : 0;
  const title = kinds.length === 1 ? `Import ${KIND_LABEL[kinds[0]!].toLowerCase()}s from JSON` : "Import from JSON";
  return (
    <Modal title={title} onClose={() => (busy ? undefined : onClose())} wide>
      <div className="import">
        <p className="muted small">
          Paste JSON or choose a file: one object or a list, up to 100 and 1 MB. Each object has a <code>kind</code> ({kinds.join(", ")}). Ids, workspaces and who made it are ignored.
          Everything is checked the same way as when you make it by hand.
        </p>
        <textarea
          className="import-text"
          aria-label="JSON"
          rows={8}
          spellCheck={false}
          placeholder={`[{"kind": "${kinds[0]}", …}]`}
          value={text}
          disabled={busy}
          onChange={(e) => {
            setText(e.target.value);
            setResults(null);
            setPicked({});
            setFileNote("");
          }}
        />
        <div className="row import-file">
          {/* a visually hidden file input inside a label styled as a secondary button: the keyboard reaches the input, the label shows its focus */}
          <label className={"button secondary import-pick" + (busy ? " disabled" : "")}>
            Choose file
            <input ref={fileRef} className="sr-only" type="file" accept=".json,application/json,text/plain" aria-label="JSON file" disabled={busy} onChange={(e) => readFile(e.target.files?.[0])} />
          </label>
          <span className="muted small import-file-note">{fileNote || "No file chosen"}</span>
        </div>
        {parsed?.error && (
          <p className="danger" role="alert">
            {parsed.error}
          </p>
        )}
        {rows.length > 0 && (
          <>
            <h3 className="import-head">
              Preview <span className="muted small">{good.length} of {rows.length} will be created</span>
            </h3>
            {needsOrder(good.map((g) => g.r)) && <p className="muted small import-order">{CREATION_ORDER_NOTE}</p>}
            <ul className="import-rows" aria-label="Preview">
              {plan.map(({ r, t, problems, after }) => {
                const res = results?.[r.index];
                const targets = r.kind ? (targetsOf.get(r.kind) ?? []) : [];
                return (
                  <li key={r.index} className={"import-row" + (problems.length || (res && !res.ok) ? " bad" : "")} data-state={res ? (res.ok ? "created" : "failed") : problems.length ? "skipped" : "ready"}>
                    <div className="import-line">
                      {r.kind ? <span className="pill pill-key">{KIND_LABEL[r.kind]}</span> : <span className="pill pill-danger">?</span>}
                      <strong className="import-name">{describeRow(r)}</strong>
                      {r.kind && targets.length > 0 && problems.length === 0 && !res && (
                        <select value={t?.value ?? ""} onChange={(e) => setPicked((p) => ({ ...p, [r.index]: e.target.value }))} aria-label={`Where to make ${describeRow(r)}`} disabled={busy || targets.length < 2}>
                          {targets.map((x) => (
                            <option key={x.value} value={x.value}>
                              {x.label}
                            </option>
                          ))}
                        </select>
                      )}
                      {res?.ok && <span className="pill pill-accent">Created</span>}
                      {res && !res.ok && <span className="pill pill-danger">Failed</span>}
                      {!res && problems.length > 0 && <span className="pill pill-muted">Skipped</span>}
                    </div>
                    {problems.map((p) => (
                      <div key={p} className="import-note danger small">
                        {p}
                      </div>
                    ))}
                    {problems.length === 0 &&
                      after.map((a) => (
                        <div key={a.key} className="import-note muted small" data-pending="">
                          Uses {a.what} “{a.name}” from this list: will be created first
                        </div>
                      ))}
                    {res && !res.ok && <div className="import-note danger small">{res.message}</div>}
                    {r.dropped.length > 0 && <div className="import-note muted small">Ignored: {r.dropped.join(", ")}</div>}
                  </li>
                );
              })}
            </ul>
          </>
        )}
        <div className="row end">
          <button type="button" className="button secondary" onClick={onClose} disabled={busy}>
            {done ? "Close" : "Cancel"}
          </button>
          {!done && (
            <button type="button" className="button" onClick={() => void run()} disabled={busy || good.length === 0 || waiting}>
              {busy ? "Creating…" : good.length > 0 ? `Create ${good.length}` : "Create"}
            </button>
          )}
          {done && <span className="muted small" role="status">{made} created{results && Object.keys(results).length > made ? `, ${Object.keys(results).length - made} failed` : ""}</span>}
        </div>
      </div>
    </Modal>
  );
}
