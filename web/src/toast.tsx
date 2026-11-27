// The toast: a short message at the bottom of the page, with an Undo when the change can be taken
// back in one click. One component for the board, the table and the gig panel's actions.
import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type Gig } from "./api";

export interface ToastMsg {
  text: string;
  undo?: () => void;
}

// useToast: the toast on screen and a way to show one. It goes away by itself, later when it has an
// Undo (time to reach for the button).
export function useToast(): [ToastMsg | null, (text: string, undo?: () => void) => void, () => void] {
  const [toast, setToast] = useState<ToastMsg | null>(null);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), toast.undo ? 6000 : 4000);
    return () => clearTimeout(t);
  }, [toast]);
  const show = useCallback((text: string, undo?: () => void) => setToast({ text, undo }), []);
  const clear = useCallback(() => setToast(null), []);
  return [toast, show, clear];
}

export function Toast({ toast, onClose }: { toast: ToastMsg | null; onClose: () => void }) {
  if (!toast) return null;
  return (
    <div className="toast" role="status">
      {toast.text}
      {toast.undo && (
        <button
          type="button"
          className="toast-undo"
          onClick={() => {
            const undo = toast.undo;
            onClose();
            undo?.();
          }}
        >
          Undo
        </button>
      )}
    </div>
  );
}

// unarchiveUndo: the Undo of an archive (the card's button, the gig panel's): unarchive the gig and
// say how that went through `report`, which also refreshes whatever shows the gig.
export function unarchiveUndo(gig: Pick<Gig, "id" | "key">, report: (message: string) => void): () => void {
  return () =>
    void api
      .unarchiveGig(gig.id)
      .then(() => report(`${gig.key} is back on the board`))
      .catch((e: unknown) => report(`Couldn't unarchive ${gig.key}: ${e instanceof ApiError ? e.message : "try again from the gig"}`));
}
