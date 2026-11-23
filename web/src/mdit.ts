// mdit: the one markdown-it setup gigg renders Markdown with (no HTML, links found in text, a single
// newline is a line break, task lists). The renderer (markdown.tsx) sanitizes its output; the visual
// editor's round-trip check (richmd.ts) compares what two texts render to.
import MarkdownIt from "markdown-it";
import { taskLists } from "./mdtasks";

export const mdit = new MarkdownIt({ html: false, linkify: true, breaks: true });

mdit.use(taskLists);
