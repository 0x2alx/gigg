// How an agent runs a task: model and effort for claude -p, always-run (worked whatever the usage
// budget, day or night) and a tool allowlist (the runner passes exactly those tools). Empty
// model/effort = the agent's default; no tools = the runner's usual mode.
import { useEffect, useId, useState } from "react";
import { HelpButton } from "./help";

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
  tools_error?: string | null; // the allowlist text has a rule that isn't one (the form can't be sent)
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
  useEffect(() => setText(value.join("\n")), [value]);
  const { rules, bad } = parseTools(text);
  useEffect(() => {
    onError?.(bad); // report only when it changes (a returned value would be taken for a cleanup)
  }, [bad]); // eslint-disable-line react-hooks/exhaustive-deps
  const commit = () => {
    if (!bad && JSON.stringify(rules) !== JSON.stringify(value)) onCommit(rules);
  };
  return (
    <span className="tools-input">
      <textarea
        rows={Math.min(8, Math.max(3, rules.length + 1))}
        value={text}
        disabled={disabled}
        placeholder={"none: the agent's usual mode\nRead\nBash(git status *)"}
        aria-label="Allowed tools"
        aria-invalid={!!bad}
        spellCheck={false}
        autoCapitalize="off"
        onChange={(e) => setText(e.target.value)}
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

// RunSettingsFields: the run fields for a form (new task, template, recurring task).
// bare: no frame or legend (the caller's own section heading names it)
export function RunSettingsFields({ value, onChange, canAlways, canTools, bare }: { value: RunSettings; onChange: (v: RunSettings) => void; canAlways: boolean; canTools: boolean; bare?: boolean }) {
  return (
    <fieldset className={"span2 run-settings" + (bare ? " bare" : "")}>
      <legend className={bare ? "sr-only" : "field-label"}>
        Agent run {!bare && <HelpButton topic="agents" label="Help: how agents run tasks" />}
      </legend>
      <label>
        Model
        <ModelInput value={value.model} onCommit={(model) => onChange({ ...value, model })} />
      </label>
      <label>
        Effort
        <EffortSelect value={value.effort} onChange={(effort) => onChange({ ...value, effort })} />
      </label>
      <label className="admin-check run-always" title={canAlways ? "" : "Needs the Always-run tasks permission"}>
        <input type="checkbox" checked={value.always_run} disabled={!canAlways && !value.always_run} onChange={(e) => onChange({ ...value, always_run: e.target.checked })} />
        <span>
          Always run <span className="muted small">whatever the usage budget, day or night (cron-like tasks)</span>
        </span>
      </label>
      {(canTools || value.allowed_tools.length > 0) && (
        <label className="span2" title={canTools ? "" : "Needs the Task tool allowlists permission"}>
          Allowed tools{" "}
          <span className="muted small">one permission rule per line; the agent gets exactly these (claude -p --allowedTools), nothing else</span>
          <ToolsInput
            value={value.allowed_tools}
            disabled={!canTools}
            onCommit={(allowed_tools) => onChange({ ...value, allowed_tools, tools_error: null })}
            onError={(bad) => {
              if ((bad ?? null) !== (value.tools_error ?? null)) onChange({ ...value, tools_error: bad });
            }}
          />
        </label>
      )}
    </fieldset>
  );
}

// runBody: the fields to send (empty ones left out on create).
export function runBody(v: RunSettings): Partial<RunSettings> {
  return {
    ...(v.model ? { model: v.model } : {}),
    ...(v.effort ? { effort: v.effort } : {}),
    ...(v.always_run ? { always_run: true } : {}),
    ...(v.allowed_tools.length ? { allowed_tools: v.allowed_tools } : {}),
  };
}
