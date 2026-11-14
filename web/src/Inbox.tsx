import { useEffect, useState } from "react";
import { Link } from "wouter";
import { api, ApiError, fmtTime, type InboxItem } from "./api";
import { useApp } from "./App";
import { Markdown } from "./markdown";
import { Avatar, PriorityBadge, StatusBadge } from "./ui";
import { HelpButton } from "./help";

// Everything waiting for a human, across the workspaces in reach, with the latest question.
export function InboxPage() {
  const { users, refresh } = useApp();
  const [items, setItems] = useState<InboxItem[] | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    api
      .inbox()
      .then((r) => {
        setItems(r.items);
        refresh();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't load the inbox."));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return (
    <div className="inbox">
      <header className="page-head">
        <div>
          <h1>Inbox<HelpButton topic="inbox" label="Help: inbox and notifications" /></h1>
          <p className="muted small">Tasks waiting for you, oldest question first.</p>
        </div>
        {items && <span className="count count-alert big">{items.length}</span>}
      </header>
      {err && <p className="danger">{err}</p>}
      {items && items.length === 0 && <p className="muted">Nothing is waiting for you.</p>}
      {items?.map((it) => {
        const asker = it.question ? users.get(it.question.author_id) : undefined;
        return (
          <article key={it.task.id} className="card inbox-item">
            <div className="inbox-head">
              <Link href={`/board/p/${it.task.project_id}/t/${it.task.key}`} className="inbox-title">
                <span className="inbox-key">{it.task.key}</span> {it.task.title}
              </Link>
              <div className="chips">
                <span className="pill pill-ws">{it.workspace}</span>
                <span className="pill pill-muted">{it.project}</span>
                <StatusBadge name={it.task.status_name} category={it.task.status_category} />
                <PriorityBadge priority={it.task.priority} />
                <span className="muted small">{fmtTime(it.task.updated_at)}</span>
              </div>
            </div>
            {it.question && !it.question.deleted && (
              <div className="question">
                <div className="comment-head">
                  {asker && <Avatar user={asker} />}
                  <strong>{it.question.author_username}</strong> <span className="muted small">asks</span>
                </div>
                <Markdown source={it.question.body_md} />
              </div>
            )}
          </article>
        );
      })}
    </div>
  );
}
