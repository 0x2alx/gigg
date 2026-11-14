import { useEffect, useState } from "react";

// The phone breakpoint, shared with the CSS (@media (max-width: 720px)).
export const NARROW = "(max-width: 720px)";

// useNarrow: true at phone widths (the drawer menu, the board's column tabs, the head-actions menu).
export function useNarrow(query = NARROW): boolean {
  const [narrow, setNarrow] = useState(() => typeof matchMedia === "function" && matchMedia(query).matches);
  useEffect(() => {
    if (typeof matchMedia !== "function") return;
    const m = matchMedia(query);
    const on = () => setNarrow(m.matches);
    on();
    m.addEventListener("change", on);
    return () => m.removeEventListener("change", on);
  }, [query]);
  return narrow;
}
