import { describe, expect, it } from "vitest";
import { clampPan, doubleTap, FIT, MAX_ZOOM, panBy, stepTo, swipeStep, typingIn, wheelFactor, zoomAt, zoomed } from "./zoom";

// The Lightbox's zoom: a scale and an offset on the image's fitted box (here 800 x 600).
const W = 800;
const H = 600;
// where an image point (ix, iy) lands on screen, relative to the fitted box
const at = (v: { s: number; x: number; y: number }, ix: number, iy: number) => [v.x + ix * v.s, v.y + iy * v.s];

describe("zoom around the pointer", () => {
  it("keeps the image point under the pointer in place", () => {
    const v = zoomAt(FIT, 2, 200, 150, W, H);
    expect(v.s).toBe(2);
    expect(at(v, 200, 150)).toEqual([200, 150]);
    // zooming again elsewhere keeps that new point too
    const w = zoomAt(v, 1.5, 500, 400, W, H);
    const ix = (500 - v.x) / v.s;
    const iy = (400 - v.y) / v.s;
    expect(at(w, ix, iy).map((n) => Math.round(n))).toEqual([500, 400]);
  });

  it("never goes below fit or above the maximum", () => {
    expect(zoomAt(FIT, 0.5, 100, 100, W, H)).toEqual(FIT);
    expect(zoomAt({ s: 2, x: -100, y: -100 }, 0.4, 100, 100, W, H)).toEqual(FIT);
    expect(zoomAt(FIT, 100, 0, 0, W, H).s).toBe(MAX_ZOOM);
  });

  it("zooming out near a corner pulls the image back over its box", () => {
    // zoomed 4x with the bottom-right corner in view, then out to 2x around the top-left
    const v = zoomAt({ s: 4, x: -2400, y: -1800 }, 0.5, 0, 0, W, H);
    expect(v.s).toBe(2);
    expect(v.x).toBeGreaterThanOrEqual(W - 2 * W);
    expect(v.y).toBeGreaterThanOrEqual(H - 2 * H);
  });

  it("zoomed tells a zoomed view from fit", () => {
    expect(zoomed(FIT)).toBe(false);
    expect(zoomed({ s: 1.5, x: 0, y: 0 })).toBe(true);
  });
});

describe("pan", () => {
  it("moves a zoomed image, but no edge past the same edge of its box", () => {
    const v = { s: 2, x: -400, y: -300 };
    expect(panBy(v, 100, -50, W, H)).toEqual({ s: 2, x: -300, y: -350 });
    expect(panBy(v, 1000, 1000, W, H)).toEqual({ s: 2, x: 0, y: 0 });
    expect(panBy(v, -1000, -1000, W, H)).toEqual({ s: 2, x: -800, y: -600 });
  });

  it("an image at fit does not move", () => {
    expect(panBy(FIT, 100, 100, W, H)).toEqual(FIT);
    expect(clampPan({ s: 1, x: 40, y: 0 }, W, H)).toEqual(FIT);
  });
});

describe("wheel", () => {
  it("up zooms in, down zooms out, a pinch (ctrl) faster than a wheel notch of the same size", () => {
    expect(wheelFactor(-100, 0, false)).toBeGreaterThan(1);
    expect(wheelFactor(100, 0, false)).toBeLessThan(1);
    expect(wheelFactor(-10, 0, true)).toBeGreaterThan(wheelFactor(-10, 0, false));
  });

  it("line deltas count like pixels, and one huge event is capped", () => {
    expect(wheelFactor(-3, 1, false)).toBeCloseTo(wheelFactor(-48, 0, false));
    expect(wheelFactor(-100000, 0, false)).toBeCloseTo(wheelFactor(-200, 0, false));
  });
});

describe("input rules", () => {
  it("keys in a text field belong to the field", () => {
    expect(typingIn({ tagName: "INPUT", type: "search" } as unknown as EventTarget)).toBe(true);
    expect(typingIn({ tagName: "INPUT", type: "text" } as unknown as EventTarget)).toBe(true);
    expect(typingIn({ tagName: "TEXTAREA" } as unknown as EventTarget)).toBe(true);
    expect(typingIn({ tagName: "DIV", isContentEditable: true } as unknown as EventTarget)).toBe(true);
    expect(typingIn({ tagName: "INPUT", type: "checkbox" } as unknown as EventTarget)).toBe(false);
    expect(typingIn({ tagName: "BUTTON" } as unknown as EventTarget)).toBe(false);
    expect(typingIn(null)).toBe(false);
  });

  it("steps stop at both ends", () => {
    expect(stepTo(0, -1, 3)).toBe(0);
    expect(stepTo(0, 1, 3)).toBe(1);
    expect(stepTo(2, 1, 3)).toBe(2);
    expect(stepTo(0, 1, 1)).toBe(0);
  });

  it("a swipe is quick and mostly sideways; left means next", () => {
    expect(swipeStep(-120, 10, 200)).toBe(1);
    expect(swipeStep(120, -20, 200)).toBe(-1);
    expect(swipeStep(-30, 0, 200)).toBe(0); // too short
    expect(swipeStep(-120, 100, 200)).toBe(0); // too diagonal
    expect(swipeStep(-120, 0, 2000)).toBe(0); // too slow
  });

  it("a double tap is two taps close in time and place", () => {
    const first = { t: 1000, x: 100, y: 100 };
    expect(doubleTap(first, 1200, 110, 105)).toBe(true);
    expect(doubleTap(first, 1500, 100, 100)).toBe(false);
    expect(doubleTap(first, 1200, 200, 100)).toBe(false);
    expect(doubleTap(null, 1200, 100, 100)).toBe(false);
  });
});
