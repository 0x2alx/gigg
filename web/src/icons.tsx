// Icon: small line icons (24-unit grid, drawn with currentColor, 1em square) for the sidebar, panel
// and toolbar, where emoji render as boxes or in colour depending on the font. Purely decorative:
// the control around an icon carries the label.
import type { ReactNode } from "react";

const P: Record<string, ReactNode> = {
  // the board's two layouts: statuses as columns, or as rows
  columns: (
    <>
      <rect x="3" y="4" width="18" height="16" rx="2.5" />
      <path d="M9 4v16M15 4v16" />
    </>
  ),
  rows: (
    <>
      <rect x="3" y="4" width="18" height="16" rx="2.5" />
      <path d="M3 9.5h18M3 14.5h18" />
    </>
  ),
  board: (
    <>
      <rect x="3" y="3" width="18" height="18" rx="2.5" />
      <path d="M8.5 7.5v7M12 7.5v4M15.5 7.5v9" />
    </>
  ),
  dashboard: (
    <>
      <rect x="3" y="3" width="7" height="9" rx="1.5" />
      <rect x="14" y="3" width="7" height="5" rx="1.5" />
      <rect x="14" y="12" width="7" height="9" rx="1.5" />
      <rect x="3" y="16" width="7" height="5" rx="1.5" />
    </>
  ),
  chart: (
    <>
      <path d="M3 3v18h18" />
      <path d="M8 17v-5M13 17V7M18 17v-8" />
    </>
  ),
  inbox: (
    <>
      <path d="M22 12h-6l-2 3h-4l-2-3H2" />
      <path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z" />
    </>
  ),
  mail: (
    <>
      <rect x="2.5" y="4.5" width="19" height="15" rx="2.5" />
      <path d="m3 7 9 6 9-6" />
    </>
  ),
  bell: (
    <>
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
      <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
    </>
  ),
  check: (
    <>
      <rect x="3" y="3" width="18" height="18" rx="2.5" />
      <path d="m8.5 12 2.5 2.5 4.5-5" />
    </>
  ),
  star: <path d="m12 2.8 2.85 5.78 6.37.93-4.61 4.49 1.09 6.35L12 17.35l-5.7 3 1.09-6.35-4.61-4.49 6.37-.93z" />,
  // a cog: a hub, a ring and eight short, thick teeth (the card's menu)
  gear: (
    <>
      <circle cx="12" cy="12" r="2.6" />
      <circle cx="12" cy="12" r="6.2" />
      <path strokeWidth="3.2" strokeLinecap="butt" d="M17.73 14.37L20.59 15.56M14.37 17.73L15.56 20.59M9.63 17.73L8.44 20.59M6.27 14.37L3.41 15.56M6.27 9.63L3.41 8.44M9.63 6.27L8.44 3.41M14.37 6.27L15.56 3.41M17.73 9.63L20.59 8.44" />
    </>
  ),
  settings: (
    <>
      <path d="M20 7h-9M14 17H4" />
      <circle cx="17" cy="17" r="3" />
      <circle cx="7" cy="7" r="3" />
    </>
  ),
  theme: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" stroke="none" />
    </>
  ),
  archive: (
    <>
      <rect x="2.5" y="3.5" width="19" height="5" rx="1" />
      <path d="M4.5 8.5V19a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V8.5M10 12.5h4" />
    </>
  ),
  layers: (
    <>
      <path d="m12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z" />
      <path d="m22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65M22 17.65l-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65" />
    </>
  ),
  users: (
    <>
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />
    </>
  ),
  shield: <path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z" />,
  key: (
    <>
      <circle cx="7.5" cy="15.5" r="5.5" />
      <path d="m21 2-9.6 9.6M15.5 7.5l3 3L22 7l-3-3" />
    </>
  ),
  list: <path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01" />,
  filter: <path d="M3.5 5h17l-6.5 8v5.5l-4 2V13z" />,
  x: <path d="M18 6 6 18M6 6l12 12" />,
  menu: <path d="M4 6h16M4 12h16M4 18h16" />,
  logout: (
    <>
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <path d="m16 17 5-5-5-5M21 12H9" />
    </>
  ),
  pencil: <path d="M21.17 6.81a1 1 0 0 0-3.99-3.99L3.84 16.17a2 2 0 0 0-.5.83l-1.32 4.35a.5.5 0 0 0 .62.62l4.35-1.32a2 2 0 0 0 .83-.5z" />,
  // make shared: two lines merging into one
  share: <path d="M6 3v6a6 6 0 0 0 6 6h0M18 3v6a6 6 0 0 1-6 6M12 15v6" />,
  expand: <path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7" />,
  shrink: <path d="M4 14h6v6M20 10h-6V4M14 10l7-7M3 21l7-7" />,
  "chevron-right": <path d="m9 18 6-6-6-6" />,
  "chevron-down": <path d="m6 9 6 6 6-6" />,
  "chevron-up": <path d="m18 15-6-6-6 6" />,
  "arrow-right": <path d="M5 12h14M13 6l6 6-6 6" />,
  checklist: <path d="m3 7 2 2 4-4M3 17l2 2 4-4M13 6h8M13 12h8M13 18h8" />,
  subgigs: <path d="M21 12h-8M21 6H8M21 18h-8M3 6v4a2 2 0 0 0 2 2h3M3 10v6a2 2 0 0 0 2 2h3" />,
  link: (
    <>
      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
    </>
  ),
  fields: (
    <>
      <rect x="3" y="5" width="18" height="14" rx="2" />
      <path d="M7 10h4M7 14h10" />
    </>
  ),
  pause: (
    <>
      <rect x="6" y="4" width="4" height="16" rx="1" />
      <rect x="14" y="4" width="4" height="16" rx="1" />
    </>
  ),
  play: <path d="M6 4v16l14-8z" />,
  sort: <path d="M7 20V4M3 8l4-4 4 4M17 4v16M13 16l4 4 4-4" />,
  refresh: (
    <>
      <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8M21 3v5h-5" />
      <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16M8 16H3v5" />
    </>
  ),
  clock: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </>
  ),
  template: (
    <>
      <rect x="8" y="8" width="13" height="13" rx="2" />
      <path d="M4 16a2 2 0 0 1-1-1.73V5a2 2 0 0 1 2-2h9.27A2 2 0 0 1 16 4" />
    </>
  ),
  trash: <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M10 11v6M14 11v6" />,
  lock: (
    <>
      <rect x="4" y="11" width="16" height="10" rx="2" />
      <path d="M8 11V7a4 4 0 0 1 8 0v4" />
    </>
  ),
  copy: (
    <>
      <rect x="9" y="9" width="12" height="12" rx="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </>
  ),
  undo: <path d="M9 14 4 9l5-5M4 9h10.5a5.5 5.5 0 0 1 0 11H11" />,
  // the agent mark on an avatar: a small robot head (two eyes, an antenna), readable at 10px
  // always run: a lightning bolt
  bolt: <path d="M13 2L4.5 13.5H11L10 22l8.5-11.5H12z" />,
  bot: (
    <>
      <rect x="4" y="8" width="16" height="12" rx="3.5" />
      <path d="M12 8V4" />
      <circle cx="9" cy="14" r="1.6" fill="currentColor" stroke="none" />
      <circle cx="15" cy="14" r="1.6" fill="currentColor" stroke="none" />
    </>
  ),
  comment: <path d="M21 12a8 8 0 0 1-11.6 7.1L4 20.5l1.4-4.9A8 8 0 1 1 21 12z" />,
  calendar: (
    <>
      <rect x="3.5" y="5" width="17" height="15.5" rx="2.5" />
      <path d="M3.5 10h17M8 3v4M16 3v4" />
    </>
  ),
  plus: <path d="M12 5v14M5 12h14" />,
  alert: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7.5v5.5" />
      <circle cx="12" cy="16.3" r="1.1" fill="currentColor" stroke="none" />
    </>
  ),
  // a question mark alone, centred in the box (for the round help buttons: a typed "?" sits where
  // each theme's font puts it, often off centre)
  help: (
    <>
      <path d="M9.2 8.4a2.8 2.8 0 0 1 5.6 1c0 1.9-2.8 2.4-2.8 4.3" />
      <circle cx="12" cy="17.5" r="1.25" fill="currentColor" stroke="none" />
    </>
  ),
  question: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M9.6 9.3a2.5 2.5 0 0 1 4.8.9c0 1.7-2.4 2.2-2.4 3.8" />
      <circle cx="12" cy="17" r="1.1" fill="currentColor" stroke="none" />
    </>
  ),
};

export type IconName = keyof typeof P;

export function Icon({ name, className, strokeWidth = 1.8 }: { name: string; className?: string; strokeWidth?: number }) {
  return (
    <svg
      className={"ico" + (className ? " " + className : "")}
      viewBox="0 0 24 24"
      width="1em"
      height="1em"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {P[name] ?? null}
    </svg>
  );
}
