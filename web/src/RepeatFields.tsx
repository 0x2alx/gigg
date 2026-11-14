// The "Repeat" block of the task modal: frequency, days, the time window, lead time, end, and a live
// preview of the next occurrences (computed client-side, same rules as the server).
import { useMemo, type ReactNode } from "react";
import { WEEKDAYS, fmtWallWindow, localTZ, ordinal, previewOccurrences, repeatProblem, ruleOf, windowMinutes, type Freq, type RepeatState } from "./recurrence";

const FREQS: [Freq, string][] = [
  ["daily", "Daily"],
  ["weekly", "Weekly"],
  ["monthly", "Monthly"],
];
const UNIT: Record<Freq, [string, string]> = { daily: ["day", "days"], weekly: ["week", "weeks"], monthly: ["month", "months"] };

const int = (v: string, min: number) => {
  const n = Math.floor(Number(v));
  return Number.isFinite(n) ? Math.max(min, n) : min;
};

// timingOnly: just a schedule's timing (no end, overlap or pause: those belong to a recurring task).
export function RepeatFields({ value: s, onChange, allowNever = true, showPaused, timingOnly, children }: {
  value: RepeatState; onChange: (s: RepeatState) => void; allowNever?: boolean; showPaused?: boolean; timingOnly?: boolean; children?: ReactNode;
}) {
  const set = (p: Partial<RepeatState>) => onChange({ ...s, ...p });
  const problem = repeatProblem(s);
  const preview = useMemo(() => {
    if (!s.freq || problem) return [];
    const now = Date.now();
    return previewOccurrences(ruleOf(s, now), now, 3);
  }, [s, problem]);
  const minutes = windowMinutes(s);
  const toggleDay = (d: number) => set({ weekdays: s.weekdays.includes(d) ? s.weekdays.filter((x) => x !== d) : [...s.weekdays, d] });

  return (
    <div className="repeat span2" role="group" aria-label="Repeat">
      <div className="repeat-row">
        <span className="field-label">{timingOnly ? "⏱ Timing" : "⟳ Repeat"}</span>
        <div className="seg" role="radiogroup" aria-label="Repeat">
          {allowNever && (
            <button type="button" role="radio" aria-checked={!s.freq} className={!s.freq ? "on" : ""} onClick={() => set({ freq: "" })}>
              Never
            </button>
          )}
          {FREQS.map(([f, label]) => (
            <button key={f} type="button" role="radio" aria-checked={s.freq === f} className={s.freq === f ? "on" : ""} onClick={() => set({ freq: f })}>
              {label}
            </button>
          ))}
        </div>
      </div>
      {s.freq && (
        <>
          {children}
          <div className="repeat-row">
            <label className="inline">
              every
              <input type="number" className="num" min={1} max={365} value={s.interval} onChange={(e) => set({ interval: int(e.target.value, 1) })} aria-label="Repeat every" />
              {UNIT[s.freq][s.interval === 1 ? 0 : 1]}
            </label>
            {s.freq === "weekly" && (
              <div className="wd-chips" role="group" aria-label="Weekdays">
                {WEEKDAYS.map((d, i) => (
                  <button key={d} type="button" className={"wd-chip" + (s.weekdays.includes(i + 1) ? " on" : "")} aria-pressed={s.weekdays.includes(i + 1)} aria-label={d} onClick={() => toggleDay(i + 1)}>
                    {d.slice(0, 2)}
                  </button>
                ))}
              </div>
            )}
            {s.freq === "monthly" && (
              <label className="inline">
                on
                <select value={s.monthDay} onChange={(e) => set({ monthDay: Number(e.target.value) })} aria-label="Day of month">
                  {Array.from({ length: 31 }, (_, i) => (
                    <option key={i + 1} value={i + 1}>
                      the {ordinal(i + 1)}
                    </option>
                  ))}
                  <option value={-1}>the last day</option>
                </select>
              </label>
            )}
          </div>
          <div className="repeat-row">
            <div className="seg" role="radiogroup" aria-label="Window">
              <button type="button" role="radio" aria-checked={s.allDay} className={s.allDay ? "on" : ""} onClick={() => set({ allDay: true })}>
                All day
              </button>
              <button type="button" role="radio" aria-checked={!s.allDay} className={!s.allDay ? "on" : ""} onClick={() => set({ allDay: false })}>
                Time window
              </button>
            </div>
            {!s.allDay && (
              <>
                <label className="inline">
                  from
                  <input type="time" className="time" value={s.start} onChange={(e) => set({ start: e.target.value })} aria-label="Window opens at" required />
                </label>
                <label className="inline">
                  for
                  <input type="number" className="num" min={1} value={s.len} onChange={(e) => set({ len: int(e.target.value, 1) })} aria-label="Window length" />
                  <select value={s.unit} onChange={(e) => set({ unit: e.target.value as "h" | "m" })} aria-label="Window length unit">
                    <option value="h">hours</option>
                    <option value="m">minutes</option>
                  </select>
                </label>
              </>
            )}
          </div>
          <div className="repeat-row">
            <label className="inline">
              create
              <input type="number" className="num" min={0} max={10080} value={s.lead} onChange={(e) => set({ lead: int(e.target.value, 0) })} aria-label="Create minutes early" />
              min early
            </label>
            {!timingOnly && (
            <label className="inline">
              ends
              <select value={s.endMode} onChange={(e) => set({ endMode: e.target.value as RepeatState["endMode"] })} aria-label="Ends">
                <option value="never">never</option>
                <option value="date">on date</option>
                <option value="count">after</option>
              </select>
              {s.endMode === "date" && <input type="date" className="date" value={s.endDate} onChange={(e) => set({ endDate: e.target.value })} aria-label="End date" />}
              {s.endMode === "count" && (
                <>
                  <input type="number" className="num" min={1} value={s.count} onChange={(e) => set({ count: int(e.target.value, 1) })} aria-label="End after N times" />
                  times
                </>
              )}
            </label>
            )}
          </div>
          {!timingOnly && (
          <div className="repeat-row">
            <label className="check">
              <input type="checkbox" checked={s.overlap === "create"} onChange={(e) => set({ overlap: e.target.checked ? "create" : "skip" })} />
              also when the previous one is still open
            </label>
            {showPaused && (
              <label className="check">
                <input type="checkbox" checked={s.paused} onChange={(e) => set({ paused: e.target.checked })} />
                paused
              </label>
            )}
          </div>
          )}
          {problem ? (
            <p className="danger small" role="alert">
              {problem}
            </p>
          ) : (
            <div className="repeat-preview" aria-label="Next occurrences">
              <span className="muted small">Next</span>
              {preview.map((ms) => (
                <span key={ms} className="pill pill-muted">
                  {fmtWallWindow(ms, minutes)}
                </span>
              ))}
              {preview.length === 0 && <span className="pill pill-danger">no more occurrences</span>}
              {s.tz !== localTZ() && <span className="pill pill-ws">{s.tz}</span>}
            </div>
          )}
        </>
      )}
    </div>
  );
}
