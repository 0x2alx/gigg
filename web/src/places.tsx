// Places: the workspace / project filter of the lists that span every workspace (recurring gigs,
// templates, schedules). The value is "" (everything), "w:<id>" (a workspace) or "p:<id>" (a project).
import type { WorkspaceTree } from "./App";

export type Place = string;

// placeMatch: whether something in workspace `ws` (and project `pid`; null: the whole workspace, as a
// template for any project) shows under the filter. A project filter keeps workspace-wide things of
// that project's workspace, since they apply there too.
export function placeMatch(place: Place, tree: WorkspaceTree[], ws: number, pid: number | null): boolean {
  if (!place) return true;
  const id = Number(place.slice(2));
  if (place.startsWith("w:")) return ws === id;
  if (place.startsWith("p:")) {
    if (pid === id) return true;
    return pid === null && tree.some((w) => w.id === ws && w.projects.some((p) => p.id === id));
  }
  return true;
}

// placeStart: where something new starts under the filter: its project, the workspace's first project,
// else the first workspace (with a project) that `ok` allows.
export function placeStart(place: Place, tree: WorkspaceTree[], ok: (w: WorkspaceTree) => boolean): { ws: WorkspaceTree; projectId: number } | null {
  const id = Number(place.slice(2));
  if (place.startsWith("p:")) {
    const ws = tree.find((w) => ok(w) && w.projects.some((p) => p.id === id));
    if (ws) return { ws, projectId: id };
  }
  if (place.startsWith("w:")) {
    const ws = tree.find((w) => w.id === id && ok(w) && w.projects.length > 0);
    if (ws) return { ws, projectId: ws.projects[0]!.id };
  }
  const ws = tree.find((w) => ok(w) && w.projects.length > 0);
  return ws ? { ws, projectId: ws.projects[0]!.id } : null;
}

// optionOf: the option showing a place. A project of a workspace listed without its projects (just one,
// or projects off) shows as that workspace.
export function optionOf(place: Place, tree: WorkspaceTree[], projects = true): Place {
  if (!place.startsWith("p:")) return place;
  const id = Number(place.slice(2));
  const w = tree.find((x) => x.projects.some((p) => p.id === id));
  if (!w) return "";
  return projects && w.projects.length > 1 ? place : `w:${w.id}`;
}

// PlaceFilter: "Everywhere", each workspace and (with projects) its projects, as one select.
// plain: only the select, to sit in a row of other plain filters (the table's), without the "Show" label.
export function PlaceFilter({ value, onChange, tree, projects = true, label = "Show", plain }: {
  value: Place; onChange: (v: Place) => void; tree: WorkspaceTree[]; projects?: boolean; label?: string; plain?: boolean;
}) {
  if (tree.length < 2 && !(projects && (tree[0]?.projects.length ?? 0) > 1)) return null;
  const shown = optionOf(value, tree, projects);
  const select = (
      <select value={shown} onChange={(e) => onChange(e.target.value)} className={shown ? "active" : undefined} aria-label={plain ? "Workspace or project" : label}>
        <option value="">{tree.length > 1 ? "All workspaces" : "All projects"}</option>
        {tree.map((w) =>
          projects && w.projects.length > 1 ? (
            <optgroup key={w.id} label={w.name}>
              <option value={`w:${w.id}`}>{w.name}: all projects</option>
              {w.projects.map((p) => (
                <option key={p.id} value={`p:${p.id}`}>
                  {p.key} · {p.name}
                </option>
              ))}
            </optgroup>
          ) : (
            <option key={w.id} value={`w:${w.id}`}>
              {w.name}
            </option>
          ),
        )}
      </select>
  );
  if (plain) return select;
  return (
    <label className="bf-pick place-filter">
      <span className="bf-pick-label" aria-hidden="true">{label}</span>
      {select}
    </label>
  );
}
