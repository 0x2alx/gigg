// The Errands page: every errand in reach (commands the agents' executors run on a schedule, sandboxed),
// failing first; filters; a side panel with the command, the run history and each run's output; the
// editor. Errands make no gigs: their runs live here.
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type FormEvent } from "react";
import { useLocation } from "wouter";
import { ApiError, fmtTime, hasCap } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { runnerApi, type RunnerState } from "./Dashboard";
import { PlaceFilter, placeMatch, type Place } from "./places";
import { WorkspaceBadge } from "./WorkspaceBadge";
import { RfRow } from "./RepeatFields";
import { WEEKDAYS, localTZ, ordinal } from "./recurrence";
import { HelpButton } from "./help";
import { Icon } from "./icons";
import { Avatar, ClearFilters, ConfirmModal, Modal } from "./ui";
import { PRIORITY_COLOR } from "./theme";
import {
  RUN_STATUS_TEXT,
  RUN_STATUS_TIP,
  bytesText,
  errandApi,
  errandBody,
  errandProblem,
  detaching,
  defaultErrandForm,
  describeErrand,
  durMS,
  formOfErrand,
  frequent,
  relTime,
  schedText,
  type Errand,
  type ErrandForm,
  type ErrandFreq,
  type ErrandRun,
  contentOf,
  sameContent,
  templateBodyOf,
  type ErrandTemplate,
} from "./errands";

type StateFilter = "" | "failing" | "paused";


// stateOf: what the list shows first about an errand.
function stateOf(c: Errand): { text: string; tip: string; color: string; cls: string } {
  if (c.paused) return { text: "paused", tip: "Paused: no runs until resumed", color: "var(--muted)", cls: "paused" };
  if (c.state === "failing")
    return { text: "failing", tip: `Its last ${c.fail_streak > 1 ? `${c.fail_streak} runs` : "run"} failed${c.failing_since ? `, since ${fmtTime(c.failing_since)}` : ""}`, color: "var(--danger)", cls: "failing" };
  if (c.state === "ok") return { text: "ok", tip: "Its last run succeeded", color: "color-mix(in srgb, var(--ok) 75%, var(--text))", cls: "ok" };
  return { text: "new", tip: "It hasn't run yet", color: PRIORITY_COLOR[2] ?? "var(--accent)", cls: "never" };
}

function RunStatus({ run }: { run: ErrandRun }) {
  const tone = run.status === "ok" ? "ok" : run.status === "running" ? "running" : run.status === "missed" || run.status === "skipped" ? "muted" : "bad";
  return (
    <span className={"errand-run-status errand-run-" + tone} data-tip={RUN_STATUS_TIP[run.status]}>
      {RUN_STATUS_TEXT[run.status]}
      {run.exit_code !== null && run.status !== "ok" && <span className="errand-exit"> · exit {run.exit_code}</span>}
    </span>
  );
}

const runAt = (r: ErrandRun) => r.started_at ?? r.scheduled_for ?? r.created_at;

export function ErrandsPage({ open }: { open?: number }) {
  const { tree, me, users } = useApp();
  const [, navigate] = useLocation();
  const [items, setItems] = useState<Errand[] | null>(null);
  const [runner, setRunner] = useState<RunnerState[]>([]);
  const [err, setErr] = useState("");
  const [place, setPlace] = useState<Place>("");
  const [state, setState] = useState<StateFilter>(() => (new URLSearchParams(location.search).get("state") === "failing" ? "failing" : ""));
  const [q, setQ] = useState("");
  const [editing, setEditing] = useState<Errand | "new" | null>(null);
  const [library, setLibrary] = useState(false); // the shared errand templates (instance admins)
  const [now, setNow] = useState(Date.now());

  const wsKey = tree.map((w) => w.id).join(",");
  const seq = useRef(0);
  const load = useCallback(() => {
    const n = ++seq.current;
    Promise.allSettled(tree.map((w) => errandApi.list(w.id))).then((res) => {
      if (n !== seq.current) return;
      const all: Errand[] = [];
      const bad: string[] = [];
      res.forEach((r, i) => (r.status === "fulfilled" ? all.push(...r.value.items) : bad.push(tree[i]!.name)));
      setItems(all);
      setNow(Date.now());
      setErr(bad.length ? `Couldn't load the errands of ${bad.join(", ")}.` : "");
    });
    runnerApi
      .list()
      .then((r) => setRunner(r.items))
      .catch(() => {});
  }, [wsKey]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    load();
    const t = setInterval(load, 30_000);
    return () => clearInterval(t);
  }, [load]);

  const wsOf = useCallback((id: number) => tree.find((w) => w.id === id), [tree]);
  const canEdit = useCallback((ws: number) => {
    const w = wsOf(ws);
    return !!w && hasCap(me, "errands", ws);
  }, [wsOf, me]);
  const writable = useMemo(() => tree.filter((w) => canEdit(w.id)), [tree, canEdit]);

  const inPlace = useMemo(() => (items ?? []).filter((c) => placeMatch(place, tree, c.workspace_id, null)), [items, place, tree]);
  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const order = (c: Errand) => (c.paused ? 2 : c.state === "failing" ? 0 : 1);
    return inPlace
      .filter((c) => (state === "failing" ? c.state === "failing" && !c.paused : state === "paused" ? c.paused : true))
      .filter((c) => !needle || c.name.toLowerCase().includes(needle) || c.command.toLowerCase().includes(needle) || c.worker_username.toLowerCase().includes(needle))
      .sort((a, b) => order(a) - order(b) || a.name.localeCompare(b.name));
  }, [inPlace, state, q]);
  const failing = inPlace.filter((c) => c.state === "failing" && !c.paused).length;
  const paused = inPlace.filter((c) => c.paused).length;
  const pausedWs = runner.filter((r) => r.settings.errands_paused && placeMatch(place, tree, r.workspace_id, null));
  const canPauseWs = (ws: number) => me.admin || hasCap(me, "pause_runs", ws);
  const resumable = pausedWs.filter((r) => canPauseWs(r.workspace_id));
  const wsPaused = new Set(runner.filter((r) => r.settings.errands_paused).map((r) => r.workspace_id));
  const tz = localTZ();

  const selected = open ? (items ?? []).find((c) => c.id === open) : undefined;
  const onSaved = (c: Errand) => {
    setItems((xs) => (xs ? (xs.some((x) => x.id === c.id) ? xs.map((x) => (x.id === c.id ? c : x)) : [...xs, c]) : [c]));
    load();
  };

  return (
    <div className="errands">
      <header className="page-head">
        <div>
          <h1>
            Errands
            <HelpButton topic="errands" label="Help: errands" />
          </h1>
          <p className="muted small">Commands the agents run on a schedule, in their sandbox. Each run is recorded here; errands make no gigs.</p>
        </div>
        <div className="row">
          {me.admin && (
            <button type="button" className="button secondary" onClick={() => setLibrary(true)} data-tip="Errand templates every workspace can start an errand from">
              <Icon name="template" /> Shared templates
            </button>
          )}
          {writable.length > 0 && (
            <button type="button" className="button" onClick={() => setEditing("new")}>
              <Icon name="plus" /> New errand
            </button>
          )}
        </div>
      </header>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      {pausedWs.length > 0 && (
        <div className="banner banner-danger" role="status">
          <Icon name="pause" />
          <span className="grow">
            <strong>Errands paused:</strong> {pausedWs.map((r) => r.name).join(", ")}
          </span>
          {resumable.length > 0 && (
            <button type="button" className="button secondary small" onClick={() => runnerApi.resume(resumable.map((r) => r.workspace_id), { gigs: false, errands: true }).then(load).catch((e: unknown) => setErr(e instanceof Error ? e.message : "That didn't work."))}>
              <Icon name="play" /> Resume errands
            </button>
          )}
        </div>
      )}
      <div className="list-bar errand-bar">
        {tree.length > 1 && <PlaceFilter value={place} onChange={setPlace} tree={tree} projects={false} />}
        <div className="admin-seg" role="radiogroup" aria-label="State">
          {(
            [
              ["", "All", inPlace.length],
              ["failing", "Failing", failing],
              ["paused", "Paused", paused],
            ] as [StateFilter, string, number][]
          ).map(([v, label, n]) => (
            <button key={v} type="button" role="radio" aria-checked={state === v} className={"admin-seg-btn" + (state === v ? " on" : "")} onClick={() => setState(v)}>
              {label}
              <span className={"admin-seg-count" + (v === "failing" && n > 0 ? " danger" : "")}>{n}</span>
            </button>
          ))}
        </div>
        <input type="search" className="errand-search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Find an errand, command or agent" aria-label="Find" />
        <ClearFilters active={place !== "" || state !== "" || q !== ""} onClear={() => (setPlace(""), setState(""), setQ(""))} />
      </div>
      {items === null && !err && <p className="muted">Loading…</p>}
      {items !== null && shown.length === 0 && (
        <div className="errand-empty card">
          {items.length === 0 ? (
            <>
              <p>
                <strong>No errands yet.</strong>
              </p>
              <p className="muted small">
                An errand runs a shell command for an agent on a schedule (every 5 minutes, every day at 03:30…), in that agent's sandbox, and keeps each run's exit code and output here. Use it for checks and
                housekeeping that need no thinking; use a recurring gig when the agent should work on something.
              </p>
            </>
          ) : (
            <p className="muted">None match. Clear the filters to see them all.</p>
          )}
        </div>
      )}
      {shown.length > 0 && (
        <div className="card errand-table-card">
          <table className="errand-table">
            <thead>
              <tr>
                <th>Errand</th>
                <th>Schedule</th>
                <th>Agent</th>
                <th>Last run</th>
                <th>Next</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((c) => {
                const st = stateOf(c);
                const ws = wsOf(c.workspace_id);
                const worker = users.get(c.worker_id);
                return (
                  <tr key={c.id} className={"errand-row errand-" + st.cls + (open === c.id ? " on" : "")} onClick={() => navigate(`/errands/${c.id}`)}>
                    <td className="errand-name-cell">
                      <span className="errand-state" style={{ "--c": st.color } as CSSProperties} data-tip={st.tip}>
                        <span className="dot" aria-hidden="true" />
                        <span className="errand-state-text">{st.text}</span>
                      </span>
                      <a
                        href={`/errands/${c.id}`}
                        className="errand-name"
                        onClick={(e) => {
                          e.preventDefault();
                          e.stopPropagation();
                          navigate(`/errands/${c.id}`);
                        }}
                      >
                        {c.name}
                      </a>
                      {c.unsandboxed && (
                        <span className="pill pill-danger small" data-tip="Runs outside the agent's sandbox">
                          no sandbox
                        </span>
                      )}
                      {ws && tree.length > 1 && <WorkspaceBadge ws={ws} />}
                    </td>
                    <td className="errand-sched" data-label="Schedule">
                      {schedText(c, tz)}
                    </td>
                    <td className="errand-worker" data-label="Agent">
                      <span className="errand-worker-name">
                        {worker ? <Avatar user={worker} size="xs" tip={c.worker_username} /> : null}
                        {c.worker_username}
                      </span>
                    </td>
                    <td className="errand-last" data-label="Last run">
                      {c.last_run ? (
                        <span className="errand-last-v">
                          <RunStatus run={c.last_run} />
                          <span className="muted small" title={fmtTime(runAt(c.last_run))}>
                            {relTime(runAt(c.last_run), now)}
                            {c.last_run.duration_ms !== null && ` · ${durMS(c.last_run.duration_ms)}`}
                          </span>
                        </span>
                      ) : (
                        <span className="muted small">never</span>
                      )}
                    </td>
                    <td className="errand-next" data-label="Next">
                      {c.paused || wsPaused.has(c.workspace_id) ? (
                        <span className="muted small">paused</span>
                      ) : c.run_now_at ? (
                        <span className="small" data-tip="Asked to run now: at its executor's next poll">
                          now
                        </span>
                      ) : c.next_at ? (
                        <span className="small" title={fmtTime(c.next_at)}>
                          {c.next_at <= now ? "due" : relTime(c.next_at, now)}
                        </span>
                      ) : (
                        <span className="muted small">none</span>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {selected && (
        <ErrandPanel
          key={selected.id}
          errand={selected}
          canEdit={canEdit(selected.workspace_id)}
          canUnsandbox={hasCap(me, "errand_unsandboxed", selected.workspace_id)}
          onClose={() => navigate("/errands")}
          onEdit={() => setEditing(selected)}
          onChanged={onSaved}
          onDeleted={() => {
            navigate("/errands");
            setItems((xs) => xs?.filter((x) => x.id !== selected.id) ?? null);
          }}
        />
      )}
      {open && items !== null && !selected && !err && (
        <aside className="panel errand-panel" aria-label="Errand">
          <button type="button" className="icon-button close" onClick={() => navigate("/errands")} aria-label="Close">
            <Icon name="x" />
          </button>
          <p className="muted">This errand doesn't exist, or you can't see it.</p>
        </aside>
      )}
      {library && <SharedTemplates ws={writable[0] ?? tree[0]!} onClose={() => setLibrary(false)} />}
      {editing && (editing !== "new" || writable.length > 0) && (
        <ErrandEditor
          errand={editing === "new" ? null : editing}
          choices={writable}
          initialWs={editing === "new" ? (writable.find((w) => placeMatch(place, tree, w.id, null)) ?? writable[0]!) : wsOf(editing.workspace_id)!}
          onClose={() => setEditing(null)}
          onSaved={(c) => {
            onSaved(c);
            setEditing(null);
            navigate(`/errands/${c.id}`);
          }}
        />
      )}
    </div>
  );
}

function ErrandPanel({ errand: c, canEdit, canUnsandbox, onClose, onEdit, onChanged, onDeleted }: {
  errand: Errand;
  canEdit: boolean;
  canUnsandbox: boolean;
  onClose: () => void;
  onEdit: () => void;
  onChanged: (c: Errand) => void;
  onDeleted: () => void;
}) {
  const { tree, users } = useApp();
  const ws = tree.find((w) => w.id === c.workspace_id);
  const [runs, setRuns] = useState<ErrandRun[] | null>(null);
  const [more, setMore] = useState(false);
  const [openRun, setOpenRun] = useState<number | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const panelRef = useRef<HTMLElement>(null);
  const st = stateOf(c);
  const setter = c.unsandboxed_set_by !== null ? users.get(c.unsandboxed_set_by) : undefined;
  // starting a no-sandbox command (run now, resume) is for people who may set one
  const startLocked = c.unsandboxed && !canUnsandbox;
  const lockTip = "Runs without the sandbox: running it now or resuming it needs the No-sandbox errands permission";
  const worker = users.get(c.worker_id);
  const lastId = c.last_run?.id;
  const lastStatus = c.last_run?.status;

  const PAGE = 30;
  useEffect(() => {
    errandApi
      .runs(c.id, 0, PAGE)
      .then((r) => {
        setRuns(r.items);
        setMore(r.items.length === PAGE);
      })
      .catch((e: unknown) => setErr(e instanceof Error ? e.message : "Couldn't load the runs."));
  }, [c.id, lastId, lastStatus]);
  useEffect(() => panelRef.current?.focus(), []);
  useEffect(() => {
    const k = (e: KeyboardEvent) => e.key === "Escape" && !document.querySelector("[role='dialog']") && onClose();
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, [onClose]);
  const older = () =>
    runs &&
    errandApi.runs(c.id, runs[runs.length - 1]!.id, PAGE).then((r) => {
      setRuns([...runs, ...r.items]);
      setMore(r.items.length === PAGE);
    });

  const act = (p: Promise<Errand | void>, after?: () => void) => {
    setBusy(true);
    setErr("");
    p.then((x) => {
      if (x) onChanged(x);
      after?.();
    })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "That didn't work."))
      .finally(() => setBusy(false));
  };

  return (
    <aside ref={panelRef} tabIndex={-1} className="panel errand-panel" aria-label={`Errand ${c.name}`} style={{ "--panel-c": st.color } as CSSProperties}>
      <div className="tp-top">
        {ws && <WorkspaceBadge ws={ws} />}
        <span className="errand-state" style={{ "--c": st.color } as CSSProperties} data-tip={st.tip}>
          <span className="dot" aria-hidden="true" />
          {st.text}
        </span>
        <span className="grow" />
        <button type="button" className="icon-button" onClick={onClose} aria-label="Close">
          <Icon name="x" />
        </button>
      </div>
      <h2 className="panel-title">{c.name}</h2>
      {canEdit && (
        <div className="row wrap errand-actions">
          <button type="button" className="button small" disabled={busy || c.run_now_at !== null || startLocked} onClick={() => act(errandApi.runNow(c.id))} data-tip={startLocked ? lockTip : "Runs once at the agent's next poll (within about a minute), whatever the schedule"}>
            <Icon name="play" /> {c.run_now_at ? "Run requested" : "Run now"}
          </button>
          <button type="button" className="button secondary small" disabled={busy || (c.paused && startLocked)} data-tip={c.paused && startLocked ? lockTip : undefined} onClick={() => act(c.paused ? errandApi.resume(c.id) : errandApi.pause(c.id))}>
            <Icon name={c.paused ? "play" : "pause"} /> {c.paused ? "Resume" : "Pause"}
          </button>
          <button type="button" className="button secondary small" disabled={busy} onClick={onEdit}>
            <Icon name="pencil" /> Edit
          </button>
          <span className="grow" />
          {confirm ? (
            <>
              <span className="small">Delete this errand and its runs?</span>
              <button type="button" className="button danger-button small" disabled={busy} onClick={() => act(errandApi.remove(c.id), onDeleted)}>
                Delete
              </button>
              <button type="button" className="button secondary small" onClick={() => setConfirm(false)}>
                Keep
              </button>
            </>
          ) : (
            <button type="button" className="button danger-button small" onClick={() => setConfirm(true)} aria-label={`Delete ${c.name}`}>
              <Icon name="trash" /> Delete
            </button>
          )}
        </div>
      )}
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      {c.unsandboxed && (
        <p className="banner banner-danger small errand-note" role="note">
          <Icon name="alert" />
          <span>
            Runs <strong>outside the agent's sandbox</strong>
            {setter ? `, turned off by ${setter.display_name || setter.username}` : ""}. Any change by someone without the No-sandbox errands permission puts it back in the sandbox.
          </span>
        </p>
      )}
      <dl className="props errand-props">
        <dt>Schedule</dt>
        <dd>
          {schedText(c, localTZ())}
          {c.schedule_name !== c.schedule_text && !c.schedule_text.startsWith(c.schedule_name) && <span className="muted small"> · schedule “{c.schedule_name}”</span>}
        </dd>
        <dt>Next run</dt>
        <dd>{c.paused ? "paused" : c.run_now_at ? "now (requested)" : c.next_at ? fmtTime(c.next_at) : "none"}</dd>
        <dt>Agent</dt>
        <dd className="errand-worker">
          {worker && <Avatar user={worker} size="xs" />} {c.worker_username}
        </dd>
        <dt>Timeout</dt>
        <dd>{durMS(c.timeout_s * 1000)}</dd>
        <dt>Output</dt>
        <dd>{c.output === "host" ? "kept on the agent's host only (gigg gets the exit code and timing)" : "the last 64 KiB of each run, kept here"}</dd>
        <dt>On failure</dt>
        <dd>{c.on_failure_gig ? "a gig for the agent at the first failure" : "shown here (and the agent's alert)"}</dd>
        {c.template_uses > 1 && (
          <>
            <dt>Shared</dt>
            <dd>its template is shared by {c.template_uses} errands</dd>
          </>
        )}
      </dl>
      <h3 className="errand-h">Command</h3>
      <pre className="errand-cmd">{c.command}</pre>
      {c.description_md && <p className="errand-desc">{c.description_md}</p>}
      <h3 className="errand-h">Runs</h3>
      {runs === null && !err && <p className="muted">Loading…</p>}
      {runs !== null && runs.length === 0 && <p className="muted small">No runs yet.</p>}
      {runs !== null && runs.length > 0 && (
        <ol className="errand-runs">
          {runs.map((r) => (
            <RunRow key={r.id} run={r} open={openRun === r.id} output={c.output} onToggle={() => setOpenRun((x) => (x === r.id ? null : r.id))} />
          ))}
        </ol>
      )}
      {more && (
        <button type="button" className="button secondary small" onClick={older}>
          Older runs
        </button>
      )}
    </aside>
  );
}

function RunRow({ run: r, open, output, onToggle }: { run: ErrandRun; open: boolean; output: "gigg" | "host"; onToggle: () => void }) {
  const [full, setFull] = useState<ErrandRun | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    if (open && !full && r.has_output) errandApi.run(r.id).then(setFull).catch((e: unknown) => setErr(e instanceof Error ? e.message : "Couldn't load the output."));
  }, [open, full, r.id, r.has_output]);
  const expandable = r.has_output || !!r.error || r.status === "failed" || r.status === "timeout";
  return (
    <li className={"errand-run" + (open ? " open" : "")}>
      <button type="button" className="errand-run-head" onClick={onToggle} aria-expanded={open} disabled={!expandable}>
        <RunStatus run={r} />
        <span className="errand-run-when" title={r.scheduled_for ? `Slot ${fmtTime(r.scheduled_for)}` : "Asked to run now"}>
          {fmtTime(runAt(r))}
          {r.scheduled_for === null && <span className="muted small"> · run now</span>}
        </span>
        <span className="muted small errand-run-meta">
          {durMS(r.duration_ms)}
          {r.output_bytes ? ` · ${bytesText(r.output_bytes)}` : ""}
          {!r.sandboxed && r.status !== "missed" && r.status !== "skipped" ? " · no sandbox" : ""}
        </span>
        {expandable && <Icon name={open ? "chevron-up" : "chevron-down"} className="errand-run-chev" />}
      </button>
      {open && (
        <div className="errand-run-body">
          {r.error && <p className="danger small">{r.error}</p>}
          {err && <p className="danger small">{err}</p>}
          {r.has_output ? (
            full ? (
              full.output_tail !== undefined ? (
                <>
                  {r.output_bytes !== null && r.output_bytes > 65536 && <p className="muted small">The last 64 KiB of {bytesText(r.output_bytes)}:</p>}
                  <pre className="errand-out">{full.output_tail || "(empty)"}</pre>
                </>
              ) : (
                <p className="muted small">Your key can't read errand output.</p>
              )
            ) : (
              !err && <p className="muted small">Loading…</p>
            )
          ) : (
            <p className="muted small">{output === "host" ? "This errand keeps its output on the agent's host." : "No output."}</p>
          )}
        </div>
      )}
    </li>
  );
}

const FREQS: [ErrandFreq, string][] = [
  ["minutes", "Minutes"],
  ["hours", "Hours"],
  ["daily", "Daily"],
  ["weekly", "Weekly"],
  ["monthly", "Monthly"],
];
const UNIT: Record<ErrandFreq, [string, string]> = {
  minutes: ["minute", "minutes"],
  hours: ["hour", "hours"],
  daily: ["day", "days"],
  weekly: ["week", "weeks"],
  monthly: ["month", "months"],
};

// SharedTemplates: the shared errand templates (instance admins): list, new, edit, delete. Each is
// edited in the errand editor (its command half).
function SharedTemplates({ ws, onClose }: { ws: WorkspaceTree; onClose: () => void }) {
  const [items, setItems] = useState<ErrandTemplate[] | null>(null);
  const [err, setErr] = useState("");
  const [editing, setEditing] = useState<ErrandTemplate | "new" | null>(null);
  const [confirm, setConfirm] = useState<number | null>(null);
  const load = useCallback(() => {
    errandApi.sharedTemplates().then(
      (r) => setItems(r.items.sort((a, b) => a.name.localeCompare(b.name))),
      (e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't load the shared errand templates."),
    );
  }, []);
  useEffect(load, [load]);
  const remove = (t: ErrandTemplate) =>
    errandApi.removeTemplate(t.id).then(
      () => {
        setConfirm(null);
        load();
      },
      (e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't delete it."),
    );
  if (editing)
    return (
      <ErrandEditor
        errand={null}
        template={editing}
        choices={[ws]}
        initialWs={ws}
        onClose={() => setEditing(null)}
        onSaved={() => undefined}
        onTemplateSaved={() => {
          setEditing(null);
          load();
        }}
      />
    );
  return (
    <Modal title="Shared errand templates" onClose={onClose} wide>
      <p className="muted small">Every workspace can start an errand from these. Changing one changes every errand that uses it as is; an errand that changes its content gets its own copy.</p>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      {items === null && !err && <p className="muted">Loading…</p>}
      {items?.length === 0 && <p className="muted">No shared errand templates yet.</p>}
      {!!items?.length && (
        <ul className="schedules" aria-label="Shared errand templates">
          {items.map((t) => (
            <li key={t.id} className="schedule">
              <div className="schedule-top">
                <span className="pill pill-accent">
                  <Icon name="template" />
                </span>
                <strong className="schedule-title">{t.name}</strong>
                <span className="pill pill-shared">Shared</span>
                {t.unsandboxed && <span className="pill pill-danger">no sandbox</span>}
              </div>
              <code className="schedule-rule errand-cmd-line">{t.command.split("\n")[0]}</code>
              <div className="chips">
                <span className={"pill " + (t.uses > 0 ? "pill-accent" : "pill-muted")}>
                  {t.uses} errand{t.uses === 1 ? "" : "s"}
                </span>
                <span className="pill pill-muted">timeout {durMS(t.timeout_s * 1000)}</span>
              </div>
              <div className="schedule-actions">
                {confirm === t.id ? (
                  <>
                    <span className="small">Delete this shared template?</span>
                    <button type="button" className="button danger-button small" onClick={() => remove(t)}>
                      Delete
                    </button>
                    <button type="button" className="button secondary small" onClick={() => setConfirm(null)}>
                      Keep
                    </button>
                  </>
                ) : (
                  <>
                    <button type="button" className="button secondary small" onClick={() => setEditing(t)} aria-label={`Edit ${t.name}`}>
                      <Icon name="pencil" /> Edit
                    </button>
                    <button
                      type="button"
                      className="button danger-button small"
                      onClick={() => setConfirm(t.id)}
                      disabled={t.uses > 0}
                      title={t.uses > 0 ? "Errands use it: delete them or change their content first" : undefined}
                      aria-label={`Delete ${t.name}`}
                    >
                      <Icon name="trash" /> Delete
                    </button>
                  </>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      <div className="row end">
        <button type="button" className="button" onClick={() => setEditing("new")}>
          + New shared template
        </button>
      </div>
    </Modal>
  );
}

// ErrandEditor: a new errand, an errand, or (template) a shared errand template: then only its command half
// (name, command, timeout, output, sandbox, notes), saved to the shared library.
function ErrandEditor({ errand, template, choices, initialWs, onClose, onSaved, onTemplateSaved }: {
  errand: Errand | null; template?: ErrandTemplate | "new"; choices: WorkspaceTree[]; initialWs: WorkspaceTree; onClose: () => void; onSaved: (c: Errand) => void; onTemplateSaved?: () => void;
}) {
  const { me, users, members } = useApp();
  const tplMode = !!template;
  const tpl = template && template !== "new" ? template : null;
  const [wsId, setWsId] = useState(initialWs.id);
  const ws = errand ? initialWs : (choices.find((w) => w.id === wsId) ?? initialWs);
  const [f, setF] = useState<ErrandForm>(() => (errand ? formOfErrand(errand) : tpl ? { ...defaultErrandForm(), ...contentOf(tpl) } : defaultErrandForm()));
  const set = (p: Partial<ErrandForm>) => setF((x) => ({ ...x, ...p }));
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  // a new errand may start from an errand template of its workspace or a shared one
  const [library, setLibrary] = useState<ErrandTemplate[]>([]);
  const [picked, setPicked] = useState<ErrandTemplate | null>(null);
  useEffect(() => {
    if (errand || tplMode) return;
    let live = true;
    errandApi.templates(ws.id).then(
      (r) => live && setLibrary(r.items.sort((a, b) => Number(!!b.shared) - Number(!!a.shared) || a.name.localeCompare(b.name))),
      () => live && setLibrary([]),
    );
    setPicked(null);
    return () => {
      live = false;
    };
  }, [ws.id, errand, tplMode]);
  const pick = (t: ErrandTemplate | null) => {
    setPicked(t);
    if (t) set(contentOf(t));
  };
  const usesPicked = !!picked && sameContent(f, picked);
  const canUnsandbox = tplMode || hasCap(me, "errand_unsandboxed", ws.id);
  // another timeout than the default, or a change (a picked command brings its own, used as is)
  const canTimeout = tplMode || hasCap(me, "run_timeout", ws.id);
  const manager = hasCap(me, "manage_workspace", ws.id);
  // the agents who can run it: yourself (an agent), or with Manage the workspace any agent member
  const agents = useMemo(() => (members.get(ws.id) ?? []).map((id) => users.get(id)).filter((u) => u && u.kind === "agent"), [members, users, ws.id]);
  const defaultWorker = me.user.kind === "agent" ? me.user.username : (users.get(ws.owner_id ?? 0)?.kind === "agent" ? users.get(ws.owner_id!)!.username : agents[0]?.username) ?? "";
  const worker = f.worker || defaultWorker;
  const problem = errandProblem(f) ?? (!worker && !tplMode ? "Pick the agent that runs it." : null);
  const fr = frequent(f.freq);
  const toggleDay = (d: number) => set({ weekdays: f.weekdays.includes(d) ? f.weekdays.filter((x) => x !== d) : [...f.weekdays, d] });
  const int = (v: string, d: number) => (Number.isFinite(parseInt(v, 10)) ? parseInt(v, 10) : d);
  const loosing = !!errand?.unsandboxed && !canUnsandbox;

  // an edit that takes the errand off a shared template or schedule asks first
  const [askDetach, setAskDetach] = useState<string[] | null>(null);
  const submit = (e: FormEvent | null, confirmed = false) => {
    e?.preventDefault();
    if (problem) return setErr(problem);
    setBusy(true);
    setErr("");
    const failed = (e: unknown) => setErr(e instanceof ApiError ? (e.status === 412 ? "Someone changed this meanwhile. Close and reopen it." : e.message) : "Couldn't save it.");
    if (tplMode) {
      const tb = templateBodyOf(f);
      (tpl ? errandApi.putTemplate(tpl.id, tb, tpl.version) : errandApi.createSharedTemplate(tb))
        .then(() => onTemplateSaved?.())
        .catch(failed)
        .finally(() => setBusy(false));
      return;
    }
    const body = errandBody({ ...f, worker }, errand, canUnsandbox, picked);
    const leaving = detaching(errand, body);
    if (leaving.length && !confirmed) {
      setBusy(false);
      setAskDetach(leaving);
      return;
    }
    setAskDetach(null);
    (errand ? errandApi.patch(errand.id, body, errand.version) : errandApi.create(ws.id, body))
      .then(onSaved)
      .catch((e: unknown) => setErr(e instanceof ApiError ? (e.status === 412 ? "Someone changed this errand meanwhile. Close and reopen it." : e.message) : "Couldn't save the errand."))
      .finally(() => setBusy(false));
  };

  return (
    <Modal title={tplMode ? (tpl ? `Edit shared errand template ${tpl.name}` : "New shared errand template") : errand ? `Edit ${errand.name}` : "New errand"} onClose={onClose} wide>
      <form className="form-grid errand-form" onSubmit={submit} aria-label={tplMode ? "Shared errand template" : errand ? "Edit errand" : "New errand"}>
        {tpl && tpl.uses > 0 && (
          <p className="span2 small muted" role="note">
            {tpl.uses} errand{tpl.uses === 1 ? " uses" : "s use"} this template as is: changing it changes {tpl.uses === 1 ? "it" : "them all"}, in every workspace.
          </p>
        )}
        {!errand && !tplMode && choices.length > 1 && (
          <label className="span2">
            Workspace
            <select value={ws.id} onChange={(e) => (setWsId(Number(e.target.value)), set({ worker: "" }))}>
              {choices.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.name}
                </option>
              ))}
            </select>
          </label>
        )}
        {errand && errand.template_uses > 1 && (
          <p className="span2 small muted" role="note">
            {errand.template_uses} errands share this command: changing its name, command, timeout or output changes them all.
          </p>
        )}
        {!errand && !tplMode && library.length > 0 && (
          <label className="span2">
            Start from
            <select value={picked?.id ?? ""} onChange={(e) => pick(library.find((t) => t.id === Number(e.target.value)) ?? null)}>
              <option value="">— blank —</option>
              {library.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                  {t.shared ? " (shared)" : ""}
                </option>
              ))}
            </select>
            {picked && (
              <span className="muted small">
                {usesPicked
                  ? `Uses “${picked.name}” as is: changes to it reach this errand too.${picked.unsandboxed && !hasCap(me, "errand_unsandboxed", ws.id) ? " It gets its own copy, in the sandbox." : ""}`
                  : `Changed from “${picked.name}”: saved as this errand's own template.`}
              </span>
            )}
          </label>
        )}
        <label className="span2">
          Name
          <input value={f.name} onChange={(e) => set({ name: e.target.value })} maxLength={80} placeholder="Disk space check" data-autofocus="" required />
        </label>
        <label className="span2">
          <span>
            Command <span className="muted small">run by bash in the agent's sandbox; exit 0 = ok</span>
          </span>
          <textarea className="mono errand-cmd-input" rows={5} value={f.command} onChange={(e) => set({ command: e.target.value })} spellCheck={false} placeholder={"df -h / | awk 'NR==2 && $5+0 > 90 {exit 1}'"} required />
        </label>
        {!tplMode && (
        <div className="repeat span2" role="group" aria-label="Schedule">
          <div className="rf">
            <RfRow label="Repeat">
              <div className="seg" role="radiogroup" aria-label="Repeat">
                {FREQS.map(([v, label]) => (
                  <button
                    key={v}
                    type="button"
                    role="radio"
                    aria-checked={f.freq === v}
                    className={f.freq === v ? "on" : ""}
                    onClick={() => set({ freq: v, interval: v === f.freq ? f.interval : v === "minutes" ? 15 : 1, weekdays: v === "weekly" && f.weekdays.length === 0 ? [1] : frequent(v) && f.freq === "weekly" ? [] : f.weekdays })}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </RfRow>
            <RfRow label="Every">
              <input type="number" className="num" min={1} max={fr ? (f.freq === "minutes" ? 360 : 24) : 365} value={f.interval} onChange={(e) => set({ interval: int(e.target.value, 1) })} aria-label="Repeat every" />
              <span className="rf-unit">{UNIT[f.freq][f.interval === 1 ? 0 : 1]}</span>
            </RfRow>
            {(f.freq === "weekly" || fr) && (
              <RfRow label={fr ? "Only on" : "On"}>
                <div className="wd-chips" role="group" aria-label="Weekdays">
                  {WEEKDAYS.map((d, i) => (
                    <button key={d} type="button" className={"wd-chip" + (f.weekdays.includes(i + 1) ? " on" : "")} aria-pressed={f.weekdays.includes(i + 1)} aria-label={d} onClick={() => toggleDay(i + 1)}>
                      {d.slice(0, 2)}
                    </button>
                  ))}
                </div>
                {fr && f.weekdays.length === 0 && <span className="muted small">every day</span>}
              </RfRow>
            )}
            {f.freq === "monthly" && (
              <RfRow label="On">
                <select value={f.monthDay} onChange={(e) => set({ monthDay: Number(e.target.value) })} aria-label="Day of the month">
                  {Array.from({ length: 31 }, (_, i) => (
                    <option key={i + 1} value={i + 1}>
                      the {ordinal(i + 1)}
                    </option>
                  ))}
                  <option value={-1}>the last day</option>
                </select>
              </RfRow>
            )}
            {fr ? (
              <RfRow label="Window">
                <label className="check">
                  <input type="checkbox" checked={f.limited} onChange={(e) => set({ limited: e.target.checked })} /> only between
                </label>
                {f.limited && (
                  <span className="rf-pair">
                    <input type="time" className="time" value={f.from} onChange={(e) => set({ from: e.target.value })} aria-label="From" required />
                    <span className="rf-unit">and</span>
                    <input type="time" className="time" value={f.to} onChange={(e) => set({ to: e.target.value })} aria-label="To" required />
                  </span>
                )}
              </RfRow>
            ) : (
              <RfRow label="At">
                <input type="time" className="time" value={f.at} onChange={(e) => set({ at: e.target.value })} aria-label="Runs at" required />
              </RfRow>
            )}
            <RfRow label="Time zone">
              <input className="errand-tz" value={f.tz} onChange={(e) => set({ tz: e.target.value })} aria-label="Time zone" spellCheck={false} />
            </RfRow>
          </div>
          <p className="errand-preview small">
            <Icon name="clock" /> {describeErrand(f)}
          </p>
        </div>
        )}
        <label title={canTimeout ? undefined : "Changing it needs the Run timeouts permission"}>
          <span>
            Timeout <span className="muted small">seconds, 10–43200 (12 h)</span>
          </span>
          <input type="number" min={10} max={43200} value={f.timeout} disabled={!canTimeout} onChange={(e) => set({ timeout: int(e.target.value, 300) })} />
        </label>
        {!tplMode && (
        <label>
          Agent
          {manager || (errand && errand.worker_username !== me.user.username) ? (
            <select value={worker} onChange={(e) => set({ worker: e.target.value })} disabled={!manager}>
              {!agents.some((a) => a!.username === worker) && worker && <option value={worker}>{worker}</option>}
              {agents.map((a) => (
                <option key={a!.id} value={a!.username}>
                  {a!.display_name || a!.username}
                </option>
              ))}
            </select>
          ) : (
            <input value={worker || "(you need an agent)"} disabled />
          )}
        </label>
        )}
        <fieldset className="span2 errand-opts">
          <legend className="field-label">Output</legend>
          <label className="check">
            <input type="radio" name="output" checked={f.output === "gigg"} onChange={() => set({ output: "gigg" })} /> keep the last 64 KiB of each run here
          </label>
          <label className="check">
            <input type="radio" name="output" checked={f.output === "host"} onChange={() => set({ output: "host" })} /> keep it on the agent's host only <span className="muted small">(for output that shouldn't leave it)</span>
          </label>
        </fieldset>
        {!tplMode && (
        <label className="check span2">
          <input type="checkbox" checked={f.onFailureGig} onChange={(e) => set({ onFailureGig: e.target.checked })} /> Open a gig for the agent when it starts failing
        </label>
        )}
        {canUnsandbox && (
          <div className="span2 errand-unsandbox">
            <label className="check">
              <input type="checkbox" checked={f.unsandboxed} onChange={(e) => set({ unsandboxed: e.target.checked })} /> Run outside the sandbox
            </label>
            {f.unsandboxed && (
              <p className="banner banner-danger small errand-note" role="note">
                <Icon name="alert" />
                <span>
                  The command runs as the agent with <strong>none of the sandbox's limits</strong>: every file the agent's user can reach, any network host. Only for commands you've read and trust. Anyone
                  without this permission who changes the errand puts it back in the sandbox.
                </span>
              </p>
            )}
          </div>
        )}
        {loosing && (
          <p className="span2 banner banner-danger small errand-note" role="note">
            <Icon name="alert" />
            <span>This errand runs outside the sandbox. Saving any change puts it back in the sandbox (only people with the No-sandbox errands permission can keep it out).</span>
          </p>
        )}
        <label className="span2">
          <span>
            Notes <span className="muted small">optional: what it checks, what to do when it fails</span>
          </span>
          <textarea rows={2} value={f.description} onChange={(e) => set({ description: e.target.value })} />
        </label>
        {err && (
          <p className="danger span2" role="alert">
            {err}
          </p>
        )}
        <div className="row end span2">
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="button" disabled={busy || !!problem} title={problem ?? undefined}>
            {tplMode ? "Save template" : errand ? "Save" : "Create errand"}
          </button>
        </div>
      </form>
      {askDetach && (
        <ConfirmModal title="Leave the shared library?" confirmLabel="Save anyway" onConfirm={() => submit(null, true)} onCancel={() => setAskDetach(null)}>
          <p>Saving this edit moves this errand off</p>
          <ul>
            {askDetach.map((d) => (
              <li key={d}>{d}.</li>
            ))}
          </ul>
          <p className="muted small">Later changes an admin makes to the shared one won't reach it. The shared one and the other workspaces stay as they are.</p>
        </ConfirmModal>
      )}
    </Modal>
  );
}
