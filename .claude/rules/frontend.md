# Frontend Rules — React + Lattice CSS

The OpenMANETd frontend is a React 18 SPA built with Vite. The visual system is
**Lattice** — a tactical C2 aesthetic with near-black surfaces, a single cyan
accent with glow, monospace for data and sans-serif for prose, and sharp
corners (no border radius). All visual tokens live in
`frontend/src/styles/lattice.css`. App-shell primitives live in
`frontend/src/Layout.css`. Page and component CSS exists only for layout glue
that genuinely doesn't fit a primitive — never to redefine colors, typography,
borders, or spacing rhythm.

These rules are non-negotiable. If a design ask conflicts with Lattice, raise
it instead of silently inventing a new style.

---

## Required skills

Two skills MUST be invoked when working on the frontend. Do not skip them —
they encode patterns that are not duplicated in this rules file.

- **`react-best-practices`** — invoke whenever writing, reviewing, or
  refactoring any React code under `frontend/src/`. Vercel's 70-rule
  performance guide covers waterfalls, bundle size, re-render and rendering
  perf, and client/server data-fetching patterns. Resource-constrained
  target devices (MIPS routers, embedded ARM) make these rules non-optional
  here — performance regressions on the host show up immediately in the field.
- **`react-view-transitions`** — invoke whenever animating between UI states:
  route changes, panel enter/exit, list reorder, shared-element transitions,
  or anything mentioning `<ViewTransition>` / `startViewTransition` /
  transition types. This skill is the source of truth for animation in the
  app — Lattice deliberately avoids third-party animation libraries, so the
  browser's native View Transition API is the only sanctioned approach.
  Honor `prefers-reduced-motion` per Lattice; the skill aligns with that.

If a skill's guidance and these Lattice rules ever conflict (e.g. a perf
recommendation that would require a hard-coded color or a third-party
library), Lattice and the user's instructions win — raise the conflict
rather than silently breaking the design system.

---

## Core principles

1. **Lattice is the design system.** Reach for an existing primitive
   (`.lat-panel`, `.lat-btn`, `.lat-chip`, `.lat-table`, `.lat-field`, `.kv`,
   `.big-num`, `.pbar`, etc.) before writing any new CSS. If a primitive is
   close-but-not-quite, add a modifier class to `lattice.css` rather than
   forking the style in a page CSS file.
2. **Tokens, not literals.** Use the CSS variables defined in `:root`. Never
   hard-code hex colors, font stacks, or px values that duplicate a token.
3. **Mobile-first and field-usable.** Every page must be usable on a 360px
   wide phone in direct sunlight. Touch targets ≥ 44px tall. No horizontal
   page scroll. Test the breakpoint, do not guess.
4. **Sharp corners, no rounding.** `--radius` is `0`. The only round elements
   in the system are status dots, signal/meter pips, and the PTT ring.
5. **Restraint with motion and color.** Glow is reserved for live/active
   state. Color is reserved for status (`--ok`, `--warn`, `--crit`, `--accent`).
   Body copy is `--text` or `--muted`. No gradients other than the grid overlay.
6. **No external UI libraries.** No MUI, no Chakra, no Tailwind, no daisyUI,
   no shadcn. Lattice is the system.

---

## Design tokens (must use, never redefine)

| Token | Use for |
|-------|---------|
| `--bg`, `--bg-2` | Page background, deepest surface |
| `--surface`, `--surface-2`, `--surface-hi` | Panels, inputs, hover state |
| `--border` | Default 1px panel/input border |
| `--border-hi` | Active / focused / interactive border |
| `--border-subtle` | Hairline separators inside panels |
| `--text` | Primary text |
| `--muted` | Labels, secondary text, inactive state |
| `--dim` | Tertiary text, disabled, hairline ticks |
| `--accent`, `--accent-2` | Primary action, focus, live data, glow |
| `--ok` / `--warn` / `--crit` | Success / warning / critical status only |
| `--font-mono` | All data, labels, table content, headings, buttons |
| `--font-sans` | Long prose only (rare in this app) |
| `--radius` | Always `0` — never override |

Legacy aliases (`--green`, `--red`, `--yellow`, `--orange`, `--blue`) still
resolve, but **do not introduce new uses** — pick the semantic token instead
(`--ok`, `--crit`, `--warn`, `--accent`).

---

## Typography rules

- Default font is `--font-mono` at 13px (set on `body`). Inherit it; do not
  re-declare on every element.
- **Section labels and panel titles** use uppercase mono, letter-spaced
  (`text-transform: uppercase; letter-spacing: 0.18em`), in `--muted` at
  9–11px. Use `<h3>` inside `.lat-panel` and let `.lat-panel h3` style it.
- **Big numeric readouts** use `.big-num` (36px, `--accent`, with optional
  `.unit` child).
- **Body copy** is 11–13px mono. Avoid sans-serif except for genuine prose.
- **Never use `font-weight: bold` on body copy.** Weight 600/700 is reserved
  for primary buttons and the brand title.
- Letter-spacing > 0.08em is mandatory on any uppercase text. Lowercase text
  uses default tracking.

---

## Layout primitives

The app shell (`.layout-desktop`, `.layout-mobile`, `.sidebar`, `.bottom-tab-bar`,
`.lat-topbar`, `.lat-view-header`, `.lat-body`) is owned by `Layout.css` and
`Layout.jsx`. Pages should not redefine these classes.

**A page is composed of:**

```jsx
<>
  <div className="lat-topbar"> … node id + status chips … </div>
  <div className="lat-view-header">
    <div>
      <h2>Page Title</h2>
      <div className="crumb">Breadcrumb / context</div>
    </div>
    <div className="lat-view-toolbar"> … action buttons … </div>
  </div>
  <div className="lat-body grid-3">
    <div className="lat-panel"> … </div>
    <div className="lat-panel col-span-2"> … </div>
  </div>
</>
```

**Body grid options:** `.lat-body.grid-2x`, `.grid-3`, `.grid-4`. Panels can
span columns with `.col-span-2`, `.col-span-3`, `.col-span-all`. The grid
collapses to 2 columns at 900px and 1 column at 640px automatically — do not
duplicate that logic in page CSS.

**When a page needs a custom inner layout** (e.g. KPI rows, two-column panel
internals): write a small grid in the page CSS file, name it with a page
prefix (`.dashboard-kpi-row`, `.settings-grid`), and provide media queries
for 900px and 640px. Mirror the breakpoints used in `Layout.css`.

---

## Panels

`.lat-panel` is the only container for grouped content. Structure:

```jsx
<div className="lat-panel">
  <div className="panel-head">
    <h3>Panel Title</h3>
    <div className="actions">
      <button>refresh</button>
    </div>
  </div>
  <div className="kv"><span className="k">Field</span><span className="v">Value</span></div>
  …
</div>
```

- Always use a `<h3>` inside `.lat-panel` — the cascade styles it correctly.
- Use `.kv` rows for label/value pairs. Status uses `.v.ok|warn|crit|accent`.
- Use `.lat-table` inside a `.table-scroll` wrapper for scrollable tabular data.
- Loading and empty states are first-class:
  - **Loading:** muted, uppercase, mono, centered. Re-use the `.dashboard-loading`
    style only on the dashboard; for other pages use `<div className="lat-panel">Loading…</div>`
    or a page-local loading class.
  - **Empty:** short uppercase muted line: `No mesh peers.` Do not use icons
    or illustrations.
  - **Error:** use `.lat-alert.crit` with a one-line cause.

---

## Buttons and inputs

- Use `.lat-btn` for every clickable action. Modifiers: `.primary`, `.ghost`,
  `.danger`, `.danger.solid`.
- Use `.lat-input`, `.lat-textarea`, `.lat-select` for all form controls.
  Wrap each in `.lat-field` with a `<label>` so the uppercase muted label
  styling applies automatically.
- Use `.lat-toggle` for boolean switches (track + thumb). Toggle wrappers
  must be `<button>` elements with the `.on` class controlled by state, not
  `<input type="checkbox">`.
- Use `input[type="range"].lat-slider` for sliders.
- **Never style a raw `<button>` or `<input>` per-page.** If a primitive is
  missing a variant, add the variant to `lattice.css` and reuse it everywhere.
- Touch targets: buttons are min 32px tall by default. On primary actions
  used on mobile, raise to 44px via a page-local rule that bumps padding —
  do not change the primitive.

---

## Status indicators

| Use | Primitive |
|-----|-----------|
| Inline status pill in a topbar | `.lat-chip` (+ `.ok` / `.warn` / `.crit`) |
| Tiny status dot inside a row | `.dot-i` (+ `.ok` / `.warn` / `.crit`) |
| Numeric readout with status color | `.big-num` (+ `.ok` / `.warn` / `.crit`) |
| Bar gauge (CPU, mem, signal pct) | `.pbar` row with `.pbar-label` / `.pbar-val` |
| 5-bar signal indicator | `.sig-bars` with per-pip `.on` / `.warn` |
| Audio level meter | `.meter-row` with per-segment `.on` / `.hot` / `.peak` |
| Sparkline (in-panel history) | `.spark` (with optional `.warn`) |
| One-line callout | `.lat-alert` (+ `.ok` / `.warn` / `.crit`) |

Color semantics are fixed and **must not be remapped per-page**:

- `--ok` → success, healthy, connected, link-up
- `--warn` → degraded but functional, partial data, retrying
- `--crit` → failure, disconnected, error condition
- `--accent` → live/active/selected — never used to mean "good"

---

## React component conventions

- **Functional components only.** No class components.
- **Named exports for utilities, default export for the page/component.**
  `export default function Dashboard() {…}`.
- **Hooks at the top of the body**, in this order: `useState`, `useRef`,
  custom hooks (`useMeshStatus` etc.), `useMemo`, `useCallback`, `useEffect`.
- **Polling uses `useVisibleInterval`** — never `setInterval` directly. This
  pauses polling when the tab is hidden and is required for battery life on
  field devices.
- **Subscribe to shared real-time state via the `useMeshStatus`,
  `useMeshTopology`, `useGnssStatus` hooks** rather than each page polling
  its own copy.
- **Memoize derived data** with `useMemo` when the input is a polled object
  and the derivation is non-trivial (sorting, filtering, formatting tables).
- **Keep effects narrow.** One effect per concern (subscribe, poll, derive).
- **Cleanup in every effect that subscribes or starts a timer.** Return the
  unsubscribe / clearInterval function.
- **No prop drilling for global state** — use the `contexts/` providers
  (`useAuth`, etc.) or a service module under `services/`.
- **Do not put logic inside JSX expressions.** Compute it above the return,
  bind to a const.
- **Component file = component name** (`PttButton.jsx` exports `PttButton`).
  Co-located CSS file uses the same base name (`PttButton.css`).

---

## File organization

```
frontend/src/
  pages/         One folder-less file per route, plus page-local CSS.
  components/    Reusable widgets shared by 2+ pages.
  hooks/         Custom React hooks (no JSX).
  services/      Transport, stores, websocket, audio engine. No JSX.
  contexts/      React contexts and their providers.
  utils/         Pure helpers, no React.
  gen/           AUTO-GENERATED protobuf clients — never edit.
  styles/        lattice.css and other shared stylesheets.
  __tests__/     Vitest tests, mirrors the src/ tree.
```

- A widget used by exactly one page lives next to (or inside) that page,
  not in `components/`. Promote to `components/` only when a second consumer
  appears.
- Generated code under `gen/` is read-only — regenerate with `make buf`.

---

## Page-local CSS rules

A page CSS file is allowed only for:

1. The page-specific grid that arranges panels.
2. A handful of page-local utility classes (e.g. `.dashboard-kpi-row`,
   `.settings-grid`, `.power-selector`) that compose Lattice primitives.
3. Responsive breakpoints at 900px and 640px that match the body grid.

A page CSS file **must not**:

- Redeclare colors, fonts, font sizes outside the token system.
- Restyle `.lat-panel`, `.lat-btn`, `.lat-chip`, `.lat-table`, `.kv`, etc.
  (no descendant selectors that override Lattice primitives).
- Introduce border-radius, drop shadows, or gradients.
- Use `!important` except in a `prefers-reduced-motion` block.

If you find yourself fighting a primitive, the fix is to extend `lattice.css`,
not to override it locally.

---

## Responsive rules

- Breakpoints: **900px** (desktop → tablet, 4-col grid drops to 2-col) and
  **640px** (tablet → phone, all grids collapse to 1-col, padding tightens).
- The shell switches to mobile bottom-tab layout below **768px**, controlled
  in `Layout.jsx`.
- Tables that exceed available width must be wrapped in `.table-scroll`.
- `lat-topbar .chips` switches to horizontal scroll under 640px — use that
  pattern for any other chip-row layout.
- Test on a 360×640 viewport before declaring a page complete. The frontend
  is used in the field on phones; desktop-only is a regression.

---

## Accessibility

- All interactive elements must be `<button>`, `<a>`, or have `role` set.
  Do not attach `onClick` to `<div>`.
- Form controls must have a `<label>` (preferred) or `aria-label`.
- Focus styles are provided by Lattice (`:focus-visible` rings on inputs and
  buttons). Do not remove them.
- Provide `aria-checked`, `aria-pressed`, `role="radiogroup"` etc. on custom
  toggle UIs (see `PowerSelector` in `SettingsWireless.jsx` for the pattern).
- Color is never the only signal — pair `--crit` with a "FAIL" label, not a
  red dot alone.
- Honor `prefers-reduced-motion` — Lattice already disables glow and
  transitions in that media query; do not re-enable them locally.

---

## Testing

Defer to `.claude/rules/testing.md` for test infrastructure. Frontend-specific
points:

- Co-locate tests under `frontend/src/__tests__/` mirroring the source tree.
- Use Vitest + jsdom + `@testing-library/react`.
- Use `createRouterTransport` from `@connectrpc/connect` for ConnectRPC
  service tests instead of mocking HTTP.
- Visual / layout regressions are not auto-checked — manually verify on a
  mobile viewport (360px wide) and a desktop viewport before completion.
- Run `make lint-frontend` and `make test-frontend` before declaring frontend
  work done.

---

## Build and dev workflow

- Vite hot-reload dev server (against a remote daemon):
  ```bash
  cd frontend && VITE_API_TARGET=http://<host>:8081 pnpm run dev
  ```
- Edits under `frontend/src/` are **not** served by the embedded daemon
  binary until you run `make frontend` — `static/` is embedded at compile
  time.
- Before committing frontend work that the user will exercise via the
  daemon binary: `make frontend && make lint-frontend && make test-frontend`.

---

## Code-review checklist

When writing or reviewing frontend code, verify:

1. Every visual element uses a Lattice primitive or a token-based composition;
   no hard-coded colors, no `border-radius`, no third-party UI library.
2. Page CSS contains only layout and page-local composition — no overrides
   of primitive selectors.
3. The page renders correctly at 360px wide; touch targets are ≥ 44px on
   primary mobile actions.
4. Polling uses `useVisibleInterval`; effects clean up; goroutine-equivalent
   leaks (subscriptions, timers, websockets) all unsubscribe.
5. Loading, empty, and error states are present and Lattice-styled.
6. Status colors follow semantics (`--ok`/`--warn`/`--crit`/`--accent`).
7. Generated code under `frontend/src/gen/` was not edited by hand.
8. `make frontend` was re-run if the changes need to ship through the
   daemon binary.
9. The `react-best-practices` skill was consulted for any non-trivial React
   change, and `react-view-transitions` was consulted whenever animation
   between UI states was added or modified.
