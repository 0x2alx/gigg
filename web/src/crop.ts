// Square crop maths for the avatar dialog: the image is shown scaled in a square box of `box` px, at
// `zoom` × the scale that just covers the box, moved by `off` (where its top-left corner sits, relative
// to the box's, in box px: always ≤ 0, and never so far that the box shows past the image's edge).

export interface Pt {
  x: number;
  y: number;
}

export const MIN_ZOOM = 1;
export const MAX_ZOOM = 4;

// coverScale: the scale at zoom 1, where the image's short side fills the box.
export const coverScale = (iw: number, ih: number, box: number): number => box / Math.min(iw, ih);

export const clampZoom = (z: number): number => Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, z));

// clampOffset keeps the box covered by the image.
export function clampOffset(off: Pt, iw: number, ih: number, scale: number, box: number): Pt {
  const lim = (v: number, size: number) => Math.min(0, Math.max(box - size * scale, v));
  return { x: lim(off.x, iw), y: lim(off.y, ih) };
}

// centred: the offset that centres the image in the box at this scale.
export const centred = (iw: number, ih: number, scale: number, box: number): Pt => ({ x: (box - iw * scale) / 2, y: (box - ih * scale) / 2 });

// zoomAround: the offset after changing the scale so the point p of the box (box px) stays put.
export function zoomAround(off: Pt, from: number, to: number, p: Pt): Pt {
  const k = to / from;
  return { x: p.x - (p.x - off.x) * k, y: p.y - (p.y - off.y) * k };
}

// sourceSquare: the part of the image (image px) the box shows.
export function sourceSquare(off: Pt, scale: number, box: number): { sx: number; sy: number; size: number } {
  return { sx: -off.x / scale, sy: -off.y / scale, size: box / scale };
}
