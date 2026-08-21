# ccplant brand assets

The mark is a simple two-leaf sprout. Runtime assets preserve the approved
raster artwork without vector reconstruction.

| Asset | Use |
| --- | --- |
| `ccplant-logo-approved.png` | High-resolution copy of the active transparent logo |
| `ccplant-logo-icon-matched.png` | Active transparent logo using the approved app icon mark |
| `ccplant-app-icon-approved.png` | Active high-resolution sprout app icon master |
| `ccplant-sprout-transparent.png` | Transparent sprout used by the logo and monochrome tray asset |
| `/icon-{192,256,384,512}x*.png` | Generated PWA icons |
| `/favicon.ico` | Browser favicon with multiple sizes |
| `../../../native/src-tauri/icons/tray-template.png` | Monochrome macOS menu-bar template |

The macOS tray uses a separate transparent template image, allowing AppKit to
adapt the mark automatically for light, dark, and pressed menu-bar states.

The app icon master and icon-matched logo preserve the conversation-approved
raster artwork. Runtime PWA, favicon, and native icon sizes are derived from the
high-resolution app icon master without redrawing it.

## Palette

- Midnight: `#071A1D`
- Emerald: `#2EDC91`
- Light-background emerald: `#159B65`
- Mist: `#DCE9E7`
