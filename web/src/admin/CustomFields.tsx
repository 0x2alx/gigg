// Custom fields of a workspace: list, add, rename / edit choices, delete (values go with it).
// Needs the manager role and the custom_fields capability; an admin session has both.
import { useCallback, useEffect, useState, type FormEvent, type KeyboardEvent } from "react";
import { useApp } from "../App";
import { hasCap } from "../api";
import { call } from "../http";
import { colorFor } from "../theme";
import { LabelChip, Modal, Pill } from "../ui";
import { errMsg, type AdminWorkspace } from "./adminApi";
import { ConfirmButton, Empty, ErrorLine } from "./common";

export type FieldKind = "text" | "number" | "date" | "select" | "checkbox" | "url";
export const FIELD_KINDS: { kind: FieldKind; label: string; icon: string }[] = [
  { kind: "text", label: "Text", icon: "¶" },
  { kind: "number", label: "Number", icon: "#" },
  { kind: "date", label: "Date", icon: "◷" },
  { kind: "select", label: "Choice", icon: "▾" },
  { kind: "checkbox", label: "Checkbox", icon: "☑" },
  { kind: "url", label: "Link", icon: "↗" },
];

export interface CustomField {
  id: number;
  name: string;
  kind: FieldKind;
  options: string[] | null;
  version: number;
}

const fieldsApi = {
  list: (ws: number) => call<{ items: CustomField[] }>("GET", `/workspaces/${ws}/fields`),
  create: (ws: number, body: { name: string; kind: FieldKind; options?: string[] }) => call<CustomField>("POST", `/workspaces/${ws}/fields`, body),
  update: (f: CustomField, body: { name: string; kind: FieldKind; options?: string[] }) => call<CustomField>("PUT", `/fields/${f.id}`, body, f.version),
  remove: (id: number) => call<void>("DELETE", `/fields/${id}`),
};

// cleanOptions: trimmed, non-empty, unique (case-insensitive) choices in order.
export function cleanOptions(opts: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const o of opts) {
    const t = o.trim();
    if (!t || seen.has(t.toLowerCase())) continue;
    seen.add(t.toLowerCase());
    out.push(t);
  }
  return out;
}

function fieldBody(name: string, kind: FieldKind, options: string[]) {
  return kind === "select" ? { name: name.trim(), kind, options: cleanOptions(options) } : { name: name.trim(), kind };
}

const kindOf = (k: string) => FIELD_KINDS.find((x) => x.kind === k) ?? { kind: k as FieldKind, label: k, icon: "•" };

export function CustomFields({ ws }: { ws: AdminWorkspace }) {
  const { me } = useApp();
  const allowed = hasCap(me, "custom_fields", ws.id);
  const [items, setItems] = useState<CustomField[] | null>(null);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<CustomField | null>(null);
  const [name, setName] = useState("");
  const [kind, setKind] = useState<FieldKind>("text");
  const [options, setOptions] = useState<string[]>([]);

  const load = useCallback(() => {
    fieldsApi
      .list(ws.id)
      .then((r) => {
        setItems(r.items);
        setError("");
      })
      .catch((e: unknown) => setError(errMsg(e)));
  }, [ws.id]);
  useEffect(load, [load]);

  const add = (e: FormEvent) => {
    e.preventDefault();
    if (kind === "select" && cleanOptions(options).length === 0) {
      setError("A choice field needs at least one choice.");
      return;
    }
    fieldsApi
      .create(ws.id, fieldBody(name, kind, options))
      .then(() => {
        setName("");
        setOptions([]);
        load();
      })
      .catch((err: unknown) => setError(errMsg(err)));
  };

  return (
    <section className="card" aria-label="Custom fields">
      <div className="admin-section-head">
        <h2>Custom fields</h2>
        <span className="muted small">Extra properties on every gig of {ws.name}.</span>
        {!allowed && <span className="pill pill-muted">you can't change these</span>}
      </div>
      <ErrorLine error={error} />
      {items && items.length === 0 && <Empty>No custom fields yet.</Empty>}
      <ul className="admin-list">
        {(items ?? []).map((f) => {
          const k = kindOf(f.kind);
          return (
            <li key={f.id} className="admin-list-row">
              <span className="admin-name">
                <span className="nav-icon" aria-hidden="true">
                  {k.icon}
                </span>
                {f.name}
              </span>
              <span className="pill pill-ws">{k.label}</span>
              {f.kind === "select" && (
                <span className="chips cf-opts">
                  {(f.options ?? []).map((o) => (
                    <Pill key={o} color={colorFor(o)}>
                      {o}
                    </Pill>
                  ))}
                </span>
              )}
              <span className="grow" />
              {allowed && (
                <>
                  <button type="button" className="button secondary small" onClick={() => setEditing(f)}>
                    Edit
                  </button>
                  <ConfirmButton
                    label="Delete"
                    title={`Delete field ${f.name}?`}
                    message="Its value is removed from every gig. This can't be undone."
                    confirmLabel="Delete field"
                    onConfirm={() => fieldsApi.remove(f.id).then(load)}
                  />
                </>
              )}
            </li>
          );
        })}
      </ul>
      {allowed && (
        <form className="admin-add cf-add" onSubmit={add} aria-label="Add a custom field">
          <div className="row">
            <label className="grow">
              New field
              <input value={name} required maxLength={60} placeholder="e.g. Customer" onChange={(e) => setName(e.target.value)} />
            </label>
            <label>
              Kind
              <select value={kind} onChange={(e) => setKind(e.target.value as FieldKind)}>
                {FIELD_KINDS.map((k) => (
                  <option key={k.kind} value={k.kind}>
                    {k.icon} {k.label}
                  </option>
                ))}
              </select>
            </label>
            <button type="submit" className="button" disabled={!name.trim()}>
              Add
            </button>
          </div>
          {kind === "select" && <OptionsEditor value={options} onChange={setOptions} />}
        </form>
      )}
      {editing && (
        <EditField
          field={editing}
          onClose={() => setEditing(null)}
          onDone={() => {
            setEditing(null);
            load();
          }}
        />
      )}
    </section>
  );
}

function EditField({ field, onClose, onDone }: { field: CustomField; onClose: () => void; onDone: () => void }) {
  const [name, setName] = useState(field.name);
  const [options, setOptions] = useState<string[]>(field.options ?? []);
  const [error, setError] = useState("");
  const removed = (field.options ?? []).filter((o) => !cleanOptions(options).includes(o));
  const save = (e: FormEvent) => {
    e.preventDefault();
    if (field.kind === "select" && cleanOptions(options).length === 0) {
      setError("A choice field needs at least one choice.");
      return;
    }
    fieldsApi
      .update(field, fieldBody(name, field.kind, options))
      .then(onDone)
      .catch((err: unknown) => setError(errMsg(err)));
  };
  return (
    <Modal title={`Edit field ${field.name}`} onClose={onClose}>
      <form className="form-grid" onSubmit={save}>
        <label>
          Name
          <input value={name} required maxLength={60} onChange={(e) => setName(e.target.value)} />
        </label>
        <div className="field">
          <span className="field-label">Kind</span>
          <span className="pill pill-ws">{kindOf(field.kind).label}</span>
          <span className="muted small"> can't change</span>
        </div>
        {field.kind === "select" && (
          <div className="span2">
            <OptionsEditor value={options} onChange={setOptions} />
            {removed.length > 0 && <p className="muted small">Gigs set to {removed.map((r) => `“${r}”`).join(", ")} keep that value until edited.</p>}
          </div>
        )}
        <div className="span2">
          <ErrorLine error={error} />
        </div>
        <div className="row end span2">
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="button" disabled={!name.trim()}>
            Save
          </button>
        </div>
      </form>
    </Modal>
  );
}

// OptionsEditor: the choices of a select field as removable chips plus an input (Enter or comma adds).
function OptionsEditor({ value, onChange }: { value: string[]; onChange: (v: string[]) => void }) {
  const [text, setText] = useState("");
  const add = () => {
    const parts = text.split(",");
    const next = cleanOptions([...value, ...parts]);
    if (next.length > 50) return;
    onChange(next);
    setText("");
  };
  const key = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      add();
    } else if (e.key === "Backspace" && !text && value.length) onChange(value.slice(0, -1));
  };
  return (
    <div className="cf-options">
      <span className="field-label">Choices</span>
      <div className="chips cf-chips">
        {value.map((o) => (
          <LabelChip key={o} label={{ id: 0, name: o, color: null }} onRemove={() => onChange(value.filter((x) => x !== o))} />
        ))}
        <input className="cf-opt-input" value={text} placeholder="Add a choice, Enter" onChange={(e) => setText(e.target.value)} onKeyDown={key} onBlur={() => text.trim() && add()} aria-label="Add a choice" />
      </div>
    </div>
  );
}
