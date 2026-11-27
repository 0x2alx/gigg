// Keyboard shortcuts: the one list the ? overlay shows, and which key press means which app-wide
// shortcut. Pure (no DOM), so it's tested on its own; keyboard.tsx wires it to the page.

export type ShortcutAction = "help" | "search" | "new";

export interface Shortcut {
  keys: string[][]; // alternatives, each a chord: [["Ctrl", "K"], ["⌘", "K"]]
  what: string;
}

export interface ShortcutGroup {
  title: string;
  items: Shortcut[];
}

// SHORTCUTS: every shortcut gigg has, as the overlay lists them. App-wide ones first; the rest work
// where they say.
export const SHORTCUTS: ShortcutGroup[] = [
  {
    title: "Anywhere",
    items: [
      { keys: [["?"]], what: "Show these shortcuts" },
      { keys: [["Ctrl", "K"], ["⌘", "K"]], what: "Command palette: jump to a gig, project or page, or search" },
      { keys: [["/"]], what: "Search: the page's search field when it's in view, else the command palette" },
      { keys: [["N"]], what: "New gig" },
      { keys: [["Esc"]], what: "Close a dialog, the gig panel or a menu" },
    ],
  },
  {
    title: "Menus, lists and pickers",
    items: [
      { keys: [["↑"], ["↓"]], what: "Move through the account menu and the command palette" },
      { keys: [["Enter"]], what: "Run the highlighted command" },
      { keys: [["←"], ["→"]], what: "Pick the previous or next colour in a row of swatches" },
    ],
  },
  {
    title: "Writing (descriptions and comments)",
    items: [
      { keys: [["Ctrl", "B"]], what: "Bold" },
      { keys: [["Ctrl", "I"]], what: "Italic" },
      { keys: [["Ctrl", "K"]], what: "Link (inside the editor, instead of the palette)" },
      { keys: [["Ctrl", "Enter"]], what: "Post the comment, or save the title and description" },
      { keys: [["Tab"], ["Shift", "Tab"]], what: "Indent or outdent a list item" },
      { keys: [["@"]], what: "Mention someone" },
    ],
  },
];

export interface KeyLike {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  isComposing?: boolean;
  defaultPrevented?: boolean;
}

export interface TargetLike {
  tagName?: string;
  type?: string;
  isContentEditable?: boolean;
}

// inputs you don't type into: a shortcut still works while one of them has the focus
const NOT_TYPED = new Set(["button", "checkbox", "radio", "range", "submit", "reset", "color", "file", "image"]);

// typingIn: the focus is somewhere keys are text (a field, a select, the rich editor), so single-key
// shortcuts stay out of the way.
export function typingIn(t: TargetLike | null | undefined): boolean {
  if (!t) return false;
  if (t.isContentEditable) return true;
  const tag = (t.tagName ?? "").toUpperCase();
  if (tag === "TEXTAREA" || tag === "SELECT") return true;
  if (tag === "INPUT") return !NOT_TYPED.has((t.type ?? "text").toLowerCase());
  return false;
}

// shortcutFor: the app-wide shortcut a key press asks for, or null. Single keys only, without Ctrl,
// ⌘ or Alt (those belong to the browser and the editors), and never while typing or composing.
export function shortcutFor(e: KeyLike, target: TargetLike | null | undefined): ShortcutAction | null {
  if (e.defaultPrevented || e.isComposing || e.ctrlKey || e.metaKey || e.altKey) return null;
  if (typingIn(target)) return null;
  if (e.key === "?") return "help";
  if (e.key === "/") return "search";
  if (e.key === "n" || e.key === "N") return "new";
  return null;
}
