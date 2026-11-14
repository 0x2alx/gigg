#!/bin/sh
# Render every SVG source to the PNG/ICO sizes the site needs.
set -e
S=/b/out/svg; P=/b/out/png; mkdir -p $P
r() { rsvg-convert -w "$2" -h "$2" "$S/$1.svg" -o "$P/$3"; }
r favicon-16 16 favicon-16.png
for n in 32 48 64; do r favicon $n favicon-$n.png; done
magick $P/favicon-16.png $P/favicon-32.png $P/favicon-48.png $P/favicon.ico
r maskable 180 apple-touch-icon.png
for n in 192 512; do r icon $n icon-$n.png; r maskable $n maskable-$n.png; done
r icon 1024 icon-1024.png
r icon-light 512 icon-light-512.png
r logo-mark 512 logo-mark-512.png
r logo-mark 128 logo-mark-128.png
r logo-mark-light 512 logo-mark-light-512.png
r logo-mono-violet 512 logo-mono-512.png
rsvg-convert -w 1600 -h 400 $S/banner.svg -o $P/banner-1600x400.png
rsvg-convert -w 1200 -h 630 $S/og-image.svg -o $P/og-image-1200x630.png
# a contact sheet to review everything at once
magick montage -background '#2a2a2a' -geometry 220x220+12+12 -tile 6x \
  $P/icon-512.png $P/maskable-512.png $P/icon-light-512.png $P/logo-mark-512.png $P/logo-mark-light-512.png $P/logo-mono-512.png \
  $P/review-icons.png
ls -la $P
