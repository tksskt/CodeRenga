# CodeRenga 詳細設計書 v0.8 - Embedded Init / Split Config / JSON Tool Calls

## 1. 初期化テンプレート

`coderenga --init` は、ビルド時に `embed.FS` へ格納した `templates/coderenga.d/` を実行ファイル横へ展開する。リリース実行時に外部 `templates/` は要求しない。

- 通常起動は埋め込みテンプレートを展開しない。
- 生成先は `<binary-dir>/coderenga.d/` とする。
- 生成先が存在する場合は、既存ファイルを一切変更せず終了する。
- テンポラリディレクトリへ全ファイルとSQLite DBを生成した後、ディレクトリを配置する。

## 2. 設定ファイルの責務

```text
coderenga.d/
  config.json
  llm.json
  mcp.json
  tools.json
  coderenga.db
  prompts/
    default.md
    compact.md
  modes/
    coder.md
    architect.md
    debug.md
    reviewer.md
```

- `config.json`: バージョン、既定mode/profile、状態DB名
- `llm.json`: OpenAI互換接続先とprofile
- `mcp.json`: MCP server定義
- `tools.json`: Tool policy、shell policy、plugin定義
- `prompts/*.md`: システムプロンプトと圧縮プロンプト
- `modes/*.md`: modeごとの追加指示と権限メタデータ

通常起動時に `config.json` または `llm.json` が無い場合は自動生成せず、`--init` を案内する。旧単一ファイル形式を検出した場合も自動移行せず、再初期化または手動移行を案内する。

`--config <path>` は指定した `config.json` を読み、同じディレクトリの `llm.json`、`mcp.json`、`tools.json` を関連設定として扱う。指定ファイルを読めない場合はパスと原因を表示する。

## 3. Tool Call契約

LLMからRuntimeへの正式なTool Callは、次のJSONオブジェクトだけとする。

```json
{
  "tool": "builtin.read_file",
  "arguments": {
    "path": "README.md"
  }
}
```

- Tool名は完全修飾名とする。
- トップレベルフィールドは `tool` と `arguments` だけとする。
- XML風タグ、独自token、`name` フィールド形式は実行しない。
- Tool Call応答はユーザーへ最終回答として表示しない。
- RuntimeはTool RegistryとPolicy Engineを経由して実行し、結果を会話履歴へ追加してLLMへ再送する。
- 不正なJSONはTool Call形式の誤りとして報告する。
- 挨拶や一般会話は自然文で応答し、Toolを要求しない。Runtimeも既知の単純な挨拶に対する副作用Tool要求を実行しない。
- Tool実装の失敗はOSの生エラーでloopを中断せず、失敗したTool ResultとしてLLMへ返す。
- 直前のTool Result後に同一Tool名・同一引数が連続した場合は反復loopとして停止し、Tool名、引数、直前結果をエラーへ含める。
- default turn limit（既定値: 16）、または `--max-turns` で指定した上限に達した場合は、上限までのTool Call履歴をエラーへ含める。

## 4. dry-run

`--dry-run` では読み取り専用Toolを実行できる。次のToolはExecutorで副作用ありと分類し、Tool実装を呼び出さない。

- `builtin.write_file`
- `builtin.apply_patch`
- `shell.run`
- `plugin.*`
- `mcp.*`

RuntimeはTool名と引数を実行予定として表示し、`dry_run=true`、`executed=false`、`<tool> was not executed`を含む結果を会話履歴へ追加する。write/patchではファイルを書いていないこと、shellではコマンドを実行していないことを明記する。

LLMが実行済みと誤回答した場合でも、その文面は最終表示に採用しない。Runtimeが「予定を表示しただけで副作用は実行していない」という最終回答を出す。プロジェクト配下へファイルやpatchディレクトリを生成しない。

## 5. no-persist

`--no-persist` は既存の `coderenga.d/coderenga.db` を開かず、セッション、Tool Result、loop検出用状態をin-memory SQLiteへ保存する。通常会話やTool失敗が発生しても永続DBの長さと更新日時を変更しない。






## 6. Agent modeとFileMutator policy

初期modeは `coder=write:allow`、`debug=write:confirm`、`architect/reviewer=write:false` とする。`builtin.write_file` と `builtin.apply_patch` はFileMutatorであり、将来のファイル変更Toolも同じinterfaceを実装する。

初期 `tools.json` はwrite/applyをallow、shell.runをconfirmとする。判断はTool、tools.json、mode、動的policyの最大危険度を採り、BlockとConfirmをAllowで緩和しない。dry-runはBlockを尊重し、許可または確認対象の副作用Toolは実行せず予定だけを返す。

## 7. Non-interactive execution

`--non-interactive` は親エージェント向けの実行形態である。Allowは実行し、Blockは拒否する。Confirm/Unknownは自動承認せず、y/N promptも表示せず、次のエラーで停止する。

```text
coderenga: operation requires confirmation, but --non-interactive is enabled.
tool: <fully-qualified-tool-name>
```
## 8. llama.cpp native tool_calls phase 1

`llm.json` profiles may set `toolProtocol`.

- Empty or `prompt_json`: keep the legacy CodeRenga protocol where the assistant returns one JSON object with `tool` and `arguments`.
- `llamacpp_tools`: use llama.cpp server OpenAI-compatible `/v1/chat/completions` native `message.tool_calls`.

Phase 1 is intentionally limited to llama.cpp server native tool calls:

- non-stream chat completions only;
- `parallel_tool_calls` defaults to `false` and `parallelToolCalls:false` is recommended;
- `tool_choice` accepts `auto`, `none`, or `required`, defaulting to `auto`;
- only tool calls from `message.tool_calls` are executable;
- JSON-like text in `content` or `reasoning_content` is never parsed as a tool call;
- built-in tools that expose JSON Schema are sent in the `tools` request field;
- if the generated native tool set is empty, `tools`, `tool_choice`, and `parallel_tool_calls` are omitted and the request is treated as a normal non-stream chat completion;
- `extraBody` is only for additive provider parameters. It cannot override CodeRenga-managed chat body fields: `model`, `messages`, `stream`, `tools`, `tool_choice`, or `parallel_tool_calls`;
- `stream:false` is always forced by CodeRenga for `llamacpp_tools`. When tools are present, `parallel_tool_calls:false` is also forced for Phase 1;
- dot-qualified internal names are converted for transport, for example `builtin.read_file` becomes `builtin__read_file`, and responses are mapped back before execution. If two internal names map to the same transport name, native tool set generation fails with an explicit collision error.

The native loop stores assistant tool-call messages and tool results in per-run `nativeHistory` so the next native request has the required OpenAI-compatible trace. The normal conversation store persists the user instruction and final assistant answer only; intermediate native tool-call trace is recorded in the runtime transcript for the current run and is not yet normalized into the durable conversation history. TODO: define a durable normalized native trace before relying on session resume, compaction, or audit flows for mid-loop native tool details.

llama.cpp native tool calling requires `llama-server --jinja` and a tool-aware chat template. If a server does not return `message.tool_calls` for a dummy tool call, use `toolProtocol:"prompt_json"` instead.

## 9. Tool loop runtime reminders and terminal status

The runtime keeps lightweight per-run state while executing a tool loop.

- Non-interactive execution never prompts for approval. Confirm/unknown tools fail immediately with the tool name, reason, and a remediation hint unless the user explicitly passed `--auto-approve` for the matching category.
- `--auto-approve` accepts `read`, `write`, `shell`, `exec`, `git`, `dangerous`, and `all`. Shell execution is intentionally excluded from the default non-interactive behavior and requires `--auto-approve shell` or `--auto-approve all`.
- The loop records the last successful tool, the last tool result summary, the last successful shell command and exit code, repeated failed shell commands, and files that may have been changed by built-in file mutators.
- If the same shell command fails twice consecutively, the next model input includes a reminder not to retry it again and to inspect the error, fix files, or provide the final answer.
- If a verification command such as `go test ./...` succeeds, the next model input reminds the model not to rerun the same verification unless files changed. A duplicate successful verification may be skipped by runtime state.
- When two turns or fewer remain before `--max-turns`, the next input asks the model to minimize tool use and provide the final answer when implementation or verification is complete. On the final turn it asks the model to avoid tools unless unavoidable.
- If the loop still reaches `--max-turns`, the error includes the last successful tool, last tool result summary, last successful shell command with exit code, possibly changed files, and an explicit note that no final answer was generated.

These reminders apply to both `prompt_json` and `llamacpp_tools` protocols.
## 10. Public contract preservation in distributed prompts

The distributed prompt templates include public contract preservation guidance. This is a permanent runtime instruction, not a smoke-test prompt workaround.

- `templates/coderenga.d/prompts/default.md` contains `Public contract preservation`.
- `templates/coderenga.d/modes/coder.md` contains `Public contract discipline`.
- The templates are embedded by `templates/embed.go` and are written by `coderenga --init` into new `coderenga.d/` directories.
- Existing `coderenga.d/` directories are not overwritten automatically. There is currently no `--init --force` and no automatic prompt migration.

Existing users should copy the new sections into their local `coderenga.d/prompts/default.md` and `coderenga.d/modes/coder.md`, then restart CodeRenga or run `/reload-prompts` in the REPL.

The guidance requires exact preservation of user-visible contract names such as JSON keys, CLI flags, output formats, file names, function names, exported identifiers, and documented examples. For example, a spec field named `line` must remain `line`, not `line_number`, `lineNo`, or `lineNum`.
