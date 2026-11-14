// The project's task templates (plus the workspace-wide ones): list with badges; new, edit, delete.
import { useCallback, useEffect, useState, type CSSProperties } from "react";
import { api, ApiError, type Status, type Tag, type Template } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { TaskModal } from "./TaskModal";
import { Avatar, LabelChip, Modal, PriorityBadge } from "./ui";
import { HelpButton } from "./help";
import { Icon } from "./icons";

export function TemplatesModal({ ws, projectId, statuses, labels, canEdit, onClose, onChanged }: {
  ws: WorkspaceTree;
  projectId: number;
  statuses: Status[];
  labels: Tag[];
  canEdit: boolean;
  onClose: () => void;
  onChanged: (message?: string) => void;
}) {
  const { users } = useApp();
  const [items, setItems] = useState<Template[] | null>(null);
  const [err, setErr] = useState("");
  const [editing, setEditing] = useState<Template | "new" | null>(null);
  const [confirm, setConfirm] = useState<number | null>(null);
  const closeEdit = useCallback(() => setEditing(null), []);

  const load = useCallback(() => {
    api
      .templates(ws.id, projectId)
      .then((r) => setItems(r.items))
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't load the templates."));
  }, [ws.id, projectId]);
  useEffect(load, [load]);

  const remove = (t: Template) => {
    setErr("");
    api
      .deleteTemplate(t.id)
      .then(() => {
        setItems((xs) => xs?.filter((x) => x.id !== t.id) ?? null);
        setConfirm(null);
      })
      .catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't delete the template."));
  };

  if (editing) {
    return (
      <TaskModal
        ws={ws}
        projectId={projectId}
        statuses={statuses}
        labels={labels}
        template={editing}
        onClose={closeEdit}
        onCreated={(msg) => {
          load();
          onChanged(msg);
        }}
      />
    );
  }

  return (
    <Modal title="Templates" onClose={onClose} wide help={<HelpButton topic="recurring" label="Help: templates, schedules and recurring tasks" />}>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      {items === null && !err && <p className="muted">Loading…</p>}
      {items?.length === 0 && <p className="muted">No templates yet. Save one from the new-task form, or make one here.</p>}
      {items && items.length > 0 && (
        <ul className="schedules" aria-label="Templates">
          {items.map((t) => (
            <li key={t.id} className="schedule" style={t.color ? ({ "--row-c": t.color } as CSSProperties) : undefined}>
              <div className="schedule-top">
                <span className="pill pill-accent"><Icon name="template" /></span>
                <strong className="schedule-title">{t.name}</strong>
                {t.project_id === null ? <span className="pill pill-ws">any project</span> : <span className="pill pill-key">{ws.projects.find((p) => p.id === t.project_id)?.key ?? "?"}</span>}
                <PriorityBadge priority={t.priority} />
              </div>
              <div className="schedule-rule">{t.title}</div>
              <div className="chips">
                {(t.recurring ?? 0) > 0 && (
                  <span className="pill pill-accent" title="Recurring tasks that create their tasks from this template: editing it changes their future tasks">
                    ⟳ {t.recurring} recurring
                  </span>
                )}
                {(t.checklist?.length ?? 0) > 0 && <span className="pill pill-muted">☑ {t.checklist!.length} items</span>}
                {Object.keys(t.custom ?? {}).length > 0 && <span className="pill pill-muted">⚑ {Object.keys(t.custom!).length} fields</span>}
                {(t.labels ?? []).map((n) => (
                  <LabelChip key={n} label={labels.find((l) => l.name.toLowerCase() === n.toLowerCase()) ?? { id: 0, name: n, color: null }} />
                ))}
                {(t.assignees ?? []).map((id) => {
                  const u = users.get(id);
                  return u ? <Avatar key={id} user={u} /> : null;
                })}
              </div>
              {canEdit && (
                <div className="schedule-actions">
                  {confirm === t.id ? (
                    <>
                      <span className="small">Delete this template? Tasks made from it stay.</span>
                      <button type="button" className="button danger-button small" onClick={() => remove(t)}>
                        Delete
                      </button>
                      <button type="button" className="button secondary small" onClick={() => setConfirm(null)}>
                        Keep
                      </button>
                    </>
                  ) : (
                    <>
                      <button type="button" className="button secondary small" onClick={() => setEditing(t)} aria-label={`Edit ${t.name}`}>
                        <Icon name="pencil" /> Edit
                      </button>
                      <button
                        type="button"
                        className="button danger-button small"
                        onClick={() => setConfirm(t.id)}
                        disabled={(t.recurring ?? 0) > 0}
                        title={(t.recurring ?? 0) > 0 ? "Recurring tasks use this template: delete them or give them another template first" : undefined}
                        aria-label={`Delete ${t.name}`}
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
      {canEdit && (
        <div className="row end">
          <button type="button" className="button" onClick={() => setEditing("new")}>
            + New template
          </button>
        </div>
      )}
    </Modal>
  );
}
