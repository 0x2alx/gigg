import { useCallback, useEffect, useRef, useState, type CSSProperties, type FormEvent } from "react";
import { usePanelOutsideClose } from "./panelClose";
import { api, ApiError, fmtTime, hasCap, resumeCommand, type Comment, type CustomField, type Recurrence, type Run, type Status, type Tag, type Gig, type User } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { assignable } from "./fields";
import { Markdown, setMentionUsers } from "./markdown";
import { MarkdownEditor } from "./MarkdownEditor";
import { ChecklistSection, CustomFieldsSection, DependenciesSection, Row, Section, SubgigsSection } from "./GigSections";
import { runFrom } from "./runsettings";
import { GigEditor } from "./GigEditor";
import { Clamp } from "./Clamp";
import { HistorySection } from "./GigHistory";
import { describeRule } from "./recurrence";
import { CATEGORY_COLOR } from "./theme";
import { Avatar, EXIT_MS, Pill, PriorityBadge, StatusBadge, GigBadges, UserBadge, reducedMotion } from "./ui";
import { WorkspaceBadge } from "./WorkspaceBadge";
import { Icon } from "./icons";

const kindBadge: Record<Comment["kind"], [string, string] | null> = {
  comment: null,
  report: ["report", "#0090FF"],
  question: ["question", "#E5484D"],
  resume_request: ["resume request", "#8E4EC6"],
  system: ["system", "#8B8D98"],
};

const runColor: Record<string, string> = {
  running: "#F76B15", done: "#46A758", needs_human: "#E5484D", error: "#E5484D", timeout: "#FFC53D", interrupted: "#8B8D98", parked: "#3E63DD",
};

const FULL_KEY = "gigg.panel.full";

export function GigPanel({ gigKey, ws, statuses, labels, canWrite, onClose, onChanged, onMoved, onOpenSchedule }: {
  gigKey: string;
  ws: WorkspaceTree;
  statuses: Status[];
  labels: Tag[];
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
  onMoved: (t: Gig) => void;
  onOpenSchedule?: (id: number) => void;
}) {
  const { users, members, me } = useApp();
  setMentionUsers(users);
  const [gig, setGig] = useState<Gig | null>(null);
  const [fields, setFields] = useState<CustomField[]>([]);
  const [comments, setComments] = useState<Comment[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [err, setErr] = useState("");
  const [schedule, setSchedule] = useState<Recurrence | "gone" | null>(null);
  // full: the panel covers the whole view under the app bar. Every open starts as the side panel
  // (it slides in, and a click beside it closes it); the expand button widens this one only.
  const [full, setFull] = useState(false);
  const toggleFull = () => setFull((f) => !f);
  useEffect(() => {
    try {
      localStorage.removeItem(FULL_KEY); // the choice older versions kept per browser
    } catch {
      /* storage unavailable: nothing kept */
    }
  }, []);

  const shownId = useRef<number | null>(null); // the gig on screen, so a new one never shows the last one's runs
  // the gig asked for now: a slow answer for the previous one (after a quick switch) is dropped
  const keyRef = useRef(gigKey);
  keyRef.current = gigKey;
  // keepErr: a reload after a failed save keeps that save's message on screen
  const load = useCallback((keepErr?: boolean) => {
    const asked = gigKey;
    api
      .gig(gigKey)
      .then((t) => {
        if (keyRef.current !== asked) return;
        if (shownId.current !== t.id) {
          shownId.current = t.id;
          setComments([]);
          setRuns([]);
        }
        setGig(t);
        if (keepErr !== true) setErr("");
        return Promise.all([api.comments(t.id), api.runs(t.id)]).then(([c, r]) => {
          if (keyRef.current !== asked) return;
          setComments(c.items);
          setRuns(r.items);
        });
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't load the gig."));
  }, [gigKey]);
  useEffect(load, [load]);

  useEffect(() => {
    let live = true;
    api.fields(ws.id).then((r) => live && setFields(r.items), () => live && setFields([]));
    return () => {
      live = false;
    };
  }, [ws.id]);

  // Live refresh: another client changed this gig (or something on it), reload it (debounced).
  const gigId = gig?.id ?? null;
  const loadRef = useRef(load);
  loadRef.current = load;
  useEffect(() => {
    if (gigId === null) return;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const on = (e: Event) => {
      const d = (e as CustomEvent<{ workspaceId?: number; item?: { action?: string; entity_type?: string; entity_id?: number | null; data?: unknown } }>).detail;
      const it = d?.item;
      if (!it) return;
      const data = (it.data ?? {}) as { gig_id?: unknown };
      const hit = (it.entity_id === gigId && (!it.entity_type || it.entity_type === "gig")) || data.gig_id === gigId;
      if (!hit) return;
      if (timer) clearTimeout(timer);
      // a progress note changes the runs only; anything else reloads the gig, its comments and runs
      timer = setTimeout(() => (it.action === "run.progress" ? api.runs(gigId).then((r) => setRuns(r.items), () => undefined) : loadRef.current(true)), 400);
    };
    window.addEventListener("gigg:activity", on);
    return () => {
      window.removeEventListener("gigg:activity", on);
      if (timer) clearTimeout(timer);
    };
  }, [gigId]);

  const recurrenceId = gig?.recurrence_id ?? null;
  useEffect(() => {
    setSchedule(null);
    if (!recurrenceId) return;
    let live = true;
    api
      .recurrence(recurrenceId)
      .then((r) => live && setSchedule(r))
      .catch(() => live && setSchedule("gone"));
    return () => {
      live = false;
    };
  }, [recurrenceId]);

  // flashDirty: when a field in the panel holds unsaved text, scroll to it, focus it and flash it
  // (instead of closing the panel and losing the text). True when there was one.
  const panelRef = useRef<HTMLElement>(null);
  const flashDirty = useCallback((): boolean => {
    const el = panelRef.current?.querySelector<HTMLElement>("[data-dirty]");
    if (!el) return false;
    // in a folded section (an answer typed in Agent session): unfold it first, then show the field
    const folded = el.closest<HTMLElement>(".tsec-body[hidden]");
    if (folded) {
      folded.parentElement?.querySelector<HTMLButtonElement>(".tsec-head")?.click();
      setTimeout(() => flashDirtyRef.current(), 0);
      return true;
    }
    el.classList.remove("dirty-flash");
    void el.offsetWidth; // restart the animation
    el.classList.add("dirty-flash");
    el.scrollIntoView({ block: "nearest", behavior: "smooth" });
    (el.matches("input, textarea") ? el : el.querySelector<HTMLElement>("textarea, input"))?.focus({ preventScroll: true });
    setTimeout(() => el.classList.remove("dirty-flash"), 1300);
    return true;
  }, []);
  const flashDirtyRef = useRef(flashDirty);
  flashDirtyRef.current = flashDirty;
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  // closing slides the panel out first; opening another gig meanwhile cancels it
  // (the closing state names the gig being closed, so a newly opened gig never renders as closing)
  const [closingKey, setClosingKey] = useState<string | null>(null);
  const closing = closingKey === gigKey;
  const closeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const close = useCallback(() => {
    if (reducedMotion()) return closeRef.current();
    if (closeTimer.current) return;
    setClosingKey(keyRef.current);
    closeTimer.current = setTimeout(() => {
      closeTimer.current = undefined;
      closeRef.current();
    }, EXIT_MS);
  }, []);
  useEffect(() => {
    clearTimeout(closeTimer.current);
    closeTimer.current = undefined;
    setClosingKey(null);
  }, [gigKey]);
  useEffect(() => () => clearTimeout(closeTimer.current), []);

  // Esc closes (not over a modal; an open inline editor closes first; unsaved text is flashed).
  useEffect(() => {
    const esc = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented || document.querySelector('[aria-modal="true"], .modal-backdrop')) return;
      if (flashDirty()) return;
      // an open title/description editor (no changes, else flashed above) closes first
      const cancel = panelRef.current?.querySelector<HTMLButtonElement>('form[aria-label="Edit gig"] [data-cancel]');
      if (cancel) cancel.click();
      else close();
    };
    document.addEventListener("keydown", esc);
    return () => document.removeEventListener("keydown", esc);
  }, [flashDirty, close]);

  // a click outside closes it, like ✕ (not while a field holds unsaved text)
  usePanelOutsideClose(panelRef, close, flashDirty);

  // Focus: the panel takes focus when it opens or shows another gig (keyboard users land in it);
  // on close, focus goes back to what had it before (the card), if that is still on the page.
  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const panel = panelRef.current;
    return () => {
      const active = document.activeElement;
      if (opener?.isConnected && (!active || active === document.body || panel?.contains(active))) opener.focus({ preventScroll: true });
    };
  }, []);
  useEffect(() => {
    const panel = panelRef.current;
    if (panel && !panel.contains(document.activeElement)) panel.focus({ preventScroll: true });
  }, [gigKey]);

  // act: saves a change; resolves to whether it worked (the error shows at the top)
  const act = (p: Promise<Gig>, after?: (t: Gig) => void): Promise<boolean> => {
    const asked = gigKey;
    return p
      .then((t) => {
        if (keyRef.current !== asked) {
          onChanged(); // saved, but another gig is on screen now: only the board refreshes
          return true;
        }
        setGig(t);
        setErr("");
        onChanged();
        after?.(t);
        load();
        return true;
      })
      .catch((e: unknown) => {
        lastErr.current = e;
        if (keyRef.current !== asked) return false; // not this gig's error
        setErr(
          e instanceof ApiError && e.status === 412
            ? "This gig changed while you were editing. Save again to replace it with your text, or Cancel to see the new version."
            : e instanceof ApiError
              ? e.message
              : "That didn't work.",
        );
        return false;
      });
  };

  const [cloning, setCloning] = useState(false);
  const lastErr = useRef<unknown>(null); // the last failed save's error (saveText tells a conflict apart)

  // labels and assignees save on each click: show the change at once and send the saves one at a
  // time, each with the latest list (two quick clicks never undo each other)
  const [liveLabels, setLiveLabels] = useLiveList(gig?.id ?? 0, gig ? gig.labels.map((l) => l.name) : [], (v) => act(api.patchGig(gig!.id, { labels: v })));
  const [liveAssignees, setLiveAssignees] = useLiveList(gig?.id ?? 0, gig?.assignees ?? [], (v) => act(api.patchGig(gig!.id, { assignees: v })));

  if (!gig) {
    return (
      <aside ref={panelRef} tabIndex={-1} className={"panel" + (full ? " panel-full" : "") + (closing ? " closing" : "")} inert={closing || undefined} aria-label="Gig">
        <button type="button" className="icon-button close" onClick={close} aria-label="Close">
          <Icon name="x" />
        </button>
        <p className={err ? "danger" : "muted"}>{err || "Loading…"}</p>
      </aside>
    );
  }

  const cmd = resumeCommand(gig.session_id);
  const lastRun = runs[0];
  // the agent is waiting for an answer: the Agent session (where the answer box is) opens, also when
  // this happens while the panel is open; the id makes each new question open it again
  const waitingRun = gig.last_run?.status === "needs_human" ? gig.last_run.id : 0;
  // the newest question of a waiting gig is never folded: it's the thing to answer
  const openQuestion = gig.status_category === "waiting" ? [...comments].reverse().find((c) => c.kind === "question" && !c.deleted)?.id : undefined;
  const creator = users.get(gig.created_by);
  const candidates = assignable(users, members.get(ws.id) ?? []);
  const status = statuses.find((s) => s.id === gig.status_id);
  const overdue = gig.due_at !== null && gig.due_at < Date.now() && gig.status_category !== "done";
  const can = (cap: string) => canWrite && hasCap(me, cap, gig.workspace_id, gig.project_id);
  const onGig = (t: Gig) => {
    setGig(t);
    setErr("");
    onChanged();
  };

  return (
    <aside ref={panelRef} tabIndex={-1} className={"panel" + (full ? " panel-full" : "") + (closing ? " closing" : "")} inert={closing || undefined} aria-label={`Gig ${gig.key}`} style={gig.color ? ({ "--panel-c": gig.color } as CSSProperties) : undefined}>
      <div className="tp-top">
        <WorkspaceBadge ws={ws} />
        <span className="pill pill-key tp-key" title={`Gig key ${gig.key}`}>
          {gig.key}
        </span>
        <div className="tp-badges">
          <StatusBadge name={gig.status_name} category={gig.status_category} color={status?.color} />
          <PriorityBadge priority={gig.priority} />
          <GigBadges gig={gig} />
          {!!gig.blocked_by && (
            <span className="pill pill-danger" title="Agents won't pick it up until its blockers are done">
              <Icon name="lock" /> Blocked by {gig.blocked_by} open
            </span>
          )}
          {!!gig.archived_at && <span className="pill pill-muted"><Icon name="archive" /> archived</span>}
          {gig.deleted && <span className="pill pill-danger">in trash</span>}
        </div>
        <button
          type="button"
          className="icon-button tp-expand"
          onClick={toggleFull}
          aria-pressed={full}
          aria-label={full ? "Back to the side panel" : "Expand to the full view"}
          title={full ? "Back to the side panel" : "Expand to the full view"}
        >
          <Icon name={full ? "shrink" : "expand"} />
        </button>
        <button type="button" className="icon-button close" onClick={close} aria-label="Close" title="Close (Esc)">
          <Icon name="x" />
        </button>
      </div>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      {!!gig.archived_at && (
        <div className="banner banner-muted" role="status">
          <Icon name="archive" /> Archived {fmtTime(gig.archived_at)}: hidden from the board and lists.
          {can("archive_gigs") && (
            <button type="button" className="button secondary small" onClick={() => act(api.unarchiveGig(gig.id))}>
              <Icon name="undo" /> Unarchive
            </button>
          )}
        </div>
      )}
      {!!gig.blocked_by && (
        <div className="banner banner-danger" role="status">
          <Icon name="lock" /> Blocked by {gig.blocked_by} open gig{gig.blocked_by === 1 ? "" : "s"}
          {gig.blocked_by_subgigs ? ` (${gig.blocked_by_subgigs === gig.blocked_by ? (gig.blocked_by === 1 ? "its subgig" : "its subgigs") : `${gig.blocked_by_subgigs} of them its subgig${gig.blocked_by_subgigs === 1 ? "" : "s"}`})` : ""}: agents won't pick it up until {gig.blocked_by === 1 ? "it is" : "they are"} done.
        </div>
      )}

      <GigEditor
        key={gig.id}
        mode="gig"
        v={{
          title: gig.title,
          description_md: gig.description_md,
          priority: gig.priority,
          labels: liveLabels,
          assignees: liveAssignees,
          color: gig.color,
          start_at: gig.start_at,
          due_at: gig.due_at,
          not_after: gig.not_after ?? null,
          project: gig.project_id,
          status: gig.status_id,
          run: runFrom(gig),
        }}
        set={(c) => {
          if (c.project !== undefined) act(api.moveGig(gig.id, { project: c.project }), (t) => onMoved(t));
          else if (c.status !== undefined) act(api.moveGig(gig.id, { status: c.status }));
          else if (c.labels) setLiveLabels(c.labels);
          else if (c.assignees) setLiveAssignees(c.assignees);
          else act(api.patchGig(gig.id, c));
        }}
        setRun={(c) => act(api.patchGig(gig.id, c))}
        textVersion={gig.version}
        saveText={(changes, version) => {
          // only what the editor changed, onto the version it opened on: a title fix never puts back a
          // description someone (or an agent) rewrote meanwhile, and a rewrite of the same text is refused
          if (Object.keys(changes).length === 0) return Promise.resolve(true);
          lastErr.current = null;
          return act(api.patchGig(gig.id, changes, version)).then((ok) => {
            if (ok) return true;
            load(true);
            return lastErr.current instanceof ApiError && lastErr.current.status === 412 ? "conflict" : false;
          });
        }}
        canWrite={canWrite}
        ws={ws}
        statuses={statuses}
        currentStatus={{ name: gig.status_name, category: gig.status_category }}
        labelTags={gig.labels}
        dates
        knownLabels={labels}
        candidates={candidates}
        alwaysLocked={!can("always_run")}
        timeoutLock={can("run_timeout") ? undefined : "all"}
        canTools={can("allowed_tools")}
        overdue={overdue}
        aliases={gig.aliases}
        people={
          <>
          {(creator || gig.assignees.length > 0) && (
              <div className="tp-people" aria-label="People">
                {creator && <Avatar user={creator} size="l" />}
                {gig.assignees.length > 0 && <span className="arrow" aria-hidden="true"><Icon name="arrow-right" /></span>}
                {gig.assignees.map((id) => {
                  const u = users.get(id);
                  return u ? <Avatar key={id} user={u} size="l" /> : null;
                })}
                <span className="muted small">
                  {creator ? `${creator.display_name || creator.username}` : ""}
                  {gig.assignees.length > 0 ? ` → ${gig.assignees.map((id) => users.get(id)?.display_name || users.get(id)?.username || "#" + id).join(", ")}` : ""}
                </span>
              </div>
            )}
          </>
        }
        leftSecs={
          <>
            <ChecklistSection key={gig.id} gig={gig} canEdit={can("edit_gigs")} onGig={onGig} />
            <SubgigsSection gig={gig} wsId={ws.id} canCreate={can("create_gigs") && !gig.deleted} canEdit={can("edit_gigs") && !gig.deleted} assignable={candidates.map((u) => u.id)} onOpen={onMoved} onGig={onGig} onChanged={onChanged} />
          </>
        }
        mainEnd={
          <section className="tp-box comments" aria-label="Comments">
            <h3>
              <Icon name="comment" /> Comments <span className="count">{comments.length}</span>
            </h3>
            {comments.length === 0 && <p className="muted">No comments yet.</p>}
            {comments.map((c) => {
              const author = users.get(c.author_id ?? 0);
              const kb = kindBadge[c.kind];
              return (
                <article key={c.id} className={"comment kind-" + c.kind}>
                  <div className="comment-head">
                    {author ? <Avatar user={author} size="l" /> : null}
                    <strong title={`@${c.author_username}`}>{author?.display_name || c.author_username}</strong>
                    {c.author_kind === "agent" && <span className="pill pill-agent">agent</span>}
                    {kb && <Pill color={kb[1]}>{kb[0]}</Pill>}
                    <span className="muted small">{fmtTime(c.created_at)}</span>
                  </div>
                  {c.deleted ? (
                    <p className="muted">Deleted.</p>
                  ) : (
                    <Clamp label="comment" off={c.id === openQuestion}>
                      <Markdown source={c.body_md} />
                    </Clamp>
                  )}
                </article>
              );
            })}
            {ws.role !== "viewer" && <AddComment gigId={gig.id} users={candidates} onDone={load} />}
          </section>
        }
        whenEnd={
          <>
              {!!gig.missed_at && (
                <Row tip="Its window closed before anyone started it" label="Missed" wide className="missed-row">
                  <span className="pill pill-danger">missed {fmtTime(gig.missed_at)}</span>
                  {canWrite && (
                    <>
                      <button type="button" className="button secondary small" onClick={() => act(api.patchGig(gig.id, { not_after: Date.now() + 3_600_000 }))}>
                        ↻ Reopen window 1 h
                      </button>
                      <button type="button" className="button secondary small" onClick={() => act(api.patchGig(gig.id, { not_after: Date.now() + 86_400_000 }))}>
                        ↻ 1 day
                      </button>
                    </>
                  )}
                </Row>
              )}
              {!!gig.recurrence_id && (
                <Row tip="Made by a recurring gig: open it to see its template and schedule" label="Repeats" wide className="schedule-ref">
                  {schedule === "gone" ? (
                    <span className="muted">from a deleted schedule</span>
                  ) : schedule ? (
                    <>
                      <button type="button" className="link" onClick={() => onOpenSchedule?.(schedule.id)} disabled={!onOpenSchedule}>
                        ⟳ from schedule “{schedule.title}”
                      </button>
                      <span className="muted small">{describeRule(schedule)}</span>
                      {schedule.paused && <span className="pill pill-danger">paused</span>}
                    </>
                  ) : (
                    <span className="muted">…</span>
                  )}
                  {!!gig.occurrence_at && <span className="pill pill-muted">occurrence {fmtTime(gig.occurrence_at)}</span>}
                </Row>
              )}
          </>
        }
        detailsEnd={
          <>
              <Row label="Created by" wide className="created">
                <UserBadge user={creator} /> <span className="muted small">{fmtTime(gig.created_at)}</span>
              </Row>
              <Row label="Updated" wide className="muted small">
                {fmtTime(gig.updated_at)}
              </Row>
          </>
        }
        sideMid={
          <>
          {(cmd || runs.length > 0) && (
            <div className="tsecs" key={"agent-" + gig.id}>
              <Section
                label="Agent session"
                title="Agent session"
                icon="play"
                // folded, except when the agent is waiting for an answer: the answer box is in here
                defaultOpen={waitingRun > 0}
                openOn={waitingRun}
                keepMounted // a half-typed answer survives folding
                badges={
                  lastRun && (
                    <>
                      <Pill color={runColor[lastRun.status] ?? "#8B8D98"}>{lastRun.status.replace("_", " ")}</Pill>
                      {runs.length > 0 && (
                        <span className="tsec-count">
                          {runs.length} run{runs.length === 1 ? "" : "s"} · ${gig.cost_usd_total.toFixed(2)}
                        </span>
                      )}
                    </>
                  )
                }
              >
                {cmd && (
                  <div className="cmd">
                    <code>{cmd}</code>
                    <button type="button" className="button secondary small" onClick={() => void navigator.clipboard?.writeText(cmd)}>
                      Copy
                    </button>
                  </div>
                )}
                <div className="runs">
                  {runs.slice(0, 5).map((r) => (
                    <div key={r.id} className="run">
                      <Pill color={runColor[r.status] ?? "#8B8D98"}>{r.status.replace("_", " ")}</Pill>
                      <span className="pill pill-muted">{r.mode || "run"} #{r.attempt}</span>
                      <span className="small">{r.worker_username}</span>
                      <span className="muted small">{fmtTime(r.started_at)}</span>
                      {r.duration_ms !== null && <span className="pill pill-muted">{Math.round(r.duration_ms / 1000)} s</span>}
                      {r.cost_usd !== null && <span className="pill pill-muted">${r.cost_usd.toFixed(2)}</span>}
                      {r.num_turns !== null && <span className="pill pill-muted">{r.num_turns} turns</span>}
                      {r.status === "running" && r.progress_note && (
                        <span className="small">
                          {r.progress_pct !== null && `${r.progress_pct}% · `}
                          {r.progress_note}
                        </span>
                      )}
                    </div>
                  ))}
                </div>
                {lastRun?.transcript_path && (
                  <p className="small">
                    <span className="muted">Transcript </span>
                    <code className="path">{lastRun.transcript_path}</code>
                  </p>
                )}
                {canWrite && gig.session_id && <Resume gig={gig} onDone={(t) => act(Promise.resolve(t))} />}
              </Section>
            </div>
          )}
          </>
        }
        rightSecs={
          <>
            <DependenciesSection gig={gig} wsId={ws.id} canEdit={can("edit_gigs")} onGig={onGig} onOpen={onMoved} />
            <CustomFieldsSection gig={gig} fields={fields} canEdit={canWrite} onGig={onGig} />
            <HistorySection gig={gig} />
          </>
        }
        sideEnd={
          <>
          {canWrite && (
            <div className="tp-actions">
              <button
                type="button"
                className="button secondary"
                disabled={cloning}
                onClick={() => {
                  if (cloning) return;
                  setCloning(true); // a double click makes one copy
                  void act(api.cloneGig(gig.id), (t) => onMoved(t)).finally(() => setCloning(false));
                }}
              >
                <Icon name="copy" /> Clone
              </button>
              {can("archive_gigs") &&
                !gig.deleted &&
                (gig.archived_at ? (
                  <button type="button" className="button secondary" onClick={() => act(api.unarchiveGig(gig.id))}>
                    <Icon name="undo" /> Unarchive
                  </button>
                ) : (
                  <button type="button" className="button secondary" onClick={() => act(api.archiveGig(gig.id))}>
                    <Icon name="archive" /> Archive
                  </button>
                ))}
              {gig.deleted ? (
                <button type="button" className="button secondary" onClick={() => act(api.restoreGig(gig.id))}>
                  <Icon name="undo" /> Restore
                </button>
              ) : (
                <button type="button" className="button danger-button" onClick={() => act(api.deleteGig(gig.id))}>
                  <Icon name="trash" /> Delete
                </button>
              )}
            </div>
          )}
          </>
        }
      />
    </aside>
  );
}

function Resume({ gig, onDone }: { gig: Gig; onDone: (t: Gig) => void }) {
  const [text, setText] = useState("");
  const [err, setErr] = useState("");
  const queued = gig.resume_requested_at !== null && gig.status_category === "todo";
  const [busy, setBusy] = useState(false);
  const sending = useRef(false); // a double click (before the re-render disables the button) sends once
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (sending.current) return;
    sending.current = true;
    setBusy(true);
    api
      .resumeGig(gig.id, text)
      .then((t) => {
        setText("");
        onDone(t);
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't queue the resume."))
      .finally(() => {
        sending.current = false;
        setBusy(false);
      });
  };
  return (
    <form className="resume" onSubmit={submit} aria-label="Resume">
      {queued && <Pill color={CATEGORY_COLOR.todo!}>resume queued for the runner's next pass</Pill>}
      <textarea placeholder="Answer or instructions for the next run (Markdown)" data-dirty={text.trim() ? "" : undefined} value={text} onChange={(e) => setText(e.target.value)} rows={3} />
      {err && <p className="danger">{err}</p>}
      <button type="submit" className="button" disabled={busy}>
        ▶ Resume
      </button>
      <p className="muted small">gigg records the request and moves the gig back to To-Do; the runner continues the same session.</p>
    </form>
  );
}

function AddComment({ gigId, users, onDone }: { gigId: number; users: User[]; onDone: () => void }) {
  const [text, setText] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const sending = useRef(false); // Ctrl+Enter twice or a double click posts once
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (sending.current) return;
    sending.current = true;
    setBusy(true);
    api
      .addComment(gigId, text)
      .then(() => {
        setText("");
        onDone();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't post the comment."))
      .finally(() => {
        sending.current = false;
        setBusy(false);
      });
  };
  return (
    <form className="add-comment" onSubmit={submit} aria-label="Add comment">
      <MarkdownEditor
        placeholder="Comment (Markdown) · @ to mention"
        value={text}
        onChange={setText}
        users={users}
        minRows={3}
        required
        aria-label="Comment"
        dirty={!!text.trim()}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
            e.preventDefault();
            e.currentTarget.form?.requestSubmit();
          }
        }}
      />
      {err && <p className="danger">{err}</p>}
      <button type="submit" className="button" disabled={busy}>
        Comment
      </button>
    </form>
  );
}

// useLiveList: a list that saves on each change. The new value shows at once; saves go one at a time,
// each sending the latest value, and the server's value shows again once they're done. key: the
// gig (another gig starts clean).
function useLiveList<T>(key: number, server: T[], save: (v: T[]) => Promise<unknown>): [T[], (v: T[]) => void] {
  const [local, setLocal] = useState<T[] | null>(null);
  const saveRef = useRef(save);
  saveRef.current = save;
  const busy = useRef(false);
  const next = useRef<T[] | null>(null);
  useEffect(() => {
    setLocal(null);
    next.current = null;
  }, [key]);
  const flush = () => {
    const v = next.current;
    if (!v) {
      busy.current = false;
      setLocal(null);
      return;
    }
    next.current = null;
    busy.current = true;
    void saveRef.current(v).finally(flush);
  };
  const set = (v: T[]) => {
    setLocal(v);
    next.current = v;
    if (!busy.current) flush();
  };
  return [local ?? server, set];
}
