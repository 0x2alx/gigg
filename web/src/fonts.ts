// Fonts: the 20 the theme editor offers ship with gigg (Fontsource packages, OFL licensed), so no
// page ever contacts Google on its own. Their @font-face rules land in the app's CSS; a browser
// only downloads a font file when a theme uses it. Any other Google Fonts family loads from Google
// only after someone in this browser allows it with the button on the Themes page.
import "@fontsource-variable/inter/wght.css";
import "@fontsource-variable/oxanium/wght.css";
import "@fontsource-variable/orbitron/wght.css";
import "@fontsource-variable/tektur/wght.css";
import "@fontsource-variable/rubik/wght.css";
import "@fontsource-variable/nunito/wght.css";
import "@fontsource-variable/comfortaa/wght.css";
import "@fontsource-variable/caveat/wght.css";
import "@fontsource-variable/playfair-display/wght.css";
import "@fontsource-variable/lora/wght.css";
import "@fontsource-variable/space-grotesk/wght.css";
import "@fontsource/audiowide/400.css";
import "@fontsource/michroma/400.css";
import "@fontsource/share-tech-mono/400.css";
import "@fontsource/pacifico/400.css";
import "@fontsource/bebas-neue/400.css";
import "@fontsource/righteous/400.css";
import "@fontsource/press-start-2p/400.css";
// Rajdhani and Chakra Petch are drawn at 600 only (their lighter cuts are too thin for the UI)
import "@fontsource/rajdhani/600.css";
import "@fontsource/chakra-petch/600.css";

// BUNDLED_FAMILY: a theme's font name -> the CSS family that the bundled @font-face rules declare.
const BUNDLED_FAMILY: Record<string, string> = {
  Inter: "Inter Variable",
  Audiowide: "Audiowide",
  Oxanium: "Oxanium Variable",
  Orbitron: "Orbitron Variable",
  Michroma: "Michroma",
  Tektur: "Tektur Variable",
  Rubik: "Rubik Variable",
  Rajdhani: "Rajdhani",
  "Chakra Petch": "Chakra Petch",
  "Share Tech Mono": "Share Tech Mono",
  Nunito: "Nunito Variable",
  Comfortaa: "Comfortaa Variable",
  Caveat: "Caveat Variable",
  Pacifico: "Pacifico",
  "Playfair Display": "Playfair Display Variable",
  Lora: "Lora Variable",
  "Bebas Neue": "Bebas Neue",
  Righteous: "Righteous",
  "Space Grotesk": "Space Grotesk Variable",
  "Press Start 2P": "Press Start 2P",
};

export const BUNDLED_FONTS = Object.keys(BUNDLED_FAMILY);

// bundledFamily: the CSS family for a bundled font (matched without regard to case or spaces), else null.
export function bundledFamily(font: string): string | null {
  const n = norm(font);
  for (const [k, v] of Object.entries(BUNDLED_FAMILY)) if (norm(k) === n) return v;
  return null;
}

export function norm(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]/g, "");
}

// ---- Google Fonts, by consent only

const CONSENT_KEY = "gigg.googleFonts";
export const FONTS_ALLOWED_EVENT = "gigg:google-fonts-allowed";

// googleAllowed: someone in this browser pressed "Load from Google Fonts" (kept per browser: it's
// the browser, not the server, that contacts Google).
export function googleAllowed(): boolean {
  try {
    return localStorage.getItem(CONSENT_KEY) === "1";
  } catch {
    return false;
  }
}

// allowGoogle records the consent (for this page even when storage is blocked) and tells the app to
// apply the theme again, which then loads its font.
let allowedThisPage = false;
export function allowGoogle(): void {
  allowedThisPage = true;
  try {
    localStorage.setItem(CONSENT_KEY, "1");
  } catch {
    // storage blocked: the consent lasts until the page reloads
  }
  window.dispatchEvent(new Event(FONTS_ALLOWED_EVENT));
}

// remoteAllowed: the server's GIGG_REMOTE_FONTS (its Content-Security-Policy blocks Google when off).
let remote = true;
export function setRemoteFonts(on: boolean): void {
  remote = on;
}
export function remoteFonts(): boolean {
  return remote;
}

export function googleOk(): boolean {
  return remote && (allowedThisPage || googleAllowed());
}

export function validFamily(f: string): boolean {
  return /^[A-Za-z0-9 ]{1,60}$/.test(f);
}

// loadGoogleFont adds the stylesheet for a family (weights: Google's "wght@400;700" suffix or "").
// Callers check googleOk() first.
export function loadGoogleFont(family: string, weights = ""): void {
  if (!validFamily(family)) return;
  const id = `font-${norm(family)}-${weights.replace(/[^0-9]/g, "-")}`;
  if (document.getElementById(id)) return;
  const link = document.createElement("link");
  link.id = id;
  link.rel = "stylesheet";
  link.href = `https://fonts.googleapis.com/css2?family=${encodeURIComponent(family).replace(/%20/g, "+")}${weights}&display=swap`;
  document.head.appendChild(link);
}
