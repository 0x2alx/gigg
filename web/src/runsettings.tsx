// How an agent runs a gig: model and effort for claude -p, always-run (worked whatever the usage
// budget, day or night), a tool allowlist (a runner should allow exactly those tools) and a run
// timeout. Empty model/effort/timeout = the agent's default; no tools = the runner's usual mode.
// Auto-archive rides along (it isn't about the run, but every editor carries it the same way).
import { useEffect, useId, useRef, useState } from "react";

// Aliases always mean the latest of that family; full ids pin one version. Anything else can be typed.
export const MODEL_SUGGESTIONS = ["opus", "sonnet", "haiku", "fable", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-2"];
export const EFFORTS = ["low", "medium", "high", "xhigh", "max"] as const;

// validModel mirrors the server: one claude -p argument, letters, digits and . _ : - [ ].
export function validModel(s: string): boolean {
  return s === "" || /^[A-Za-z0-9][A-Za-z0-9._:[\]-]{0,63}$/.test(s);
}

export interface RunSettings {
  model: string;
  effort: string;
  always_run: boolean;
  allowed_tools: string[];
  run_timeout_minutes: number | null; // null: the agent's default (60)
  auto_archive_minutes: number | null; // archived this long after done or cancelled; null: never
  tools_error?: string | null; // the allowlist text has a rule that isn't one (the form can't be sent)
}

// runFrom: the run settings a gig, template or recurring gig has.
export function runFrom(x?: { model?: string; effort?: string; always_run?: boolean; allowed_tools?: string[] | null; run_timeout_minutes?: number | null; auto_archive_minutes?: number | null } | null): RunSettings {
  return {
    model: x?.model ?? "",
    effort: x?.effort ?? "",
    always_run: !!x?.always_run,
    allowed_tools: x?.allowed_tools ?? [],
    run_timeout_minutes: x?.run_timeout_minutes ?? null,
    auto_archive_minutes: x?.auto_archive_minutes ?? null,
  };
}

// The choices offered (minutes). The server takes any value in range; one set elsewhere is shown too.
export const TIMEOUT_CHOICES = [15, 30, 45, 60, 90, 120, 180, 240, 300];
export const ARCHIVE_CHOICES = [60, 360, 720, 1440, 4320, 10080, 43200];

// fmtMinutes: 45 min, 2 h, 1 h 30 min, 3 days.
export function fmtMinutes(m: number): string {
  if (m % 1440 === 0) return m === 1440 ? "1 day" : `${m / 1440} days`;
  if (m >= 60) return m % 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${m / 60} h`;
  return `${m} min`;
}

// MinutesSelect: a duration from a list, or none (`none` names what none means). lock "clear": only
// none can be chosen (taking a value off needs no permission on a new item); "all": read-only.
export function MinutesSelect({ value, onChange, choices, none, label, lock, title }: {
  value: number | null; onChange: (v: number | null) => void; choices: number[]; none: string; label: string; lock?: "clear" | "all"; title?: string;
}) {
  const opts = lock ? (value != null ? [value] : []) : value != null && !choices.includes(value) ? [...choices, value].sort((a, b) => a - b) : choices;
  return (
    <select value={value ?? ""} disabled={lock === "all" || (lock === "clear" && value == null)} aria-label={label} title={title} onChange={(e) => onChange(e.target.value ? Number(e.target.value) : null)}>
      <option value="">{none}</option>
      {opts.map((m) => (
        <option key={m} value={m}>
          {fmtMinutes(m)}
        </option>
      ))}
    </select>
  );
}

// validTool mirrors the server: Tool or Tool(specifier), never a flag, no control characters; commas
// and spaces only inside the specifier and no parentheses in it (how claude splits --allowedTools).
export function validTool(s: string): boolean {
  return s.length <= 500 && /^[A-Za-z][A-Za-z0-9_-]*(\([^()\x00-\x1f\x7f]+\))?$/.test(s);
}

// parseTools: one rule per line; blank lines and repeats dropped. bad = the first rule that isn't one.
export function parseTools(text: string): { rules: string[]; bad: string | null } {
  const rules: string[] = [];
  for (const line of text.split("\n")) {
    const r = line.trim();
    if (!r || rules.includes(r)) continue;
    if (!validTool(r)) return { rules, bad: r };
    rules.push(r);
  }
  return { rules, bad: rules.length > 64 ? "more than 64 rules" : null };
}

// ToolsInput: the allowlist as text, one rule per line; commits on blur when every rule is valid.
export function ToolsInput({ value, onCommit, onError, disabled }: { value: string[]; onCommit: (v: string[]) => void; onError?: (bad: string | null) => void; disabled?: boolean }) {
  const [text, setText] = useState(value.join("\n"));
  // follow the saved value when it really changes (not on every new but equal array), and never while
  // someone is typing in the box (a live reload would wipe the rules not yet committed)
  const box = useRef<HTMLTextAreaElement>(null);
  const saved = value.join("\n");
  useEffect(() => {
    if (document.activeElement !== box.current) setText(saved);
  }, [saved]);
  const { rules, bad } = parseTools(text);
  useEffect(() => {
    onError?.(bad); // report only when it changes (a returned value would be taken for a cleanup)
  }, [bad]); // eslint-disable-line react-hooks/exhaustive-deps
  // only text typed since the box got the focus is saved: leaving it untouched takes the saved value
  // (someone may have changed the list meanwhile; putting the old text back would undo that)
  const typed = useRef(false);
  const commit = () => {
    if (!typed.current) {
      setText(saved);
      return;
    }
    if (bad) return; // kept (and still "typed") until it's fixed
    typed.current = false;
    if (JSON.stringify(rules) !== JSON.stringify(value)) onCommit(rules);
  };
  return (
    <span className="tools-input">
      <textarea
        ref={box}
        rows={Math.min(8, Math.max(3, rules.length + 1))}
        value={text}
        disabled={disabled}
        placeholder={"none: the agent's usual mode\nRead\nBash(git status *)"}
        aria-label="Allowed tools"
        aria-invalid={!!bad}
        spellCheck={false}
        autoCapitalize="off"
        onChange={(e) => {
          typed.current = true;
          setText(e.target.value);
        }}
        onBlur={commit}
      />
      {bad && <span className="danger small">not one permission rule: {bad}</span>}
    </span>
  );
}

// ModelInput: a text box with suggestions; commits on blur or Enter when the name is valid.
export function ModelInput({ value, onCommit, disabled, label = "Model" }: { value: string; onCommit: (v: string) => void; disabled?: boolean; label?: string }) {
  const list = useId();
  const [text, setText] = useState(value);
  useEffect(() => setText(value), [value]);
  const ok = validModel(text.trim());
  const commit = () => {
    const v = text.trim();
    if (ok && v !== value) onCommit(v);
  };
  return (
    <span className="model-input">
      <input
        value={text}
        list={list}
        disabled={disabled}
        placeholder="agent default"
        aria-label={label}
        aria-invalid={!ok}
        maxLength={64}
        spellCheck={false}
        autoCapitalize="off"
        onChange={(e) => setText(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            commit();
          }
        }}
      />
      <datalist id={list}>
        {MODEL_SUGGESTIONS.map((m) => (
          <option key={m} value={m} />
        ))}
      </datalist>
      {!ok && <span className="danger small">letters, digits and . _ : - [ ] only</span>}
    </span>
  );
}

export function EffortSelect({ value, onChange, disabled, label = "Effort" }: { value: string; onChange: (v: string) => void; disabled?: boolean; label?: string }) {
  return (
    <select value={value} disabled={disabled} aria-label={label} onChange={(e) => onChange(e.target.value)}>
      <option value="">agent default</option>
      {EFFORTS.map((e) => (
        <option key={e} value={e}>
          {e}
        </option>
      ))}
    </select>
  );
}

// runBody: the fields to send (empty ones left out on create).
export function runBody(v: RunSettings): Partial<RunSettings> {
  return {
    ...(v.model ? { model: v.model } : {}),
    ...(v.effort ? { effort: v.effort } : {}),
    ...(v.always_run ? { always_run: true } : {}),
    ...(v.allowed_tools.length ? { allowed_tools: v.allowed_tools } : {}),
    ...(v.run_timeout_minutes != null ? { run_timeout_minutes: v.run_timeout_minutes } : {}),
    ...(v.auto_archive_minutes != null ? { auto_archive_minutes: v.auto_archive_minutes } : {}),
  };
}
