# App icons

`icon.svg` (rounded tile: favicon and `any` manifest icons) and
`icon-maskable.svg` (full bleed, glyph inside the 80% safe zone: maskable
manifest icon and iOS `apple-touch-icon`) are the sources. Both draw the Lucide
ticket glyph credited in [`../LICENSES.txt`](../LICENSES.txt) on the brand plum.

The PNGs are rendered from them at exact pixel sizes in Chromium (an `<img>` of
the SVG, screenshotted with a transparent background):

| Output | Source | Size |
|---|---|---|
| `icon-192.png` | `icon.svg` | 192 |
| `icon-512.png` | `icon.svg` | 512 |
| `icon-maskable-512.png` | `icon-maskable.svg` | 512 |
| `apple-touch-icon.png` | `icon-maskable.svg` | 180 |

Re-render all four after changing either SVG. The colours are hex because icon
and manifest consumers don't all parse `oklch()`; keep them matched to `--brand`
and `--paper` in `views/styles/app.css`.
