import { useEffect, useState } from "react";
import { Link } from "wouter";
import { api, ApiError, fmtTime, type InboxItem } from "./api";
import { useApp } from "./App";
import { Markdown } from "./markdown";
import { Avatar, PriorityBadge, StatusBadge } from "./ui";
import { HelpButton } from "./help";
import { fromHere } from "./views/viewsApi";

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
          <h1>
            Inbox
            {items && items.length > 0 && <span className="count count-alert big">{items.length}</span>}
            <HelpButton topic="inbox" label="Help: inbox and notifications" />
          </h1>
          <p className="muted small">Gigs waiting for you, oldest question first.</p>
        </div>
      </header>
      {err && <p className="danger">{err}</p>}
      {items && items.length === 0 && <p className="muted">Nothing is waiting for you.</p>}
      {items?.map((it) => {
        const asker = it.question ? users.get(it.question.author_id) : undefined;
        return (
          <article key={it.gig.id} className="card inbox-item">
            <div className="inbox-head">
              <Link href={`/board/p/${it.gig.project_id}/g/${it.gig.key}`} state={fromHere().state} className="inbox-title">
                <span className="inbox-key">{it.gig.key}</span> {it.gig.title}
              </Link>
              <div className="chips">
                <span className="pill pill-ws">{it.workspace}</span>
                <span className="pill pill-muted">{it.project}</span>
                <StatusBadge name={it.gig.status_name} category={it.gig.status_category} />
                <PriorityBadge priority={it.gig.priority} />
                <span className="muted small">{fmtTime(it.gig.updated_at)}</span>
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
