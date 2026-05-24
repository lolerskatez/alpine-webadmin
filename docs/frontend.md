# Alpine WebAdmin — Frontend System

## Overview

The frontend is a single-page administrative interface built with **Alpine.js** (no React, no heavy SPA framework). All assets are embedded into the Go binary via `embed.FS`. The design is **dark-mode by default** with a dense operational layout inspired by Proxmox, pfSense, and OpenWrt LuCI.

## Architecture

```
cmd/webadmin/main.go
    └── internal/frontend/embed.go
            └── assets/
                ├── index.html   (830 lines, ~36KB)
                ├── app.css      (~18KB, ~4KB gzipped)
                ├── app.js       (~15KB, ~4KB gzipped)
                └── alpine.min.js (~39KB from CDN)
```

| Asset | Size (raw) | Size (gzipped) | Purpose |
|-------|-----------|----------------|---------|
| `index.html` | ~36KB | ~6KB | Structure, all 9 feature sections |
| `app.css` | ~18KB | ~4KB | Dark-mode CSS, dense UI, responsive |
| `app.js` | ~15KB | ~4KB | Alpine.js data component, API handlers |
| `alpine.min.js` | ~39KB | ~13KB | Alpine.js 3.14.3 runtime |
| **Total** | **~108KB** | **~27KB** | |

CSS is well under the 150KB target.

## File Structure

```
internal/frontend/
├── embed.go              # Go embed directive
└── assets/
    ├── index.html         # Single HTML template with all sections
    ├── app.css            # Dark-mode-first CSS architecture
    ├── app.js             # Alpine.js app() component
    └── alpine.min.js      # Alpine.js runtime (fetched at build time)
```

## Features

| Feature | Section | Data Source |
|---------|---------|-------------|
| Real-time dashboard | Dashboard | WebSocket telemetry |
| Service controls | Services | `/api/services` + IPC |
| Log streaming | Logs | SSE `/api/logs` |
| User management | Users | `/api/users` + IPC |
| Storage management | Storage | `/api/storage` |
| Network editor | Network | `/api/network` |
| Package management | Packages | `/api/packages/*` + IPC |
| System alerts | Alerts banner | Client-side |
| Session management | Sessions | `/api/sessions` |

## CSS Architecture

### Design Principles

1. **Dark-mode default** — All colors defined via CSS custom properties on `:root`
2. **Dense information density** — 13px base font, compact tables, minimal padding
3. **CSS variables for theming** — Single source of truth for colors, spacing, radii
4. **No utility classes** — Semantic component classes keep CSS small and cacheable
5. **Reduced motion support** — `prefers-reduced-motion: reduce` disables animations

### Color Palette

```css
--bg-body:       #0b1121;
--bg-sidebar:    #0f172a;
--bg-panel:      #1e293b;
--bg-hover:      #334155;
--bg-selected:   #2563eb;
--text-primary:  #f1f5f9;
--text-secondary:#94a3b8;
--text-muted:    #64748b;
--accent:        #3b82f6;
--success:       #22c55e;
--warning:       #eab308;
--danger:        #ef4444;
--info:          #06b6d4;
```

### Responsive Breakpoints

| Breakpoint | Behavior |
|------------|----------|
| `> 768px` | Fixed sidebar (200px), full table layouts |
| `<= 768px` | Slide-out sidebar, 2-column metrics, stacked toolbars |
| `<= 480px` | Single-column metrics, vertical action buttons |

### Accessibility

- **Skip link** — "Skip to main content" for keyboard users
- **Focus visible** — `outline: 2px solid var(--accent)` on all interactive elements
- **ARIA roles** — `role="table"`, `role="row"`, `role="cell"`, `role="alert"`, `role="dialog"`, `role="log"`
- **ARIA current** — `aria-current="page"` on active nav item
- **ARIA live** — Log container uses `aria-live="polite"` for progress updates
- **ARIA labels** — All action buttons have descriptive `aria-label`
- **Keyboard nav** — `Escape` closes sidebar and modals
- **High contrast** — `@media (prefers-contrast: more)` increases border widths
- **Reduced motion** — Respects `prefers-reduced-motion`

## Alpine.js Component Design

### Single Root Component

```js
function app() {
    return {
        // State
        authenticated: false,
        tab: 'dashboard',
        telemetry: {},
        services: [],
        packages: [],
        // ...

        // Lifecycle
        initApp() { /* check session */ },
        cleanup() { /* close WS, SSE, timers */ },

        // Navigation
        switchTab(name) { /* load data, focus main */ },

        // API methods per section
        loadServices() { /* fetch + render */ },
        serviceAction(name, action) { /* POST + refresh */ },

        // Real-time
        initWS() { /* WebSocket with auto-reconnect */ },
        startLogs() { /* EventSource SSE */ },

        // Utilities
        showAlert(msg, level) { /* toast system */ },
        formatKB(kb) { /* human-readable */ },
    };
}
```

### State Isolation

All state lives in the single `app()` component. There are no child components — this avoids Alpine.js's inter-component communication overhead and keeps the reactive tree shallow.

### DOM Efficiency

- `x-show` toggles visibility without destroying/recreating DOM
- `x-for` uses `:key` for stable list rendering
- Tables use sticky headers to avoid layout shifts
- Progress logs cap at 500 lines to prevent memory growth

## WebSocket Handling

```
WebSocket /ws
    ├── onmessage: telemetry JSON
    │       └── updates this.telemetry
    ├── onclose: auto-reconnect after 3s
    │       └── only if still authenticated
    └── rate: 2s intervals from telemetry.Collector
```

The WebSocket bridges Go's `telemetry.Hub` to Alpine's reactive state. Only telemetry data flows over WebSocket; all other interactions use standard HTTP fetch.

## SSE Log Streaming

```
EventSource /api/logs
    ├── streams dmesg -w output
    ├── client caps at 500 lines
    └── toggle on/off via UI button
```

## Mobile / Emergency Fallback

### Mobile Layout (< 768px)

- Sidebar becomes a slide-out drawer with overlay
- Top bar shows hamburger menu + title + logout
- Metrics collapse to 2 columns, then 1 column
- Action buttons stack vertically in tables
- Search inputs fill full width

### Print Fallback

```css
@media print {
    .sidebar, .mobile-header, .toolbar { display: none !important; }
    .main { margin-left: 0; }
    body { background: #fff; color: #000; }
}
```

Useful for emergency documentation export when remote access is degraded.

## Performance Optimizations

| Technique | Benefit |
|-----------|---------|
| **Alpine.js over React** | No VDOM diff, direct DOM mutations, ~39KB vs ~130KB+ |
| **Single `app()` component** | Shallow reactive tree, no prop drilling |
| **CSS custom properties** | No runtime theming JS, GPU-composited transitions |
| **Sticky table headers** | Eliminates repeated layout calculations |
| **Log line cap (500)** | Prevents unbounded memory growth |
| **WebSocket reconnect** | Exponential backoff not needed — fixed 3s delay |
| **Tab lazy-loading** | Data fetched only when section visited |
| **Package op polling** | 2s interval, stops when op completes |
| **CSS transitions only** | No JS animation frames for simple fades |
| **Embedded assets** | No external CDN dependencies, faster cold start |

### Bundle Size Analysis

| Asset | Raw | Gzipped |
|-------|-----|---------|
| HTML | 36KB | 6KB |
| CSS | 18KB | 4KB |
| app.js | 15KB | 4KB |
| alpine.min.js | 39KB | 13KB |
| **Total** | **108KB** | **27KB** |

First paint: ~27KB transferred. With HTTP/2 server push (if enabled), all assets load in parallel.

## Security

- **CSP** — `Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'`
- **No inline scripts** — All JS in `app.js`
- **CSRF tokens** — All mutating requests include `X-CSRF-Token` header
- **Session cookie** — `__Host-SID` with `SameSite=Lax` (set by backend)
- **No eval** — Alpine.js doesn't use `eval()`

## Build Instructions

```bash
# Download Alpine.js runtime (first build only)
make deps

# Build everything
make build

# Or manually:
curl -fsSL -o internal/frontend/assets/alpine.min.js \
  https://cdn.jsdelivr.net/npm/alpinejs@3.14.3/dist/cdn.min.js
CGO_ENABLED=0 go build -o bin/webadmin ./cmd/webadmin
```

## Known Lint Warning

```
'meta[name=theme-color]' is not supported by Firefox
```

This is a **progressive enhancement** for mobile browser chrome theming (Chrome, Edge, Safari). Firefox ignores it harmlessly. No functional impact.

## Future Enhancements

- **Light mode toggle** — Add `data-theme="light"` override, swap CSS variable values
- **Keyboard shortcuts** — `?` for help, `g` + `d` for dashboard, etc.
- **Service dependency graph** — Visualize OpenRC dependencies
- **Network topology** — Interface link-state diagram
- **i18n** — Extract strings to JSON locale files
