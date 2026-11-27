// The "Repeat" block of the gig modal: frequency, days, the time window, lead time, end, and a live
// preview of the next occurrences (computed client-side, same rules as the server).
import { useId, useMemo, type ReactNode } from "react";
import { TimeInput } from "./fields";
import { Segmented } from "./ui";
import { WEEKDAYS, fmtWallWindow, localTZ, numIn, ordinal, previewOccurrences, repeatIssue, ruleOf, windowMinutes, type Freq, type RepeatField, type RepeatState } from "./recurrence";

const FREQS: [Freq, string][] = [
  ["daily", "Daily"],
  ["weekly", "Weekly"],
  ["monthly", "Monthly"],
];
const UNIT: Record<Freq, [string, string]> = { daily: ["day", "days"], weekly: ["week", "weeks"], monthly: ["month", "months"] };

// a number field's value: what was typed, blank while it's empty (NaN in the state)
const shown = (n: number) => (Number.isNaN(n) ? "" : n);

// timingOnly: just a schedule's timing (no end, overlap or pause: those belong to a recurring gig).
// A value out of range stays as typed: the field is marked and says why under it, and the form waits.
export function RepeatFields({ value: s, onChange, allowNever = true, showPaused, timingOnly, children }: {
  value: RepeatState; onChange: (s: RepeatState) => void; allowNever?: boolean; showPaused?: boolean; timingOnly?: boolean; children?: ReactNode;
}) {
  const set = (p: Partial<RepeatState>) => onChange({ ...s, ...p });
  const issue = repeatIssue(s);
  const errId = useId();
  const preview = useMemo(() => {
    if (!s.freq || issue) return [];
    const now = Date.now();
    return previewOccurrences(ruleOf(s, now), now, 3);
  }, [s, issue]);
  const minutes = windowMinutes(s);
  const toggleDay = (d: number) => set({ weekdays: s.weekdays.includes(d) ? s.weekdays.filter((x) => x !== d) : [...s.weekdays, d] });
  // the marks for the field the issue is on: aria-invalid, and the message read with it
  const bad = (f: RepeatField) => (issue?.field === f ? { "aria-invalid": true as const, "aria-describedby": errId } : {});
  const err = (f: RepeatField) =>
    issue?.field === f && (
      <p className="danger small rf-note rf-err" id={errId} role="alert">
        {issue.msg}
      </p>
    );

  return (
    <div className="repeat span2" role="group" aria-label="Repeat">
      <div className="rf">
        <RfRow label={timingOnly ? "Timing" : "Repeat"}>
          <Segmented<Freq | "">
            label="Repeat"
            options={allowNever ? [["", "Never"], ...FREQS] : FREQS}
            value={s.freq}
            onChange={(freq) => set({ freq })}
            selectWhenNarrow
          />
        </RfRow>
        {s.freq && (
          <>
            {children}
            <RfRow label="Every">
              <input type="number" className="num" min={1} max={365} step={1} value={shown(s.interval)} onChange={(e) => set({ interval: numIn(e.target.value) })} aria-label="Repeat every" {...bad("interval")} />
              <span className="rf-unit">{UNIT[s.freq][s.interval === 1 ? 0 : 1]}</span>
              {err("interval")}
            </RfRow>
            {s.freq === "weekly" && (
              <RfRow label="On">
                <div className="wd-chips" role="group" aria-label="Weekdays" {...bad("weekdays")}>
                  {WEEKDAYS.map((d, i) => (
                    <button key={d} type="button" className={"wd-chip" + (s.weekdays.includes(i + 1) ? " on" : "")} aria-pressed={s.weekdays.includes(i + 1)} aria-label={d} onClick={() => toggleDay(i + 1)}>
                      {d.slice(0, 2)}
                    </button>
                  ))}
                </div>
                {err("weekdays")}
              </RfRow>
            )}
            {s.freq === "monthly" && (
              <RfRow label="On">
                <select value={s.monthDay} onChange={(e) => set({ monthDay: Number(e.target.value) })} aria-label="Day of month">
                  {Array.from({ length: 31 }, (_, i) => (
                    <option key={i + 1} value={i + 1}>
                      the {ordinal(i + 1)}
                    </option>
                  ))}
                  <option value={-1}>the last day</option>
                </select>
              </RfRow>
            )}
            <RfRow label="Time">
              <div className="seg" role="radiogroup" aria-label="Window">
                <button type="button" role="radio" aria-checked={s.allDay} className={s.allDay ? "on" : ""} onClick={() => set({ allDay: true })}>
                  All day
                </button>
                <button type="button" role="radio" aria-checked={!s.allDay} className={!s.allDay ? "on" : ""} onClick={() => set({ allDay: false })}>
                  Time window
                </button>
              </div>
            </RfRow>
            {!s.allDay && (
              <RfRow label="From">
                <TimeInput className="time" value={s.start} onChange={(v) => set({ start: v })} aria-label="Window opens at (24-hour clock)" invalid={issue?.field === "start"} aria-describedby={issue?.field === "start" ? errId : undefined} />
                <span className="rf-pair">
                  <span className="rf-unit">for</span>
                  <input type="number" className="num" min={s.unit === "h" ? 0.1 : 5} step="any" value={shown(s.len)} onChange={(e) => set({ len: numIn(e.target.value) })} aria-label="Window length" {...bad("len")} />
                  <select value={s.unit} onChange={(e) => set({ unit: e.target.value as "h" | "m" })} aria-label="Window length unit">
                    <option value="h">hours</option>
                    <option value="m">minutes</option>
                  </select>
                </span>
                {err("start")}
                {err("len")}
              </RfRow>
            )}
            <RfRow label="Create">
              <input type="number" className="num" min={0} max={10080} step={1} value={shown(s.lead)} onChange={(e) => set({ lead: numIn(e.target.value) })} aria-label="Create minutes early" {...bad("lead")} />
              <span className="rf-unit">min early</span>
              {err("lead")}
            </RfRow>
            {!timingOnly && (
              <RfRow label="Ends">
                <select value={s.endMode} onChange={(e) => set({ endMode: e.target.value as RepeatState["endMode"] })} aria-label="Ends">
                  <option value="never">never</option>
                  <option value="date">on date</option>
                  <option value="count">after</option>
                </select>
                {s.endMode === "date" && <input type="date" className="date" value={s.endDate} onChange={(e) => set({ endDate: e.target.value })} aria-label="End date" {...bad("endDate")} />}
                {s.endMode === "count" && (
                  <span className="rf-pair">
                    <input type="number" className="num" min={1} step={1} value={shown(s.count)} onChange={(e) => set({ count: numIn(e.target.value) })} aria-label="End after N times" {...bad("count")} />
                    <span className="rf-unit">times</span>
                  </span>
                )}
                {err("endDate")}
                {err("count")}
              </RfRow>
            )}
            {!timingOnly && (
              <RfRow label="Options" stack>
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
              </RfRow>
            )}
            <RfRow label="Next">
              {issue ? (
                <span className="muted small">— fix the timing above</span>
              ) : showPaused && s.paused ? (
                <div className="repeat-preview" aria-label="Next occurrences">
                  <span className="pill pill-muted">paused: no gigs until it's resumed</span>
                </div>
              ) : (
                <div className="repeat-preview" aria-label="Next occurrences">
                  {preview.map((ms) => (
                    <span key={ms} className="pill pill-muted">
                      {fmtWallWindow(ms, minutes)}
                    </span>
                  ))}
                  {preview.length === 0 && <span className="pill pill-danger">no more occurrences</span>}
                  {s.tz !== localTZ() && <span className="pill pill-ws">{s.tz}</span>}
                </div>
              )}
            </RfRow>
          </>
        )}
      </div>
    </div>
  );
}

// RfRow: one row of the repeat grid, a label in the first column and its controls in the second
// (stacked under the label when the block is narrow). `stack` puts the controls one per line.
export function RfRow({ label, stack, children }: { label: ReactNode; stack?: boolean; children: ReactNode }) {
  return (
    <>
      <span className="rf-l">{label}</span>
      <div className={"rf-c" + (stack ? " rf-stack" : "")}>{children}</div>
    </>
  );
}
