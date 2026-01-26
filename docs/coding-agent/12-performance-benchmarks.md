# Performance Benchmarks (Draft)

## Goal
Compare renderer options (Bubble Tea vs tcell) under identical workloads to quantify:
- Render time per frame
- Input latency
- CPU usage
- Memory usage (RSS + Go heap)

## Metrics
- `render_ms_avg`, `render_ms_p95`, `render_ms_stddev`
- `draw_ms_avg`, `draw_ms_p95`, `draw_ms_stddev`
- `fps_avg`
- `frame_drops` (count of frames > 16ms)
- `input_latency_ms_avg`, `input_latency_ms_p95`
- `rss_mb_peak`
- `heap_mb_peak`
- `gc_pause_ms_avg`, `gc_pause_ms_p95`
- `idle_cpu_percent`

## Workloads
1) **Large transcript**
   - 1k / 5k / 10k message lines
   - static (no streaming)

2) **Streaming output**
   - append N lines at 20–50 Hz for 30s

3) **Tool expansion**
   - toggle 50 tool blocks (expand/collapse)

4) **Mixed**
   - stream output while scrolling and editing input

5) **Scroll stress**
   - continuous scroll while streaming output

6) **Long-line wrapping**
   - wide code blocks + ANSI color; wrap to viewport width

7) **Large tool output**
   - 1–5 MB streamed in chunks

8) **Paste latency**
   - simulate 100-line paste into editor

9) **Idle baseline**
   - no updates for 30s

## Benchmark Harness Design

### Common components
- **Workload generator**: emits synthetic events (new message, tool chunk, toggle, scroll)
- **Event bus**: feeds UI in both implementations
- **Metrics collector**: records timestamps and memory

### Implementation outline (Go)
- Create `internal/bench` with:
  - `workload.go`: deterministic event stream (seeded RNG)
  - `metrics.go`: record time, p95, stddev, RSS, heap, GC pauses
  - `replay.go`: play events at fixed rates

### Renderer adapters
- `bench_bubbletea.go`: minimal Bubble Tea UI
- `bench_tcell.go`: minimal tcell UI

Both renderers should expose:
```go
type Renderer interface {
  Init(width, height int)
  Apply(evt BenchEvent)          // updates view model
  Render()                       // draw to terminal buffer
  MeasureRender() time.Duration  // record time
}
```

## Measuring Memory
- Use `runtime.ReadMemStats` for heap
- Use OS-specific RSS:
  - Linux: read `/proc/self/status` and parse `VmRSS`

## Measuring Input Latency
- Inject a synthetic input event
- Record time until view reflects the change

## Measuring GC pauses
- Use `debug.ReadGCStats` to capture pauses during runs

## Output Format
- Emit JSON summary to stdout, one record per run
```json
{
  "renderer": "tcell",
  "workload": "stream_20hz",
  "render_ms_avg": 3.2,
  "render_ms_p95": 5.8,
  "render_ms_stddev": 0.9,
  "draw_ms_avg": 1.4,
  "draw_ms_p95": 2.7,
  "frame_drops": 12,
  "fps_avg": 58,
  "rss_mb_peak": 42.1,
  "heap_mb_peak": 18.3,
  "gc_pause_ms_avg": 0.4,
  "gc_pause_ms_p95": 1.2,
  "input_latency_ms_avg": 12.4,
  "input_latency_ms_p95": 21.7,
  "idle_cpu_percent": 0.8
}
```

## Implementation Steps (later)
1) Build workload generator + metrics collector.
2) Implement Bubble Tea adapter with minimal view.
3) Implement tcell adapter with minimal view.
4) Run each workload for fixed duration (e.g., 30s).
5) Compare metrics; decide baseline renderer.
