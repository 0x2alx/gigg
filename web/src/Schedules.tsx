// The workspace's schedules: named timing (rule, window, create-early) that recurring tasks pair with
// templates. List, new, edit (every recurring task on it moves), delete when unused.
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, ApiError, type Schedule, type ScheduleBody } from "./api";
import type { WorkspaceTree } from "./App";
import { RepeatFields } from "./RepeatFields";
import { defaultRepeat, describeRule, localTZ, repeatFromRule, repeatProblem, repeatRule, type RepeatState } from "./recurrence";
import { Modal } from "./ui";
import { HelpButton } from "./help";
import { Icon } from "./icons";

// scheduleBody: a schedule's timing from the repeat form.
export function scheduleBody(name: string, s: RepeatState): ScheduleBody {
  const r = repeatRule(s);
  const b: ScheduleBody = { name: name.trim(), freq: r.freq, interval: r.interval, tz: r.tz, window_start: r.window_start, window_minutes: r.window_minutes, lead_minutes: r.lead_minutes };
  if (r.weekdays) b.weekdays = r.weekdays;
  if (r.month_day !== undefined) b.month_day = r.month_day;
  return b;
}

// repeatOfSchedule: the repeat form state for a schedule (its own end/overlap/pause fields unused).
export function repeatOfSchedule(s: Schedule, base?: RepeatState): RepeatState {
  const r = repeatFromRule({ ...s, starts_on: 0, overlap: "skip", paused: false });
  return base ? { ...base, freq: r.freq, interval: r.interval, weekdays: r.weekdays, monthDay: r.monthDay, allDay: r.allDay, start: r.start, len: r.len, unit: r.unit, lead: r.lead, tz: r.tz } : r;
}

export function SchedulesModal({ ws, canWrite, onClose, onChanged }: {
  ws: WorkspaceTree;
  canWrite: boolean;
  onClose: () => void;
  onChanged: (message?: string) => void;
}) {
  const [items, setItems] = useState<Schedule[] | null>(null);
  const [err, setErr] = useState("");
  const [editing, setEditing] = useState<Schedule | "new" | null>(null);
  const [confirm, setConfirm] = useState<number | null>(null);
  const closeEdit = useCallback(() => setEditing(null), []);
  const load = useCallback(() => {
    api
      .schedules(ws.id)
      .then((r) => setItems(r.items))
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't load the schedules."));
  }, [ws.id]);
  useEffect(load, [load]);

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

  if (editing) {
    return (
      <ScheduleModal
        ws={ws}
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
    <Modal title="Schedules" onClose={onClose} wide help={<HelpButton topic="recurring" label="Help: templates, schedules and recurring tasks" />}>
      <p className="muted small">When recurring tasks happen. Pair one with a template in ⟳ Recurring (or with Repeat in a new task); changing a schedule moves every recurring task on it.</p>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      {items === null && !err && <p className="muted">Loading…</p>}
      {items?.length === 0 && <p className="muted">No schedules yet.</p>}
      {items && items.length > 0 && (
        <ul className="schedules" aria-label="Schedules">
          {items.map((s) => (
            <li key={s.id} className="schedule">
              <div className="schedule-top">
                <span className="pill pill-accent"><Icon name="clock" /></span>
                <strong className="schedule-title">{s.name}</strong>
                {s.tz !== tz && <span className="pill pill-ws">{s.tz}</span>}
              </div>
              {!s.name.startsWith(describeRule(s)) && (
                <div className="schedule-rule">
                  {describeRule(s)}
                  {s.lead_minutes > 0 && <span className="muted small"> · created {s.lead_minutes} min early</span>}
                </div>
              )}
              <div className="chips">
                <span className={"pill " + (s.uses > 0 ? "pill-accent" : "pill-muted")}>
                  ⟳ {s.uses} recurring task{s.uses === 1 ? "" : "s"}
                </span>
              </div>
              {canWrite && (
                <div className="schedule-actions">
                  {confirm === s.id ? (
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
                      <button type="button" className="button secondary small" onClick={() => setEditing(s)} aria-label={`Edit ${s.name}`}>
                        <Icon name="pencil" /> Edit
                      </button>
                      <button
                        type="button"
                        className="button danger-button small"
                        onClick={() => setConfirm(s.id)}
                        disabled={s.uses > 0}
                        title={s.uses > 0 ? "Recurring tasks use this schedule: delete them or give them another schedule first" : undefined}
                        aria-label={`Delete ${s.name}`}
                      >
                        <Icon name="trash" /> Delete
                      </button>
                    </>
                  )}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      {canWrite && (
        <div className="row end">
          <button type="button" className="button" onClick={() => setEditing("new")}>
            + New schedule
          </button>
        </div>
      )}
    </Modal>
  );
}

function ScheduleModal({ ws, schedule, onClose, onSaved }: { ws: WorkspaceTree; schedule?: Schedule; onClose: () => void; onSaved: (msg?: string) => void }) {
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
    (schedule ? api.putSchedule(schedule.id, body, schedule.version) : api.createSchedule(ws.id, body))
      .then((s) => {
        onSaved(`Schedule “${s.name}” saved`);
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
            ⟳ {schedule.uses} recurring task{schedule.uses === 1 ? " runs" : "s run"} on this schedule: changing the timing moves {schedule.uses === 1 ? "it" : "them all"}.
          </p>
        )}
        <label className="span2">
          Name <span className="muted small">leave empty to name it after the timing</span>
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

// sameTiming: the repeat form still holds exactly the schedule's timing.
export function sameTiming(s: RepeatState, sch: Pick<Schedule, "freq" | "interval" | "weekdays" | "month_day" | "tz" | "window_start" | "window_minutes" | "lead_minutes">): boolean {
  const r = repeatRule(s);
  if (r.freq !== sch.freq || r.interval !== (sch.interval || 1) || r.tz !== sch.tz || r.window_start !== sch.window_start) return false;
  if (r.window_minutes !== sch.window_minutes || r.lead_minutes !== sch.lead_minutes) return false;
  if (r.freq === "weekly" && JSON.stringify(r.weekdays ?? []) !== JSON.stringify([...(sch.weekdays ?? [])].sort((a, b) => a - b))) return false;
  if (r.freq === "monthly" && r.month_day !== sch.month_day) return false;
  return true;
}
