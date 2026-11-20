// "Make shared…": turns a workspace's schedule, gig template or errand template into a shared one in one
// step. It opens on the dry-run plan (the copies that are merged, the recurring gigs and errands that move,
// the copies left alone and why); the confirm button runs it.
import { useEffect, useState } from "react";
import { api, ApiError, type ShareKind, type SharePlan } from "./api";
import { Modal } from "./ui";
import { byWorkspace, doneMessage, KIND_NAME, planSummary } from "./makeShared";

export function MakeSharedDialog({ kind, id, name, onClose, onDone }: {
  kind: ShareKind; id: number; name: string; onClose: () => void; onDone: (message: string) => void;
}) {
  const [plan, setPlan] = useState<SharePlan | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let live = true;
    api
      .makeShared(kind, id, true)
      .then((p) => live && setPlan(p))
      .catch((e: unknown) => live && setErr(e instanceof ApiError ? e.message : "Couldn't plan it."));
    return () => {
      live = false;
    };
  }, [kind, id]);
  const run = () => {
    setBusy(true);
    setErr("");
    api
      .makeShared(kind, id, false)
      .then((p) => {
        onDone(doneMessage(p));
        onClose();
      })
      .catch((e: unknown) => {
        setErr(e instanceof ApiError ? e.message : "Couldn't make it shared.");
        setBusy(false);
      });
  };
  const word = KIND_NAME[kind];
  return (
    <Modal title={`Make “${name}” shared`} onClose={() => !busy && onClose()} wide>
      <div className="make-shared" aria-label="Make shared">
        <p className="small muted">
          Makes a shared {word} that every workspace can use. Identical copies in the workspaces are merged into it: what uses them moves to the shared one, and the copies are deleted. “{name}” is deleted too.
        </p>
        {!plan && !err && <p className="muted">Looking for copies…</p>}
        {plan && (
          <>
            <p role="status">{planSummary(plan)}</p>
            <h3 className="errand-h">Deleted once their users have moved</h3>
            <ul className="make-shared-list" aria-label="Copies">
              <li>
                <strong>{plan.source.name}</strong> <span className="muted small">{plan.source.workspace} · this one</span>
              </li>
              {plan.copies.map((c) => (
                <li key={c.id}>
                  <strong>{c.name}</strong> <span className="muted small">{c.workspace}</span>
                </li>
              ))}
            </ul>
            {plan.users.length > 0 && (
              <>
                <h3 className="errand-h">Moves to the shared {word}</h3>
                {byWorkspace(plan.users).map((g) => (
                  <div key={g.workspace}>
                    <div className="small muted">{g.workspace}</div>
                    <ul className="make-shared-list" aria-label={`Users in ${g.workspace}`}>
                      {g.users.map((u) => (
                        <li key={u.kind + u.id}>
                          {u.kind === "errand" ? "Errand" : "Recurring gig"} <strong>{u.label}</strong>
                        </li>
                      ))}
                    </ul>
                  </div>
                ))}
              </>
            )}
            {plan.skipped.length > 0 && (
              <>
                <h3 className="errand-h">Left alone</h3>
                <ul className="make-shared-list" aria-label="Skipped copies">
                  {plan.skipped.map((c) => (
                    <li key={c.id}>
                      <strong>{c.name}</strong> <span className="muted small">{c.workspace} · {c.reason}</span>
                    </li>
                  ))}
                </ul>
              </>
            )}
          </>
        )}
        {err && (
          <p className="danger" role="alert">
            {err}
          </p>
        )}
        <div className="row end">
          <button type="button" className="button secondary" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button type="button" className="button" onClick={run} disabled={!plan || busy}>
            Make shared
          </button>
        </div>
      </div>
    </Modal>
  );
}
