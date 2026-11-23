// previews: what a placeholder URL (a file pasted into a gig that doesn't exist yet, uploaded on
// Create) shows in the visual editor meanwhile: the file itself, from a blob: URL in this tab.
const shown = new Map<string, string>();

export function setPreview(url: string, file: File): void {
  if (!/^pending-upload:\d+$/.test(url) || typeof URL.createObjectURL !== "function") return;
  const old = shown.get(url);
  if (old) URL.revokeObjectURL(old);
  shown.set(url, URL.createObjectURL(file));
}

export const previewOf = (url: string): string | undefined => shown.get(url);

export function clearPreviews(): void {
  for (const u of shown.values()) URL.revokeObjectURL(u);
  shown.clear();
}
