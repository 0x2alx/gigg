// Collapsed board columns, per project, remembered in this browser (a convenience: when storage
// is unavailable the choice lasts for the page).
const key = (projectId: number) => `gigg.board.collapsed.${projectId}`;

export function loadCollapsed(projectId: number): number[] {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(key(projectId)) ?? "[]");
    return Array.isArray(v) ? v.filter((x): x is number => Number.isInteger(x)) : [];
  } catch {
    return [];
  }
}

export function saveCollapsed(projectId: number, ids: number[]): void {
  try {
    if (ids.length) localStorage.setItem(key(projectId), JSON.stringify(ids));
    else localStorage.removeItem(key(projectId));
  } catch {
    /* storage unavailable */
  }
}

export const toggleId = (ids: number[], id: number): number[] => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]);

// The board's grid: a collapsed column is a narrow strip, the others share the rest.
export const boardColumns = (statusIds: number[], collapsed: number[]): string =>
  statusIds.map((id) => (collapsed.includes(id) ? "2.6rem" : "minmax(0, 1fr)")).join(" ");
