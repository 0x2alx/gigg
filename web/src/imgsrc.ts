// The images and same-origin links Markdown in comments and descriptions may use (no DOM here, so it
// is testable).

const IMG_SRC = /^(?:\/api\/v1\/users\/\d+\/avatar(?:\?v=\d+)?|\/api\/v1\/attachments\/\d+\/(?:file|thumb)|\/(?!api\/|\/)[\w./-]+\.(?:png|jpe?g|gif|webp|svg|ico))$/i;

// imageSrcOK: an image src a comment may use (only images this server serves: avatars, attachments
// and static image files; no protocol-relative or parent-directory paths)
export const imageSrcOK = (src: string): boolean => IMG_SRC.test(src) && !src.includes("/.");

// attachmentHref: the one same-origin link Markdown may use besides http(s) and mailto: an
// attachment's file (optionally as a download), so a report or a log can be linked from text.
export const attachmentHrefOK = (href: string): boolean => /^\/api\/v1\/attachments\/\d+\/file(?:\?download=true)?$/.test(href);

// attachmentOf: the attachment id an image or link points at (null for anything else).
export function attachmentOf(url: string): number | null {
  const m = /^\/api\/v1\/attachments\/(\d+)\/(?:file|thumb)(?:\?download=true)?$/.exec(url);
  return m ? Number(m[1]) : null;
}
