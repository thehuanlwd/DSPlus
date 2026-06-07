# DSPlus Analysis Service Design

## Goal

Add an analysis module to DSPlus that groups low-level proxy logs into task-level sessions, summarizes repeated request/response data, and exports a single Markdown report for expert review. The module runs inside the DSPlus process but stays logically separate from proxy forwarding, so analysis failures should not affect API traffic.

## Confirmed Scope

- The analysis service starts with DSPlus; no separate process in the first version.
- Persistence is supported but disabled by default for privacy.
- Markdown is the only export format in the first version.
- Session grouping is automatic-first.
- No model calls are made for analysis; all summaries are deterministic.
- Existing request log UI remains available; a new analysis view is added.

## Architecture

Proxy handling emits structured `TraceEvent` records in addition to existing `LogEntry` records. `Logger` continues to power the live dashboard and per-request details. A new `AnalysisService` consumes `TraceEvent` values asynchronously, maintains an in-memory index of sessions, optionally writes JSONL records, and exposes `/api/analysis/*` endpoints.

The proxy path should never block on analysis persistence. If the analysis channel is full or disk writes fail, DSPlus records a warning and continues forwarding traffic.

## Data Model

`TraceEvent` is the durable fact layer. It contains event id, time, log id, inferred session id, turn id, phase (`primary`, `analyzer`, `retry`, `debug`), format, route, status, latency, model, upstream, request metadata, response metadata, and raw payload references.

`RequestMeta` stores model, stream, thinking mode, reasoning effort, max tokens, message counts, role counts, last user message summary, system prompt transformation state, and extra prompt injection state.

`ResponseMeta` stores finish/stop reason, token usage, cache hit/miss, reasoning/content sizes, Anti-Loop trigger state, analyzer judgment, and retry model.

`ConversationSession` groups events into task-level sessions. It tracks time range, request count, models, token totals, cache totals, turns, errors, retries, and the reason for grouping.

`SessionSummary` is the compact UI/export representation derived from a `ConversationSession`.

## Session Inference

Strong signals win first: explicit `conversation_id`, `session_id`, `thread_id`, `chat_id`, or `metadata.session_id`. If absent, OpenAI requests use a stable context fingerprint from normalized message roles and content summaries. Anthropic requests use system summary, message roles, and latest user summary. Anti-Loop analyzer and retry events inherit the parent request session and turn.

Weak fallback uses client/source, path, model, time proximity, and context continuity. The default session gap is 30 minutes. Each primary request creates a new turn; related analyzer/retry/debug events share that turn.

## Persistence

Config defaults are privacy-preserving:

- `analysis_enabled`: `true` for in-memory analysis.
- `analysis_persistence`: `false`.
- `analysis_persist_raw_bodies`: `false`.
- `analysis_retention_days`: `7`.
- `analysis_session_gap_min`: `30`.

When persistence is enabled, JSONL files are written under `analysis_logs/YYYY-MM-DD.jsonl`. With raw body persistence disabled, events store summaries, hashes, and truncated previews only. Full request/response bodies are stored only when the explicit raw body option is enabled.

## API

- `GET /api/analysis/status`
- `GET /api/analysis/sessions?limit=50&offset=0`
- `GET /api/analysis/sessions/{id}`
- `GET /api/analysis/sessions/{id}/events`
- `GET /api/analysis/sessions/{id}/export.md`

The existing `/api/logs` endpoints are unchanged.

## UI

Add an `分析` tab. The tab shows a task/session list, selected session summary, turn timeline, key parameter snapshots, Anti-Loop phases, and event rows. Each event can expand to show raw original request, transformed request, and response when available. A top-level button downloads the Markdown export.

The settings page adds privacy-focused analysis options with clear copy warning that raw request/response persistence may include sensitive business content.

## Markdown Export

The report uses this structure:

- Summary: session id, time range, models, request count, tokens, cache, errors, retries.
- Task Context: chronological user/system context summary.
- Timeline: primary request, transform summary, response result, token usage, Anti-Loop analyzer/retry.
- Key Parameters: deduplicated snapshots for model, stream, thinking, reasoning effort, max tokens, prompt placement, and extra prompt placement.
- Reasoning Trace: captured reasoning snippets or a note that reasoning was not recorded.
- Diagnosis Evidence: factual failures, repeated requests, length/max token endings, retries, 502/network errors, and scanner errors.
- Raw Payload Appendix: event-numbered original request, transformed request, and response body when captured.

## Testing

Add focused Go tests for metadata extraction, session inference, JSONL write behavior with raw bodies disabled, Markdown export with and without raw payloads, and API handler responses. Existing proxy and logging tests should continue to pass.

## Non-Goals

- No separate analysis process.
- No SQLite database in the first version.
- No AI-generated diagnosis in the first version.
- No ZIP export in the first version.
- No automatic cleanup implementation beyond retention configuration and display.
