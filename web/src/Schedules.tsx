// Every schedule in reach, across the workspaces (an optional workspace filter): named timing (rule,
// window, create-early) that recurring gigs pair with templates. List, new, edit (every recurring gig
// on it moves), delete when unused. Minutes/hours timings belong to errands and are left out (the Errands page has them).
// Shared schedules (every workspace's) come with each workspace's list: shown once, under any filter, changed
// only with the Shared library permission; a workspace that retimes a recurring gig on one moves it to a schedule of its own.
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { api, ApiError, canShare, canWriteIn, hasCap, isGigSchedule, type GigSchedule as Schedule } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { optionOf, PlaceFilter, placeMatch, type Place } from "./places";
import { RepeatFields } from "./RepeatFields";
import { defaultRepeat, describeRule, fmtLead, localTZ, sameAsRule, repeatProblem, repeatRule, type RepeatState } from "./recurrence";
import { repeatOfSchedule, scheduleBody } from "./scheduleTiming";
import { ClearFilters, Modal } from "./ui";
import { HelpButton } from "./help";
import { Icon } from "./icons";
import { copyJson, ImportJsonModal, importTargets } from "./ImportJsonModal";
import { MakeSharedDialog } from "./MakeSharedDialog";


export function SchedulesModal({ initialPlace = "", onClose, onChanged }: {
  initialPlace?: Place; // "" or "w:<id>" (a project filter shows its workspace's)
  onClose: () => void;
  onChanged: (message?: string) => void;
}) {
  const { tree, me, users } = useApp();
  const [items, setItems] = useState<Schedule[] | null>(null);
  const [place, setPlace] = useState<Place>(() => optionOf(initialPlace, tree, false));
  const [err, setErr] = useState("");
  const [editing, setEditing] = useState<Schedule | "new" | null>(null);
  const [confirm, setConfirm] = useState<number | null>(null);
  const [importing, setImporting] = useState(false);
  const [sharing, setSharing] = useState<Schedule | null>(null); // Make shared…
  const closeEdit = useCallback(() => setEditing(null), []);
  const wsKey = tree.map((w) => w.id).join(",");
  const loadSeq = useRef(0);
  const load = useCallback(() => {
    const seq = ++loadSeq.current;
    Promise.allSettled(tree.map((w) => api.schedules(w.id))).then((res) => {
      if (seq !== loadSeq.current) return; // a newer load is on its way
      const all: Schedule[] = [];
      const bad: string[] = [];
      res.forEach((r, i) => (r.status === "fulfilled" ? all.push(...r.value.items.filter(isGigSchedule)) : bad.push(tree[i]!.name)));
      const seen = new Set<number>(); // a shared one comes with every workspace's list
      setItems(all.filter((x) => !seen.has(x.id) && !!seen.add(x.id)).sort((a, b) => Number(!!a.shared) - Number(!!b.shared) || a.name.localeCompare(b.name)));
      setErr(bad.length ? `Couldn't load the schedules of ${bad.join(", ")}.` : "");
    });
  }, [wsKey]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(load, [load]);
  const wsOf = useCallback((id: number) => tree.find((w) => w.id === id), [tree]);
  const canWriteWs = useCallback((w: WorkspaceTree) => canWriteIn(w) && hasCap(me, "recurring_gigs", w.id), [me]);
  const shown = useMemo(() => (items ?? []).filter((x) => x.shared || placeMatch(place, tree, x.workspace_id, null)), [items, place, tree]);
  const writable = useMemo(() => tree.filter(canWriteWs), [tree, canWriteWs]);
  const startWs = (place.startsWith("w:") ? writable.find((w) => w.id === Number(place.slice(2))) : undefined) ?? writable[0] ?? (canShare(me) ? tree[0] : undefined);

  const remove = (s: Schedule) => {
    setErr("");
    api
      .deleteSchedule(s.id)
      .then(() => {
        setItems((xs) => xs?.filter((x) => x.id !== s.id) ?? null);
        setConfirm(null);
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't delete the schedule."));
  };

  const copy = (s: Schedule) => copyJson("schedule", s, users).then(onChanged, (e: unknown) => setErr(e instanceof Error ? e.message : "Couldn't copy."));
  if (importing)
    return (
      <ImportJsonModal
        kinds={["schedule"]}
        initialPlace={place}
        onClose={() => setImporting(false)}
        onDone={(msg) => {
          load();
          onChanged(msg);
        }}
      />
    );
  const editWs = editing && editing !== "new" ? (editing.shared ? (writable[0] ?? tree[0]) : wsOf(editing.workspace_id)) : startWs;
  if (editing && editWs) {
    return (
      <ScheduleModal
        ws={editWs}
        choices={editing === "new" ? writable : undefined}
        canShare={editing === "new" && canShare(me)}
        schedule={editing === "new" ? undefined : editing}
        onClose={closeEdit}
        onSaved={(msg) => {
          load();
          onChanged(msg);
        }}
      />
    );
  }
  const tz = localTZ();
  return (
    <Modal title="Schedules" onClose={onClose} wide help={<HelpButton topic="recurring" label="Help: templates, schedules and recurring gigs" />}>
      <p className="muted small">When recurring gigs happen. Pair one with a template in ⟳ Recurring (or with Repeat in a new gig); changing a schedule moves every recurring gig on it.</p>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      <div className="list-bar">
        <PlaceFilter value={place} onChange={setPlace} tree={tree} projects={false} />
        <ClearFilters active={place !== ""} onClear={() => setPlace("")} />
        {items && <span className="muted small">{shown.length === items.length ? `${items.length} schedule${items.length === 1 ? "" : "s"}` : `${shown.length} of ${items.length}`}</span>}
        {startWs && (
          <button type="button" className="button small list-new" onClick={() => setEditing("new")}>
            + New schedule
          </button>
        )}
        {importTargets("schedule", tree, me).length > 0 && (
          <button type="button" className={"button secondary small" + (startWs ? "" : " list-new")} onClick={() => setImporting(true)}>
            Import JSON
          </button>
        )}
      </div>
      {items === null && !err && <p className="muted">Loading…</p>}
      {items !== null && shown.length === 0 && <p className="muted">{items.length === 0 ? "No schedules yet." : "None here. Show everything to see the others."}</p>}
      {shown.length > 0 && (
        <ul className="schedules" aria-label="Schedules">
          {shown.map((s) => {
            const sws = s.shared ? undefined : wsOf(s.workspace_id);
            const canWrite = s.shared ? canShare(me) : !!sws && canWriteWs(sws);
            return (
            <li key={s.id} className="schedule rec-card">
              <div className="schedule-top">
                <span className="pill pill-accent"><Icon name="clock" /></span>
                <strong className="schedule-title">{s.name}</strong>
                <span className="rec-state">
                  {s.tz !== tz && <span className="pill pill-ws">{s.tz}</span>}
                  {s.shared ? (
                    <span className="pill pill-shared" data-tip="Offered in every workspace; changed with the Shared library permission">
                      Shared
                    </span>
                  ) : (
                    sws && (
                      <span className="pill pill-muted" title={`Only in ${sws.name}`}>
                        {sws.name}
                      </span>
                    )
                  )}
                </span>
              </div>
              {/* the timing, unless the name is only the timing already */}
              {!sameAsRule(s.name, describeRule(s)) && <div className="schedule-rule">{describeRule(s)}</div>}
              <div className="chips">
                <span className={"pill " + (s.uses > 0 ? "pill-accent" : "pill-muted")}>
                  ⟳ {s.uses} recurring gig{s.uses === 1 ? "" : "s"}
                </span>
                {!!s.errand_uses && (
                  <span className="pill pill-accent" data-tip="Errands run on this schedule too: changing it moves them">
                    <Icon name="clock" /> {s.errand_uses} errand{s.errand_uses === 1 ? "" : "s"}
                  </span>
                )}
                {s.lead_minutes > 0 && (
                  <span className="pill pill-muted" title="Each gig is created this long before its window opens">
                    {fmtLead(s.lead_minutes)} early
                  </span>
                )}
              </div>
              <div className="rec-foot">
                <div className="schedule-actions">
                  {canWrite && confirm === s.id ? (
                    <>
                      <span className="small">Delete this schedule?</span>
                      <button type="button" className="button danger-button small" onClick={() => remove(s)}>
                        Delete
                      </button>
                      <button type="button" className="button secondary small" onClick={() => setConfirm(null)}>
                        Keep
                      </button>
                    </>
                  ) : (
                    <>
                      <button type="button" className="button secondary small" onClick={() => void copy(s)} aria-label={`Copy ${s.name} as JSON`}>
                        <Icon name="copy" /> Copy JSON
                      </button>
                      {!s.shared && canShare(me) && (
                        <button type="button" className="button secondary small" onClick={() => setSharing(s)} aria-label={`Make ${s.name} shared`}>
                          <Icon name="share" /> Make shared…
                        </button>
                      )}
                      {canWrite && (
                        <>
                          <button type="button" className="button secondary small" onClick={() => setEditing(s)} aria-label={`Edit ${s.name}`}>
                            <Icon name="pencil" /> Edit
                          </button>
                          <button
                            type="button"
                            className="button danger-button small"
                            onClick={() => setConfirm(s.id)}
                            disabled={s.uses > 0 || !!s.errand_uses}
                            title={s.uses > 0 || s.errand_uses ? "Recurring gigs or errands use this schedule: delete them or give them another schedule first" : undefined}
                            aria-label={`Delete ${s.name}`}
                          >
                            <Icon name="trash" /> Delete
                          </button>
                        </>
                      )}
                    </>
                  )}
                </div>
              </div>
            </li>
            );
          })}
        </ul>
      )}
      {sharing && (
        <MakeSharedDialog
          kind="schedule"
          id={sharing.id}
          name={sharing.name}
          onClose={() => setSharing(null)}
          onDone={(msg) => {
            load();
            onChanged(msg);
          }}
        />
      )}
    </Modal>
  );
}

// ScheduleModal: new or edit. canShare (Shared library, new): the workspace list offers "Shared: every
// workspace" (wsId 0).
function ScheduleModal({ ws: ws0, choices, canShare, schedule, onClose, onSaved }: {
  ws: WorkspaceTree; choices?: WorkspaceTree[]; canShare?: boolean; schedule?: Schedule; onClose: () => void; onSaved: (msg?: string) => void;
}) {
  // with Shared library and no workspace to make schedules in, a new schedule starts (and stays) shared
  const [wsId, setWsId] = useState(canShare && choices?.length === 0 ? 0 : ws0.id);
  const ws = choices?.find((w) => w.id === wsId) ?? ws0;
  const shared = schedule ? !!schedule.shared : canShare && wsId === 0;
  const [name, setName] = useState(schedule?.name ?? "");
  const [repeat, setRepeat] = useState<RepeatState>(() => (schedule ? repeatOfSchedule(schedule) : { ...defaultRepeat(), freq: "weekly" }));
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const problem = repeatProblem(repeat);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (problem) return setErr(problem);
    setBusy(true);
    setErr("");
    const body = scheduleBody(name, repeat);
    (schedule ? api.putSchedule(schedule.id, body, schedule.version) : shared ? api.createSharedSchedule(body) : api.createSchedule(ws.id, body))
      .then((s) => {
        onSaved(`${s.shared ? "Shared schedule" : "Schedule"} “${s.name}” saved`);
        onClose();
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? (e.status === 412 ? "Someone changed this schedule meanwhile. Close and reopen it." : e.message) : "Couldn't save the schedule."))
      .finally(() => setBusy(false));
  };
  return (
    <Modal title={schedule ? "Edit schedule" : "New schedule"} onClose={onClose} wide>
      <form className="form-grid" onSubmit={submit} aria-label={schedule ? "Edit schedule" : "New schedule"}>
        {schedule && schedule.uses > 0 && (
          <p className="span2 small muted sched-tpl" role="note">
            ⟳ {schedule.uses} recurring gig{schedule.uses === 1 ? " runs" : "s run"} on this schedule: changing the timing moves {schedule.uses === 1 ? "it" : "them all"}.
          </p>
        )}
        {schedule?.shared && (
          <p className="span2 small muted sched-tpl" role="note">
            Shared: every workspace may use this schedule. Changing it moves every recurring gig and errand on it, in every workspace.
          </p>
        )}
        {((choices && choices.length > 1) || canShare) && (
          <label className="span2">
            Workspace
            <select value={shared ? 0 : ws.id} onChange={(e) => setWsId(Number(e.target.value))}>
              {canShare && <option value={0}>Shared: every workspace</option>}
              {(choices ?? [ws0]).map((w) => (
                <option key={w.id} value={w.id}>
                  {w.name}
                </option>
              ))}
            </select>
          </label>
        )}
        <label className="span2">
          <span>
            Name <span className="muted small">leave empty to name it after the timing</span>
          </span>
          <input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} placeholder={repeat.freq ? describeRule({ ...repeatRule(repeat), weekdays: repeatRule(repeat).weekdays ?? null, month_day: repeatRule(repeat).month_day ?? null }) : ""} data-autofocus="" />
        </label>
        <RepeatFields value={repeat} onChange={setRepeat} allowNever={false} timingOnly />
        {err && (
          <p className="danger span2" role="alert">
            {err}
          </p>
        )}
        <div className="row end span2">
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="button" disabled={busy || !!problem}>
            Save schedule
          </button>
        </div>
      </form>
    </Modal>
  );
}
