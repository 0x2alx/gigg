// LazyEditor: MarkdownEditor loaded on first use. The visual editor (ProseMirror, the Markdown
// parser) is most of its weight and the board doesn't need it until a gig is open, so it comes in
// its own chunk; meanwhile an empty editor frame of the same size holds the place.
import { lazy, Suspense } from "react";
import type { MarkdownEditorProps } from "./MarkdownEditor";

const load = () => import("./MarkdownEditor");
const Editor = lazy(() => load().then((m) => ({ default: m.MarkdownEditor })));

// preloadEditor: starts the download early (a gig panel opening), so the editor is there when needed
export const preloadEditor = (): void => void load();

export function MarkdownEditor(p: MarkdownEditorProps) {
  return (
    <Suspense
      fallback={
        <div className={"mde mde-loading" + ((p.minRows ?? 4) >= 6 ? " mde-tall" : "")} aria-busy="true" aria-label={p["aria-label"]}>
          <div className="mde-bar" />
          <div className="rt-content" />
        </div>
      }
    >
      <Editor {...p} />
    </Suspense>
  );
}
