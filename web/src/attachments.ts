// Attachments: the pure helpers behind the gig panel's Attachments section (what kind of tile or row
// an attachment gets, sizes in words, the file URLs, who may remove one).
import type { Attachment } from "./api";

// Shown: how the section shows one attachment. image: a thumbnail tile (the lightbox opens the
// file); video and audio: a row with a player; markdown and text: a row that opens the viewer;
// file: a row with a download (with its preview when the server made one, as for TIFF or HEIC);
// link and commit: a chip.
export type Shown = "image" | "video" | "audio" | "markdown" | "text" | "file" | "link" | "commit";

export function shownAs(a: Pick<Attachment, "kind" | "mime" | "inline">): Shown {
  if (a.kind === "link" || a.kind === "commit") return a.kind;
  const m = a.mime ?? "";
  if (!a.inline) return "file"; // the server serves only what it marked inline in the page
  if (m.startsWith("image/")) return "image";
  if (m.startsWith("video/")) return "video";
  if (m.startsWith("audio/")) return "audio";
  if (m === "text/markdown") return "markdown";
  if (m === "text/plain") return "text";
  return "file";
}

export interface AttachmentGroups {
  images: Attachment[];
  media: Attachment[]; // video and audio: rows with a player
  docs: Attachment[]; // Markdown, plain text and other files: one list of rows
  refs: Attachment[]; // links and commits
}

// groupAttachments keeps the server's order (oldest first) inside each group.
// Without Download attachments every file is listed by name only (canDownload false: no previews).
export function groupAttachments(items: Attachment[], canDownload = true): AttachmentGroups {
  const g: AttachmentGroups = { images: [], media: [], docs: [], refs: [] };
  for (const a of items) {
    const s = canDownload || a.kind !== "file" ? shownAs(a) : "file";
    if (s === "image") g.images.push(a);
    else if (s === "video" || s === "audio") g.media.push(a);
    else if (s === "link" || s === "commit") g.refs.push(a);
    else g.docs.push(a);
  }
  return g;
}

// fmtSize: bytes in words, 1024-based like the server's limits (10 MB = 10 MiB): 512 B, 4.2 KB, 12 MB.
export function fmtSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  if (i === 0) return `${bytes} B`;
  const r = v < 10 ? Math.round(v * 10) / 10 : Math.round(v);
  // 1023.6 KB rounds to 1024: say 1 MB instead
  if (r >= 1024 && i < units.length - 1) return `1 ${units[i + 1]}`;
  return `${r} ${units[i]}`;
}

// shortCommit: a commit id as people read it (the first 7 characters).
export const shortCommit = (v: string) => v.trim().slice(0, 7);

// refKind: what "Add link" makes of the text typed: a commit id (7 to 64 hex digits) or a link.
export function refKind(v: string): "commit" | "link" {
  return /^[0-9a-f]{7,64}$/i.test(v.trim()) ? "commit" : "link";
}

export const fileURL = (id: number, download = false) => `/api/v1/attachments/${id}/file${download ? "?download=true" : ""}`;
export const thumbURL = (id: number) => `/api/v1/attachments/${id}/thumb`;

// tileSrc: what an image tile shows: the server's preview, or the file itself (scaled by CSS) when
// there is none (AVIF, a tiny image).
export const tileSrc = (a: Pick<Attachment, "id" | "has_thumb">) => (a.has_thumb ? thumbURL(a.id) : fileURL(a.id));

// attachmentName: what a file or reference is called on screen (its label wins).
export function attachmentName(a: Pick<Attachment, "kind" | "label" | "filename" | "value">): string {
  if (a.label) return a.label;
  if (a.kind === "file") return a.filename ?? "file";
  if (a.kind === "commit") return shortCommit(a.value);
  return a.value;
}

// canRemove: your own attachment with Add attachments, anyone's with Delete attachments (the server's rule).
export function canRemove(a: Pick<Attachment, "created_by">, me: number, canAdd: boolean, canDeleteAny: boolean): boolean {
  return canDeleteAny || (canAdd && a.created_by === me);
}
