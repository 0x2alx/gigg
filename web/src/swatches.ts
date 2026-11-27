// Pure helpers for the colour swatch groups (fields.tsx SwatchGroup): which swatches a role offers, and
// where the arrow keys go.

// swatchList: the presets, with the current colour in front when it isn't one of them. The order
// stays put while you pick among the presets, so the arrow keys walk the row instead of bouncing
// between two swatches that swap places.
export function swatchList(current: string, presets: string[]): string[] {
  const up = presets.map((c) => c.toUpperCase());
  const list = [...new Set(up)];
  const cur = current.toUpperCase();
  return list.includes(cur) ? list : [cur, ...list];
}

// rovingNext: the index a key moves to in a row of n radios (the arrows wrap, Home and End jump to
// the ends), or -1 for a key the group doesn't handle.
export function rovingNext(key: string, i: number, n: number): number {
  if (n <= 0) return -1;
  switch (key) {
    case "ArrowRight":
    case "ArrowDown":
      return (i + 1) % n;
    case "ArrowLeft":
    case "ArrowUp":
      return (i - 1 + n) % n;
    case "Home":
      return 0;
    case "End":
      return n - 1;
    default:
      return -1;
  }
}
