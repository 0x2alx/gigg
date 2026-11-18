import { useEffect, useRef, type RefObject } from "react";

// usePanelOutsideClose: a click outside a side panel closes it, like its ✕ (the gig panel and the
// errand panel share this). Not when the press started inside (drag-select), not in modals,
// popovers or menus, not when the click opened something else (the URL changed: another gig or
// errand, another page; or an element marked data-opens-gig, which opens another gig in place),
// and not when `keep` says so (a field holding unsaved text).
export function usePanelOutsideClose(panelRef: RefObject<HTMLElement | null>, close: () => void, keep?: () => boolean) {
  // the latest callbacks, so the listeners stay put across renders (a re-render between press and
  // release would otherwise forget the press)
  const cb = useRef({ close, keep });
  cb.current = { close, keep };
  useEffect(() => {
    let down: string | null = null;
    const outside = (t: EventTarget | null) => {
      const panel = panelRef.current;
      if (!panel || !(t instanceof Element) || !t.isConnected || panel.contains(t)) return false;
      return !t.closest('.modal-backdrop, [role="dialog"], [aria-modal="true"], .bell-pop, .menu, .mention-list, .toast, .appbar, [data-opens-gig]');
    };
    const onDown = (e: PointerEvent) => {
      down = e.button === 0 && !document.querySelector('[aria-modal="true"]') && outside(e.target) ? window.location.pathname : null;
    };
    const onClick = (e: MouseEvent) => {
      const path = down;
      down = null;
      if (path === null || e.button !== 0 || !outside(e.target) || window.location.pathname !== path) return;
      if (cb.current.keep?.()) return;
      cb.current.close();
    };
    document.addEventListener("pointerdown", onDown, true);
    document.addEventListener("click", onClick);
    return () => {
      document.removeEventListener("pointerdown", onDown, true);
      document.removeEventListener("click", onClick);
    };
  }, [panelRef]);
}
