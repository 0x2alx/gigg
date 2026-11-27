// Lightbox: the one viewer for what a gig shows full size. The Attachments section opens it on its
// images and text files, Markdown (descriptions, comments) on the images of that one text.
// - Several items: ‹ › at the sides, the Left/Right keys or a swipe step through them in order, and
//   "3 / 12" in the head says where you are. No wrapping: the arrow at an end is hidden.
// - Images: the wheel (or a trackpad pinch, which arrives as ctrl+wheel) zooms around the pointer, a
//   finger pinch on a phone too; zoomed in, dragging pans. A double-click or double tap, or the 0 key,
//   goes back to fit (from fit, a double-click zooms in there). Left/Right step only at fit, and the
//   next image opens at fit.
// - Fullscreen: the head's button or the F key shows the image area on the whole screen; the first
//   Esc leaves fullscreen (the Modal's rule), the second closes.
// - Esc, ✕ or a click beside the image closes.
import { Fragment, useCallback, useEffect, useLayoutEffect, useReducer, useRef, useState, type MouseEvent, type PointerEvent, type ReactNode } from "react";
import { Icon } from "./icons";
import { Modal } from "./ui";
import { doubleTap, FIT, panBy, stepTo, swipeStep, typingIn, wheelFactor, zoomAt, zoomed, type View } from "./zoom";

export interface ViewItem {
  key: string; // stable across reloads of the list
  title: string;
  image?: { src: string; alt: string }; // an image: zoom, pan and fullscreen
  body?: ReactNode; // anything else, shown in a text dialog (a Markdown or text file)
  foot?: ReactNode;
}

const canFullscreen = () => typeof document !== "undefined" && !!document.fullscreenEnabled;

interface Gesture {
  x0: number;
  y0: number;
  t0: number;
  moved: boolean;
  pinched: boolean;
  pinch?: { d: number; mx: number; my: number };
}

export function Lightbox({ items, start = 0, onClose }: { items: ViewItem[]; start?: number; onClose: () => void }) {
  // the item shown, by key: the list can change while open (an attachment added or removed live);
  // when the shown one goes, its neighbour takes its place
  const [atKey, setAtKey] = useState(() => items[start]?.key ?? "");
  const lastAt = useRef(start);
  const n = items.length;
  let i = items.findIndex((x) => x.key === atKey);
  if (i < 0) i = Math.max(0, Math.min(lastAt.current, n - 1));
  lastAt.current = i;
  const item = items[i];
  const itemsRef = useRef(items);
  itemsRef.current = items;
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    if (n === 0) closeRef.current();
  }, [n]);

  const stage = useRef<HTMLDivElement>(null); // the image area (what goes fullscreen)
  const body = useRef<HTMLDivElement>(null);
  const prevBtn = useRef<HTMLButtonElement>(null);
  const nextBtn = useRef<HTMLButtonElement>(null);

  // the zoom belongs to one item: another item (stepping) is at fit without a reset to forget
  const cur = useRef({ key: "", i: 0, n: 0, image: false });
  cur.current = { key: item?.key ?? "", i, n, image: !!item?.image };
  const zoom = useRef<{ key: string; v: View }>({ key: "", v: FIT });
  const [, rerender] = useReducer((x: number) => x + 1, 0);
  const getV = () => (zoom.current.key === cur.current.key ? zoom.current.v : FIT);
  const setV = (v: View) => {
    zoom.current = { key: cur.current.key, v };
    rerender();
  };
  const v = getV();

  // a step taken while an arrow had the focus (its click, or a key after it) keeps the focus on an
  // arrow: the same one, or the other when that one goes away (the end, or a text file's layout)
  const refocus = useRef(0);
  const go = useCallback((d: number) => {
    const c = cur.current;
    const to = stepTo(c.i, d, c.n);
    if (to === c.i) return false;
    const a = document.activeElement;
    refocus.current = a && (a === prevBtn.current || a === nextBtn.current) ? d : 0;
    setAtKey(itemsRef.current[to]!.key);
    return true;
  }, []);
  useLayoutEffect(() => {
    const d = refocus.current;
    refocus.current = 0;
    const a = document.activeElement;
    if (!d || (a && (a === prevBtn.current || a === nextBtn.current))) return;
    const [same, other] = d < 0 ? [prevBtn.current, nextBtn.current] : [nextBtn.current, prevBtn.current];
    (same ?? other)?.focus();
  }, [item?.key]);

  // the fitted box of the image (where it sits unzoomed), from layout, not from the drawn transform
  const box = () => {
    const s = stage.current;
    const img = s?.querySelector("img");
    if (!s || !img) return null;
    const r = s.getBoundingClientRect();
    return { left: r.left + s.clientLeft + img.offsetLeft, top: r.top + s.clientTop + img.offsetTop, w: img.offsetWidth, h: img.offsetHeight };
  };
  const zoomAround = (factor: number, cx: number, cy: number) => {
    const b = box();
    if (b) setV(zoomAt(getV(), factor, cx - b.left, cy - b.top, b.w, b.h));
  };
  const toggleZoom = (cx: number, cy: number) => (zoomed(getV()) ? setV(FIT) : zoomAround(2.5, cx, cy));

  // fullscreen: on the image area; it follows the browser's state (Esc, the system's own exit)
  const [fs, setFs] = useState(false);
  const toggleFs = useCallback(() => {
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
    else void stage.current?.requestFullscreen().catch(() => undefined);
  }, []);
  useEffect(() => {
    const change = () => {
      setFs(!!stage.current && document.fullscreenElement === stage.current);
      zoom.current = { key: "", v: FIT }; // the box changed size: back to fit
      rerender();
    };
    document.addEventListener("fullscreenchange", change);
    return () => {
      document.removeEventListener("fullscreenchange", change);
      const el = document.fullscreenElement;
      if (el && body.current?.contains(el)) void document.exitFullscreen().catch(() => undefined);
    };
  }, []);
  // a text file is not shown fullscreen: stepping to one leaves it
  useEffect(() => {
    if (!item?.image && document.fullscreenElement && body.current?.contains(document.fullscreenElement)) void document.exitFullscreen().catch(() => undefined);
  }, [item?.image]);

  // keys: only while this is the top dialog, never while typing in a field
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || typingIn(e.target)) return;
      const dialog = body.current?.closest('[role="dialog"]');
      if (!dialog || [...document.querySelectorAll('[role="dialog"][aria-modal="true"]')].pop() !== dialog) return;
      if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
        if (zoomed(getV())) return;
        if (go(e.key === "ArrowLeft" ? -1 : 1)) e.preventDefault();
      } else if ((e.key === "f" || e.key === "F") && cur.current.image && canFullscreen()) {
        e.preventDefault();
        toggleFs();
      } else if (e.key === "0" && cur.current.image) {
        e.preventDefault();
        setV(FIT);
      }
    };
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
    // getV and setV read refs only
  }, [go, toggleFs]);

  // the wheel zooms: a native listener, since React's is passive and can't keep the page from scrolling
  const isImage = !!item?.image;
  useEffect(() => {
    const el = stage.current;
    if (!el) return;
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      zoomAround(wheelFactor(e.deltaY, e.deltaMode, e.ctrlKey), e.clientX, e.clientY);
    };
    el.addEventListener("wheel", wheel, { passive: false });
    return () => el.removeEventListener("wheel", wheel);
    // zoomAround reads refs only; the stage comes and goes with the image
  }, [isImage]);

  // pointers: drag pans a zoomed image, two fingers pinch, a quick sideways swipe steps (touch and
  // pen; a mouse drag never steps), a double tap toggles the zoom
  const ptrs = useRef(new Map<number, { x: number; y: number }>());
  const gesture = useRef<Gesture | null>(null);
  const lastTap = useRef<{ t: number; x: number; y: number } | null>(null);
  const tapZoomAt = useRef(-1e9);
  const closeOnClick = useRef(false);
  const pair = () => {
    const [a, b] = [...ptrs.current.values()];
    return { d: Math.max(1, Math.hypot(a!.x - b!.x, a!.y - b!.y)), mx: (a!.x + b!.x) / 2, my: (a!.y + b!.y) / 2 };
  };
  const down = (e: PointerEvent<HTMLDivElement>) => {
    // a click closes only when it starts beside the image: with the pointer captured, every click
    // here lands on the stage, a click on the image too
    closeOnClick.current = e.target === e.currentTarget;
    if ((e.pointerType === "mouse" && e.button !== 0) || (e.target as HTMLElement).closest("button")) return;
    ptrs.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    try {
      e.currentTarget.setPointerCapture(e.pointerId);
    } catch {
      // a synthetic pointer: no capture, the moves still arrive over the stage
    }
    if (ptrs.current.size === 1) gesture.current = { x0: e.clientX, y0: e.clientY, t0: e.timeStamp, moved: false, pinched: false };
    else if (ptrs.current.size === 2 && gesture.current) Object.assign(gesture.current, { moved: true, pinched: true, pinch: pair() });
  };
  const move = (e: PointerEvent<HTMLDivElement>) => {
    const p = ptrs.current.get(e.pointerId);
    const g = gesture.current;
    if (!p || !g) return;
    const dx = e.clientX - p.x;
    const dy = e.clientY - p.y;
    p.x = e.clientX;
    p.y = e.clientY;
    if (Math.hypot(e.clientX - g.x0, e.clientY - g.y0) > 6) {
      g.moved = true;
      closeOnClick.current = false; // the click after a drag or a swipe is not a click beside the image
    }
    const b = box();
    if (!b) return;
    if (g.pinch && ptrs.current.size >= 2) {
      const now = pair();
      const zv = zoomAt(getV(), now.d / g.pinch.d, now.mx - b.left, now.my - b.top, b.w, b.h);
      setV(panBy(zv, now.mx - g.pinch.mx, now.my - g.pinch.my, b.w, b.h));
      g.pinch = now;
    } else if (zoomed(getV())) setV(panBy(getV(), dx, dy, b.w, b.h));
  };
  const up = (e: PointerEvent<HTMLDivElement>) => {
    if (!ptrs.current.delete(e.pointerId)) return;
    const g = gesture.current;
    if (g && ptrs.current.size === 1) g.pinch = undefined; // one finger left: it pans on
    if (ptrs.current.size > 0) return;
    gesture.current = null;
    if (!g || e.type === "pointercancel") return;
    if (e.pointerType === "mouse" || g.pinched) return;
    if (g.moved) {
      if (!zoomed(getV())) go(swipeStep(e.clientX - g.x0, e.clientY - g.y0, e.timeStamp - g.t0));
      return;
    }
    if (doubleTap(lastTap.current, e.timeStamp, e.clientX, e.clientY)) {
      lastTap.current = null;
      tapZoomAt.current = e.timeStamp;
      toggleZoom(e.clientX, e.clientY);
    } else lastTap.current = { t: e.timeStamp, x: e.clientX, y: e.clientY };
  };
  const click = () => {
    const close = closeOnClick.current;
    closeOnClick.current = false;
    if (close) onClose();
  };
  const dbl = (e: MouseEvent<HTMLDivElement>) => {
    if (e.timeStamp - tapZoomAt.current < 600) return; // a double tap did it already
    // on the image (by position: the captured pointer sends the double-click to the stage)
    const r = e.currentTarget.querySelector("img")?.getBoundingClientRect();
    if (r && e.clientX >= r.left && e.clientX <= r.right && e.clientY >= r.top && e.clientY <= r.bottom) toggleZoom(e.clientX, e.clientY);
  };

  if (!item) return null;
  const many = n > 1;
  const arrows = many && (
    <>
      {i > 0 && (
        <button ref={prevBtn} type="button" className="lbx-arrow lbx-prev" onClick={() => go(-1)} aria-label="Previous" title="Previous (←)">
          <Icon name="chevron-left" />
        </button>
      )}
      {i < n - 1 && (
        <button ref={nextBtn} type="button" className="lbx-arrow lbx-next" onClick={() => go(1)} aria-label="Next" title="Next (→)">
          <Icon name="chevron-right" />
        </button>
      )}
    </>
  );
  const tools = (
    <>
      {many && (
        <span className="lbx-count" aria-live="polite" aria-label={`${i + 1} of ${n}`}>
          {i + 1} / {n}
        </span>
      )}
      {item.image && canFullscreen() && (
        <button type="button" className="icon-button lbx-fs" onClick={toggleFs} aria-label="Fullscreen" title="Fullscreen (F)">
          <Icon name="fullscreen" />
        </button>
      )}
    </>
  );

  return (
    <Modal title={item.title} onClose={onClose} xl={!!item.image} wide={!item.image} expandable={!item.image} tools={tools}>
      <div ref={body} className={"lbx" + (many ? " lbx-many" : "")}>
        {item.image ? (
          <div
            ref={stage}
            className={"att-lightbox" + (zoomed(v) ? " zoomed" : "") + (fs ? " lbx-full" : "")}
            onClick={click}
            onDoubleClick={dbl}
            onPointerDown={down}
            onPointerMove={move}
            onPointerUp={up}
            onPointerCancel={up}
          >
            <img
              key={item.key}
              src={item.image.src}
              alt={item.image.alt}
              draggable={false}
              style={zoomed(v) ? { transform: `translate(${v.x}px, ${v.y}px) scale(${v.s})` } : undefined}
            />
            {arrows}
            {fs && (
              <button type="button" className="lbx-arrow lbx-unfull" onClick={toggleFs} aria-label="Leave fullscreen" title="Leave fullscreen (Esc)">
                <Icon name="fullscreen-exit" />
              </button>
            )}
          </div>
        ) : (
          <div className="lbx-doc">
            {/* keyed: each file loads afresh; the arrows stay (and keep the focus) */}
            <Fragment key={item.key}>{item.body}</Fragment>
            {arrows}
          </div>
        )}
      </div>
      {item.foot}
    </Modal>
  );
}
