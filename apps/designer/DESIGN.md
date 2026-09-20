# Console design system

KNOTT uses a graphite navigation shell, warm neutral work surfaces, and an emerald action color. Semantic colors are reserved for execution state, warnings, and node types.

- `src/styles/console.css` owns theme tokens, shared component styling, page layouts, responsive rules, and reduced-motion behavior.
- `src/index.css` supplies structural component styles; previous competing theme layers have been removed.
- `src/components/Layout.jsx` owns grouped navigation, workspace chrome, mobile navigation, keyboard search, badges, and notifications.
- Overview and workflow registry use dedicated semantic layouts. Other operational views share tables, cards, form controls, and split panes.

## Interaction conventions

Primary buttons advance the main workflow. Secondary actions use bordered neutral buttons; tertiary actions use transparent buttons. Search opens with Ctrl/Cmd+K, supports Tab/Enter, and closes with Escape. The active navigation item exposes `aria-current`. Focus rings remain visible and reduced-motion preferences disable animation.

At phone sizes, navigation becomes a drawer, metrics use two columns, registry cards use one column, and operational panes stack. The workflow studio uses an Add step button in place of its palette and places selected-node properties beneath the canvas. Wide data tables scroll within their container.

## Data states

Dashboard metrics use real API responses. Connection failures display an explicit error with retry; previously loaded values remain visible and are marked as stale. Empty states guide the operator to a useful next action. No production sample statistics are introduced.

## Verification

Run `npm test` and `npm run build` in `apps/designer`. Navigation and dashboard tests cover keyboard routing, drawer dismissal, server errors, retry, and workflow creation routing. Browser checks use a separate disposable backend state directory; seeded examples and verification runs are not production data.
