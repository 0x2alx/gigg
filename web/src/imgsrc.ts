// The images Markdown in comments and descriptions may show (no DOM here, so it is testable).

const IMG_SRC = /^(?:\/api\/v1\/users\/\d+\/avatar(?:\?v=\d+)?|\/(?!api\/|\/)[\w./-]+\.(?:png|jpe?g|gif|webp|svg|ico))$/i;

// imageSrcOK: an image src a comment may use (only images this server serves; no protocol-relative or
// parent-directory paths)
export const imageSrcOK = (src: string): boolean => IMG_SRC.test(src) && !src.includes("/.");
