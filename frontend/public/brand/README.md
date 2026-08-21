# ccplant brand assets

The mark combines a sprout with branching AI-agent session routes. All final
artwork uses flat colors—there are no gradients, glows, sparkles, or shadows.

| Asset | Use |
| --- | --- |
| `ccplant-mark.svg` | Standalone mark on dark surfaces |
| `ccplant-app-icon.svg` | Scalable app-icon master |
| `ccplant-logo.svg` | Horizontal wordmark on dark surfaces |
| `ccplant-logo-light.svg` | Horizontal wordmark on light surfaces |
| `ccplant-logo-approved.png` | High-resolution transparent logo candidate approved in chat |
| `ccplant-logo-icon-matched.png` | Transparent logo using the approved app icon mark |
| `ccplant-app-icon-approved.png` | High-resolution no-sparkle app icon candidate approved in chat |
| `/icon-{192,256,384,512}x*.png` | Generated PWA icons |
| `/favicon.ico` | Browser favicon with multiple sizes |
| `../../../native/src-tauri/icons/tray-template.svg` | Monochrome macOS menu-bar master |

The macOS tray uses a separate transparent template image, allowing AppKit to
adapt the mark automatically for light, dark, and pressed menu-bar states.

The approved and icon-matched PNG files are review candidates and are not wired
into the applications yet. They preserve the conversation-approved raster
artwork for GitHub review before replacing the current runtime assets.

## Palette

- Midnight: `#071A1D`
- Emerald: `#2EDC91`
- Light-background emerald: `#159B65`
- Mist: `#DCE9E7`
