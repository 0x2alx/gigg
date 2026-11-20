// Metrics: what was done over a period, in numbers and small charts: gigs created and done, agent runs
// by outcome, cost and tokens, errands, recurring gigs. Everything comes from GET /metrics and is drawn
// here as inline SVG (no chart library, no outside calls). You see what you may see anywhere else.
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { useApp } from "./App";
import { localTZ } from "./recurrence";
import { CATEGORY_COLOR } from "./theme";
import {
  MAX_DAYS,
  PERIODS,
  bucketFor,
  bucketLabel,
  bucketTitle,
  daysInRange,
  fmtCount,
  fmtDur,
  fmtPct,
  fmtUSD,
  labelEvery,
  metricsApi,
  niceMax,
  onTime,
  periodRange,
  today,
  type Bucket,
  type MetricBucket,
  type MetricGroup,
  type MetricsResult,
  type Period,
} from "./metrics";

interface Filters {
  workspace: string;
  project: number;
  assignee: number;
  kind: "" | "human" | "agent";
}
const NO_FILTERS: Filters = { workspace: "", project: 0, assignee: 0, kind: "" };

export function MetricsPage() {
  const { tree, users } = useApp();
  const tz = localTZ();
  const [period, setPeriod] = useState<Period>("30d");
  const [range, setRange] = useState(() => periodRange("30d", tz));
  const [bucket, setBucket] = useState<Bucket>("day");
  const [f, setF] = useState<Filters>(NO_FILTERS);
  const [data, setData] = useState<MetricsResult | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const seq = useRef(0);

  const days = daysInRange(range.from, range.to);
  const rangeErr = !range.from || !range.to ? "Pick both dates." : days < 1 ? "The first day is after the last." : days > MAX_DAYS ? `The range can be at most ${MAX_DAYS} days.` : "";

  const pickPeriod = (p: Period) => {
    setPeriod(p);
    if (p === "custom") return;
    const r = periodRange(p, tz);
    setRange(r);
    setBucket(bucketFor(r.from, r.to));
  };
  const pickDate = (which: "from" | "to", v: string) => {
    const r = { ...range, [which]: v };
    setRange(r);
    if (r.from && r.to && daysInRange(r.from, r.to) >= 1) setBucket(bucketFor(r.from, r.to));
  };

  const load = useCallback(() => {
    if (rangeErr) return;
    const mine = ++seq.current;
    setBusy(true);
    metricsApi
      .get({ from: range.from, to: range.to, bucket, tz, workspace: f.workspace, project: f.project, assignee: f.assignee, worker_kind: f.kind })
      .then((r) => {
        if (mine !== seq.current) return;
        setData(r);
        setErr("");
      })
      .catch((e: unknown) => mine === seq.current && setErr(e instanceof Error ? e.message : "Couldn't load the metrics."))
      .finally(() => mine === seq.current && setBusy(false));
  }, [range.from, range.to, bucket, tz, f, rangeErr]);
  useEffect(load, [load]);

  const wsOf = tree.find((w) => w.slug === f.workspace);
  const projects = (wsOf ? [wsOf] : tree).flatMap((w) => w.projects.map((p) => ({ id: p.id, name: p.name, ws: w.name })));
  const people = useMemo(() => [...users.values()].sort((a, b) => (a.display_name || a.username).localeCompare(b.display_name || b.username)), [users]);
  const filtered = (f.workspace !== "" || f.project !== 0 || f.assignee !== 0 || f.kind !== "");
  const t = data?.totals;
  const empty = !!t && !t.gigs_created && !t.gigs_done && !t.gigs_cancelled && !t.runs && !t.errand_runs && !t.recurring_on_time && !t.recurring_missed;

  return (
    <div className="metrics">
      <header className="page-head">
        <div>
          <h1>Metrics</h1>
          <p className="muted small">Gigs, agent runs, cost and errands over a period, for the workspaces and projects you can see. Days are cut in {data?.tz ?? tz}.</p>
        </div>
      </header>

      <div className="mx-controls">
        <div className="view-switch" role="group" aria-label="Period">
          {PERIODS.map((p) => (
            <button key={p.key} type="button" className={"vs-item" + (period === p.key ? " on" : "")} aria-pressed={period === p.key} onClick={() => pickPeriod(p.key)}>
              {p.label}
            </button>
          ))}
        </div>
        {period === "custom" && (
          <div className="mx-dates">
            <label>
              <span>From</span>
              <input type="date" value={range.from} max={range.to || undefined} onChange={(e) => pickDate("from", e.target.value)} />
            </label>
            <label>
              <span>To</span>
              <input type="date" value={range.to} min={range.from || undefined} max={today(tz)} onChange={(e) => pickDate("to", e.target.value)} />
            </label>
          </div>
        )}
        <div className="view-switch" role="group" aria-label="Group by">
          {(["day", "week", "month"] as Bucket[]).map((b) => (
            <button key={b} type="button" className={"vs-item" + (bucket === b ? " on" : "")} aria-pressed={bucket === b} onClick={() => setBucket(b)}>
              {b === "day" ? "Day" : b === "week" ? "Week" : "Month"}
            </button>
          ))}
        </div>
      </div>
      <div className="mx-controls mx-filters">
        <label className="inline">
          <span>Workspace</span>
          <select value={f.workspace} onChange={(e) => setF({ ...f, workspace: e.target.value, project: 0 })}>
            <option value="">All</option>
            {tree.map((w) => (
              <option key={w.id} value={w.slug}>
                {w.name}
              </option>
            ))}
          </select>
        </label>
        <label className="inline">
          <span>Project</span>
          <select value={f.project} onChange={(e) => setF({ ...f, project: Number(e.target.value) })}>
            <option value={0}>All</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {wsOf ? p.name : `${p.ws} / ${p.name}`}
              </option>
            ))}
          </select>
        </label>
        <label className="inline">
          <span>Person</span>
          <select value={f.assignee} onChange={(e) => setF({ ...f, assignee: Number(e.target.value) })}>
            <option value={0}>Anyone</option>
            {people.map((u) => (
              <option key={u.id} value={u.id}>
                {u.display_name || u.username}
                {u.kind === "agent" ? " (agent)" : ""}
              </option>
            ))}
          </select>
        </label>
        <label className="inline">
          <span>Kind</span>
          <select value={f.kind} onChange={(e) => setF({ ...f, kind: e.target.value as Filters["kind"] })}>
            <option value="">Humans and agents</option>
            <option value="human">Humans</option>
            <option value="agent">Agents</option>
          </select>
        </label>
        {filtered && (
          <button type="button" className="button secondary small" onClick={() => setF(NO_FILTERS)}>
            Clear filters
          </button>
        )}
      </div>

      {rangeErr && <p className="danger">{rangeErr}</p>}
      {err && <p className="danger">{err}</p>}
      {!data && !err && !rangeErr && <p className="muted">Loading…</p>}
      {data && t && (
        <div className={"mx-body" + (busy ? " busy" : "")} aria-busy={busy}>
          <div className="mx-tiles">
            <Tile label="Gigs done" value={fmtCount(t.gigs_done)} sub={`${fmtCount(t.gigs_created)} created, ${fmtCount(t.gigs_cancelled)} cancelled`} />
            <Tile label="Open now" value={fmtCount(t.open_now)} sub="not done or cancelled" />
            <Tile label="Median cycle time" value={fmtDur(t.cycle_median_ms)} sub={t.cycle_count ? `created to done, ${fmtCount(t.cycle_count)} gigs` : "no gig done"} />
            <Tile label="Agent runs" value={fmtCount(t.runs)} sub={t.success_rate == null ? "none ended" : `${fmtPct(t.success_rate)} done, ${fmtPct(t.failure_rate)} failed, ${fmtPct(t.timeout_rate)} timed out`} />
            <Tile label="Run time" value={fmtDur(t.run_ms || null)} sub={t.run_avg_ms == null ? "no timed run" : `${fmtDur(t.run_avg_ms)} per run`} />
            <Tile label="Cost" value={fmtUSD(t.cost_usd)} sub={`${fmtCount(t.tokens_in + t.tokens_out)} tokens (+${fmtCount(t.tokens_cache)} cached)`} />
            <Tile label="Errand runs" value={fmtCount(t.errand_runs)} sub={`${fmtCount(t.errand_failed + t.errand_timeout)} failed, ${fmtCount(t.errand_missed)} missed`} />
            <Tile label="Recurring on time" value={fmtPct(onTime(t))} sub={`${fmtCount(t.recurring_on_time)} on time, ${fmtCount(t.recurring_missed)} missed`} />
          </div>
          {empty && <p className="muted mx-empty">Nothing happened in this period{filtered ? " for these filters" : ""}.</p>}
          {data.truncated && <p className="muted small">A very busy workspace: the median cycle time uses only the first gigs of the range.</p>}

          <ChartCard
            title="Gigs"
            bucket={data.bucket}
            buckets={data.buckets}
            series={[
              { key: "done", label: "Done", color: "var(--ok)", value: (b) => b.gigs_done },
              { key: "created", label: "Created", color: "var(--accent)", value: (b) => b.gigs_created },
            ]}
          />
          <ChartCard
            title="Agent runs by outcome"
            bucket={data.bucket}
            buckets={data.buckets}
            stacked
            series={[
              { key: "done", label: "Done", color: "var(--ok)", value: (b) => b.runs_done },
              { key: "ask", label: "Asked a person", color: "var(--accent)", value: (b) => b.runs_needs_human },
              { key: "error", label: "Failed", color: "var(--danger)", value: (b) => b.runs_error },
              { key: "timeout", label: "Timed out", color: CATEGORY_COLOR.doing ?? "#F76B15", value: (b) => b.runs_timeout },
              { key: "int", label: "Interrupted", color: "var(--muted)", value: (b) => b.runs_interrupted },
              { key: "other", label: "Parked or running", color: "var(--line-strong)", value: (b) => b.runs_parked + b.runs_running },
            ]}
          />
          <ChartCard title="Cost (USD)" bucket={data.bucket} buckets={data.buckets} fmt={fmtUSD} series={[{ key: "cost", label: "Cost", color: "var(--accent)", value: (b) => b.cost_usd }]} />
          {(t.errand_runs > 0 || t.recurring_on_time + t.recurring_missed > 0) && (
            <div className="mx-pair">
              {t.errand_runs > 0 && (
                <ChartCard
                  title="Errand runs"
                  bucket={data.bucket}
                  buckets={data.buckets}
                  stacked
                  series={[
                    { key: "ok", label: "Ok", color: "var(--ok)", value: (b) => b.errand_ok },
                    { key: "fail", label: "Failed or timed out", color: "var(--danger)", value: (b) => b.errand_failed + b.errand_timeout },
                    { key: "miss", label: "Missed or skipped", color: "var(--muted)", value: (b) => b.errand_missed + b.errand_skipped },
                  ]}
                />
              )}
              {t.recurring_on_time + t.recurring_missed > 0 && (
                <ChartCard
                  title="Recurring gigs"
                  bucket={data.bucket}
                  buckets={data.buckets}
                  stacked
                  series={[
                    { key: "on", label: "On time", color: "var(--ok)", value: (b) => b.recurring_on_time },
                    { key: "miss", label: "Missed", color: "var(--danger)", value: (b) => b.recurring_missed },
                  ]}
                />
              )}
            </div>
          )}
          <div className="mx-pair">
            <GroupTable title="By agent" first="Agent" rows={data.by_agent} />
            <GroupTable title="By model" first="Model" rows={data.by_model} blank="not reported" />
          </div>
          <GroupTable title="By project" first="Project" rows={data.by_project} />
        </div>
      )}
    </div>
  );
}

function Tile({ label, value, sub }: { label: string; value: string; sub: string }) {
  return (
    <div className="card mx-tile">
      <span className="mx-tile-label">{label}</span>
      <strong className="mx-tile-value">{value}</strong>
      <span className="mx-tile-sub muted">{sub}</span>
    </div>
  );
}

function GroupTable({ title, first, rows, blank }: { title: string; first: string; rows: MetricGroup[]; blank?: string }) {
  return (
    <section className="card mx-card" aria-label={title}>
      <h2 className="mx-h">{title}</h2>
      {rows.length === 0 ? (
        <p className="muted small">No runs.</p>
      ) : (
        <div className="mx-table-wrap">
          <table className="mx-table">
            <thead>
              <tr>
                <th>{first}</th>
                <th className="num">Runs</th>
                <th className="num">Cost</th>
                <th className="num">Tokens in</th>
                <th className="num">Tokens out</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={`${r.id ?? ""}:${r.name}`}>
                  <td className="mx-name" title={r.label && r.label !== r.name ? `${r.label} (${r.name})` : r.name}>
                    {r.name || blank || "–"}
                  </td>
                  <td className="num">{fmtCount(r.runs)}</td>
                  <td className="num">{fmtUSD(r.cost_usd)}</td>
                  <td className="num">{fmtCount(r.tokens_in)}</td>
                  <td className="num">{fmtCount(r.tokens_out)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

// ---- charts: small inline SVG bars -----------------------------------------------------------

interface Series {
  key: string;
  label: string;
  color: string; // a CSS colour or variable
  value: (b: MetricBucket) => number;
}

function ChartCard({ title, bucket, buckets, series, stacked, fmt }: { title: string; bucket: Bucket; buckets: MetricBucket[]; series: Series[]; stacked?: boolean; fmt?: (n: number) => string }) {
  const used = series.filter((s) => buckets.some((b) => s.value(b) > 0));
  return (
    <section className="card mx-card" aria-label={title}>
      <div className="mx-card-head">
        <h2 className="mx-h">{title}</h2>
        <ul className="mx-legend">
          {(used.length ? used : series.slice(0, 1)).map((s) => (
            <li key={s.key}>
              <span className="mx-swatch" style={{ background: s.color }} aria-hidden="true" />
              {s.label}
            </li>
          ))}
        </ul>
      </div>
      <BarChart title={title} bucket={bucket} buckets={buckets} series={series} stacked={stacked} fmt={fmt ?? fmtCount} integer={!fmt} />
    </section>
  );
}

// useWidth: the pixel width of an element (so the chart's text keeps its size on a phone).
function useWidth(): [(el: HTMLDivElement | null) => void, number] {
  const [w, setW] = useState(0);
  const ro = useRef<ResizeObserver | null>(null);
  const ref = useCallback((el: HTMLDivElement | null) => {
    ro.current?.disconnect();
    ro.current = null;
    if (!el) return;
    setW(el.clientWidth);
    if (typeof ResizeObserver === "function") {
      ro.current = new ResizeObserver(() => setW(el.clientWidth));
      ro.current.observe(el);
    }
  }, []);
  return [ref, w];
}

const H = 176;
const PAD = { l: 40, r: 8, t: 8, b: 24 };

export function BarChart({ title, bucket, buckets, series, stacked, fmt, integer = true }: { title: string; bucket: Bucket; buckets: MetricBucket[]; series: Series[]; stacked?: boolean; fmt: (n: number) => string; integer?: boolean }) {
  const [ref, width] = useWidth();
  const W = Math.max(width, 160);
  const plotW = W - PAD.l - PAD.r;
  const plotH = H - PAD.t - PAD.b;
  const n = buckets.length;
  const tops = buckets.map((b) => (stacked ? series.reduce((a, s) => a + s.value(b), 0) : Math.max(...series.map((s) => s.value(b)))));
  const top = niceMax(Math.max(0, ...tops));
  const y = (v: number) => PAD.t + plotH - (v / top) * plotH;
  const slot = plotW / Math.max(n, 1);
  const gap = slot > 6 ? Math.min(4, slot * 0.2) : slot > 2.5 ? 1 : 0;
  const barsPer = stacked ? 1 : series.length;
  const bw = Math.max((slot - gap) / barsPer, 0.5);
  const every = labelEvery(n, plotW);
  // counts get whole-number ticks only; a half that rounds onto its neighbour is left out
  const ticks = integer && !Number.isInteger(top / 2) ? [0, top] : [0, top / 2, top];
  const total = tops.reduce((a, v) => a + v, 0);
  return (
    <div ref={ref} className="mx-chart">
      <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} role="img" aria-label={`${title}: ${fmt(total)} over ${n} ${bucket}${n === 1 ? "" : "s"}`}>
        {ticks.map((v) => (
          <g key={v}>
            <line x1={PAD.l} x2={W - PAD.r} y1={y(v)} y2={y(v)} className="mx-grid" />
            <text x={PAD.l - 6} y={y(v) + 4} textAnchor="end" className="mx-tick">
              {fmt(v)}
            </text>
          </g>
        ))}
        {buckets.map((b, i) => {
          const x0 = PAD.l + i * slot + gap / 2;
          let acc = 0;
          return (
            <g key={b.date}>
              {series.map((s, k) => {
                const v = s.value(b);
                if (v <= 0) return null;
                const h = Math.max((v / top) * plotH, 1);
                const yy = stacked ? y(acc + v) : PAD.t + plotH - h;
                if (stacked) acc += v;
                return <rect key={s.key} x={x0 + (stacked ? 0 : k * bw)} y={yy} width={bw} height={h} style={{ fill: s.color } as CSSProperties} />;
              })}
              {i % every === 0 && (
                <text x={x0 + (slot - gap) / 2} y={H - 7} textAnchor={i === 0 ? "start" : "middle"} className="mx-tick">
                  {bucketLabel(b.date, bucket)}
                </text>
              )}
              <rect x={PAD.l + i * slot} y={PAD.t} width={slot} height={plotH} className="mx-hit">
                <title>
                  {bucketTitle(b.date, bucket)}
                  {series.map((s) => `\n${s.label}: ${fmt(s.value(b))}`).join("")}
                </title>
              </rect>
            </g>
          );
        })}
      </svg>
    </div>
  );
}
