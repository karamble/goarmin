# charts

goarmin's chart library on Apache ECharts. Self-contained: it imports only `react`, `echarts`,
`zrender` and its own files (a guard test enforces it), so it can move to its own package.

- `core/`: ECharts registration (tree-shaken), the `useECharts` hook (lazy init, resize,
  group connect, reduced motion, dispose) and the `<Chart>` component.
- `theme/`: the micavoss theme, built from tokens passed in as data.
- `builders/`: pure functions `(input, ctx) → EChartsOption`. All logic lives here and is
  unit-tested without a canvas. Colour by role; at most one series is lime.
- `svg/`: rings and arcs drawn as SVG (no canvas): crisp, cheap, many per page.
