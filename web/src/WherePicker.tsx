import { useEffect, useState, type CSSProperties } from "react";
import type { WorkspaceTree } from "./App";
import type { User } from "./api";
import { whereLabel, whereTree, placeName, type BoardFilter } from "./boardModel";
import { colorFor } from "./theme";
import { Avatar, usePopover } from "./ui";
import { Icon } from "./icons";

// WherePicker: the board toolbar's "Where" button. Its panel is a search box and the workspaces as
// checkbox rows with their projects indented under them (a single workspace: just its projects). It
// edits the same filter as the chips under the toolbar, through the board's own toggles, and stays
// open while you tick. The button names the selection.
export function WherePicker({ tree, filter, users, onWs, onProject, onAll }: {
  tree: WorkspaceTree[];
  filter: BoardFilter;
  users: Map<number, User>;
  onWs: (slug: string) => void;
  onProject: (id: number) => void;
  onAll: () => void;
}) {
  const { open, setOpen, ref, panelRef } = usePopover();
  const [q, setQ] = useState("");
  useEffect(() => {
    if (!open) setQ("");
  }, [open]);
  const label = whereLabel(tree, filter);
  const rows = whereTree(tree, filter, q);
  const flat = tree.length === 1;
  const none = filter.ws.length === 0 && filter.p.length === 0;
  return (
    <div
      className="more-filters where-picker"
      ref={ref}
      onBlur={(e) => {
        if (open && e.relatedTarget && !e.currentTarget.contains(e.relatedTarget as Node)) setOpen(false);
      }}
    >
      <button
        type="button"
        className={"mf-button wp-button" + (none ? "" : " on")}
        aria-haspopup="dialog"
        aria-expanded={open}
        data-tip="Choose the workspaces and projects shown"
        onClick={() => setOpen((o) => !o)}
      >
        <Icon name="layers" />
        <span className="wp-face">Where: {label}</span>
        <Icon name={open ? "chevron-up" : "chevron-down"} />
      </button>
      {open && (
        <div className="wp-panel" role="dialog" aria-label="Where" ref={panelRef}>
          <div className="wp-head">
            <input type="search" aria-label="Search workspaces and projects" placeholder="Search places" value={q} onChange={(e) => setQ(e.target.value)} autoFocus />
            <button type="button" className={"wp-all" + (none ? " on" : "")} aria-pressed={none} data-tip="Show everything (clears the workspace and project choices)" onClick={onAll}>
              All
            </button>
          </div>
          <div className="wp-tree">
            {rows.length === 0 && <div className="wp-empty muted">Nothing matches.</div>}
            {rows.map(({ ws, projects }) => {
              const c = colorFor(ws.slug, ws.color);
              const owner = ws.owner_id != null ? users.get(ws.owner_id) : undefined;
              return (
                <div key={ws.id} className="wp-ws" role="group" aria-label={ws.name}>
                  {!flat && (
                    <label className="wp-row wp-ws-row">
                      <input type="checkbox" checked={filter.ws.includes(ws.slug)} onChange={() => onWs(ws.slug)} />
                      {owner ? <Avatar user={owner} size="xs" /> : <span className="dot" style={{ background: c } as CSSProperties} aria-hidden="true" />}
                      <span className="wp-name">{ws.name}</span>
                    </label>
                  )}
                  {projects.map((pr) => (
                    <label key={pr.id} className={"wp-row wp-p-row" + (flat ? "" : " wp-indent")} data-tip={`${placeName(ws, pr)} (${pr.key})`}>
                      <input type="checkbox" checked={filter.p.includes(pr.id)} onChange={() => onProject(pr.id)} />
                      <span className="dot" style={{ background: c } as CSSProperties} aria-hidden="true" />
                      <span className="wp-name">{pr.name}</span>
                    </label>
                  ))}
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
