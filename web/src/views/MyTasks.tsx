// My tasks: everything assigned to me across my workspaces, grouped by when it's due.
import { useCallback, useEffect, useMemo, useState, type CSSProperties } from "react";
import { Link } from "wouter";
import { type Task } from "../api";
import { fmtDue } from "../dates";
import { useApp } from "../App";
import { CATEGORY_COLOR, colorFor } from "../theme";
import { PriorityIcon, StatusDot } from "../ui";
import { WorkspaceBadge } from "../WorkspaceBadge";
import { Icon } from "../icons";
import { BUCKETS, dueBucket, errMsg, isClosed, taskHref, tasksAcross, type DueBucket } from "./viewsApi";
import "./views.css";
import { HelpButton } from "../help";

const BUCKET_COLOR: Record<DueBucket, string> = {
  overdue: "var(--danger)",
  today: CATEGORY_COLOR.doing!,
  week: CATEGORY_COLOR.todo!,
  later: CATEGORY_COLOR.backlog!,
  none: "var(--faint)",
};

export function MyTasks() {
  const { me, tree } = useApp();
  const [items, setItems] = useState<Task[] | null>(null);
  const [failed, setFailed] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [includeDone, setIncludeDone] = useState(false);

  const load = useCallback(() => {
    setError("");
    tasksAcross(tree, { assignee: me.user.id, sort: "due,-priority", limit: 500 })
      .then((r) => {
        setItems(r.items);
        setFailed(r.failed);
      })
      .catch((e: unknown) => setError(errMsg(e)));
  }, [tree, me.user.id]);
  useEffect(load, [load]);

  const groups = useMemo(() => {
    const g = new Map<DueBucket, Task[]>(BUCKETS.map((b) => [b.id, []]));
    for (const t of items ?? []) {
      if (!includeDone && isClosed(t.status_category)) continue;
      g.get(dueBucket(t.due_at))!.push(t);
    }
    for (const list of g.values()) list.sort((a, b) => (a.due_at ?? Infinity) - (b.due_at ?? Infinity) || b.priority - a.priority || b.updated_at - a.updated_at);
    return g;
  }, [items, includeDone]);

  const total = [...groups.values()].reduce((n, l) => n + l.length, 0);
  const wsOf = (id: number) => tree.find((w) => w.id === id);

  return (
    <div className="vw-page vw-narrow">
      <header className="page-head">
        <div>
          <div className="crumb">
            <span className="muted small">{items === null ? "Loading…" : `${total} task${total === 1 ? "" : "s"} across ${tree.length} workspace${tree.length === 1 ? "" : "s"}`}</span>
          </div>
          <h1>My tasks<HelpButton topic="views" label="Help: my tasks, tables and saved views" /></h1>
        </div>
        <div className="row vw-head-actions">
          <label className="vw-toggle">
            <input type="checkbox" checked={includeDone} onChange={(e) => setIncludeDone(e.target.checked)} />
            Include done
          </label>
          <button type="button" className="button secondary small" onClick={load}>
            <Icon name="refresh" /> Refresh
          </button>
        </div>
      </header>

      {error && (
        <p className="danger" role="alert">
          {error}
        </p>
      )}
      {failed.length > 0 && <p className="danger small">Couldn't load: {failed.join(", ")}</p>}
      {items !== null && total === 0 && (
        <div className="card vw-empty-card">
          <div className="vw-empty-icon" aria-hidden="true">
            <Icon name="check" />
          </div>
          <div>Nothing assigned to you{includeDone ? "" : " that's still open"}.</div>
        </div>
      )}

      {BUCKETS.map((b) => {
        const list = groups.get(b.id)!;
        if (list.length === 0) return null;
        return (
          <section key={b.id} className="vw-group" aria-label={b.label}>
            <h2 className="vw-group-head">
              <span className="dot" style={{ background: BUCKET_COLOR[b.id] }} />
              <span>{b.label}</span>
              <span className="count">{list.length}</span>
            </h2>
            <ul className="card vw-list">
              {list.map((t) => {
                const w = wsOf(t.workspace_id);
                const p = w?.projects.find((x) => x.id === t.project_id);
                const late = b.id === "overdue" && !isClosed(t.status_category);
                return (
                  <li key={t.id}>
                    <Link href={taskHref(tree, t)} className="vw-list-row" style={t.color ? ({ "--row-c": t.color } as CSSProperties) : undefined}>
                      <span className="vw-key">{t.key}</span>
                      <span className={"vw-list-title" + (isClosed(t.status_category) ? " vw-closed" : "")}>{t.title}</span>
                      <span className="vw-list-meta">
                        {w && tree.length > 1 && <WorkspaceBadge ws={w} />}
                        {p && (
                          <span className="proj-tag" title={p.name}>
                            <span className="dot" style={{ background: colorFor(p.key, p.color) }} aria-hidden="true" />
                            {p.name}
                          </span>
                        )}
                        <StatusDot name={t.status_name} category={t.status_category} />
                        <PriorityIcon priority={t.priority} />
                        {t.due_at && <span className={"vw-due" + (late ? " overdue" : "")}>{fmtDue(t.due_at)}</span>}
                      </span>
                    </Link>
                  </li>
                );
              })}
            </ul>
          </section>
        );
      })}
    </div>
  );
}
