// The theme screen: gallery, editor with a live preview,
// colour dialog, import (anything the format accepts, JSON found inside prose), Copy JSON, "make one
// with AI". Themes are saved on the account (server-sanitised); the selection follows the user.
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api, ApiError } from "./api";
import { useApp, toTheme } from "./App";
import {
  BUILTINS, BUNDLED_FONTS, COLOUR_KEYS, ROLE_LABEL, SYSTEM_FONTS, aiPrompt, contrastProblems, findTheme, importText, mix, nearestPassing,
  parseColour, ratio, readableOn, sane, secondaryOf, themeJSON, type ColourKey, type Theme,
} from "./theme";
import { Modal } from "./ui";
import { HelpButton } from "./help";
import { Icon } from "./icons";

function useToast(): [string, (m: string) => void] {
  const [msg, setMsg] = useState("");
  const timer = useRef<number>(0);
  return [
    msg,
    (m: string) => {
      setMsg(m);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setMsg(""), 3500);
    },
  ];
}

export function ThemesPage() {
  const { themes } = useApp();
  const [editing, setEditing] = useState<Theme | null>(null);
  const [importing, setImporting] = useState(false);
  const [aiOpen, setAiOpen] = useState(false);
  const [toast, show] = useToast();
  const [notes, setNotes] = useState<string[]>([]);

  const copy = (t: Theme) => {
    void navigator.clipboard?.writeText(themeJSON(t));
    show(`"${t.name}" copied as JSON.`);
  };
  const duplicate = (t: Theme) => setEditing({ ...t, id: "", name: `${t.name} copy`.slice(0, 40) });
  const remove = async (t: Theme) => {
    if (!window.confirm(`Delete "${t.name}"?`)) return;
    await api.deleteTheme(t.id);
    await themes.reload();
  };
  const save = async (t: Theme) => {
    const id = t.id || `custom-${Date.now()}`;
    const r = await api.putTheme(id, JSON.parse(themeJSON(sane(t))));
    if (r.dropped_oldest) setNotes(["Only your 50 most recent themes were kept."]);
    await themes.select(id);
    setEditing(null);
    show(`Saved "${t.name}" and switched to it.`);
  };

  if (editing) return <Editor initial={editing} onCancel={() => setEditing(null)} onSave={save} />;

  const selected = themes.custom.find((c) => c.id === themes.selectedId) ?? findTheme(themes.selectedId) ?? themes.active;
  const customActions = (t: Theme): CardAction[] => [
    { icon: <Icon name="pencil" />, label: "Edit", run: () => setEditing(t) },
    { icon: <Icon name="copy" />, label: "Duplicate", run: () => duplicate(t) },
    { icon: "{ }", label: "Copy JSON", run: () => copy(t) },
    { icon: <Icon name="trash" />, label: "Delete", run: () => void remove(t), danger: true },
  ];
  const builtinActions = (t: Theme): CardAction[] => [
    { icon: <Icon name="copy" />, label: "Duplicate", run: () => duplicate(t) },
    { icon: "{ }", label: "Copy JSON", run: () => copy(t) },
  ];

  return (
    <div className="themes">
      <header className="page-head tg-head">
        <div>
          <h1>Themes<HelpButton topic="themes" label="Help: themes" /></h1>
          <p className="muted small">Saved on your account, so they follow you to every device. Import and share them as JSON.</p>
        </div>
        <div className="tg-create" role="group" aria-label="New theme">
          <button type="button" className="button" onClick={() => setEditing({ ...themes.active, id: "", name: "My theme", widget: undefined })}>
            + New theme
          </button>
          <button type="button" className="button secondary" onClick={() => setAiOpen(true)}>
            ✨ Make with AI
          </button>
          <button type="button" className="button secondary" onClick={() => setImporting(true)}>
            ⇩ Import
          </button>
        </div>
      </header>

      <section className="card tg-current" aria-label="Current theme">
        <span className="tg-current-tile" aria-hidden="true">
          <MiniPreview t={themes.active} />
        </span>
        <div className="tg-current-text">
          <span className="muted small">Current theme</span>
          <strong className="tg-current-name">{selected.name}</strong>
          {themes.active.id !== selected.id && <span className="muted small">Showing {themes.active.name} while your system is in light mode.</span>}
          <ThemeChips t={selected} />
        </div>
        <label className="switch tg-match">
          <input type="checkbox" checked={themes.matchSystem} onChange={(e) => void themes.setMatchSystem(e.target.checked)} />
          <span>
            Match the system
            <span className="muted small tg-match-hint">Daywalker while your system is in light mode.</span>
          </span>
        </label>
      </section>

      {toast && (
        <div className="toast" role="status">
          {toast}
        </div>
      )}
      {notes.length > 0 && (
        <ul className="notes">
          {notes.map((n, i) => (
            <li key={i}>{n}</li>
          ))}
        </ul>
      )}

      <section className="tg-section" aria-labelledby="tg-yours">
        <div className="tg-section-head">
          <h2 id="tg-yours">Your themes</h2>
          {themes.custom.length > 0 && <span className="count">{themes.custom.length}</span>}
        </div>
        {themes.custom.length === 0 ? (
          <p className="muted tg-empty">None yet. Create one, make one with AI, or duplicate a built-in theme below.</p>
        ) : (
          <div className="tg-grid">
            {themes.custom.map((t) => (
              <ThemeCard key={t.id} t={t} selected={t.id === themes.selectedId} onPick={() => void themes.select(t.id)} actions={customActions(t)} />
            ))}
          </div>
        )}
      </section>

      <section className="tg-section" aria-labelledby="tg-builtin">
        <div className="tg-section-head">
          <h2 id="tg-builtin">Built-in</h2>
          <span className="muted small">These never change. Duplicate one to edit a copy of your own.</span>
        </div>
        <div className="tg-grid">
          {BUILTINS.map((t) => (
            <ThemeCard key={t.id} t={t} selected={t.id === themes.selectedId} onPick={() => void themes.select(t.id)} actions={builtinActions(t)} />
          ))}
        </div>
      </section>
      {importing && (
        <ImportModal
          onClose={() => setImporting(false)}
          onDone={(summary, n) => {
            show(summary);
            setNotes(n);
          }}
        />
      )}
      {aiOpen && (
        <AIModal
          onClose={() => setAiOpen(false)}
          onDone={(summary, n) => {
            show(summary);
            setNotes(n);
          }}
        />
      )}
    </div>
  );
}

function fontStack(font: string): string {
  return font === "Phone default" || SYSTEM_FONTS.includes(font) ? "system-ui, sans-serif" : `"${font}", system-ui, sans-serif`;
}

interface CardAction {
  icon: ReactNode;
  label: string;
  run: () => void;
  danger?: boolean;
}

// MiniPreview: a small card drawn in the theme's own colours (data colours, set inline).
function MiniPreview({ t, flush }: { t: Theme; flush?: boolean }) {
  const r = t.cornerDp;
  return (
    <span className="tg-mini" style={{ background: t.background, color: t.text, fontFamily: fontStack(t.font), borderRadius: flush ? 0 : r }}>
      <span className="theme-mini" style={{ background: t.surface, border: `${t.borderDp}px solid ${t.border}`, borderRadius: r }}>
        <span className="theme-mini-title">Card title</span>
        <span className="theme-mini-muted" style={{ color: t.mutedText }}>
          Secondary text
        </span>
        <span className="theme-mini-button" style={{ background: t.accent, color: t.onAccent, borderRadius: (3 * r) / 4 }}>
          Button
        </span>
      </span>
    </span>
  );
}

function ThemeChips({ t }: { t: Theme }) {
  return (
    <span className="tg-chips">
      <span className="pill pill-muted">{t.dark ? "◐ dark" : "◑ light"}</span>
      <span className="pill pill-muted tg-font" title={`Font: ${t.font}`}>
        Aa {t.font}
      </span>
    </span>
  );
}

function ThemeCard({ t, selected, onPick, actions }: { t: Theme; selected: boolean; onPick: () => void; actions: CardAction[] }) {
  return (
    <article className={"tg-card" + (selected ? " selected" : "")} aria-label={t.name}>
      <button type="button" className="tg-preview" onClick={onPick} aria-pressed={selected} aria-label={selected ? `${t.name}, current theme` : `Use ${t.name}`} title={selected ? "Current theme" : `Use ${t.name}`}>
        <MiniPreview t={t} flush />
        {selected && (
          <span className="theme-check" style={{ background: t.accent, color: t.onAccent }} aria-hidden="true">
            ✓
          </span>
        )}
      </button>
      <div className="tg-meta">
        <div className="tg-name-row">
          <span className="tg-name" title={t.name}>
            {t.name}
          </span>
          {selected && <span className="pill pill-accent">current</span>}
        </div>
        {t.description && <p className="tg-desc muted small">{t.description}</p>}
        <div className="tg-foot">
          <ThemeChips t={t} />
          <span className="tg-tools" role="group" aria-label={`${t.name} actions`}>
            {actions.map((a) => (
              <button key={a.label} type="button" className={"icon-button tg-tool" + (a.danger ? " tg-tool-danger" : "")} onClick={a.run} title={a.label} aria-label={`${a.label} ${t.name}`}>
                {a.icon}
              </button>
            ))}
          </span>
        </div>
      </div>
    </article>
  );
}

// ---- editor

const PRESETS = ["#000000", "#FFFFFF", "#100318", "#1C2529", "#F2F1EE", "#0C140D", "#0A0E14", "#BE5CFF", "#CDFFFB", "#3E4C59", "#F0B429",
  "#FFE600", "#00E5FF", "#FFC107", "#FE2AFF", "#E5484D", "#46A758", "#0090FF"];

function Editor({ initial, onCancel, onSave }: { initial: Theme; onCancel: () => void; onSave: (t: Theme) => Promise<void> }) {
  const [t, setT] = useState<Theme>(initial);
  const [flash, setFlash] = useState<string>("");
  const [err, setErr] = useState("");
  const [dialog, setDialog] = useState<ColourKey | null>(null);
  const [moreFonts, setMoreFonts] = useState(false);
  const problems = contrastProblems(t);
  const set = (patch: Partial<Theme>, highlight: string) => {
    setT((x) => ({ ...x, ...patch }));
    setFlash(highlight);
    window.setTimeout(() => setFlash((f) => (f === highlight ? "" : f)), 600);
  };
  const colourOf = (k: ColourKey) => (k === "secondary" ? secondaryOf(t) : t[k]);
  const save = () => onSave(sane(t)).catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Couldn't save the theme."));
  useEffect(() => {
    for (const f of BUNDLED_FONTS) loadPreviewFont(f);
  }, []);

  return (
    <div className="theme-editor">
      <div className="editor-bar">
        <button type="button" className="button secondary" onClick={onCancel}>
          <Icon name="x" /> Cancel
        </button>
        <h1>{initial.id ? `Edit ${initial.name}` : "New theme"}</h1>
        <button type="button" className="button" onClick={() => void save()}>
          Save
        </button>
      </div>
      {err && <p className="danger">{err}</p>}
      <Preview t={t} flash={flash} />
      <div className="editor-rows">
        <label className="editor-row">
          <span>Name</span>
          <input value={t.name} maxLength={40} onChange={(e) => set({ name: e.target.value }, "background")} />
        </label>
        <label className="editor-row switch">
          <input type="checkbox" checked={t.dark} onChange={(e) => set({ dark: e.target.checked }, "background")} />
          <span>Dark theme</span>
        </label>
        {([...COLOUR_KEYS.filter((k) => k !== "accent" && k !== "onAccent" && k !== "text" && k !== "mutedText" && k !== "danger"), "accent", "onAccent", "secondary", "text", "mutedText", "danger"] as ColourKey[]).map((k) => (
          <div key={k} className="editor-row">
            <span>{ROLE_LABEL[k]}</span>
            <div className="swatches">
              {k === "secondary" && (
                <button type="button" className={"swatch auto" + (t.secondary === null ? " on" : "")} aria-label={`${ROLE_LABEL[k]}: Auto${t.secondary === null ? ", selected" : ""}`}
                  title="Auto: a deeper shade of the accent; it follows the accent when you change it." style={{ background: secondaryOf({ ...t, secondary: null }) }}
                  onClick={() => set({ secondary: null }, k)}>
                  A
                </button>
              )}
              {dedupe([colourOf(k), ...PRESETS]).map((c) => (
                <button key={c} type="button" className={"swatch" + (colourOf(k) === c && !(k === "secondary" && t.secondary === null) ? " on" : "")} style={{ background: c }}
                  aria-label={`${ROLE_LABEL[k]}: ${c}${colourOf(k) === c ? ", selected" : ""}`} onClick={() => set({ [k]: c } as Partial<Theme>, k)} />
              ))}
              <button type="button" className="swatch custom" aria-label={`${ROLE_LABEL[k]}: custom colour`} onClick={() => setDialog(k)}>
                ✎
              </button>
            </div>
          </div>
        ))}
        <div className="editor-row">
          <span>Font</span>
          <div className="font-picker">
            {[...BUNDLED_FONTS, ...SYSTEM_FONTS].map((f) => (
              <button key={f} type="button" className={"font-opt" + (t.font === f ? " on" : "")} style={{ fontFamily: fontStack(f) }} onClick={() => set({ font: f }, "text")}>
                {f}
              </button>
            ))}
            {moreFonts ? (
              <input autoFocus placeholder="Any Google Fonts family, e.g. Roboto Slab" defaultValue={BUNDLED_FONTS.includes(t.font) || SYSTEM_FONTS.includes(t.font) ? "" : t.font}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    const v = (e.target as HTMLInputElement).value.trim();
                    if (/^[A-Za-z0-9 ]{1,60}$/.test(v)) {
                      loadPreviewFont(v);
                      set({ font: v }, "text");
                    }
                  }
                }} />
            ) : (
              <button type="button" className="font-opt" onClick={() => setMoreFonts(true)}>
                More Google fonts…
              </button>
            )}
          </div>
        </div>
        <label className="editor-row">
          <span>Font weight</span>
          <select value={t.fontWeight} onChange={(e) => set({ fontWeight: e.target.value as Theme["fontWeight"] }, "text")}>
            {["Light", "Regular", "Medium", "Bold"].map((w) => (
              <option key={w}>{w}</option>
            ))}
          </select>
        </label>
        <label className="editor-row">
          <span>Text size {t.textScalePct}%</span>
          <input type="range" min={85} max={130} value={t.textScalePct} onChange={(e) => set({ textScalePct: Number(e.target.value) }, "text")} />
        </label>
        <label className="editor-row">
          <span>Corners {t.cornerDp}</span>
          <input type="range" min={0} max={28} value={t.cornerDp} onChange={(e) => set({ cornerDp: Number(e.target.value) }, "surface")} />
        </label>
        <label className="editor-row">
          <span>Border width {t.borderDp}</span>
          <input type="range" min={0} max={3} value={t.borderDp} onChange={(e) => set({ borderDp: Number(e.target.value) }, "border")} />
        </label>
        <div className="editor-row">
          <span>Contrast</span>
          {problems.length === 0 ? (
            <span className="pill" style={{ background: "#46A758", color: readableOn("#46A758") }}>
              ✓ everything readable
            </span>
          ) : (
            <ul className="problems">
              {problems.map((p) => (
                <li key={p.label}>
                  <span className="danger">{p.label}</span> <span className="muted small">({p.have.toFixed(1)}:1, needs {p.need}:1)</span>{" "}
                  <button type="button" className="button small" onClick={() => set({ [p.fg]: nearestPassing(colourOf(p.fg), colourOf(p.bg), p.need) } as Partial<Theme>, p.fg)}>
                    Fix
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
      {dialog && (
        <ColourDialog
          label={ROLE_LABEL[dialog]}
          initial={colourOf(dialog)}
          onClose={() => setDialog(null)}
          onPick={(c) => {
            set({ [dialog]: c } as Partial<Theme>, dialog);
            setDialog(null);
          }}
        />
      )}
    </div>
  );
}

function dedupe(cs: string[]): string[] {
  return [...new Set(cs.map((c) => c.toUpperCase()))];
}

function loadPreviewFont(f: string) {
  if (SYSTEM_FONTS.includes(f) || !/^[A-Za-z0-9 ]{1,60}$/.test(f)) return;
  const id = "font-" + f.toLowerCase().replace(/[^a-z0-9]/g, "");
  if (document.getElementById(id)) return;
  const link = document.createElement("link");
  link.id = id;
  link.rel = "stylesheet";
  link.href = `https://fonts.googleapis.com/css2?family=${encodeURIComponent(f).replace(/%20/g, "+")}&display=swap`;
  document.head.appendChild(link);
}

// The pinned preview: a mock screen in the edited theme. Labels match the editor rows; the element
// that a row changes flashes for ~600 ms.
function Preview({ t, flash }: { t: Theme; flash: string }) {
  const r = t.cornerDp;
  const sec = secondaryOf(t);
  const onSec = ratio(t.text, sec) >= 4.5 ? t.text : t.accent;
  const fw = { Light: 300, Regular: 400, Medium: 500, Bold: 700 }[t.fontWeight];
  const hl = (k: string) => (flash === k ? " flash" : "");
  return (
    <div className={"preview" + hl("background")} style={{ background: t.background, color: t.text, fontFamily: fontStack(t.font), fontWeight: fw, fontSize: `${t.textScalePct}%`, borderRadius: r * 1.5 }}>
      <div className="preview-row">
        <strong>Background: {t.name}</strong>
        <span className={hl("danger")} style={{ color: t.danger }}>
          Warnings
        </span>
      </div>
      <div className={"preview-card" + hl("surface") + hl("border")} style={{ background: t.surface, border: `${t.borderDp}px solid ${t.border}`, borderRadius: r }}>
        <strong>Cards</strong> <span style={{ color: t.border }}>Borders</span>
        <div className={hl("mutedText")} style={{ color: t.mutedText }}>
          Secondary text
        </div>
        <div className="preview-row">
          <span className={"preview-input" + hl("surfaceVariant")} style={{ background: t.surfaceVariant, borderRadius: (3 * r) / 4 }}>
            Inputs
          </span>
          <span className={"preview-chip" + hl("surfaceVariant")} style={{ background: t.surfaceVariant, borderRadius: r / 2 }}>
            Chips
          </span>
        </div>
        <div className="preview-row">
          <span className={"preview-button" + hl("accent") + hl("onAccent")} style={{ background: t.accent, color: t.onAccent, borderRadius: (3 * r) / 4 }}>
            Accent
          </span>
          <span className={"preview-button" + hl("secondary")} style={{ background: sec, color: onSec, borderRadius: (3 * r) / 4 }}>
            Secondary buttons
          </span>
        </div>
      </div>
    </div>
  );
}

// ---- colour dialog: saturation/value square + hue bar, hex (or rgb()), R/G/B, old vs new

function toHSV(hex: string): [number, number, number] {
  const n = parseInt(hex.slice(1), 16);
  const [r, g, b] = [(n >> 16) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255];
  const max = Math.max(r, g, b);
  const d = max - Math.min(r, g, b);
  let h = 0;
  if (d) h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
  return [((h * 60) + 360) % 360, max ? d / max : 0, max];
}

function fromHSV(h: number, s: number, v: number): string {
  const f = (n: number) => {
    const k = (n + h / 60) % 6;
    return v - v * s * Math.max(0, Math.min(k, 4 - k, 1));
  };
  return "#" + [f(5), f(3), f(1)].map((x) => Math.round(x * 255).toString(16).padStart(2, "0")).join("").toUpperCase();
}

function ColourDialog({ label, initial, onClose, onPick }: { label: string; initial: string; onClose: () => void; onPick: (c: string) => void }) {
  const [c, setC] = useState(initial);
  const [hsv, setHSV] = useState(() => toHSV(initial));
  const [text, setText] = useState(initial);
  const sq = useRef<HTMLDivElement>(null);
  const rgb = useMemo(() => {
    const n = parseInt(c.slice(1), 16);
    return [n >> 16, (n >> 8) & 255, n & 255] as [number, number, number];
  }, [c]);
  const fromHex = (h: string) => {
    setC(h);
    setHSV(toHSV(h));
    setText(h);
  };
  const drag = (e: React.PointerEvent) => {
    const box = sq.current!.getBoundingClientRect();
    const s = Math.min(1, Math.max(0, (e.clientX - box.left) / box.width));
    const v = 1 - Math.min(1, Math.max(0, (e.clientY - box.top) / box.height));
    const h = fromHSV(hsv[0], s, v);
    setHSV([hsv[0], s, v]);
    setC(h);
    setText(h);
  };
  const setChannel = (i: number, val: number) => {
    const next = [...rgb] as [number, number, number];
    next[i] = Math.max(0, Math.min(255, val || 0));
    fromHex("#" + next.map((x) => x.toString(16).padStart(2, "0")).join("").toUpperCase());
  };
  return (
    <Modal title={`${label}: custom colour`} onClose={onClose}>
      <div className="colour-dialog">
        <div ref={sq} className="sv" style={{ background: `linear-gradient(to top, #000, transparent), linear-gradient(to right, #fff, ${fromHSV(hsv[0], 1, 1)})` }}
          onPointerDown={(e) => {
            (e.target as HTMLElement).setPointerCapture(e.pointerId);
            drag(e);
          }}
          onPointerMove={(e) => e.buttons && drag(e)} role="slider" aria-label="Saturation and brightness" aria-valuetext={c} tabIndex={0}>
          <span className="sv-dot" style={{ left: `${hsv[1] * 100}%`, top: `${(1 - hsv[2]) * 100}%`, borderColor: readableOn(c) }} />
        </div>
        <input type="range" className="hue" min={0} max={359} value={Math.round(hsv[0])} aria-label="Hue"
          onChange={(e) => {
            const h = Number(e.target.value);
            const hex = fromHSV(h, hsv[1], hsv[2]);
            setHSV([h, hsv[1], hsv[2]]);
            setC(hex);
            setText(hex);
          }} />
        <label>
          Hex or rgb(r, g, b)
          <input value={text} onChange={(e) => {
            setText(e.target.value);
            const p = parseColour(e.target.value);
            if (p) {
              setC(p);
              setHSV(toHSV(p));
            }
          }} />
        </label>
        {(["R", "G", "B"] as const).map((ch, i) => {
          const lo = [...rgb] as [number, number, number];
          const hi = [...rgb] as [number, number, number];
          lo[i] = 0;
          hi[i] = 255;
          const css = (a: number[]) => `rgb(${a.join(",")})`;
          return (
            <div key={ch} className="channel">
              <span>{ch}</span>
              <input type="range" min={0} max={255} value={rgb[i]} onChange={(e) => setChannel(i, Number(e.target.value))}
                style={{ background: `linear-gradient(to right, ${css(lo)}, ${css(hi)})` }} aria-label={ch} />
              <input type="number" min={0} max={255} value={rgb[i]} onChange={(e) => setChannel(i, Number(e.target.value))} aria-label={`${ch} value`} />
            </div>
          );
        })}
        <div className="old-new">
          <span style={{ background: initial, color: readableOn(initial) }}>Old {initial}</span>
          <span style={{ background: c, color: readableOn(c) }}>New {c}</span>
        </div>
        <div className="row end">
          <button type="button" className="button secondary" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="button" onClick={() => onPick(c)}>
            Use {c}
          </button>
        </div>
      </div>
    </Modal>
  );
}

// ---- import and AI

function useImporter(onDone: (summary: string, notes: string[]) => void) {
  const { themes } = useApp();
  return async (text: string) => {
    const all = [...themes.custom, ...BUILTINS];
    const res = importText(text, themes.active, (n) => {
      const k = n.toLowerCase();
      return all.find((t) => t.id.toLowerCase() === k || t.name.toLowerCase() === k) ?? findTheme(n);
    });
    let last = "";
    for (const t of res.themes) {
      const id = t.id || `custom-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
      const r = await api.putTheme(id, JSON.parse(themeJSON(t)));
      if (r.dropped_oldest) res.notes.push("Only your 50 most recent themes were kept.");
      last = id;
    }
    const sel = res.selectId ?? (last || undefined);
    if (sel) await themes.select(sel, res.matchPhone ?? themes.matchSystem);
    else await themes.reload();
    onDone(res.summary, res.notes);
  };
}

function ImportModal({ onClose, onDone }: { onClose: () => void; onDone: (summary: string, notes: string[]) => void }) {
  const [text, setText] = useState("");
  const [err, setErr] = useState("");
  const run = useImporter(onDone);
  return (
    <Modal title="Import a theme" onClose={onClose} wide>
      <p className="muted small">
        Paste a theme's JSON, a list of themes, a full theme backup, a theme's name, or an AI's whole answer: the JSON is found wherever it is.
      </p>
      <textarea rows={12} value={text} onChange={(e) => setText(e.target.value)} placeholder='{ "name": "Sunset", "dark": true, ... }' />
      {err && <p className="danger">{err}</p>}
      <div className="row end">
        <button type="button" className="button secondary" onClick={onClose}>
          Cancel
        </button>
        <button type="button" className="button" disabled={!text.trim()} onClick={() => run(text).then(onClose).catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Import failed."))}>
          Import
        </button>
      </div>
    </Modal>
  );
}

function AIModal({ onClose, onDone }: { onClose: () => void; onDone: (summary: string, notes: string[]) => void }) {
  const { themes } = useApp();
  const [want, setWant] = useState("");
  const [answer, setAnswer] = useState("");
  const [copied, setCopied] = useState(false);
  const [err, setErr] = useState("");
  const run = useImporter(onDone);
  return (
    <Modal title="Make a theme with AI" onClose={onClose} wide>
      <ol className="steps">
        <li>
          Describe the theme you want.
          <textarea rows={3} value={want} onChange={(e) => setWant(e.target.value)} placeholder="Warm sunset colours, rounded corners, a techy font, bigger text." />
        </li>
        <li>
          Copy the full prompt and paste it into your assistant.{" "}
          <button type="button" className="button secondary small" disabled={!want.trim()}
            onClick={() => {
              void navigator.clipboard?.writeText(aiPrompt(want, themes.active));
              setCopied(true);
            }}>
            {copied ? "✓ Copied" : "Copy full prompt"}
          </button>
        </li>
        <li>
          Paste its answer here.
          <textarea rows={8} value={answer} onChange={(e) => setAnswer(e.target.value)} />
        </li>
      </ol>
      {err && <p className="danger">{err}</p>}
      <div className="row end">
        <button type="button" className="button secondary" onClick={onClose}>
          Cancel
        </button>
        <button type="button" className="button" disabled={!answer.trim()} onClick={() => run(answer).then(onClose).catch((e: unknown) => setErr(e instanceof ApiError ? e.message : "Import failed."))}>
          Use this theme
        </button>
      </div>
    </Modal>
  );
}

// re-exported so tests can reach them
export { mix, toTheme };
