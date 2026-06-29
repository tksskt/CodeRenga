# CodeRenga

[English](README.md) | [日本語](README.ja.md)

CodeRenga is a lightweight Go CLI coding agent implemented from the v0.8 design in `docs/`.

## Name and concept

CodeRenga is named after the Japanese collaborative linked-verse tradition, **renga**. A renga is not completed by one poet: each participant receives the preceding verse, preserves its context, and contributes the next verse.

CodeRenga applies the same idea to software development. A cloud LLM considers architecture and direction, a local LLM receives that intent and implements it, and tools connect the diff, execution, and verification steps. Rather than assigning everything to one AI, CodeRenga links multiple intelligences and execution environments to shape code one verse at a time.

**Cloud LLM thinks. Local LLM builds. CodeRenga links the verses.**

## Development

Go 1.26.4 is used locally and `go.mod` declares `go 1.25.0`. The scripts prefer `.local/go/bin` and keep module and build caches under `.local/cache/`. If PowerShell is unavailable, `make` uses `scripts/local-go.sh`, which downloads Go into `.local/go` and keeps `GOMODCACHE`, `GOCACHE`, `GOPATH`, and `GOBIN` inside `.local/`.

```powershell
make setup
make fmt
make lint
make test
make build
```

The binary is written to `.local/bin/coderenga.exe` on Windows and `.local/bin/coderenga` on macOS/Linux. Initialization templates are embedded, so the executable does not require an external `templates` directory.

## Install from GitHub Releases

Download the asset for your OS from GitHub Releases, then extract it.

- Linux amd64: `coderenga-linux-amd64.tar.gz`
- Windows amd64: `coderenga-windows-amd64.zip`
- Intel Mac: `coderenga-darwin-amd64.tar.gz`
- Apple Silicon Mac: `coderenga-darwin-arm64.tar.gz`

macOS archives are currently unsigned and not notarized, so Gatekeeper may warn on first launch.

Check the binary and initialize the default configuration:

```powershell
.\coderenga.exe --version
.\coderenga.exe --init
```

```bash
./coderenga --version
./coderenga --init
```

## Windows application icon

The Windows icon source is `assets/CodeRenga.ico`. A pinned `rsrc v0.10.2` generates `cmd/coderenga/rsrc_windows_amd64.syso`, which Go automatically links into Windows amd64 executables.

Regenerate the resource after changing the icon:

```powershell
make windows-resource
# or
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/generate-windows-resources.ps1
```

The generated `.syso` is committed, so a direct build also includes the icon:

```powershell
go build -o coderenga.exe ./cmd/coderenga
```

The `_windows_amd64.syso` suffix keeps the resource out of Linux and macOS builds.

## Usage

```powershell
.\.local\bin\coderenga.exe --help
.\.local\bin\coderenga.exe --init
.\.local\bin\coderenga.exe --cwd . "inspect this repository"
.\.local\bin\coderenga.exe --cwd . --no-persist
.\.local\bin\coderenga.exe --mode coder --non-interactive "implement the requested change"
```

Only `--init` creates `coderenga.d/` beside the binary. An explicit `--state-dir` may create its SQLite state database. `--no-persist` always uses in-memory SQLite.

`--init` creates split runtime configuration under `coderenga.d/`: `config.json`, `llm.json`, `mcp.json`, `tools.json`, external prompts, modes, and `coderenga.db`. The runtime supports OpenAI-compatible streaming and non-streaming chat completions, SQLite sessions and compaction, fully qualified built-in/shell/git/MCP/plugin tools, policy aggregation, cwd sandboxing, dry-run, MCP stdio and HTTP/SSE, and plugin soft/hard sandbox requirements.

Key REPL commands:

```text
/mode <name>              /profile <name>          /model <name>
/prompts                  /reload-prompts          /status
/db status                /session list            /session resume <id>
/session search <text>    /compact light|normal|hard
/mcp list                 /mcp tools               /tools [namespace]
/tool info <name>         /tool enable <name>      /tool disable <name>
/tool reload              /tool-policy             /exit
```

Tool execution uses fully qualified names such as `builtin.read_file`, `shell.run`, `git.diff`, `mcp.<server>.<tool>`, and `plugin.<name>`. Policy decisions are aggregated as `block > confirm > unknown > allow`; lower layers cannot weaken a stricter decision.



Tool calls use one JSON object with `tool` and `arguments`; XML-style tags are not executed. Tool results are returned to the model until it produces a final answer. In `--dry-run`, read-only tools may run, while file writes, patches, shell commands, plugins, and MCP calls are reported without execution.



Dry-run tool results explicitly report `executed=false`; contradictory model claims are not shown as the final answer. Greetings are answered without tools. Consecutive identical tool calls stop with the tool name, arguments, and prior result, while the default turn limit reports its call history. `--max-turns <n>` overrides the default turn limit (`16`). `--no-persist` uses only in-memory SQLite and does not touch the configured database file.





The initial modes use `coder write:allow`, `debug write:confirm`, and `architect/reviewer write:false`. File-mutating tools remain constrained by the cwd sandbox and `tools.json`. `--non-interactive` runs allowed operations but fails confirmation-required operations without prompting or auto-approving them.

For unattended verification, use `--auto-approve <categories>` with `--non-interactive` to explicitly approve confirmation-required categories. Supported categories are `read`, `write`, `shell`, `exec`, `git`, `dangerous`, and `all`; comma-separated values and repeated flags are accepted. Shell execution is never auto-approved by default: use `--auto-approve shell` or `--auto-approve all` only when the task and repository are trusted.

## License

MIT License. See [LICENSE](LICENSE).


### Updating existing prompt templates

New release binaries embed the templates under `templates/coderenga.d/`, and `coderenga --init` writes those templates into a new `coderenga.d/`. Existing `coderenga.d/` directories are not overwritten automatically, and CodeRenga currently has no `--init --force` or automatic prompt migration.

For an existing environment, update the public-contract guidance manually:

1. Open `coderenga.d/prompts/default.md` and add the `Public contract preservation` section from the current release template.
2. Open `coderenga.d/modes/coder.md` and add the `Public contract discipline` section from the current release template.
3. Restart CodeRenga or use `/reload-prompts` in the REPL so the updated prompt files are loaded.

The important rule is that specifications define a public contract: preserve JSON keys, CLI flags, output formats, file names, function names, exported identifiers, and documented examples exactly. For example, if a spec requires `line`, do not emit `line_number`, `lineNo`, or `lineNum`.
### llama.cpp native tool calls

The default tool protocol remains `prompt_json`. For llama.cpp server only, a profile can opt into native OpenAI-compatible tool calls with `"toolProtocol":"llamacpp_tools"`. Phase 1 uses non-stream chat completions, sends built-in tool JSON Schemas, defaults `tool_choice` to `auto`, and keeps `parallel_tool_calls:false`. Start `llama-server` with `--jinja` and a tool-aware chat template; otherwise keep using `prompt_json`.
