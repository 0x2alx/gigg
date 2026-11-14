// Audit log: the instance-wide activity, newest first, paged by id.
import { useCallback, useEffect, useMemo, useState } from "react";
import { fmtTime } from "../api";
import { colorFor } from "../theme";
import { Avatar, Pill } from "../ui";
import { adminApi, errMsg, type AdminUser, type AuditItem } from "./adminApi";
import { ActionPill, AdminPage, Empty, ErrorLine, TimeAgo } from "./common";

const PAGE = 100;
const PREFIXES = ["task", "comment", "run", "key", "user", "member", "workspace", "project", "auth"];

export function AuditPage() {
  const [items, setItems] = useState<AuditItem[]>([]);
  const [users, setUsers] = useState<Map<number, AdminUser>>(new Map());
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [more, setMore] = useState(true);
  const [prefix, setPrefix] = useState("");

  const page = useCallback((before?: number) => {
    setLoading(true);
    adminApi
      .audit(before, PAGE)
      .then((r) => {
        setItems((cur) => (before ? [...cur, ...r.items] : r.items));
        setMore(r.items.length === PAGE);
        setError("");
      })
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    page();
    adminApi
      .users()
      .then((r) => setUsers(new Map(r.items.map((u) => [u.id, u]))))
      .catch(() => undefined);
  }, [page]);

  const shown = useMemo(() => (prefix ? items.filter((i) => i.action.split(".")[0] === prefix) : items), [items, prefix]);
  const last = items[items.length - 1];

  return (
    <AdminPage
      title="Audit log"
      sub="Every change on the instance, newest first."
      actions={
        <button type="button" className="button secondary" onClick={() => page()} disabled={loading}>
          ↻ Refresh
        </button>
      }
    >
      <div className="row admin-toolbar">
        <div className="admin-seg" role="group" aria-label="Filter by kind">
          <button type="button" className={"admin-seg-btn" + (prefix === "" ? " on" : "")} aria-pressed={prefix === ""} onClick={() => setPrefix("")}>
            all
          </button>
          {PREFIXES.map((p) => (
            <button key={p} type="button" className={"admin-seg-btn" + (prefix === p ? " on" : "")} aria-pressed={prefix === p} onClick={() => setPrefix(p)}>
              {p}
            </button>
          ))}
        </div>
        {prefix && <span className="muted small">Filters the {items.length} loaded entries.</span>}
      </div>
      <ErrorLine error={error} />
      <div className="card admin-table-card">
        <table className="admin-table admin-audit">
          <thead>
            <tr>
              <th>When</th>
              <th>Actor</th>
              <th>Action</th>
              <th>Entity</th>
              <th>Via</th>
              <th>Data</th>
            </tr>
          </thead>
          <tbody>
            {shown.map((it) => (
              <AuditRow key={it.id} it={it} actor={it.actor_user_id !== null ? users.get(it.actor_user_id) : undefined} />
            ))}
          </tbody>
        </table>
        {!loading && shown.length === 0 && <Empty>No entries.</Empty>}
        <div className="row end admin-more">
          {loading && <span className="muted small">Loading…</span>}
          {more && last && !loading && (
            <button type="button" className="button secondary small" onClick={() => page(last.id)}>
              Older entries
            </button>
          )}
        </div>
      </div>
    </AdminPage>
  );
}

function AuditRow({ it, actor }: { it: AuditItem; actor: AdminUser | undefined }) {
  const [open, setOpen] = useState(false);
  const json = it.data === null || it.data === undefined ? "" : JSON.stringify(it.data);
  const summary = summarize(it.data);
  return (
    <tr>
      <td className="small admin-nowrap" title={fmtTime(it.at)}>
        <TimeAgo ms={it.at} />
      </td>
      <td>
        {it.actor_user_id === null ? (
          <span className="pill pill-muted">system</span>
        ) : actor ? (
          <span className="user-badge">
            <Avatar user={actor} size="xs" />
            <span>{actor.username}</span>
          </span>
        ) : (
          <span className="muted">user #{it.actor_user_id}</span>
        )}
        {it.key_id !== null && (
          <span className="pill pill-key" title="Acted with an API key">
            ⚿ #{it.key_id}
          </span>
        )}
      </td>
      <td>
        <ActionPill action={it.action} />
      </td>
      <td className="small admin-nowrap">
        {it.entity_type}
        {it.entity_id !== null && <span className="muted"> #{it.entity_id}</span>}
      </td>
      <td>{it.listener ? <Pill color={colorFor("listener:" + it.listener)} outline>{it.listener}</Pill> : <span className="muted small">–</span>}</td>
      <td className="admin-data">
        {json && (
          <>
            {!open && (
              <button type="button" className="admin-data-sum" onClick={() => setOpen(true)} title="Show all">
                {summary}
              </button>
            )}
            {open && (
              <>
                <pre className="admin-json">{JSON.stringify(it.data, null, 2)}</pre>
                <button type="button" className="link small" onClick={() => setOpen(false)}>
                  less
                </button>
              </>
            )}
          </>
        )}
      </td>
    </tr>
  );
}

// summarize: "key: value" pairs, short values only, cut at ~120 characters.
function summarize(data: unknown): string {
  let s: string;
  if (data && typeof data === "object" && !Array.isArray(data)) {
    s = Object.entries(data as Record<string, unknown>)
      .map(([k, v]) => {
        const val = typeof v === "string" ? v : JSON.stringify(v);
        return `${k}: ${val && val.length > 40 ? val.slice(0, 40) + "…" : val}`;
      })
      .join(" · ");
  } else {
    s = JSON.stringify(data);
  }
  return s.length > 120 ? s.slice(0, 120) + "…" : s;
}
