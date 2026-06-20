Exit code: 0
Wall time: 0.3 seconds
Output:
# CodeRenga Implementation Status

The v0.8 implementation is organized into the requested phases:

1. CLI skeleton, explicit initialization, one-shot input, and REPL.
2. External configuration, prompts, project instructions, and user modes.
3. OpenAI-compatible streaming/non-streaming client and profile/model switching.
4. SQLite migrations, sessions, messages, execution history, audit, cache, summaries, and no-persist.
5. Tool Registry, strict JSON Tool Call parser, built-in filesystem tools, cwd/symlink sandbox, and dry-run.
6. Segmented shell policy, maximum-risk aggregation, Git tools, and SQLite shell history.
7. MCP stdio and HTTP/SSE clients, initialization, discovery, namespacing, cache, policy, and audit.
8. tools.json and plugin-directory loading, JSON stdin/stdout, policy, enable/disable/reload, and hard-sandbox refusal.
9. Active summaries, recent uncompacted messages, manual/automatic compaction, and raw-message retention.

Implementation verification uses `scripts/fmt.ps1`, `scripts/lint.ps1`, `scripts/test.ps1`, and `scripts/build.ps1`.


