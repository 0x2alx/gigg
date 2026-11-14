// WorkspaceBadge: WorkspaceChip with the owner looked up among the users the app knows. Kept apart from ui.tsx,
// which stays free of the app context (its helpers are tested outside a browser).
import { useApp } from "./App";
import { WorkspaceChip } from "./ui";

export function WorkspaceBadge({ ws }: { ws: { slug: string; name: string; color?: string | null; owner_id?: number | null } }) {
  const { users } = useApp();
  return <WorkspaceChip ws={ws} owner={ws.owner_id != null ? users.get(ws.owner_id) : undefined} />;
}
