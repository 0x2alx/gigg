// Keyboard access across the app: the "Skip to board" link at the top of every page, the app-wide
// single-key shortcuts (? / N) and the ? overlay that lists every shortcut (keys.ts holds the list).
import { Fragment, useEffect, useState, type MouseEvent } from "react";
import { useLocation } from "wouter";
import { useNewGigHref } from "./AppBar";
import { SHORTCUTS, shortcutFor } from "./keys";
import { Kbd, Modal } from "./ui";

// window events: the palette's "Keyboard shortcuts" command opens the overlay; / opens the palette
// where the page has no search field of its own
export const OPEN_SHORTCUTS = "gigg:shortcuts";
export const OPEN_PALETTE = "gigg:palette";
export const openShortcuts = (): void => {
  window.dispatchEvent(new Event(OPEN_SHORTCUTS));
};

const onBoardPath = (loc: string) => loc === "/board" || loc.startsWith("/board/");
const visible = (el: HTMLElement) => el.getClientRects().length > 0;

// SkipLink: the first thing Tab reaches; shown only while it has the focus. On the board it jumps
// past the app bar and the filters to the first card (or the board when it has none); elsewhere to
// the page's content.
export function SkipLink() {
  const [loc] = useLocation();
  const board = onBoardPath(loc);
  const go = (e: MouseEvent) => {
    e.preventDefault();
    const card = board ? [...document.querySelectorAll<HTMLElement>(".board .card-open")].find(visible) : undefined;
    const target = card ?? (board ? document.querySelector<HTMLElement>(".board") : null) ?? document.getElementById("main");
    if (!target) return;
    if (!card) target.tabIndex = -1; // a container takes the focus without joining the Tab order
    target.focus();
    target.scrollIntoView({ block: "nearest" });
  };
  return (
    <a href="#main" className="skip-link" onClick={go}>
      {board ? "Skip to board" : "Skip to content"}
    </a>
  );
}

// KeyboardShortcuts: ? opens the overlay, / searches (the board's search field, else the command
// palette), N starts a new gig. Not while typing in a field or while a dialog is open. Mount once.
export function KeyboardShortcuts() {
  const [open, setOpen] = useState(false);
  const [, navigate] = useLocation();
  const newGig = useNewGigHref();
  useEffect(() => {
    const show = () => setOpen(true);
    const key = (e: KeyboardEvent) => {
      if (document.querySelector('[aria-modal="true"]')) return;
      const action = shortcutFor(e, document.activeElement as HTMLElement | null);
      if (!action) return;
      if (action === "help") setOpen(true);
      else if (action === "search") {
        const field = [...document.querySelectorAll<HTMLInputElement>('input[aria-keyshortcuts="/"]')].find(visible);
        if (field) {
          field.focus();
          field.select();
        } else window.dispatchEvent(new Event(OPEN_PALETTE));
      } else if (action === "new") {
        if (!newGig) return;
        navigate(newGig);
      }
      e.preventDefault(); // the / and ? don't end up typed in what gets the focus
    };
    window.addEventListener(OPEN_SHORTCUTS, show);
    document.addEventListener("keydown", key);
    return () => {
      window.removeEventListener(OPEN_SHORTCUTS, show);
      document.removeEventListener("keydown", key);
    };
  }, [navigate, newGig]);
  return open ? <ShortcutsOverlay onClose={() => setOpen(false)} /> : null;
}

function ShortcutsOverlay({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} wide>
      <div className="shortcuts">
        {SHORTCUTS.map((g, gi) => (
          <section key={g.title} className="shortcuts-group" aria-labelledby={`sc-${gi}`}>
            <h3 id={`sc-${gi}`}>{g.title}</h3>
            <dl>
              {g.items.map((s) => (
                <div key={s.what} className="shortcuts-row">
                  <dt>
                    {s.keys.map((chord, i) => (
                      <Fragment key={chord.join("+")}>
                        {i > 0 && <span className="muted small">or</span>}
                        <span className="shortcuts-chord">
                          {chord.map((k, j) => (
                            <Fragment key={k}>
                              {j > 0 && "+"}
                              <Kbd>{k}</Kbd>
                            </Fragment>
                          ))}
                        </span>
                      </Fragment>
                    ))}
                  </dt>
                  <dd>{s.what}</dd>
                </div>
              ))}
            </dl>
          </section>
        ))}
        <p className="muted small">Single keys (? / N) don't fire while you type in a field.</p>
      </div>
    </Modal>
  );
}
