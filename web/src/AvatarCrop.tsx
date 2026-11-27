// AvatarCrop: pick the square of a picture to keep before it's uploaded. Drag the picture (or use the
// arrow keys) to move it; the slider, the mouse wheel or + / − zoom. Saving draws that square at 256 px
// as a PNG, the size the server stores, so what you see is what you get.
import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { centred, clampOffset, clampZoom, coverScale, MAX_ZOOM, MIN_ZOOM, sourceSquare, zoomAround, type Pt } from "./crop";
import { Modal } from "./ui";

const BOX = 280; // the crop box, in CSS px (fixed: drag distances map 1:1)
const OUT = 256; // the saved picture, in px
const STEP = 12; // an arrow key moves it this far
const CENTRE: Pt = { x: BOX / 2, y: BOX / 2 };

export function AvatarCrop({ file, onCancel, onSave }: { file: File; onCancel: () => void; onSave: (png: Blob) => Promise<unknown> }) {
  const [img, setImg] = useState<HTMLImageElement | null>(null);
  const [url, setUrl] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  // zoom and offset change together (zooming moves the picture around a point), so one state
  const [view, setView] = useState<{ zoom: number; off: Pt }>({ zoom: 1, off: { x: 0, y: 0 } });
  const boxRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ id: number; x: number; y: number; off: Pt } | null>(null);

  useEffect(() => {
    const u = URL.createObjectURL(file);
    const im = new Image();
    im.onload = () => {
      const s = coverScale(im.naturalWidth, im.naturalHeight, BOX);
      setView({ zoom: 1, off: centred(im.naturalWidth, im.naturalHeight, s, BOX) });
      setImg(im);
    };
    im.onerror = () => setErr("The browser can't open this file as a picture.");
    im.src = u;
    setUrl(u);
    return () => URL.revokeObjectURL(u);
  }, [file]);

  const iw = img?.naturalWidth ?? 1;
  const ih = img?.naturalHeight ?? 1;
  const base = coverScale(iw, ih, BOX);
  const scale = base * view.zoom;

  const zoomTo = (next: (z: number) => number, p: Pt = CENTRE) =>
    setView((v) => {
      const z = clampZoom(next(v.zoom));
      return { zoom: z, off: clampOffset(zoomAround(v.off, base * v.zoom, base * z, p), iw, ih, base * z, BOX) };
    });
  const moveBy = (dx: number, dy: number, from?: Pt) =>
    setView((v) => {
      const o = from ?? v.off;
      return { ...v, off: clampOffset({ x: o.x + dx, y: o.y + dy }, iw, ih, base * v.zoom, BOX) };
    });

  // the wheel zooms around the pointer; a native listener, since React's wheel listener is passive and
  // the dialog would scroll too
  const zoomRef = useRef(zoomTo);
  zoomRef.current = zoomTo;
  useEffect(() => {
    const el = boxRef.current;
    if (!el) return;
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      const r = el.getBoundingClientRect();
      zoomRef.current((z) => z * Math.exp(-e.deltaY / 500), { x: e.clientX - r.left, y: e.clientY - r.top });
    };
    el.addEventListener("wheel", wheel, { passive: false });
    return () => el.removeEventListener("wheel", wheel);
  }, []);

  const down = (e: PointerEvent<HTMLDivElement>) => {
    if (!img) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    drag.current = { id: e.pointerId, x: e.clientX, y: e.clientY, off: view.off };
  };
  const move = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (d && d.id === e.pointerId) moveBy(e.clientX - d.x, e.clientY - d.y, d.off);
  };
  const up = (e: PointerEvent<HTMLDivElement>) => {
    if (drag.current?.id === e.pointerId) drag.current = null;
  };
  const key = (e: KeyboardEvent<HTMLDivElement>) => {
    // the picture moves the way the arrow points, as when dragging it
    const moves: Record<string, [number, number]> = { ArrowLeft: [-STEP, 0], ArrowRight: [STEP, 0], ArrowUp: [0, -STEP], ArrowDown: [0, STEP] };
    if (moves[e.key]) moveBy(...moves[e.key]!);
    else if (e.key === "+" || e.key === "=") zoomTo((z) => z * 1.1);
    else if (e.key === "-" || e.key === "_") zoomTo((z) => z / 1.1);
    else return;
    e.preventDefault();
  };

  const save = () => {
    if (!img) return;
    const c = document.createElement("canvas");
    c.width = c.height = OUT;
    const ctx = c.getContext("2d");
    if (!ctx) {
      setErr("This browser can't crop pictures here.");
      return;
    }
    ctx.imageSmoothingQuality = "high";
    const { sx, sy, size } = sourceSquare(view.off, scale, BOX);
    ctx.drawImage(img, sx, sy, size, size, 0, 0, OUT, OUT);
    setBusy(true);
    setErr("");
    c.toBlob((b) => {
      if (!b) {
        setBusy(false);
        setErr("Couldn't make the picture.");
        return;
      }
      onSave(b).catch((e: unknown) => {
        setBusy(false);
        setErr(e instanceof Error ? e.message : "Upload failed.");
      });
    }, "image/png");
  };

  return (
    <Modal title="Crop your picture" onClose={onCancel}>
      <div className="crop">
        <p className="muted small">Drag the picture to place it; zoom with the slider or the mouse wheel. The circle shows how it looks as your avatar; the whole square is kept.</p>
        <div
          ref={boxRef}
          className={"crop-box" + (img ? "" : " loading")}
          style={{ width: BOX, height: BOX }}
          tabIndex={0}
          role="group"
          aria-label="Picture to crop: the arrow keys move it, + and − zoom"
          onPointerDown={down}
          onPointerMove={move}
          onPointerUp={up}
          onPointerCancel={up}
          onKeyDown={key}
        >
          {img && (
            <img
              src={url}
              alt=""
              draggable={false}
              style={{ width: iw * scale, height: ih * scale, transform: `translate(${view.off.x}px, ${view.off.y}px)` }}
            />
          )}
          <span className="crop-ring" aria-hidden="true" />
        </div>
        <div className="crop-zoom">
          <button type="button" className="icon-button" onClick={() => zoomTo((z) => z / 1.25)} disabled={!img || view.zoom <= MIN_ZOOM} aria-label="Zoom out">
            −
          </button>
          <input
            type="range"
            min={MIN_ZOOM}
            max={MAX_ZOOM}
            step={0.01}
            value={view.zoom}
            disabled={!img}
            onChange={(e) => {
              const z = Number(e.target.value);
              zoomTo(() => z);
            }}
            aria-label="Zoom"
          />
          <button type="button" className="icon-button" onClick={() => zoomTo((z) => z * 1.25)} disabled={!img || view.zoom >= MAX_ZOOM} aria-label="Zoom in">
            +
          </button>
        </div>
        {err && (
          <p className="danger" role="alert">
            {err}
          </p>
        )}
        <div className="row end">
          <button type="button" className="button secondary" onClick={onCancel}>
            Cancel
          </button>
          <button type="button" className="button" onClick={save} disabled={!img || busy}>
            {busy ? "Saving…" : "Save picture"}
          </button>
        </div>
      </div>
    </Modal>
  );
}
