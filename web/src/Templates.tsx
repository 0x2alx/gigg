// Every gig template in reach, across the workspaces (narrowed by an optional workspace/project filter;
// a project shows its own and its workspace's "any project" ones): list with badges; new, edit, delete.
// Shared templates (every workspace's) come with each workspace's list: shown once, under any filter,
// changed only by instance admins.
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { api, ApiError, canWriteIn, hasCap, type Tag, type Template } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { PlaceFilter, placeMatch, placeStart, type Place } from "./places";
import { WorkspaceBadge } from "./WorkspaceBadge";
import { GigModal } from "./GigModal";
import { ClearFilters, Avatar, LabelChip, Modal, PriorityBadge } from "./ui";
import { HelpButton } from "./help";
import { Icon } from "./icons";


export function TemplatesModal({ initialPlace = "", onClose, onChanged }: {
  initialPlace?: Place;
  onClose: () => void;
  onChanged: (message?: string) => void;
}) {
  const { users, tree, me } = useApp();
  const [items, setItems] = useState<Template[] | null>(null);
  const [labelsOf, setLabelsOf] = useState<Map<number, Tag[]>>(new Map());
  const [place, setPlace] = useState<Place>(initialPlace);
  const [err, setErr] = useState("");
  const [editing, setEditing] = useState<Template | "new" | null>(null);
  const [confirm, setConfirm] = useState<number | null>(null);
  const closeEdit = useCallback(() => setEditing(null), []);

  const wsKey = tree.map((w) => w.id).join(",");
  const loadSeq = useRef(0);
  const load = useCallback(() => {
    const seq = ++loadSeq.current;
    Promise.allSettled(tree.map((w) => api.templates(w.id))).then((res) => {
      if (seq !== loadSeq.current) return; // a newer load is on its way
      const all: Template[] = [];
      const bad: string[] = [];
      res.forEach((r, i) => (r.status === "fulfilled" ? all.push(...r.value.items) : bad.push(tree[i]!.name)));
      const seen = new Set<number>(); // a shared one comes with every workspace's list
      setItems(all.filter((t) => !seen.has(t.id) && !!seen.add(t.id)).sort((a, b) => Number(!!a.shared) - Number(!!b.shared) || a.name.localeCompare(b.name)));
      setErr(bad.length ? `Couldn't load the templates of ${bad.join(", ")}.` : "");
    });
  }, [wsKey]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(load, [load]);
  useEffect(() => {
    Promise.allSettled(tree.map((w) => api.labels(w.slug))).then((res) =>
      setLabelsOf(new Map(res.flatMap((r, i) => (r.status === "fulfilled" ? [[tree[i]!.id, r.value.items] as [number, Tag[]]] : [])))),
    );
  }, [wsKey]); // eslint-disable-line react-hooks/exhaustive-deps
  const wsOf = useCallback((id: number) => tree.find((w) => w.id === id), [tree]);
  const canEditIn = useCallback((w: WorkspaceTree) => canWriteIn(w) && hasCap(me, "templates", w.id), [me]);
  const shown = useMemo(() => (items ?? []).filter((t) => t.shared || placeMatch(place, tree, t.workspace_id, t.project_id)), [items, place, tree]);
  const editable = useMemo(() => tree.filter(canEditIn), [tree, canEditIn]);
  const start = placeStart(place, tree, (w) => editable.includes(w));

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

  // a shared template is edited in the first workspace the admin may edit (for its labels and members)
  const editWs = editing && editing !== "new" ? (editing.shared ? (editable[0] ?? tree[0]) : wsOf(editing.workspace_id)) : undefined;
  if (editing && (editing === "new" ? start : editWs)) {
    const w = editing === "new" ? start!.ws : editWs!;
    return (
      <GigModal
        ws={w}
        projectId={editing === "new" ? start!.projectId : (editing.project_id ?? w.projects[0]?.id ?? 0)}
        statuses={[]}
        labels={labelsOf.get(w.id) ?? []}
        choices={editing === "new" ? editable : undefined}
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
    <Modal title="Templates" onClose={onClose} wide help={<HelpButton topic="recurring" label="Help: templates, schedules and recurring gigs" />}>
      {err && (
        <p className="danger" role="alert">
          {err}
        </p>
      )}
      <div className="list-bar">
        <PlaceFilter value={place} onChange={setPlace} tree={tree} />
        <ClearFilters active={place !== ""} onClear={() => setPlace("")} />
        {items && <span className="muted small">{shown.length === items.length ? `${items.length} template${items.length === 1 ? "" : "s"}` : `${shown.length} of ${items.length}`}</span>}
      </div>
      {items === null && !err && <p className="muted">Loading…</p>}
      {items !== null && shown.length === 0 && <p className="muted">{items.length === 0 ? "No templates yet. Save one from the new-gig form, or make one here." : "None here. Show everything to see the others."}</p>}
      {shown.length > 0 && (
        <ul className="schedules" aria-label="Templates">
          {shown.map((t) => {
            const tws = t.shared ? undefined : wsOf(t.workspace_id);
            const canEdit = t.shared ? me.admin : !!tws && canEditIn(tws);
            const labels = labelsOf.get(t.workspace_id) ?? [];
            return (
            <li key={t.id} className="schedule" style={t.color ? ({ "--row-c": t.color } as CSSProperties) : undefined}>
              <div className="schedule-top">
                <span className="pill pill-accent"><Icon name="template" /></span>
                <strong className="schedule-title">{t.name}</strong>
                {t.shared ? (
                  <span className="pill pill-shared" data-tip="Offered in every workspace; only instance admins change it">
                    Shared
                  </span>
                ) : (
                  tws && tree.length > 1 && <WorkspaceBadge ws={tws} />
                )}
                {t.project_id === null ? <span className="pill pill-ws">any project</span> : <span className="pill pill-key">{tws?.projects.find((p) => p.id === t.project_id)?.key ?? "?"}</span>}
                <PriorityBadge priority={t.priority} />
              </div>
              <div className="schedule-rule">{t.title}</div>
              <div className="chips">
                {(t.recurring ?? 0) > 0 && (
                  <span className="pill pill-accent" title="Recurring gigs that create their gigs from this template: editing it changes their future gigs">
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
                      <span className="small">Delete this template? Gigs made from it stay.</span>
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
                        title={(t.recurring ?? 0) > 0 ? "Recurring gigs use this template: delete them or give them another template first" : undefined}
                        aria-label={`Delete ${t.name}`}
                      >
                        <Icon name="trash" /> Delete
                      </button>
                    </>
                  )}
                </div>
              )}
            </li>
            );
          })}
        </ul>
      )}
      {start && (
        <div className="row end">
          <button type="button" className="button" onClick={() => setEditing("new")}>
            + New template
          </button>
        </div>
      )}
    </Modal>
  );
}
