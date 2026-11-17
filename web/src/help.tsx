// In-app help: the user guide's own pages (docs/guide/*.md, bundled at build time), so the help and
// the documentation can't drift apart. A page's first heading is the dialog title; its screenshots
// are left out (they live next to the docs, not in the app).
import { lazy, Suspense, useState } from "react";
import { Modal } from "./ui";

// the renderer loads with the first help dialog (it needs the DOM, and most pages never open help)
const Markdown = lazy(() => import("./markdown").then((m) => ({ default: m.Markdown })));

const pages = import.meta.glob("../../docs/guide/*.md", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

export type HelpTopic = "board" | "gigs" | "agents" | "recurring" | "errands" | "dashboard" | "inbox" | "views" | "themes" | "profile" | "admin";

export function helpPage(topic: HelpTopic): { title: string; body: string } {
  const src = pages[`../../docs/guide/${topic}.md`] ?? "";
  const lines = src.split("\n");
  const h1 = lines.findIndex((l) => l.startsWith("# "));
  const title = h1 >= 0 ? lines[h1]!.slice(2).trim() : "Help";
  const body = lines
    .filter((_, i) => i !== h1)
    .filter((l) => !/^\s*!\[[^\]]*\]\([^)]*\)\s*$/.test(l)) // screenshots
    .join("\n")
    .replace(/\[([^\]]+)\]\((?!https?:)[^)]*\)/g, "$1") // links to other doc files: keep the words
    .trim();
  return { title, body: body || "No help for this section yet." };
}

// HelpButton: a small "?" that opens the guide's page for a section.
export function HelpButton({ topic, label }: { topic: HelpTopic; label?: string }) {
  const [open, setOpen] = useState(false);
  const page = open ? helpPage(topic) : null;
  return (
    <>
      <button type="button" className="help-button" data-tip={label ?? "Help for this section"} aria-label={label ?? "Help for this section"} onClick={() => setOpen(true)}>
        ?
      </button>
      {page && (
        <Modal title={`? ${page.title}`} onClose={() => setOpen(false)} wide>
          <div className="help-body">
            <Suspense fallback={<p className="muted">Loading…</p>}>
              <Markdown source={page.body} />
            </Suspense>
          </div>
        </Modal>
      )}
    </>
  );
}
