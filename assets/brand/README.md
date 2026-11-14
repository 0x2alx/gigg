# gigg brand assets

The logo is a delta starship whose hull has three kanban columns cut into it, with fins, a nose light and two thruster flames. `concept-starship.png` is the original concept sketch. Everything here is redrawn from it as vector art: the geometry is measured from that sheet, and one ship definition in `tools/gen.py` drives every variant.

## Files

| File | Use |
|---|---|
| `svg/favicon.svg`, `png/favicon.ico` (16/32/48), `png/favicon-32.png` | Browser tab. 16 px uses `svg/favicon-16.svg`, a pixel-aligned simplification. |
| `png/apple-touch-icon.png` (180) | iOS home screen (full bleed; iOS rounds the corners) |
| `png/icon-192.png`, `png/icon-512.png`, `png/icon-1024.png` | App icon: rounded tile, border, glow, transparent corners |
| `png/maskable-192.png`, `png/maskable-512.png` | PWA maskable icons: full bleed, ship inside the central safe circle |
| `svg/logo-mark.svg`, `png/logo-mark-*.png` | Ship only on transparent (sidebar, login page, README) |
| `svg/logo-mark-light.svg`, `svg/icon-light.svg` | Light-theme versions (darker violet hull) |
| `svg/logo-mono.svg` | Single colour in `currentColor`, so the app can tint it with the theme accent |
| `png/banner-1600x400.png` | Wordmark banner (README header, repo social) |
| `png/og-image-1200x630.png` | Link preview (Open Graph / Twitter card) |

**Palette:** `#100318` background, `#BE5CFF` violet, `#E040FB` magenta, `#FFC107` amber, plus `#F4EFE6` and `#7B2FC0` for the light versions.

**Wordmark font:** Poppins Bold (SIL Open Font License). The banner and OG SVGs use it as live text, so render them where Poppins is installed. The tools image installs it.

## Regenerating

You need container tooling (podman or docker). From this directory:

```sh
podman build -t brandtool tools
podman run --rm -v "$PWD/tools:/b/src:ro" -v "$PWD/out:/b/out" brandtool sh -c "python3 /b/src/gen.py && sh /b/src/render.sh"
```

Copy `out/svg` and `out/png` back here. Change shapes or colours in `tools/gen.py`; render sizes are in `tools/render.sh`.
