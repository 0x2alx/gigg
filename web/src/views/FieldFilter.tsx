// FieldFilter: the picker for one custom field in the table's More filters: a choice or a checkbox
// is a select; a number a comparison and a number; text and links a "contains" box. Typed values
// apply on Enter or when the field is left, so the table doesn't reload on every key.
import { useEffect, useState } from "react";
import type { CustomField } from "../api";
import { joinNum, NUM_OPS, splitNum, type NumOp } from "./fieldFilters";

// value: the filter (see fieldFilters); undefined = no filter on this field. "" = no value set.
export function FieldFilter({ field: f, value, onChange }: { field: CustomField; value: string | undefined; onChange: (v: string | undefined) => void }) {
  const on = value !== undefined ? "active" : undefined;
  const label = f.name;
  if (f.kind === "select" || f.kind === "checkbox") {
    // "*": any; "-": no value set
    const opts: [string, string][] = f.kind === "checkbox" ? [["true", "Ticked"], ["false", "Not ticked"]] : (f.options ?? []).map((o) => [o, o]);
    return (
      <select className={on} value={value === undefined ? "*" : value === "" ? "-" : value} aria-label={label} onChange={(e) => onChange(e.target.value === "*" ? undefined : e.target.value === "-" ? "" : e.target.value)}>
        <option value="*">Any</option>
        {opts.map(([v, text]) => (
          <option key={v} value={v}>
            {text}
          </option>
        ))}
        {f.kind === "select" && <option value="-">No value</option>}
      </select>
    );
  }
  if (f.kind === "number") return <NumberFilter label={label} value={value} on={on} onChange={onChange} />;
  return <Draft className={on} type="text" value={value ?? ""} label={label} placeholder="contains…" onCommit={(t) => onChange(t.trim() ? t.trim() : undefined)} />;
}

// NumberFilter: a comparison and a number; the comparison can be picked before the number is typed.
function NumberFilter({ label, value, on, onChange }: { label: string; value: string | undefined; on?: string; onChange: (v: string | undefined) => void }) {
  const { op, n } = splitNum(value ?? "");
  const [pick, setPick] = useState<NumOp>(op);
  useEffect(() => {
    if (value !== undefined) setPick(op);
  }, [value, op]);
  return (
    <span className="mf-num">
      <select
        className={on}
        value={pick}
        aria-label={`${label}: comparison`}
        onChange={(e) => {
          const o = e.target.value as NumOp;
          setPick(o);
          if (n) onChange(joinNum(o, n));
        }}
      >
        {NUM_OPS.map(([o, sign]) => (
          <option key={o} value={o}>
            {sign}
          </option>
        ))}
      </select>
      <Draft className={on} type="number" value={n} label={label} placeholder="any" onCommit={(t) => onChange(joinNum(pick, t))} />
    </span>
  );
}

// Draft: a box whose text applies on Enter or when it's left.
function Draft({ value, onCommit, type, label, placeholder, className }: { value: string; onCommit: (v: string) => void; type: "text" | "number"; label: string; placeholder: string; className?: string }) {
  const [text, setText] = useState(value);
  useEffect(() => setText(value), [value]);
  return (
    <input
      type={type}
      className={className}
      value={text}
      placeholder={placeholder}
      aria-label={label}
      onChange={(e) => setText(e.target.value)}
      onBlur={() => text !== value && onCommit(text)}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          if (text !== value) onCommit(text);
        }
      }}
    />
  );
}
