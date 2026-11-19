#!/usr/bin/env python3
"""gigg brand assets: one ship geometry (measured from the concept sketch), every variant.

The ship is drawn in a 450x450 frame, symmetric about x=225; the right side mirrors the left.
Writes SVG sources to /b/out/svg; render.sh turns them into PNG/ICO.
"""
import os

OUT = "/b/out/svg"
os.makedirs(OUT, exist_ok=True)

DARK = {"bg": "#100318", "surface": "#1C0A2E", "hull_top": "#CB7BFF", "hull_bot": "#B24FF6",
        "light": "#E040FB", "flame_top": "#FFD54F", "flame_bot": "#FFB300", "border": "#BE5CFF",
        "text": "#FFC107", "tag": "#E040FB", "grid": "#BE5CFF"}
LIGHT = dict(DARK, bg="#F4EFE6", surface="#EDE4F5", hull_top="#8A3FD0", hull_bot="#6E28B4", border="#7B2FC0")

HULL = "M225,74 L350,295 L275,283 L262,297 L250,313 L200,313 L188,297 L175,283 L100,295 Z"
SLOTS = [(175, 193, 18, 78), (216, 163, 18, 108), (257, 193, 18, 78)]  # x, y, w, h
NOSE = "M225,104 L238,131 L212,131 Z"
FIN_L = "M141,151 C149,152 151,168 149,178 L131,210 C127,196 128,172 132,161 C134,155 137,151 141,151 Z"
FLAME_L = "M135,299 Q135,297 137,297 L157,297 Q159,297 159,299 C157,328 152,348 146,367 C141,348 136,328 135,299 Z"
MIRROR = 'transform="translate(450,0) scale(-1,1)"'


def ship(c, uid, mono=None, hole_nose=False):
    """The ship as SVG elements in the 450 frame. mono: one colour for everything (nose cut out)."""
    hull_fill = mono or f"url(#hull{uid})"
    light = mono or c["light"]
    flame = mono or f"url(#flame{uid})"
    slots = "".join(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="9" fill="black"/>' for x, y, w, h in SLOTS)
    nose_hole = f'<path d="{NOSE}" fill="black" stroke="black" stroke-width="6" stroke-linejoin="round"/>' if hole_nose else ""
    defs = f"""<defs>
<linearGradient id="hull{uid}" x1="0" y1="74" x2="0" y2="313" gradientUnits="userSpaceOnUse">
<stop offset="0" stop-color="{c['hull_top']}"/><stop offset="1" stop-color="{c['hull_bot']}"/></linearGradient>
<linearGradient id="flame{uid}" x1="0" y1="297" x2="0" y2="367" gradientUnits="userSpaceOnUse">
<stop offset="0" stop-color="{c['flame_top']}"/><stop offset="1" stop-color="{c['flame_bot']}"/></linearGradient>
<mask id="cut{uid}" maskUnits="userSpaceOnUse" x="0" y="0" width="450" height="450">
<path d="{HULL}" fill="white" stroke="white" stroke-width="12" stroke-linejoin="round"/>{slots}{nose_hole}</mask>
</defs>"""
    body = f"""<rect x="0" y="0" width="450" height="450" fill="{hull_fill}" mask="url(#cut{uid})"/>"""
    if not hole_nose:
        body += f'<path d="{NOSE}" fill="{light}" stroke="{light}" stroke-width="6" stroke-linejoin="round"/>'
    body += f'<path d="{FIN_L}" fill="{light}"/><path d="{FIN_L}" fill="{light}" {MIRROR}/>'
    body += f'<path d="{FLAME_L}" fill="{flame}"/><path d="{FLAME_L}" fill="{flame}" {MIRROR}/>'
    return defs + body


def place(c, uid, cx, cy, height, **kw):
    """The ship scaled so its full height (hull apex to flame tips, 293 units) is `height`, centred on (cx, cy)."""
    s = height / 293.0
    # ship bbox in the frame: x 98..352, y 74..367 -> centre (225, 220.5)
    return f'<g transform="translate({cx - 225 * s:.2f},{cy - 220.5 * s:.2f}) scale({s:.4f})">{ship(c, uid, **kw)}</g>'


def svg(w, h, inner, extra_defs=""):
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}">'
            f'{extra_defs}{inner}</svg>\n')


def write(name, text):
    with open(os.path.join(OUT, name), "w") as f:
        f.write(text)


# 1. Mark only, transparent (logo in the app, README). Square, small margin.
write("logo-mark.svg", svg(512, 512, place(DARK, "m", 256, 256, 470)))
write("logo-mark-light.svg", svg(512, 512, place(LIGHT, "ml", 256, 256, 470)))
# Monochrome: currentColor, so the app can tint it with the theme's accent.
write("logo-mono.svg", svg(512, 512, place(DARK, "mo", 256, 256, 470, mono="currentColor", hole_nose=True)))
write("logo-mono-violet.svg", svg(512, 512, place(DARK, "mv", 256, 256, 470, mono="#BE5CFF", hole_nose=True)))

# 2. App icon: rounded tile, border, soft glow; transparent outside the tile.
glow = """<defs><filter id="glow" x="-20%" y="-20%" width="140%" height="140%">
<feGaussianBlur stdDeviation="18"/></filter></defs>"""
tile = (f'<rect x="72" y="72" width="880" height="880" rx="200" fill="none" stroke="{DARK["border"]}" stroke-width="22" '
        f'opacity="0.55" filter="url(#glow)"/>'
        f'<rect x="72" y="72" width="880" height="880" rx="200" fill="{DARK["bg"]}" stroke="{DARK["border"]}" stroke-width="14"/>')
write("icon.svg", svg(1024, 1024, tile + place(DARK, "i", 512, 520, 600), glow))
tile_l = (f'<rect x="72" y="72" width="880" height="880" rx="200" fill="{LIGHT["bg"]}" stroke="{LIGHT["border"]}" stroke-width="14"/>')
write("icon-light.svg", svg(1024, 1024, tile_l + place(LIGHT, "il", 512, 520, 600)))

# 3. Maskable (PWA) and Apple touch: full-bleed square, ship inside the central safe circle.
write("maskable.svg", svg(1024, 1024, f'<rect width="1024" height="1024" fill="{DARK["bg"]}"/>' + place(DARK, "k", 512, 518, 540)))

# 4. Favicon: no border or glow (noise at 16 px), big ship on a dark rounded tile.
write("favicon.svg", svg(64, 64, f'<rect width="64" height="64" rx="14" fill="{DARK["bg"]}"/>' + place(DARK, "f", 32, 33, 56)))


# 4b. 16 px favicon: pixel-aligned simplification (hull, three slots, two flames; no fins or nose)
f16 = (f'<rect width="16" height="16" rx="3" fill="{DARK["bg"]}"/>'
       f'<path d="M8,1.5 L14.5,12 L10,12 L9.5,13 L6.5,13 L6,12 L1.5,12 Z" fill="#C46AFF"/>'
       f'<rect x="5" y="7" width="1" height="4" fill="{DARK["bg"]}"/><rect x="7.5" y="5" width="1" height="6" fill="{DARK["bg"]}"/>'
       f'<rect x="10" y="7" width="1" height="4" fill="{DARK["bg"]}"/>'
       f'<rect x="3.5" y="12.5" width="1.5" height="2.5" fill="{DARK["text"]}"/><rect x="11" y="12.5" width="1.5" height="2.5" fill="{DARK["text"]}"/>')
write("favicon-16.svg", svg(16, 16, f16))

# 5. Banner and social preview: perspective grid, ship + "gigg" wordmark (Poppins Bold).
def grid(w, h, horizon, c, lines_on=True):
    vx = w / 2
    lines = []
    for i in range(-14, 15):
        x = vx + i * (w / 9)
        lines.append(f'<line x1="{vx:.1f}" y1="{horizon}" x2="{x:.1f}" y2="{h}"/>')
    y, step = horizon, 6.0
    while y < h:
        y += step
        step *= 1.32
        lines.append(f'<line x1="0" y1="{y:.1f}" x2="{w}" y2="{y:.1f}"/>')
    return (f'<defs><linearGradient id="fade" x1="0" y1="{horizon}" x2="0" y2="{h}" gradientUnits="userSpaceOnUse">'
            f'<stop offset="0" stop-color="white" stop-opacity="0"/><stop offset="1" stop-color="white" stop-opacity="0.55"/>'
            f'</linearGradient><mask id="gridfade"><rect width="{w}" height="{h}" fill="url(#fade)"/></mask>'
            f'<radialGradient id="haze" cx="0.5" cy="{horizon / h:.3f}" r="0.6"><stop offset="0" stop-color="{c["border"]}" '
            f'stop-opacity="0.22"/><stop offset="1" stop-color="{c["border"]}" stop-opacity="0"/></radialGradient></defs>'
            f'<rect width="{w}" height="{h}" fill="{c["bg"]}"/><rect width="{w}" height="{h}" fill="url(#haze)"/>'
            + (f'<g stroke="{c["grid"]}" stroke-width="1.6" mask="url(#gridfade)">{"".join(lines)}</g>' if lines_on else ""))


def wordmark(x, baseline, size, c):
    return (f'<text x="{x}" y="{baseline}" font-family="Poppins" font-weight="700" font-size="{size}" '
            f'letter-spacing="-{size * 0.02:.1f}" fill="{c["text"]}">gigg</text>')


write("banner.svg", svg(1600, 400, grid(1600, 400, 250, DARK) + place(DARK, "b", 470, 200, 300) + wordmark(650, 262, 250, DARK)))
# social preview: the glow without the grid; ship + wordmark centred (ship ~260 wide, 50 gap, wordmark ~2.2 x
# size wide), the README's tagline centred under them
og = (grid(1200, 630, 300, DARK, lines_on=False) + place(DARK, "o", 366, 250, 300) + wordmark(546, 250, 190, DARK)
      + f'<text x="600" y="490" text-anchor="middle" font-family="Poppins" font-weight="500" font-size="38" '
        f'fill="{DARK["tag"]}">Hand work to your AI agents, and keep an eye on it.</text>')
write("og-image.svg", svg(1200, 630, og))
print("svgs:", sorted(os.listdir(OUT)))
