# Renderer Decision Notes

## Summary
We will build the TUI renderer on tcell to maximize performance and learn the low-level trade-offs.
Bubble Tea is a potential early prototype option, but the final architecture should target tcell.

## Why tcell
- Cell-buffer diffing gives the best flicker-free rendering under load.
- Lowest memory overhead (no large view strings per frame).
- Full control over layout, input, and redraw timing.
- Aligns with the goal of a fast, low-memory Go TUI.

## Trade-offs vs tview
- tview is easier to scaffold but constrains layout to widget patterns.
- tview adds state and layout overhead; less flexible for chat-style UI.
- Using tcell directly enables custom rendering for streaming tool output and large transcripts.

## Trade-offs vs Bubble Tea
- Bubble Tea redraws the whole view each tick; can stutter on heavy content.
- tcell lets us do granular redraws and optimize hot paths.
- tcell is more engineering effort but aligns with performance goals.

## Next steps
- Use the benchmark plan (see `12-performance-benchmarks.md`) once a minimal tcell UI exists.
- Prototype a minimal tcell render loop with:
  - message list
  - input box
  - status line
- Document any lessons about input handling, layout, and redraw diffing.
