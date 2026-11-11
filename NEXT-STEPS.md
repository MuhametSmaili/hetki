# Streaming finder: next steps

## Current checkpoint

Branch: `wip/streaming-finder`. This WIP checkpoint contains six implementation slices.

Implemented:
- Incremental NDJSON decoding, an immutable search index and compact list rows.
- A serial, cancellable worker with latest-query coalescing and stale-result rejection.
- Cached scope membership and item projections between query edits.
- Background snapshot replacement, including status projection and selection recovery.

Measured at 100k items using temporary limit overrides:
- Dense tracked-scope typing: **53.5 → ~1.2 ms p95** after caching.
- Replacement: **37–63 ms synchronous work → ~1 ms submission and 0.7–1.1 ms installation**.
- Replacement preparation still takes **29–250 ms in the background**.
- All 288 measured replacement UI operations across 72 final runs stayed below **1.6 ms**, including rendering.

Full checks, affected race tests and targeted PTY tests passed. These are local measurements, not cross-machine guarantees. Production limits remain **10k items, 4 MiB provider stdout and 64 KiB record framing**. Dense-scope peak memory still exceeds the provisional 256 MiB target at 100k.

## Next objective: show searchable items while loading

Open the picker immediately, publish incoming items in bounded batches, and increase the count as data arrives—similar to fzf. Today, decoding streams but selectable choices publish only after successful provider completion. The action-busy state also gates typing/navigation during loading and replacement.

### 1. Decide partial-source behavior

Before implementation, settle these observable rules:
- Can users select an item before the provider finishes? The intended interactive experience needs this.
- What happens to visible partial results if the provider fails, emits an invalid record or exceeds a limit?
- Does refresh keep the old dataset visible until the first valid new batch?

Keep record validation and duplicate-ID detection incremental. Define whether accepted partial results remain actionable after a failure; do not silently present them as a successfully completed source.

### 2. Add a streaming publication path

- Extend source/flow delivery beyond the current complete-list return value.
- Give each load and screen an identity so late batches cannot enter another screen or refresh.
- Retain validated data in immutable batches/chunks. Avoid copying, validating or rebuilding the entire accumulated dataset on every append.
- Keep queued work bounded and coalesce publication updates by item count/time. Frequent arrivals must not starve completion of the user's current query.
- Report received-item and loading/completion state accurately; distinguish the searchable revision if it trails ingestion.

### 3. Make loading interactive

- Separate provider-loading state from mutation/action-busy state so typing and navigation remain available.
- Search the published prefix in the existing worker and preserve selection by stable item ID as new results arrive.
- Validate selection against the flow's currently published items, not an obsolete rendered row.
- On selection, back, refresh or quit, cancel obsolete loading and join its owned process/work before conflicting flow transitions.
- Retain independent stale-batch and stale-search rejection even when cancellation races completion.

### 4. Verify and measure before raising limits

- Cover slow/chunked sources, rapid typing during ingestion, early selection, refresh/back/quit, duplicate or malformed records, late provider failure and limit overflow.
- Measure time to first visible/searchable items, publication UI cost, query latency during sustained ingestion, cancellation, cumulative allocations and peak memory with overlapping generations.
- Reuse the 10k/50k/100k profiles and the **under-16-ms UI target**. Exercise the actual publication path, with temporary overrides for larger datasets.
- Check total work across the full load: small individual batches can still create quadratic rebuilding or excessive CPU usage.
- Run `make check`, affected race tests and targeted PTY tests. Review this slice before continuing.

## Further measured follow-ups

- Actual status/scope changes still reconstruct data synchronously; query-only edits reuse cached data.
- Unicode/long-field matching and peak memory remain capacity constraints.
- Raise limits only after the relevant workloads pass. A larger provider stdout budget should be independent of unrelated command capture.
- Parallel search batches remain deferred until measurements justify their additional CPU/memory and lifecycle complexity.

## Starting points

- `internal/app/actions/source_stream.go`, `source.go`, `flow.go`: decoding, source completion and published choice ownership.
- `internal/app/tui/action_flow.go`: action-screen projection and application delivery.
- `internal/tui/replacement.go`, `search_worker.go`, `search.go`: preparation, worker ownership and revision acceptance.
- `internal/tui/choice.go`, `update.go`: screen transitions and busy-state input gating.
- `internal/tui/list/preparation.go`, `search.go`, `filter.go`: immutable preparation, search and projection reuse.

Detailed design and measurement records:
- `/Users/muhametsmaili/.vaults/personal/ai/Projects/hetki/plans/search-module-and-scaling.md`
- `/Users/muhametsmaili/.vaults/personal/ai/Projects/hetki/research/replacement-preparation-2026-11-11.md`

Last updated: 2026-11-11.
