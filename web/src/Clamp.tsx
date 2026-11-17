// Clamp: long text (a description, a comment) shows at most `maxRem` of height, fading out at the
// bottom, with a bar underneath that opens it fully. Text that only runs a little over the limit
// isn't clamped (hiding two lines behind a bar helps nobody). Starts closed; the owner keys it by
// gig or comment so a newly opened gig starts closed too. `off` shows it whole, without a bar.
import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { Icon } from "./icons";

const SLACK = 1.3; // clamp only when the text is over 1.3 times the limit

function remPx(): number {
  return parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
}

export function Clamp({ children, maxRem = 16, label, off = false }: { children: ReactNode; maxRem?: number; label: string; off?: boolean }) {
  const inner = useRef<HTMLDivElement>(null);
  const outer = useRef<HTMLDivElement>(null);
  const boxId = useId();
  const [long, setLong] = useState(false);
  const [open, setOpen] = useState(false);

  useLayoutEffect(() => {
    const el = inner.current;
    if (!el) return;
    const measure = () => setLong(el.scrollHeight > maxRem * remPx() * SLACK);
    measure();
    // images, fonts and a changing panel width all change the height later
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(measure) : null;
    ro?.observe(el);
    return () => ro?.disconnect();
  }, [maxRem]);
  // text that became short (edited, or a wider panel) starts folded again if it grows back
  useEffect(() => {
    if (!long) setOpen(false);
  }, [long]);

  const active = long && !off;
  const clamped = active && !open;
  const toggle = () => {
    const box = outer.current;
    if (open && box) {
      // closing a long text from below: bring its top back into view (below the panel's sticky
      // header) rather than leave the reader lost further down
      const head = box.closest(".panel")?.querySelector(".tp-top");
      const limit = head ? head.getBoundingClientRect().bottom : 0;
      if (box.getBoundingClientRect().top < limit) box.scrollIntoView({ block: "start" });
    }
    setOpen(!open);
  };
  return (
    <div ref={outer} className={"clamp" + (clamped ? " clamped" : "")}>
      <div
        className="clamp-box"
        id={boxId}
        style={clamped ? { maxHeight: `${maxRem}rem` } : undefined}
        // keyboard focus on a link in the hidden part opens the text instead of scrolling it inside the box
        onFocus={clamped ? () => setOpen(true) : undefined}
      >
        <div ref={inner}>{children}</div>
      </div>
      {active && (
        <button type="button" className="clamp-bar" aria-expanded={open} aria-controls={boxId} aria-label={`${open ? "Show less" : "Show more"} of the ${label}`} onClick={toggle}>
          <Icon name={open ? "chevron-up" : "chevron-down"} strokeWidth={2.2} />
          <span>{open ? "Show less" : "Show more"}</span>
        </button>
      )}
    </div>
  );
}
