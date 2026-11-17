// Dashboard: what the agents' runners did in the last 24 hours, per workspace, against its caps;
// the pause switch (gig runs and errand runs, two boxes), the errands' health, and the caps for admins.
import { useCallback, useEffect, useRef, useState, type CSSProperties, type FormEvent, type MouseEvent } from "react";
import { Link, useLocation } from "wouter";
import { fmtTime, hasCap } from "./api";
import { useApp } from "./App";
import { call } from "./http";
import { Avatar, Modal } from "./ui";
import { CATEGORY_COLOR, colorFor } from "./theme";
import { HelpButton } from "./help";
import { filterQuery, EMPTY_FILTER } from "./boardModel";
import { Icon } from "./icons";

export interface RunnerSettings {
  workspace_id: number;
  daily_cap: number;
  night_cap: number;
  paused_until: number | null;
  paused_open: boolean;
  paused_by: number | null;
  paused_at: number | null;
  paused: boolean;
  errands_paused: boolean;
  errands_paused_at: number | null;
  errands_paused_by: number | null;
}

export interface RunnerUsage {
  gigs_24h: number;
  always_24h: number;
  optional_24h: number;
  runs_24h: number;
  usage_pct_24h: number;
  usage_pct_7d: number;
  last_run_at: number | null;
  cost_usd_7d: number;
  unmeasured_7d: number;
}

export interface RunnerState {
  workspace_id: number;
  slug: string;
  name: string;
  settings: RunnerSettings;
  usage: RunnerUsage;
  errands?: { total: number; failing: number };
}

// Boxes: which runs a pause or resume touches (both when omitted).
export interface Boxes {
  gigs: boolean;
  errands: boolean;
}

export const runnerApi = {
  list: () => call<{ items: RunnerState[] }>("GET", "/runner"),
  pause: (workspaces: number[] | undefined, minutes: number, boxes?: Boxes) =>
    call<{ items: RunnerState[] }>("POST", "/runner/pause", { ...(workspaces ? { workspaces } : {}), minutes, ...(boxes ?? {}) }),
  resume: (workspaces: number[] | undefined, boxes?: Boxes) => call<{ items: RunnerState[] }>("POST", "/runner/resume", { ...(workspaces ? { workspaces } : {}), ...(boxes ?? {}) }),
  caps: (ws: number, daily_cap: number, night_cap: number) => call<RunnerState>("PATCH", `/workspaces/${ws}/runner`, { daily_cap, night_cap }),
};

// The pause lengths offered; 0 = until resumed.
export const PAUSE_CHOICES: { minutes: number; label: string }[] = [
  { minutes: 60, label: "1 h" },
  { minutes: 300, label: "5 h" },
  { minutes: 720, label: "12 h" },
  { minutes: 1440, label: "24 h" },
  { minutes: 0, label: "Until I say so" },
];

// pausedText: "until resumed" or "until <time>".
export function pausedText(s: Pick<RunnerSettings, "paused_open" | "paused_until">): string {
  return s.paused_open ? "until resumed" : s.paused_until ? `until ${fmtTime(s.paused_until)}` : "";
}

// pausedWhat: "gigs until 18:00, errands", "errands", "gigs until resumed".
export function pausedWhat(s: Pick<RunnerSettings, "paused" | "paused_open" | "paused_until" | "errands_paused">): string {
  const parts: string[] = [];
  if (s.paused) parts.push(`gigs ${pausedText(s)}`);
  if (s.errands_paused) parts.push("errands");
  return parts.join(", ");
}

// pct: a 0-100 share for a meter (a 0 cap is full).
export function pct(n: number, cap: number): number {
  if (cap <= 0) return 100;
  return Math.max(0, Math.min(100, Math.round((n / cap) * 100)));
}

export function DashboardPage() {
  const { me } = useApp();
  const admin = me.admin;
  // Pausing: admins, or the pause_runs permission (per workspace).
  const canPause = (ws: number) => admin || hasCap(me, "pause_runs", ws);
  const [items, setItems] = useState<RunnerState[] | null>(null);
  const [err, setErr] = useState("");
  const [pausing, setPausing] = useState(false);
  const load = useCallback(() => {
    runnerApi
      .list()
      .then((r) => {
        setItems(r.items);
        setErr("");
      })
      .catch((e: unknown) => setErr(e instanceof Error ? e.message : "Couldn't load the runners."));
  }, []);
  useEffect(() => {
    load();
    const t = setInterval(load, 60_000);
    return () => clearInterval(t);
  }, [load]);
  const paused = (items ?? []).filter((i) => i.settings.paused || i.settings.errands_paused);
  const pausable = (items ?? []).filter((i) => canPause(i.workspace_id));
  const resumable = paused.filter((i) => canPause(i.workspace_id));
  const act = (p: Promise<unknown>) =>
    p.then(load).catch((e: unknown) => setErr(e instanceof Error ? e.message : "That didn't work."));

  return (
    <div className="dash">
      <header className="page-head">
        <div>
          <h1>Dashboard<HelpButton topic="dashboard" label="Help: the dashboard and agent runs" /></h1>
          <p className="muted small">Agent gig runs in the last 24 hours, per workspace, and their errands. Always-run gigs run day and night; the others at night, within the usage budget.</p>
        </div>
        {pausable.length > 0 && (
          <button type="button" className="button secondary dash-pause" data-tip="Stop agents from starting new gigs and errands, for a while or until resumed. Runs already going finish." onClick={() => setPausing(true)}>
            <Icon name="pause" />
            Pause runs
          </button>
        )}
      </header>
      {err && <p className="danger">{err}</p>}
      {paused.length > 0 && (
        <div className="banner banner-danger dash-banner" role="status">
          <Icon name="pause" className="dash-banner-icon" />
          <strong>Paused:</strong>{" "}
          <span>{paused.map((p) => `${p.name} (${pausedWhat(p.settings)})`).join(", ")}</span>
          {resumable.length > 0 && (
            <button type="button" className="button secondary small" onClick={() => act(runnerApi.resume(resumable.map((p) => p.workspace_id)))}>
              <Icon name="play" /> Resume {resumable.length > 1 ? "all" : ""}
            </button>
          )}
        </div>
      )}
      {items === null && !err && <p className="muted">Loading…</p>}
      <div className="dash-cards">
        {(items ?? []).map((it) => (
          <RunnerCard key={it.workspace_id} it={it} admin={admin} canPause={canPause(it.workspace_id)} onAct={act} />
        ))}
      </div>
      {pausing && pausable.length > 0 && (
        <PauseModal
          items={pausable}
          onClose={() => setPausing(false)}
          onPause={(ws, minutes, boxes) => {
            setPausing(false);
            act(runnerApi.pause(ws, minutes, boxes));
          }}
        />
      )}
    </div>
  );
}

function Meter({ label, n, cap, tip }: { label: string; n: number; cap: number; tip: string }) {
  const p = pct(n, cap);
  return (
    <div className="dash-meter">
      <div className="dash-meter-head">
        <span data-tip={tip}>{label}</span>
        <span className={"dash-meter-value" + (n >= cap ? " danger" : "")}>
          {n} <span className="muted">/ {cap}</span>
        </span>
      </div>
      <div className="dash-bar" role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={cap} aria-valuenow={n}>
        <span className={"dash-bar-fill" + (n >= cap ? " full" : p >= 80 ? " warn" : "")} style={{ width: `${p}%` }} />
      </div>
    </div>
  );
}

function RunnerCard({ it, admin, canPause, onAct }: { it: RunnerState; admin: boolean; canPause: boolean; onAct: (p: Promise<unknown>) => void }) {
  const { tree, users } = useApp();
  const ws = tree.find((w) => w.id === it.workspace_id);
  const owner = ws?.owner_id != null ? users.get(ws.owner_id) : undefined;
  const { settings: s, usage: u } = it;
  const [, navigate] = useLocation();
  const [editing, setEditing] = useState(false);
  // the card opens the board filtered to this workspace: the title is the link (keyboard, middle or
  // modified click), and a plain click anywhere else on the card does the same, except on its own
  // controls, while the caps are being edited, or while selecting text (a drag or a double-click)
  const boardHref = ws ? "/board" + filterQuery({ ...EMPTY_FILTER, ws: [ws.slug] }) : undefined;
  const pending = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(pending.current), []);
  const cardClick = (e: MouseEvent<HTMLElement>) => {
    clearTimeout(pending.current); // a second click: a double-click selecting a word, not a visit
    if (!boardHref || editing || e.button !== 0 || e.defaultPrevented || e.detail > 1) return;
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    if (!e.currentTarget.contains(e.target as Node)) return; // from a portal rendered inside the card
    if ((e.target as Element).closest("a, button, input, select, textarea, label, form, [role='dialog']")) return;
    if (window.getSelection()?.toString()) return;
    // wait out a double-click before leaving
    pending.current = setTimeout(() => {
      if (!window.getSelection()?.toString()) navigate(boardHref);
    }, 250);
  };
  const [daily, setDaily] = useState(s.daily_cap);
  const [night, setNight] = useState(s.night_cap);
  const save = (e: FormEvent) => {
    e.preventDefault();
    setEditing(false);
    onAct(runnerApi.caps(it.workspace_id, daily, night));
  };
  return (
    <section
      className={"card dash-card" + (s.paused ? " paused" : "") + (ws ? " dash-card-ws" : "") + (ws && !editing ? " dash-card-link" : "")}
      style={ws ? ({ "--c": colorFor(ws.slug, ws.color) } as CSSProperties) : undefined}
      aria-label={`Runner of ${it.name}`}
      onClick={cardClick}
    >
      <div className="dash-card-head">
        <h2 className="dash-card-title">
          {owner ? <Avatar user={owner} size="l" /> : ws && <span className="dot dash-card-dot" style={{ background: colorFor(ws.slug, ws.color) }} aria-hidden="true" />}
          {boardHref ? (
            <Link href={boardHref} className="dash-card-name" title={`Open the board for ${it.name}`}>
              {it.name}
            </Link>
          ) : (
            it.name
          )}
        </h2>
        {s.paused || s.errands_paused ? (
          <span className="dash-state dash-state-paused" data-tip={s.paused && s.errands_paused ? "No new gig or errand runs start in this workspace" : s.paused ? "No new gig runs start in this workspace (errands still run)" : "No errand runs start in this workspace (gigs still run)"}>
            <span className="dot" aria-hidden="true" />
            paused: {pausedWhat(s)}
          </span>
        ) : (
          <span className="dash-state" data-tip="The agent's runner may start gigs here (within the caps)">
            <span className="dot" style={{ background: CATEGORY_COLOR.done }} aria-hidden="true" />
            running
          </span>
        )}
        <span className="grow" />
        {canPause && (s.paused || s.errands_paused) && (
          <button type="button" className="button secondary small" onClick={() => onAct(runnerApi.resume([it.workspace_id]))}>
            <Icon name="play" /> Resume
          </button>
        )}
      </div>
      <Meter label="Gigs, last 24 h" n={u.gigs_24h} cap={s.daily_cap} tip="Different gigs started in the last 24 hours, against the daily cap (a gig that goes on counts once)" />
      <Meter
        label="Budget-dependent (night)"
        n={u.optional_24h}
        cap={s.night_cap}
        tip="Gigs that aren't always-run: they only run when the usage budget allows (at night), against this cap"
      />
      <dl className="dash-facts">
        <dt data-tip="Always-run gigs run whatever the usage budget, day or night (cron-like gigs)">Always-run</dt>
        <dd>{u.always_24h} gig{u.always_24h === 1 ? "" : "s"}</dd>
        <dt data-tip="Agent sessions started or resumed in the last 24 hours (one gig can have several)">Runs</dt>
        <dd>{u.runs_24h}</dd>
        <dt>Weekly usage</dt>
        <dd title="Points of the subscription's weekly meter measured around this workspace's runs (shared meter: approximate)">
          {u.usage_pct_24h.toFixed(1)}% today · {u.usage_pct_7d.toFixed(1)}% in 7 days
          {u.unmeasured_7d > 0 && <span className="muted small"> ({u.unmeasured_7d} unmeasured)</span>}
        </dd>
        <dt data-tip="What the runs cost in the last 7 days, as the agent reports it (API-equivalent USD)">Cost (7 days)</dt>
        <dd>${u.cost_usd_7d.toFixed(2)}</dd>
        <dt>Last run</dt>
        <dd>{u.last_run_at ? fmtTime(u.last_run_at) : "never"}</dd>
        {it.errands && it.errands.total > 0 && (
          <>
            <dt data-tip="Commands the agent runs on a schedule (the Errands page)">Errands</dt>
            <dd>
              <Link href={it.errands.failing > 0 ? "/errands?state=failing" : "/errands"} className={it.errands.failing > 0 ? "danger" : undefined}>
                {it.errands.total} errand{it.errands.total === 1 ? "" : "s"}
                {it.errands.failing > 0 ? ` · ${it.errands.failing} failing` : " · all ok"}
              </Link>
            </dd>
          </>
        )}
      </dl>
      {admin &&
        (editing ? (
          <form className="row dash-caps" onSubmit={save}>
            <label>
              Gigs / 24 h
              <input type="number" min={0} max={200} value={daily} onChange={(e) => setDaily(Number(e.target.value))} />
            </label>
            <label>
              of which night
              <input type="number" min={0} max={200} value={night} onChange={(e) => setNight(Number(e.target.value))} />
            </label>
            <button type="submit" className="button small">
              Save
            </button>
            <button type="button" className="button secondary small" onClick={() => setEditing(false)}>
              Cancel
            </button>
          </form>
        ) : (
          <div className="dash-foot">
            <button type="button" className="button secondary small" data-tip="Admins: the daily gig cap and how many of those may depend on the budget" onClick={() => setEditing(true)}>
              Change caps
            </button>
          </div>
        ))}
    </section>
  );
}

function PauseModal({ items, onClose, onPause }: { items: RunnerState[]; onClose: () => void; onPause: (ws: number[] | undefined, minutes: number, boxes: Boxes) => void }) {
  const [picked, setPicked] = useState<Set<number>>(new Set(items.map((i) => i.workspace_id)));
  const [minutes, setMinutes] = useState(1440);
  const [boxes, setBoxes] = useState<Boxes>({ gigs: true, errands: true });
  const all = picked.size === items.length;
  const toggle = (id: number) =>
    setPicked((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  return (
    <Modal title="Pause agent runs" onClose={onClose}>
      <div className="pause">
        <p className="muted small">While paused the agents start no new gig (always-run ones included) and no errand. A run already going finishes. Errands stay paused until resumed; missed slots aren't caught up.</p>
        <button type="button" className="button danger pause-all" onClick={() => onPause(undefined, 1440, { gigs: true, errands: true })}>
          <Icon name="pause" /> Pause everything for 24 h
        </button>
        <fieldset className="pause-ws">
          <legend className="field-label">Or choose: what</legend>
          <label className="admin-check">
            <input type="checkbox" checked={boxes.gigs} onChange={(e) => setBoxes((b) => ({ ...b, gigs: e.target.checked }))} />
            <span>Gig runs</span>
          </label>
          <label className="admin-check">
            <input type="checkbox" checked={boxes.errands} onChange={(e) => setBoxes((b) => ({ ...b, errands: e.target.checked }))} />
            <span>Errand runs</span>
            <span className="muted small">(until resumed)</span>
          </label>
        </fieldset>
        <fieldset className="pause-ws">
          <legend className="field-label">Or choose: workspaces</legend>
          {items.map((i) => (
            <label key={i.workspace_id} className="admin-check">
              <input type="checkbox" checked={picked.has(i.workspace_id)} onChange={() => toggle(i.workspace_id)} />
              <span>{i.name}</span>
              {(i.settings.paused || i.settings.errands_paused) && <span className="pill pill-danger small">paused: {pausedWhat(i.settings)}</span>}
            </label>
          ))}
        </fieldset>
        {boxes.gigs && (
        <fieldset className="pause-len">
          <legend className="field-label">for{boxes.errands ? " (gig runs; errands pause until resumed)" : ""}</legend>
          <div className="admin-seg" role="radiogroup" aria-label="How long">
            {PAUSE_CHOICES.map((c) => (
              <button key={c.minutes} type="button" role="radio" aria-checked={minutes === c.minutes} className={"admin-seg-btn" + (minutes === c.minutes ? " on" : "")} onClick={() => setMinutes(c.minutes)}>
                {c.label}
              </button>
            ))}
          </div>
        </fieldset>
        )}
        <div className="row end">
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="button danger" disabled={picked.size === 0 || (!boxes.gigs && !boxes.errands)} onClick={() => onPause(all ? undefined : [...picked], minutes, boxes)}>
            <Icon name="pause" /> Pause {all ? "all" : picked.size}
          </button>
        </div>
      </div>
    </Modal>
  );
}
