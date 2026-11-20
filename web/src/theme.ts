// Themes in gigg's theme JSON format: the seven built-ins, applying a theme to the page, saving the
// choice on the user; the editor, custom themes, import/export and the AI prompt build on these helpers.
import { BUNDLED_FONTS, bundledFamily, googleOk, loadGoogleFont, norm, validFamily } from "./fonts";

export interface Theme {
  id: string;
  name: string;
  dark: boolean;
  background: string;
  surface: string;
  surfaceVariant: string;
  border: string;
  accent: string;
  onAccent: string;
  text: string;
  mutedText: string;
  danger: string;
  secondary: string | null;
  font: string;
  fontWeight: "Light" | "Regular" | "Medium" | "Bold";
  textScalePct: number;
  cornerDp: number;
  borderDp: number;
  description: string;
  widget?: Record<string, unknown>;
  // gigg-only extras (other apps that share the theme format ignore unknown keys; exported only when set):
  controlShape?: "pill" | "rounded"; // buttons, fields, pills and badges fully round, or following the corners
  laneColor?: string | null; // the board's column background (unset: a step between page and cards)
  laneBorder?: string | null; // the column outline (unset: none)
  logoColor?: string | null; // the gigg ship in the sidebar (unset: the accent)
}

// the gigg-only colour keys (each optional: unset means derived)
export const EXTRA_COLOURS = ["laneColor", "laneBorder", "logoColor"] as const;

// laneOf / laneBorderOf / logoOf: the column colours a theme ends up with (its own, or derived)
export const laneOf = (t: Pick<Theme, "laneColor" | "background" | "surface">): string => t.laneColor ?? mix(t.background, t.surface, 0.5);
export const laneBorderOf = (t: Pick<Theme, "laneBorder">): string | null => t.laneBorder ?? null;
export const logoOf = (t: Pick<Theme, "logoColor" | "accent">): string => t.logoColor ?? t.accent;

export const BUILTINS: Theme[] = [
  { id: "clumsy-clanker", name: "Clumsy Clanker", dark: true, background: "#120A1F", surface: "#1D1230", surfaceVariant: "#2A1A45",
    border: "#4A3275", accent: "#A77BFF", onAccent: "#160A2B", text: "#F1ECFA", mutedText: "#D9AFCF", danger: "#FF5C7A", secondary: "#7A2370",
    font: "Oxanium", fontWeight: "Bold", textScalePct: 92, cornerDp: 8, borderDp: 1, laneColor: "#170E27", laneBorder: "#33214F", logoColor: "#7FF5E0",
    description: "The signature look, alien-temple violet: deep plum panels, a violet accent, fuchsia secondary, pale pink secondary text, a pale aqua glow and bold Oxanium; square buttons." },
  { id: "nightwalker", name: "Nightwalker", dark: true, background: "#1A2226", surface: "#253136", surfaceVariant: "#2F3D43",
    border: "#2D393E", accent: "#CDFFFB", onAccent: "#12302E", text: "#EEF8F7", mutedText: "#A3B6B8", danger: "#FF8A80", secondary: null,
    font: "Oxanium", fontWeight: "Bold", textScalePct: 93, cornerDp: 9, borderDp: 1,
    description: "Slate blue-grey with a pale aqua accent, square buttons and bold Oxanium. The default in dark mode." },
  { id: "daywalker", name: "Daywalker", dark: false, background: "#ECECE6", surface: "#FBF9F3", surfaceVariant: "#F3F0E8",
    border: "#E2DFD6", accent: "#5967DB", onAccent: "#FFFFFF", text: "#1A1D24", mutedText: "#5A6170", danger: "#C62828", secondary: null,
    font: "Inter", fontWeight: "Medium", textScalePct: 90, cornerDp: 12, borderDp: 1, controlShape: "pill", laneColor: "#E3E2DA",
    description: "Soft warm-grey page, eggshell panels and cards, pill buttons and an indigo accent. The default in light mode." },
  { id: "petty-officer", name: "Petty Officer", dark: true, background: "#0C140D", surface: "#141E15", surfaceVariant: "#1E2C1F",
    border: "#3C5A38", accent: "#F0B429", onAccent: "#1A1200", text: "#ECF3EA", mutedText: "#A9BFA2", danger: "#FF5C52", secondary: "#3E6B3A",
    font: "Michroma", fontWeight: "Medium", textScalePct: 90, cornerDp: 6, borderDp: 1,
    description: "Military green with an amber accent; squared off, thin edges." },
  { id: "high-contrast", name: "High contrast", dark: true, background: "#000000", surface: "#0A0A0A", surfaceVariant: "#1A1A1A",
    border: "#FFFFFF", accent: "#FFE600", onAccent: "#000000", text: "#FFFFFF", mutedText: "#E0E0E0", danger: "#FF5C5C", secondary: null,
    font: "Phone default", fontWeight: "Medium", textScalePct: 90, cornerDp: 8, borderDp: 2,
    description: "Black, white and yellow, thick borders." },
  { id: "neon", name: "Neon", dark: true, background: "#0A0E14", surface: "#111826", surfaceVariant: "#18213A",
    border: "#26365C", accent: "#00E5FF", onAccent: "#00151A", text: "#E6F7FF", mutedText: "#8FA6C7", danger: "#FF3DCB", secondary: null,
    font: "Oxanium", fontWeight: "Medium", textScalePct: 90, cornerDp: 10, borderDp: 1, controlShape: "pill",
    description: "Deep navy with a cyan accent, pill buttons." },
  { id: "carbon", name: "Carbon", dark: true, background: "#1C1C1C", surface: "#2B2B2B", surfaceVariant: "#363636",
    border: "#404040", accent: "#EE7A64", onAccent: "#1E0B06", text: "#EDEDED", mutedText: "#A3A3A3", danger: "#F26A82", secondary: null,
    font: "Rubik", fontWeight: "Medium", textScalePct: 92, cornerDp: 8, borderDp: 1, controlShape: "pill",
    description: "Neutral charcoal greys with a coral accent and pill buttons; an ops-console feel." },
];

const RETIRED: Record<string, string> = { graphite: "nightwalker", daylight: "daywalker", signature: "clumsy-clanker" };

export function findTheme(id: string | null | undefined): Theme | undefined {
  if (!id) return undefined;
  const k = id.toLowerCase();
  const real = RETIRED[k] ?? k;
  return BUILTINS.find((t) => t.id === real || t.name.toLowerCase() === real);
}

// ---- colour helpers (spec section 4)

function rgb(hex: string): [number, number, number] {
  const h = hex.replace("#", "");
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
}

function hex([r, g, b]: [number, number, number]): string {
  return "#" + [r, g, b].map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, "0")).join("").toUpperCase();
}

export function mix(a: string, b: string, t: number): string {
  const x = rgb(a);
  const y = rgb(b);
  return hex([x[0] * (1 - t) + y[0] * t, x[1] * (1 - t) + y[1] * t, x[2] * (1 - t) + y[2] * t]);
}

export function luminance(c: string): number {
  const [r, g, b] = rgb(c).map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  }) as [number, number, number];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function ratio(a: string, b: string): number {
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

// readableOn picks black or white text for a data colour (labels, statuses, priorities).
export function readableOn(bg: string): string {
  return ratio("#FFFFFF", bg) >= ratio("#111111", bg) ? "#FFFFFF" : "#111111";
}

// Tinted badges (Pill, LabelChip): a 14% tint of the data colour over the surface, with text of the
// same hue pulled toward the theme's text colour. badgeTextShare is the largest share of the data
// colour (in percent, at most 55) in that text for which every default data colour, the accent, the
// danger and the muted colour stay readable (4.5:1 with a small margin) on their tint. Yellow and amber
// on a light surface are the weak case, so Daywalker ends up lower than the dark themes.
export const BADGE_TINT = 0.14;
export function badgeTextShare(t: Pick<Theme, "surface" | "text" | "accent" | "danger" | "mutedText">, extra: string[] = []): number {
  const colours = [...DATA_COLORS, t.accent, t.danger, t.mutedText, ...extra];
  for (let share = 55; share > 0; share -= 5) {
    if (colours.every((c) => ratio(mix(t.text, c, share / 100), mix(t.surface, c, BADGE_TINT)) >= 4.6)) return share;
  }
  return 0;
}

// ---- fonts

const SYSTEM: Record<string, string> = {
  phonedefault: "system-ui, sans-serif",
  default: "system-ui, sans-serif",
  system: "system-ui, sans-serif",
  condensed: '"Arial Narrow", sans-serif-condensed, sans-serif',
  serif: "serif",
  mono: "monospace",
  casual: '"Comic Sans MS", cursive',
  cursive: "cursive",
  medium: "system-ui, sans-serif",
  smallcaps: "system-ui, sans-serif",
};
const SINGLE_WEIGHT = new Set(["audiowide", "michroma", "sharetechmono", "pacifico", "bebasneue", "righteous", "pressstart2p"]);

// missingFontFor: the theme's font when it is neither bundled nor a system font and Google Fonts
// aren't allowed in this browser (the page then uses the system font; Themes offers the button).
export function missingFontFor(t: Pick<Theme, "font">): string | null {
  const f = t.font;
  if (SYSTEM[norm(f)] || bundledFamily(f) || !validFamily(f) || googleOk()) return null;
  return f;
}

function loadFont(family: string, fontWeight: Theme["fontWeight"]): string {
  const n = norm(family);
  if (SYSTEM[n]) return SYSTEM[n]!;
  const bundled = bundledFamily(family);
  if (bundled) return `"${bundled}", system-ui, sans-serif`;
  if (!validFamily(family) || !googleOk()) return SYSTEM.system!;
  // The weights the theme actually uses (body and heading) plus 400 (inputs), so Medium loads 400;500;700.
  const wanted = [...new Set([400, ...WEIGHTS[fontWeight], strongWeight(fontWeight)])].sort((a, b) => a - b).join(";");
  loadGoogleFont(family, SINGLE_WEIGHT.has(n) ? "" : `:wght@${wanted}`);
  return `"${family}", system-ui, sans-serif`;
}

const WEIGHTS = { Light: [300, 400], Regular: [400, 500], Medium: [500, 700], Bold: [700, 700] } as const;

// strongWeight: the weight for pills, buttons, column names and headings (--weight-strong): 600, but
// never lighter than the body and, for a Light theme, only one step above its heading.
export function strongWeight(w: Theme["fontWeight"]): number {
  const [body, heading] = WEIGHTS[w];
  return Math.max(body, heading >= 700 ? 600 : heading + 100);
}

// applyTheme writes every role as a CSS custom property (components never hard-code colours).
export function applyTheme(t: Theme): void {
  const s = document.documentElement.style;
  const set = (k: string, v: string) => s.setProperty(k, v);
  const secondary = t.secondary ?? mix(t.accent, t.surface, t.dark ? 0.72 : 0.82);
  const onSecondary = ratio(t.text, secondary) >= 4.5 ? t.text : t.accent;
  set("--bg", t.background);
  set("--surface", t.surface);
  set("--surface-variant", t.surfaceVariant);
  set("--border", t.border);
  set("--accent", t.accent);
  set("--on-accent", t.onAccent);
  set("--text", t.text);
  set("--muted", t.mutedText);
  set("--danger", t.danger);
  set("--secondary", secondary);
  set("--on-secondary", onSecondary);
  set("--low", mix(t.background, t.surface, 0.5));
  set("--lane", laneOf(t));
  set("--lane-border", laneBorderOf(t) ?? "transparent");
  set("--logo", logoOf(t));
  set("--ctl-r", t.controlShape === "pill" ? "999px" : "var(--radius-m)");
  set("--ctl-px", t.controlShape === "pill" ? "0.75rem" : "0.6rem");
  document.documentElement.dataset.shape = t.controlShape === "pill" ? "pill" : "rounded";
  set("--high", mix(t.surface, t.surfaceVariant, 0.5));
  set("--faint", mix(t.border, t.surface, 0.5));
  set("--error-container", mix(t.danger, t.surface, 0.75));
  set("--on-danger", ratio("#FFFFFF", t.danger) >= 3 ? "#FFFFFF" : "#000000");
  set("--r", `${t.cornerDp}px`);
  set("--border-w", `${t.borderDp}px`);
  set("--font", loadFont(t.font, t.fontWeight));
  set("--weight-body", String(WEIGHTS[t.fontWeight][0]));
  set("--weight-heading", String(WEIGHTS[t.fontWeight][1]));
  set("--weight-strong", String(strongWeight(t.fontWeight)));
  set("--badge-mix", `${badgeTextShare(t)}%`);
  const rec = recurringLook(t);
  set("--rec-card", rec.card);
  set("--rec-card-hover", rec.cardHover);
  set("--rec-edge", rec.edge);
  set("--rec-badge", rec.badge);
  set("--rec-text", rec.text);
  set("--rec-glow", rec.glow);
  s.fontSize = `${t.textScalePct}%`;
  s.colorScheme = t.dark ? "dark" : "light";
}

const KEY = "gigg.theme";

// initialTheme: the cached theme (first paint, before the account's choice arrives), else the
// spec's first-visit rule: Nightwalker when the system is dark, Daywalker when light.
export function initialTheme(): Theme {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw) {
      const builtin = findTheme(raw);
      if (builtin) return builtin;
      const o = JSON.parse(raw) as Theme;
      if (o && typeof o === "object" && o.background) return sane(o);
    }
  } catch {
    /* storage unavailable or corrupt */
  }
  return matchMedia("(prefers-color-scheme: dark)").matches ? BUILTINS[1]! : BUILTINS[2]!;
}

export function rememberTheme(t: Theme): void {
  try {
    localStorage.setItem(KEY, findTheme(t.id) ? t.id : JSON.stringify(t));
  } catch {
    /* storage unavailable */
  }
}

// ---- data colours for labels, users, statuses and priorities

const PALETTE = ["#E5484D", "#F76B15", "#FFC53D", "#46A758", "#12A594", "#0090FF", "#3E63DD", "#8E4EC6", "#D6409F", "#A18072"];

export function colorFor(key: string, explicit?: string | null): string {
  if (explicit) return explicit;
  let h = 0;
  for (const ch of key) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return PALETTE[h % PALETTE.length]!;
}

export const CATEGORY_COLOR: Record<string, string> = {
  backlog: "#8B8D98",
  todo: "#0090FF",
  doing: "#F76B15",
  waiting: "#E5484D",
  done: "#46A758",
  cancelled: "#6F6E77",
};

export const PRIORITY_COLOR = ["", "#0090FF", "#FFC53D", "#F76B15", "#E5484D"];

// Recurring gigs: the schedule badge, and a gig's time window while it is open.
export const SCHEDULE_COLOR = "#8E4EC6";
// Always-run badge (a gig the runner takes at any time): amber, readable on the light and the dark built-in themes
export const ALWAYS_RUN_COLOR = "#E5A400";
export const WINDOW_OPEN_COLOR = "#F76B15";

// Recurring cards (a gig a recurring gig made): the card takes a tint of SCHEDULE_COLOR and its top
// line a solid, glowing "Recurring" badge. The hue is the same in every theme (it means "recurring"
// across gigg); the rest follows the theme: dark themes get a stronger tint, a slightly deeper badge
// and a stronger glow, and the badge text is whichever of white, the theme's text or its surface reads
// best on the badge (at least 4.5:1 in every built-in theme; the tests check).
export type RecurringLook = { card: string; cardHover: string; edge: string; badge: string; text: string; glow: string };
export function recurringLook(t: Pick<Theme, "dark" | "surface" | "border" | "text">): RecurringLook {
  const c = SCHEDULE_COLOR;
  const badge = t.dark ? mix(c, t.surface, 0.12) : c;
  const text = ["#FFFFFF", t.text, t.surface].reduce((best, x) => (ratio(x, badge) > ratio(best, badge) ? x : best));
  return {
    card: mix(t.surface, c, t.dark ? 0.15 : 0.1),
    cardHover: mix(t.surface, c, t.dark ? 0.2 : 0.14),
    edge: mix(t.border, c, t.dark ? 0.75 : 0.6),
    badge,
    text,
    glow: c + (t.dark ? "CC" : "8C"), // 80% / 55% opacity
  };
}

// every colour the app picks by itself for a badge (labels, workspaces and users by name, statuses, priorities, schedule, window)
export const DATA_COLORS: string[] = [...new Set([...PALETTE, ...Object.values(CATEGORY_COLOR), ...PRIORITY_COLOR.slice(1), SCHEDULE_COLOR, WINDOW_OPEN_COLOR])];

// =====================================================================================
// Full spec: parsing, sanitising, contrast, import/export, AI prompt (pure functions).
// =====================================================================================

export const COLOUR_KEYS = ["background", "surface", "surfaceVariant", "border", "accent", "onAccent", "text", "mutedText", "danger"] as const;
export type ColourKey = (typeof COLOUR_KEYS)[number] | "secondary" | "laneColor" | "laneBorder" | "logoColor";

export const ROLE_LABEL: Record<ColourKey, string> = {
  background: "Background", surface: "Cards", surfaceVariant: "Inputs and chips", border: "Borders", accent: "Accent",
  onAccent: "Text on the accent", secondary: "Secondary buttons", text: "Text", mutedText: "Secondary text", danger: "Warnings",
  laneColor: "Board columns", laneBorder: "Column borders", logoColor: "Logo",
};

// colourOf: a role's colour as shown (secondary and the board columns fall back to their derived colour)
export function colourOf(t: Theme, k: ColourKey): string {
  if (k === "secondary") return secondaryOf(t);
  if (k === "laneColor") return laneOf(t);
  if (k === "laneBorder") return laneBorderOf(t) ?? laneOf(t);
  if (k === "logoColor") return logoOf(t);
  return t[k];
}

export { BUNDLED_FONTS };
export const SYSTEM_FONTS = ["Phone default", "Condensed", "Serif", "Mono", "Medium", "Casual", "Cursive", "Small caps"];

// parseColour: #RGB, #RGBA, #RRGGBB, #AARRGGBB (alpha first), bare, any case, or an ARGB integer
// (or rgb(r, g, b) typed in the colour dialog). Output: #RRGGBB, opaque.
export function parseColour(v: unknown): string | null {
  if (typeof v === "number") {
    if (!Number.isInteger(v) || v < 0 || v > 0xffffffff) return null;
    return "#" + (v & 0xffffff).toString(16).padStart(6, "0").toUpperCase();
  }
  if (typeof v !== "string") return null;
  const t = v.trim();
  const m = /^rgb\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})\s*\)$/i.exec(t);
  if (m) {
    const c = [m[1], m[2], m[3]].map(Number);
    return c.every((x) => x <= 255) ? hex(c as [number, number, number]) : null;
  }
  const h = t.replace(/^#/, "").toUpperCase();
  if (!/^[0-9A-F]+$/.test(h)) return null;
  if (h.length === 3 || h.length === 4) return "#" + h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
  if (h.length === 6) return "#" + h;
  if (h.length === 8) return "#" + h.slice(2);
  return null;
}

const clamp = (n: unknown, lo: number, hi: number, def: number) => {
  const x = typeof n === "number" && Number.isFinite(n) ? Math.round(n) : def;
  return Math.min(hi, Math.max(lo, x));
};

const WEIGHT_NAMES = ["Light", "Regular", "Medium", "Bold"] as const;

// sane: name trimmed (40, "My theme"), numbers clamped, colours opaque. Never trusts its input.
export function sane(t: Theme): Theme {
  const name = (t.name ?? "").trim().slice(0, 40) || "My theme";
  const out: Theme = { ...t, name, textScalePct: clamp(t.textScalePct, 85, 130, 100), cornerDp: clamp(t.cornerDp, 0, 28, 12), borderDp: clamp(t.borderDp, 0, 3, 1) };
  for (const k of COLOUR_KEYS) out[k] = parseColour(out[k]) ?? "#808080";
  out.secondary = out.secondary === null ? null : parseColour(out.secondary);
  if (!WEIGHT_NAMES.includes(out.fontWeight)) out.fontWeight = "Regular";
  if (!/^[A-Za-z0-9 ]{1,60}$/.test(out.font)) out.font = "Phone default";
  // the extras: kept when valid, otherwise left out (not null), so a theme without them stays as it was
  if (out.controlShape !== "pill") delete out.controlShape;
  for (const k of EXTRA_COLOURS) {
    const c = out[k] ? parseColour(out[k]) : null;
    if (c) out[k] = c;
    else delete out[k];
  }
  if (out.widget && typeof out.widget === "object") {
    const w = { ...out.widget };
    if (typeof w.opacityPct === "number") w.opacityPct = clamp(w.opacityPct, 0, 100, 100);
    out.widget = w;
  }
  return out;
}

// toJSON: the shared export shape and key order (secondary omitted when Auto, widget kept).
export function themeJSON(t: Theme): string {
  const o: Record<string, unknown> = { name: t.name, dark: t.dark };
  for (const k of COLOUR_KEYS) o[k] = t[k];
  if (t.secondary) o.secondary = t.secondary;
  o.font = t.font;
  o.fontWeight = t.fontWeight;
  o.textScalePct = t.textScalePct;
  o.cornerDp = t.cornerDp;
  o.borderDp = t.borderDp;
  if (t.controlShape === "pill") o.controlShape = "pill";
  if (t.laneColor) o.laneColor = t.laneColor;
  if (t.laneBorder) o.laneBorder = t.laneBorder;
  if (t.logoColor) o.logoColor = t.logoColor;
  if (t.widget) o.widget = t.widget;
  return JSON.stringify(o, null, 4);
}

// ---- contrast (spec section 4)

export interface Problem {
  fg: ColourKey; // the colour the editor's Fix changes
  bg: ColourKey | string; // the other side: a role, or a fixed data colour (#RRGGBB)
  label: string;
  need: number;
  have: number;
  kind: "contrast" | "distance"; // a contrast ratio, or how different two fills look
}

export function secondaryOf(t: Theme): string {
  return t.secondary ?? mix(t.accent, t.surface, t.dark ? 0.72 : 0.82);
}

// CHIP_ON_LANE: the least perceptual distance (OKLab) between a status chip's fill and the board
// column it sits on, so the chip stays visible. Brightness contrast alone isn't the right test here:
// a grey chip on a blue column of the same brightness is plain to see, the same blue isn't.
const CHIP_ON_LANE = 0.1;

function oklab(hexColour: string): [number, number, number] {
  const lin = rgb(hexColour).map((v) => {
    const c = v / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  const [r, g, b] = lin;
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s, 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s, 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s];
}

// colourDistance: how different two colours look (OKLab; 0 = the same, about 0.02 = barely visible).
export function colourDistance(a: string, b: string): number {
  const x = oklab(a);
  const y = oklab(b);
  return Math.hypot(x[0] - y[0], x[1] - y[1], x[2] - y[2]);
}
const CATEGORY_NAME: Record<string, string> = { backlog: "Backlog", todo: "To-Do", doing: "Doing", waiting: "Waiting", done: "Done", cancelled: "Cancelled" };

// a check: [the colour Fix changes, the other side, the minimum, the warning, how it's measured]
type Check = [ColourKey, ColourKey | string, number, string, ("contrast" | "distance")?];

// contrastChecks: every pair the editor checks. The first colour of each pair is the one Fix changes.
function contrastChecks(t: Theme): Check[] {
  const checks: Check[] = [
    ["text", "background", 4.5, "Text on the background is hard to read"],
    ["text", "surface", 4.5, "Text on cards is hard to read"],
    ["mutedText", "surface", 3, "Secondary text on cards is hard to read"],
    ["onAccent", "accent", 4.5, "Text on the accent is hard to read"],
    ["accent", "background", 3, "The accent doesn't stand out from the background"],
  ];
  if (t.secondary) checks.push(["text", "secondary", 4.5, "Text on secondary buttons is hard to read"]);
  // on the columns sit muted text ("Nothing here", "+ New gig", counts) and the status chips; the
  // lane comes first, so Fix adjusts the column colour, not the app's secondary text
  if (t.laneColor) {
    checks.push(["laneColor", "mutedText", 3, "Secondary text on the board columns is hard to read"]);
    for (const [cat, c] of Object.entries(CATEGORY_COLOR)) {
      checks.push(["laneColor", c, CHIP_ON_LANE, `The ${CATEGORY_NAME[cat] ?? cat} status chip blends into the board columns`, "distance"]);
    }
  }
  return checks;
}

function side(t: Theme, k: ColourKey | string): string {
  return k.startsWith("#") ? k : colourOf(t, k as ColourKey);
}

function measure(t: Theme, [fg, bg, , , how]: Check): number {
  return how === "distance" ? colourDistance(side(t, fg), side(t, bg)) : ratio(side(t, fg), side(t, bg));
}

export function contrastProblems(t: Theme): Problem[] {
  return contrastChecks(t)
    .map((c) => ({ fg: c[0], bg: c[1], need: c[2], label: c[3], kind: c[4] ?? "contrast", have: measure(t, c) }))
    .filter((p) => p.have < p.need);
}

// fixFor: a new value for one colour that passes every check it takes part in (on either side),
// changed as little as possible: a 5% step toward white or black at a time. When no blend passes
// them all, the one that fails the fewest (then the one closest to passing) wins. Fixing one
// warning therefore never quietly makes another one on the same colour worse.
export function fixFor(t: Theme, key: ColourKey): string {
  const start = colourOf(t, key);
  const checks = contrastChecks(t).filter(([fg, bg]) => fg === key || bg === key);
  const score = (c: string) => {
    const tt = { ...t, [key]: c } as Theme;
    let fails = 0;
    let worst = Infinity;
    for (const c of checks) {
      const r = measure(tt, c) / c[2];
      if (r < 1) fails++;
      worst = Math.min(worst, r);
    }
    return { fails, worst };
  };
  let best = { c: start, ...score(start) };
  for (let i = 1; i <= 20; i++) {
    for (const target of ["#FFFFFF", "#000000"]) {
      const c = mix(start, target, i * 0.05);
      const sc = score(c);
      if (sc.fails === 0) return c;
      if (sc.fails < best.fails || (sc.fails === best.fails && sc.worst > best.worst)) best = { c, ...sc };
    }
  }
  return best.c;
}

// nearestPassing: blend fg toward black (light bg) or white (dark bg) in 5% steps (up to 20).
// If that direction can't reach the minimum (mid-tone backgrounds), the other direction is tried.
export function nearestPassing(fg: string, bg: string, need: number): string {
  const first = luminance(bg) > 0.5 ? "#000000" : "#FFFFFF";
  for (const target of [first, first === "#000000" ? "#FFFFFF" : "#000000"]) {
    for (let i = 1; i <= 20; i++) {
      const c = mix(fg, target, i * 0.05);
      if (ratio(c, bg) >= need) return c;
    }
  }
  return ratio("#000000", bg) >= ratio("#FFFFFF", bg) ? "#000000" : "#FFFFFF";
}

// ---- import (spec section 6): object, array, full state, bare string; JSON found anywhere in text

export interface ImportResult {
  themes: Theme[];
  selectId?: string;
  matchPhone?: boolean;
  notes: string[];
  summary: string;
}

// firstJSON: the first balanced JSON object/array (or a quoted string) in pasted text (code fences, prose).
export function firstJSON(text: string): unknown {
  const t = text.trim();
  try {
    return JSON.parse(t);
  } catch {
    /* look inside */
  }
  for (let i = 0; i < t.length; i++) {
    const open = t[i];
    if (open !== "{" && open !== "[") continue;
    const close = open === "{" ? "}" : "]";
    let depth = 0;
    let inStr = false;
    for (let j = i; j < t.length; j++) {
      const c = t[j];
      if (inStr) {
        if (c === "\\") j++;
        else if (c === '"') inStr = false;
        continue;
      }
      if (c === '"') inStr = true;
      else if (c === open) depth++;
      else if (c === close && --depth === 0) {
        try {
          return JSON.parse(t.slice(i, j + 1));
        } catch {
          break;
        }
      }
    }
  }
  return undefined;
}

const RESET_SECONDARY = ["accent", "surface", "background", "text", "dark"];

// fromObject: merge an imported object over the current theme (missing fields come from it).
export function fromObject(o: Record<string, unknown>, current: Theme, notes: string[]): Theme {
  const name = typeof o.name === "string" && o.name.trim() ? o.name.trim().slice(0, 40) : "My theme";
  const t: Theme = { ...current, id: "", name, description: "", widget: undefined };
  delete t.controlShape;
  delete t.laneColor;
  delete t.laneBorder;
  delete t.logoColor;
  if (typeof o.dark === "boolean") t.dark = o.dark;
  for (const k of COLOUR_KEYS) {
    if (!(k in o)) continue;
    const c = parseColour(o[k]);
    if (c) t[k] = c;
    else notes.push(`${name}: ${k} ${JSON.stringify(o[k])} isn't a colour (write it like #FF5252), so it was kept.`);
  }
  if ("secondary" in o) {
    if (o.secondary === null) t.secondary = null;
    else {
      const c = parseColour(o.secondary);
      if (c) t.secondary = c;
      else notes.push(`${name}: secondary isn't a colour, so it was kept.`);
    }
  } else if (RESET_SECONDARY.some((k) => k in o)) {
    t.secondary = null;
  }
  const font = (o.font ?? o.bodyFont ?? o.headingFont) as unknown;
  if (typeof font === "string") {
    if (/^[A-Za-z0-9 ]{1,60}$/.test(font)) t.font = font;
    else notes.push(`${name}: font "${font}" isn't a font name, so it was kept.`);
  }
  if (typeof o.fontWeight === "string" && (WEIGHT_NAMES as readonly string[]).includes(o.fontWeight)) t.fontWeight = o.fontWeight as Theme["fontWeight"];
  for (const k of ["textScalePct", "cornerDp", "borderDp"] as const) if (typeof o[k] === "number") t[k] = o[k] as number;
  if (o.widget && typeof o.widget === "object" && !Array.isArray(o.widget)) t.widget = o.widget as Record<string, unknown>;
  if (o.controlShape === "pill") t.controlShape = "pill";
  for (const k of EXTRA_COLOURS) {
    if (!(k in o) || o[k] === null) continue;
    const c = parseColour(o[k]);
    if (c) t[k] = c;
    else notes.push(`${name}: ${k} isn't a colour, so it was left out.`);
  }
  if (typeof o.id === "string" && /^[A-Za-z0-9_-]{1,40}$/.test(o.id) && !findTheme(o.id)) t.id = o.id;
  const s = sane(t);
  for (const p of contrastProblems(s)) notes.push(`${s.name}: ${p.label.charAt(0).toLowerCase() + p.label.slice(1)}.`);
  return s;
}

export function importText(text: string, current: Theme, findByName: (n: string) => Theme | undefined): ImportResult {
  const notes: string[] = [];
  const v = firstJSON(text);
  if (typeof v === "string" || (v === undefined && /^[\w -]{2,40}$/.test(text.trim()))) {
    const name = typeof v === "string" ? v : text.trim();
    const hit = findByName(name);
    return hit
      ? { themes: [], selectId: hit.id, notes, summary: `Switched to "${hit.name}".` }
      : { themes: [], notes, summary: `There's no theme called "${name}", so the selection didn't change.` };
  }
  if (Array.isArray(v)) {
    const themes = v.filter((x) => x && typeof x === "object").map((x) => fromObject(x as Record<string, unknown>, current, notes));
    return { themes, notes, selectId: undefined, summary: `Added ${themes.length} theme${themes.length === 1 ? "" : "s"}${themes.length ? ` and switched to "${themes[themes.length - 1]!.name}"` : ""}.` };
  }
  if (v && typeof v === "object") {
    const o = v as Record<string, unknown>;
    const list = (o.custom ?? o.themes) as unknown;
    if (Array.isArray(list)) {
      const themes = list.filter((x) => x && typeof x === "object").map((x) => fromObject(x as Record<string, unknown>, current, notes));
      const sel = typeof o.selectedId === "string" ? o.selectedId : undefined;
      return { themes, selectId: sel, matchPhone: typeof o.matchPhone === "boolean" ? o.matchPhone : undefined, notes,
        summary: `Imported ${themes.length} theme${themes.length === 1 ? "" : "s"}${sel ? " and the selection" : ""}.` };
    }
    const t = fromObject(o, current, notes);
    return { themes: [t], notes, summary: `Added "${t.name}" and switched to it.` };
  }
  return { themes: [], notes, summary: "No theme JSON found in that text." };
}

// ---- AI prompt (spec section 7): one function, so docs and prompt never disagree.

export const THEME_DOCS_URL = "https://github.com/0x2alx/gigg/blob/main/docs/guide/themes.md";

export function aiPrompt(description: string, current: Theme): string {
  return [
    "I use gigg, a gig board for people and AI agents, and I want a new colour theme for it.",
    "A theme changes the colours, the font, the font weight, the text size, the corner roundness and the border width.",
    "Answer with ONLY the theme's JSON in one code block.",
    `How themes work: ${THEME_DOCS_URL}`,
    "",
    "Format (every field optional; anything left out is taken from my current theme):",
    themeJSON({ ...BUILTINS[0]!, name: "Example", widget: undefined }),
    "",
    "Colour roles:",
    "- background: the page behind everything; surface: cards, dialogs, the menu; surfaceVariant: inputs, chips, soft fills",
    "- border: outlines and dividers; accent: primary buttons, links, highlights; onAccent: text and icons on accent fills",
    "- text: main text; mutedText: secondary text (captions, helper text); danger: warnings and destructive actions",
    "- secondary: optional tonal buttons and selected chips; null means Auto (a deeper shade of the accent)",
    `Fonts: ${BUNDLED_FONTS.join(", ")}, ${SYSTEM_FONTS.join(", ")}, or any Google Fonts family by its exact name.`,
    "fontWeight: Light, Regular, Medium or Bold. textScalePct 85-130, cornerDp 0-28, borderDp 0-3. Colours as #RRGGBB.",
    "Contrast minimums: text on background and on cards 4.5:1, mutedText on cards 3:1, onAccent on accent 4.5:1, accent on background 3:1.",
    "",
    "My current theme:",
    themeJSON(current),
    "",
    "What I want:",
    description,
  ].join("\n");
}
