// Shared pieces of the admin screens: the section list (shown in the sidebar), confirmations, inline errors and small badges.
import { cloneElement, useId, useState, type ReactElement, type ReactNode } from "react";
import type { Category } from "../api";
import { CATEGORY_COLOR, PRIORITY_COLOR, colorFor } from "../theme";
import { CATEGORY_LABEL } from "../boardModel";
import { Modal, Pill } from "../ui";
import { errMsg, type Role } from "./adminApi";
import "./admin.css";
import { HelpButton } from "../help";

const TABS = [
  { href: "/workspaces", label: "Workspaces", icon: "layers" },
  { href: "/users", label: "Users", icon: "users" },
  { href: "/groups", label: "Groups", icon: "shield" },
  { href: "/keys", label: "Keys", icon: "key" },
  { href: "/audit", label: "Audit", icon: "list" },
];

export const ADMIN_TABS = TABS;

export function AdminPage({ title, sub, actions, children }: { title: string; sub?: ReactNode; actions?: ReactNode; children: ReactNode }) {
  return (
    <div className="admin">
      <header className="page-head">
        <div>
          <h1>
            {title}
            <HelpButton topic="admin" label="Help: administration" />
          </h1>
          {sub && <div className="muted small">{sub}</div>}
        </div>
        {actions && <div className="row">{actions}</div>}
      </header>
      {children}
    </div>
  );
}

export function ErrorLine({ error }: { error: string }) {
  if (!error) return null;
  return (
    <p className="danger admin-error" role="alert">
      {error}
    </p>
  );
}

// ConfirmButton asks before a destructive action and shows the server's refusal in the dialog.
// SlotLabel: a button label as wide as the widest of the labels its slot can show (Make admin / Revoke
// admin), so a row of buttons lines up with the rows above and below it in any font. The others are
// invisible and hidden from screen readers.
export function SlotLabel({ show, alts }: { show: string; alts: string[] }) {
  return (
    <span className="slot-label">
      <span>{show}</span>
      {alts
        .filter((a) => a !== show)
        .map((a) => (
          <span key={a} className="slot-ghost" aria-hidden="true">
            {a}
          </span>
        ))}
    </span>
  );
}

// `name` is the button's accessible name when the visible label doesn't say what it acts on (a row of
// "Delete" buttons: "Delete project OPS"); it starts with the visible words, so voice control still works.
export function ConfirmButton({
  label,
  name,
  title,
  message,
  confirmLabel,
  onConfirm,
  danger = true,
  small = true,
  disabled,
}: {
  label: ReactNode;
  name?: string;
  title: string;
  message: ReactNode;
  confirmLabel?: string;
  onConfirm: () => Promise<unknown>;
  danger?: boolean;
  small?: boolean;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const cls = "button" + (danger ? " danger-button" : " secondary") + (small ? " small" : "");
  const close = () => {
    setOpen(false);
    setError("");
  };
  const go = () => {
    setBusy(true);
    setError("");
    onConfirm()
      .then(() => setOpen(false))
      .catch((e: unknown) => setError(errMsg(e)))
      .finally(() => setBusy(false));
  };
  return (
    <>
      <button type="button" className={cls} onClick={() => setOpen(true)} disabled={disabled} aria-label={name}>
        {label}
      </button>
      {open && (
        <Modal title={title} onClose={close}>
          <div className="admin-confirm">
            <div>{message}</div>
            <ErrorLine error={error} />
            <div className="row end">
              <button type="button" className="button secondary" onClick={close}>
                Cancel
              </button>
              <button type="button" className={"button" + (danger ? " admin-danger-solid" : "")} onClick={go} disabled={busy}>
                {confirmLabel ?? "Confirm"}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </>
  );
}

export function RolePill({ role }: { role: Role | string }) {
  const cls = role === "manager" ? "pill pill-accent" : role === "member" ? "pill pill-ws" : "pill pill-muted";
  return <span className={cls}>{role}</span>;
}

export function CategoryDot({ category }: { category: Category }) {
  return <span className="dot" style={{ background: CATEGORY_COLOR[category] }} title={CATEGORY_LABEL[category] ?? category} aria-hidden="true" />;
}

export function CategoryPill({ category }: { category: Category }) {
  return (
    <Pill color={CATEGORY_COLOR[category] ?? colorFor(category)} outline>
      {CATEGORY_LABEL[category] ?? category}
    </Pill>
  );
}

// Audit action colours by prefix (data colours from theme.ts; unknown prefixes get a stable hash colour).
const ACTION_COLOR: Record<string, string> = {
  gig: CATEGORY_COLOR.todo!,
  comment: CATEGORY_COLOR.done!,
  run: CATEGORY_COLOR.doing!,
  key: CATEGORY_COLOR.waiting!,
  user: PRIORITY_COLOR[2]!,
  auth: CATEGORY_COLOR.backlog!,
};

export function actionColor(action: string): string {
  const prefix = action.split(".")[0] ?? action;
  return ACTION_COLOR[prefix] ?? colorFor(prefix);
}

export function ActionPill({ action }: { action: string }) {
  return <Pill color={actionColor(action)}>{action}</Pill>;
}

// relTime: "3 min ago" / "in 2 days", with the exact time in a tooltip at the call site.
export function relTime(ms: number | null | undefined): string {
  if (!ms) return "never";
  const d = ms - Date.now();
  const abs = Math.abs(d);
  const units: [number, Intl.RelativeTimeFormatUnit][] = [
    [365 * 864e5, "year"],
    [30 * 864e5, "month"],
    [864e5, "day"],
    [36e5, "hour"],
    [6e4, "minute"],
  ];
  const fmt = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  for (const [size, unit] of units) if (abs >= size) return fmt.format(Math.round(d / size), unit);
  return "just now";
}

export function TimeAgo({ ms }: { ms: number | null | undefined }) {
  if (!ms) return <span className="muted">never</span>;
  return <time dateTime={new Date(ms).toISOString()} title={new Date(ms).toLocaleString()}>{relTime(ms)}</time>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="muted admin-empty">{children}</p>;
}

// Field: a form field with its label above and an optional hint below the control, in small muted
// text. The control (one element) gets the id and aria-describedby, so the label and hint are wired.
export function Field({ label, hint, span2, className, children }: {
  label: ReactNode;
  hint?: ReactNode;
  span2?: boolean;
  className?: string;
  children: ReactElement<{ id?: string; "aria-describedby"?: string }>;
}) {
  const id = useId();
  const hintId = hint ? id + "-hint" : undefined;
  return (
    <div className={"admin-field" + (span2 ? " span2" : "") + (className ? " " + className : "")}>
      <label className="admin-field-label" htmlFor={id}>
        {label}
      </label>
      {cloneElement(children, { id, "aria-describedby": hintId })}
      {hint && (
        <span className="admin-field-hint" id={hintId}>
          {hint}
        </span>
      )}
    </div>
  );
}

// InfoLine: a subtle explanatory note (not a warning) inside a form.
export function InfoLine({ children, span2 }: { children: ReactNode; span2?: boolean }) {
  return (
    <p className={"admin-info" + (span2 ? " span2" : "")}>
      <span className="admin-info-icon" aria-hidden="true">
        i
      </span>
      <span>{children}</span>
    </p>
  );
}

// FormActions: the dialog footer, buttons on the right.
export function FormActions({ children }: { children: ReactNode }) {
  return <div className="admin-form-actions span2">{children}</div>;
}
