// Tooltips for the whole app from one listener: any element with data-tip (or a plain title, which is
// moved to data-tip so the browser's own slow tooltip doesn't double it) gets a styled tooltip on mouse
// hover (short delay), on keyboard focus (at once) and on a touch long-press. It is drawn in a fixed
// layer, so scrolling columns and dialogs don't clip it, and linked to its element with
// aria-describedby while shown.
import { useEffect, useRef, useState } from "react";

const SHOW_DELAY = 350;
const LONG_PRESS = 450;
const GAP = 8;

interface Tip {
  text: string;
  x: number; // centre of the element
  top: number;
  bottom: number;
}

// tipTarget: the nearest element with a tooltip (data-tip, a title, or an icon-only button's name); a title is converted on the way (React keeps the DOM
// attribute removed until the value changes, and then it is converted again).
function tipTarget(from: EventTarget | null): HTMLElement | null {
  if (!(from instanceof Element)) return null;
  const el = from.closest<HTMLElement>("[data-tip],[title],.icon-button[aria-label]");
  if (!el || el.closest("[data-no-tip]")) return null;
  // icon-only buttons: their accessible name is the tooltip
  if (!el.dataset.tip && !el.hasAttribute("title") && el.classList.contains("icon-button")) el.dataset.tip = el.getAttribute("aria-label") ?? "";
  const title = el.getAttribute("title");
  if (title) {
    el.dataset.tip = title;
    el.removeAttribute("title");
    // an element named only by its title keeps that name for screen readers (and follows a new title)
    const named = el.hasAttribute("aria-label") && el.dataset.tipLabel === undefined;
    if (!named && !el.hasAttribute("aria-labelledby") && !(el.textContent ?? "").trim()) {
      el.setAttribute("aria-label", title);
      el.dataset.tipLabel = "1";
    }
  }
  return el.dataset.tip ? el : null;
}

export function TooltipLayer() {
  const [tip, setTip] = useState<Tip | null>(null);
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number; below: boolean } | null>(null);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    let current: HTMLElement | null = null;
    let tabbing = false; // focus tooltips follow Tab moves only, not focus a dialog sets on opening
    let prevDescribedBy: string | null = null;
    const show = (el: HTMLElement) => {
      const r = el.getBoundingClientRect();
      current = el;
      prevDescribedBy = el.getAttribute("aria-describedby");
      el.setAttribute("aria-describedby", prevDescribedBy ? `${prevDescribedBy} gigg-tooltip` : "gigg-tooltip");
      setTip({ text: el.dataset.tip ?? "", x: r.left + r.width / 2, top: r.top, bottom: r.bottom });
    };
    const hide = () => {
      clearTimeout(timer);
      if (current) {
        if (prevDescribedBy) current.setAttribute("aria-describedby", prevDescribedBy);
        else current.removeAttribute("aria-describedby");
      }
      current = null;
      setTip(null);
    };
    const over = (e: PointerEvent) => {
      if (e.pointerType === "touch") return;
      const el = tipTarget(e.target);
      if (el === current) return;
      hide();
      if (el) timer = setTimeout(() => show(el), SHOW_DELAY);
    };
    const out = (e: PointerEvent) => {
      if (e.pointerType === "touch") return;
      const to = tipTarget(e.relatedTarget);
      if (to !== current) hide();
    };
    const focus = (e: FocusEvent) => {
      const el = tipTarget(e.target);
      if (!el) return;
      // only keyboard moves: a click focuses too (the hover handles the mouse), and a dialog that
      // focuses its first control on opening shouldn't pop a tooltip
      if (!tabbing || (e.target instanceof Element && !e.target.matches(":focus-visible"))) return;
      hide();
      show(el);
    };
    const down = (e: PointerEvent) => {
      tabbing = false;
      hide();
      if (e.pointerType !== "touch") return;
      const el = tipTarget(e.target);
      if (el) timer = setTimeout(() => show(el), LONG_PRESS);
    };
    const up = (e: PointerEvent) => {
      if (e.pointerType !== "touch") return;
      clearTimeout(timer);
      if (current) timer = setTimeout(hide, 1800);
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Tab") tabbing = true;
      else if (e.key !== "Shift") tabbing = false;
      if (e.key === "Escape" && current) hide();
    };
    document.addEventListener("pointerover", over);
    document.addEventListener("pointerout", out);
    document.addEventListener("focusin", focus);
    document.addEventListener("focusout", hide);
    document.addEventListener("pointerdown", down, true);
    document.addEventListener("pointerup", up, true);
    document.addEventListener("keydown", key, true);
    window.addEventListener("scroll", hide, true);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("pointerover", over);
      document.removeEventListener("pointerout", out);
      document.removeEventListener("focusin", focus);
      document.removeEventListener("focusout", hide);
      document.removeEventListener("pointerdown", down, true);
      document.removeEventListener("pointerup", up, true);
      document.removeEventListener("keydown", key, true);
      window.removeEventListener("scroll", hide, true);
    };
  }, []);

  // place it above the element (below when there's no room), inside the window
  useEffect(() => {
    if (!tip || !ref.current) {
      setPos(null);
      return;
    }
    const w = ref.current.offsetWidth;
    const h = ref.current.offsetHeight;
    const below = tip.top - h - GAP < 4;
    const left = Math.max(4, Math.min(window.innerWidth - w - 4, tip.x - w / 2));
    setPos({ left, top: below ? tip.bottom + GAP : tip.top - h - GAP, below });
  }, [tip]);

  if (!tip) return null;
  return (
    <div
      ref={ref}
      id="gigg-tooltip"
      role="tooltip"
      className={"tooltip" + (pos?.below ? " below" : "")}
      style={pos ? { left: pos.left, top: pos.top } : { left: -9999, top: 0, visibility: "hidden" }}
    >
      {tip.text}
    </div>
  );
}
