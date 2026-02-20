# Renderer Options (Go)

## Bubble Tea + Lipgloss
- Rendering: full view redraw each tick (alternate screen).
- Flicker: typically minimal; issues mainly from slow render or external stdout.
- Pros: fastest to build, rich ecosystem.
- Cons: no granular diffing; heavy UIs can stutter.

## tcell (low-level)
- Rendering: direct cell buffer updates with diffing.
- Flicker: excellent; you control repaint granularity.
- Pros: lowest overhead, most control, best for perf.
- Cons: higher engineering effort (build your own components/layout).

## tview (on tcell)
- Rendering: tcell under the hood; widget-based.
- Flicker: good.
- Pros: higher-level widgets, faster to scaffold.
- Cons: less flexible for custom chat layouts.

## pi-mono TUI (@mariozechner/pi-tui)
- Language: TypeScript/Node.
- Rendering: differential rendering + synchronized output (CSI 2026).
- Flicker: explicitly designed to be flicker-free.
- Tradeoff: Node runtime overhead, custom renderer maintenance.

## Recommendation
- For final build: tcell (best control + performance).
- For rapid validation: Bubble Tea (prototype) and re-evaluate.
