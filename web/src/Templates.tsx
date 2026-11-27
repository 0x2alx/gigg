// Every gig template in reach, across the workspaces (narrowed by an optional workspace/project filter;
// a project shows its own and its workspace's "any project" ones): list with badges; new, edit, delete.
// Shared templates (every workspace's) come with each workspace's list: shown once, under any filter,
// changed only with the Shared library permission (on the all-workspaces level).
import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { api, ApiError, canShare, canWriteIn, gigChoices, hasCap, type Tag, type Template } from "./api";
import { useApp, type WorkspaceTree } from "./App";
import { WorkspaceBadge } from "./WorkspaceBadge";
import { PlaceFilter, placeMatch, placeStart, templateStart, type Place } from "./places";
import { GigModal } from "./GigModal";
import { ClearFilters, Avatar, LabelChip, Modal, PriorityBadge } from "./ui";
import { HelpButton } from "./help";
import { Icon } from "./icons";
import { copyJson, ImportJsonModal, importTargets } from "./ImportJsonModal";
import { MakeSharedDialog } from "./MakeSharedDialog";


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
  const [importing, setImporting] = useState(false);
  const [sharing, setSharing] = useState<Template | null>(null); // Make shared…
  const [making, setMaking] = useState<Template | null>(null); // New gig from this
  const closeEdit = useCallback(() => setEditing(null), []);
  const closeMaking = useCallback(() => setMaking(null), []);

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
  const gigPlaces = useMemo(() => gigChoices(me, tree), [me, tree]); // where you may create gigs
  // with Shared library and no workspace to make templates in, a new template can only be shared
  const start = placeStart(place, tree, (w) => editable.includes(w)) ?? (canShare(me) ? placeStart("", tree, () => true) : null);

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

  const copy = (t: Template) => copyJson("gig_template", t, users).then(onChanged, (e: unknown) => setErr(e instanceof Error ? e.message : "Couldn't copy."));
  if (importing)
    return (
      <ImportJsonModal
        kinds={["gig_template"]}
        initialPlace={place}
        onClose={() => setImporting(false)}
        onDone={(msg) => {
          load();
          onChanged(msg);
        }}
      />
    );
  // New gig from this: the New gig form, filled from the template as New gig → From template does
  const makeAt = making ? templateStart(making, place, gigPlaces) : null;
  if (making && makeAt) {
    return (
      <GigModal
        ws={makeAt.ws}
        projectId={makeAt.projectId}
        statuses={[]}
        labels={labelsOf.get(makeAt.ws.id) ?? []}
        choices={gigPlaces}
        fromTemplate={making}
        onClose={closeMaking}
        onCreated={onChanged}
      />
    );
  }
  // a shared template is edited in the first workspace the editor may edit (for its labels and members)
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
        {start && (
          <button type="button" className="button small list-new" onClick={() => setEditing("new")}>
            + New template
          </button>
        )}
        {importTargets("gig_template", tree, me).length > 0 && (
          <button type="button" className={"button secondary small" + (start ? "" : " list-new")} onClick={() => setImporting(true)}>
            Import JSON
          </button>
        )}
      </div>
      {items === null && !err && <p className="muted">Loading…</p>}
      {items !== null && shown.length === 0 && <p className="muted">{items.length === 0 ? "No templates yet. Save one from the new-gig form, or make one here." : "None here. Show everything to see the others."}</p>}
      {shown.length > 0 && (
        <ul className="schedules" aria-label="Templates">
          {shown.map((t) => {
            const tws = t.shared ? undefined : wsOf(t.workspace_id);
            const canEdit = t.shared ? canShare(me) : !!tws && canEditIn(tws);
            const labels = labelsOf.get(t.workspace_id) ?? [];
            return (
            <li key={t.id} className="schedule rec-card" style={t.color ? ({ "--row-c": t.color } as CSSProperties) : undefined}>
              <div className="schedule-top">
                <span className="pill pill-accent"><Icon name="template" /></span>
                <strong className="schedule-title">{t.name}</strong>
                <span className="rec-state">
                  <PriorityBadge priority={t.priority} />
                  {t.project_id === null ? <span className="pill pill-ws">any project</span> : <span className="pill pill-key">{tws?.projects.find((p) => p.id === t.project_id)?.key ?? "?"}</span>}
                  {t.shared ? (
                    <span className="pill pill-shared" data-tip="Offered in every workspace; changed with the Shared library permission">
                      Shared
                    </span>
                  ) : (
                    tws && <WorkspaceBadge ws={tws} />
                  )}
                </span>
              </div>
              {/* the title its gigs get, when it says more than the template's name */}
              {t.title.trim().toLowerCase() !== t.name.trim().toLowerCase() && <div className="schedule-rule">{t.title}</div>}
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
              </div>
              {/* the foot: the actions on the left, who gets the gigs in the bottom-right corner */}
              <div className="rec-foot">
                <div className="schedule-actions">
                  {canEdit && confirm === t.id ? (
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
                      {templateStart(t, place, gigPlaces) && (
                        <button type="button" className="button small" onClick={() => setMaking(t)} aria-label={`New gig from ${t.name}`}>
                          <Icon name="plus" /> New gig
                        </button>
                      )}
                      <button type="button" className="button secondary small" onClick={() => void copy(t)} aria-label={`Copy ${t.name} as JSON`}>
                        <Icon name="copy" /> Copy JSON
                      </button>
                      {!t.shared && canShare(me) && (
                        <button type="button" className="button secondary small" onClick={() => setSharing(t)} aria-label={`Make ${t.name} shared`}>
                          <Icon name="share" /> Make shared…
                        </button>
                      )}
                      {canEdit && (
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
                    </>
                  )}
                </div>
                {(t.assignees ?? []).length > 0 && (
                  <span className="rec-people" aria-label="Assignees">
                    {(t.assignees ?? []).map((id) => {
                      const u = users.get(id);
                      return u ? <Avatar key={id} user={u} size="s" tip={`Assigned to ${u.display_name || u.username}`} /> : null;
                    })}
                  </span>
                )}
              </div>
            </li>
            );
          })}
        </ul>
      )}
      {sharing && (
        <MakeSharedDialog
          kind="template"
          id={sharing.id}
          name={sharing.name}
          onClose={() => setSharing(null)}
          onDone={(msg) => {
            load();
            onChanged(msg);
          }}
        />
      )}
    </Modal>
  );
}
