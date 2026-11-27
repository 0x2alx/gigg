// zoom: the pure geometry behind the Lightbox's zoom and pan, and the small input rules around it
// (which keys belong to a text field, what counts as a swipe or a double tap).
//
// A view is a scale and an offset applied to the image's fitted box (where it sits unzoomed):
// CSS transform translate(x, y) scale(s) with the origin at the box's top-left corner.

export interface View {
  s: number;
  x: number;
  y: number;
}

export const FIT: View = { s: 1, x: 0, y: 0 };
export const MAX_ZOOM = 8;

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

export const zoomed = (v: View) => v.s > 1.001;

// clampPan keeps the zoomed image covering its fitted box: it can be dragged until an edge of the
// image reaches the same edge of the box, never further (no empty gap opens inside the box).
export function clampPan(v: View, w: number, h: number): View {
  if (!zoomed(v)) return FIT;
  return { s: v.s, x: clamp(v.x, w - v.s * w, 0), y: clamp(v.y, h - v.s * h, 0) };
}

// zoomAt scales the view by factor around (px, py), a point given relative to the fitted box's
// top-left corner: the part of the image under that point stays under it. Back at 1, it is FIT.
export function zoomAt(v: View, factor: number, px: number, py: number, w: number, h: number): View {
  const s = clamp(v.s * factor, 1, MAX_ZOOM);
  if (s <= 1.001) return FIT;
  const ix = (px - v.x) / v.s;
  const iy = (py - v.y) / v.s;
  return clampPan({ s, x: px - ix * s, y: py - iy * s }, w, h);
}

export const panBy = (v: View, dx: number, dy: number, w: number, h: number): View => clampPan({ s: v.s, x: v.x + dx, y: v.y + dy }, w, h);

// wheelFactor: how much one wheel event zooms. A trackpad pinch arrives as a wheel event with
// ctrlKey and small deltas, so it gets a stronger rate; line and page deltas count as pixels.
export function wheelFactor(deltaY: number, deltaMode: number, ctrlKey: boolean): number {
  const px = deltaMode === 1 ? deltaY * 16 : deltaMode === 2 ? deltaY * 400 : deltaY;
  return Math.exp(-clamp(px, -200, 200) * (ctrlKey ? 0.01 : 0.002));
}

// typingIn: a key pressed here belongs to the field (a search box, an editor), not to the viewer.
const NOT_TEXT = ["button", "checkbox", "radio", "submit", "reset", "range", "color", "file", "image"];
export function typingIn(el: EventTarget | null): boolean {
  const e = el as { tagName?: unknown; type?: unknown; isContentEditable?: boolean } | null;
  if (!e || typeof e.tagName !== "string") return false;
  if (e.isContentEditable) return true;
  const tag = e.tagName.toUpperCase();
  if (tag === "TEXTAREA" || tag === "SELECT") return true;
  if (tag === "INPUT") return !NOT_TEXT.includes(String(e.type ?? "text").toLowerCase());
  return false;
}

// swipeStep: a quick, mostly sideways finger move is a step: left = the next item (+1), right = the
// previous one (-1); anything else is 0.
export function swipeStep(dx: number, dy: number, ms: number): -1 | 0 | 1 {
  if (ms > 800 || Math.abs(dx) < 50 || Math.abs(dx) < 1.5 * Math.abs(dy)) return 0;
  return dx < 0 ? 1 : -1;
}

// stepTo: the item d steps away, or the same one at either end (no wrapping around).
export const stepTo = (at: number, d: number, n: number) => (at + d >= 0 && at + d < n ? at + d : at);

// doubleTap: two taps close in time and place.
export function doubleTap(prev: { t: number; x: number; y: number } | null, t: number, x: number, y: number): boolean {
  return !!prev && t - prev.t < 320 && Math.hypot(x - prev.x, y - prev.y) < 30;
}
