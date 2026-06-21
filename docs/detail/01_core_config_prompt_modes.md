# CodeRenga 詳細設計書 v0.8 - 01 Core / Config / Prompt / Modes

> 設定ファイル分割、埋め込み初期化、JSON Tool Callについては `04_embedded_init_split_config_tool_calls.md` が本書の2章、3章、7章に優先する。

## 1. アーキテクチャ概要

CodeRenga は Go 製 CLI アプリケーションであり、以下の責務に分割する。

```text
cmd/coderenga
  main.go

internal/
  app/
    app.go
    runtime.go

  agent/
    loop.go
    turn.go
    context_builder.go
    compact.go

  llm/
    client.go
    openai_compat.go
    stream.go
    types.go

  prompt/
    loader.go
    builder.go
    modes.go
    project_instructions.go

  modes/
    registry.go
    mode.go
    policy.go

  tools/
    registry.go
    parser.go
    fs.go
    shell.go
    git.go
    mcp_bridge.go

  mcp/
    client.go
    transport.go
    stdio.go
    http_sse.go
    protocol.go
    registry.go

  safety/
    path.go
    shell_policy.go
    approval.go
    redaction.go

  config/
    config.go
    schema.go
    loader.go
    merge.go

  storage/
    db.go
    migrate.go
    sessions.go
    messages.go
    tool_runs.go
    summaries.go
    audit.go
    mcp_cache.go

  ui/
    repl.go
    once.go
    render.go
    slash_commands.go
```

## 2. 設定ファイル

### 2.1 読み込み単位

デフォルトではバイナリ横の `coderenga.d/config.json` を起点とし、同じディレクトリの分割設定を読み込む。HOME / XDG 配下は自動探索しない。

1. `config.json`: default mode/profile、state database
2. `llm.json`: LLM profileと接続設定
3. `mcp.json`: MCP server定義
4. `tools.json`: Tool policy、shell policy、plugin定義
5. 環境変数とCLI引数による一時上書き

`--config <path>` を指定した場合、そのファイルと同じディレクトリの `llm.json`、`mcp.json`、`tools.json` を読む。通常起動時は設定を生成しない。

### 2.2 最小例

`config.json`:

```json
{"version":1,"defaultMode":"coder","defaultProfile":"local","state":{"database":"coderenga.db"}}
```

`llm.json`:

```json
{"version":1,"defaultProfile":"local","profiles":{"local":{"baseURL":"http://127.0.0.1:8080/v1","apiKey":"","model":"local-model","temperature":0.2,"maxTokens":4096}}}
```

`mcp.json`:

```json
{"version":1,"servers":{}}
```

`tools.json`:

```json
{"version":1,"policies":{"builtin.read_file":"allow","builtin.write_file":"allow","builtin.apply_patch":"allow","shell.run":"confirm","git.status":"allow","git.diff":"allow"},"plugins":{}}
```

## 3. プロンプト設計

### 3.1 方針

CodeRenga はバイナリ内に固定システムプロンプトを組み込まない。

配布物には `prompts/default/system.md` を含めてもよいが、それは外部ファイルとして扱う。ユーザーは config で任意の system prompt を指定できる。

### 3.2 Prompt Builder

Prompt Builder は以下を結合し、LLM に渡す system/developer 相当のメッセージを生成する。

1. system prompt files
2. project instruction files
3. selected mode prompt
4. runtime policy summary
5. tool protocol instructions
6. active MCP tools summary

### 3.3 読み込み失敗時

`missing_system_prompt` により挙動を変える。

| 値 | 挙動 |
|---|---|
| warn | 警告を出して最小プロンプトで続行 |
| error | 起動失敗 |
| ignore | 警告なしで続行 |

## 4. プロジェクト指示ファイル

以下のファイルをプロジェクトルートから探索する。

```text
.coderenga/instructions.md
CODERENGA.md
AGENTS.md
```

複数存在する場合は config の順序で結合する。

`.coderenga/instructions.md` は CodeRenga 専用、`AGENTS.md` は既存エージェント互換として扱う。

## 5. ユーザー定義エージェントモード

### 5.1 モード定義方式

モードは config または Markdown ファイルで定義できる。

#### config 方式

```json
{
  "modes": {
    "architect": {
      "description": "調査と設計を行う。編集は禁止。",
      "prompt_file": "<binary-dir>/coderenga.d/modes/architect.md",
      "profile": "qwen",
      "permissions": {
        "read": true,
        "write": false,
        "shell": "allow_readonly",
        "mcp": true
      },
      "plan_first": true
    },
    "coder": {
      "description": "実装workerとして実装を行う。cwd内の書き込みを許可する。",
      "prompt_file": "<binary-dir>/coderenga.d/modes/coder.md",
      "profile": "devstral",
      "permissions": {
        "read": true,
        "write": "allow",
        "shell": "policy",
        "mcp": true
      },
      "plan_first": true
    }
  }
}
```

#### Markdown 方式

`.coderenga/modes/reviewer.md`:

```md
---
name: reviewer
description: 差分レビューを行う。編集は禁止。
profile: qwen
write: false
shell: allow_readonly
mcp: true
plan_first: false
---

あなたはコードレビュー担当です。
差分のリスク、バグ、テスト不足、設計崩れを優先して指摘してください。
原則としてファイル編集は行わず、修正案を提示してください。
```

### 5.2 初期modeのwrite policy

| mode | write | 用途 |
|---|---|---|
| coder | allow | 親エージェントから呼ばれる実装worker。cwd内のFileMutatorを確認なしで許可 |
| debug | confirm | 人間が確認しながら行う調査と最小修正 |
| architect | false | 調査・設計専用。FileMutatorをBlock |
| reviewer | false | レビュー専用。FileMutatorをBlock |

`write` は `FileMutator` を実装するToolに適用する。`shell.run` は対象外で、shell policyと`tools.json`に従う。最終判断は `block > confirm > unknown > allow` の最大危険度であり、modeの `write:false` と `tools.json:block` は他のallowで緩和できない。

### 5.3 モード切り替え

CLI:

```bash
coderenga --mode coder
```

REPL:

```text
/mode coder
/modes
```

モード切り替え時は、以下を更新する。

- mode prompt
- tool permissions
- default profile
- plan-first 設定
- write/shell/mcp 権限

## 6. LLM プロファイル

### 6.1 起動時指定

```bash
coderenga --profile qwen
coderenga --model qwen3.5-27b --base-url http://127.0.0.1:8080/v1
```

### 6.2 REPL 中切り替え

```text
/profile devstral
/model glm-4.7-flash
```

### 6.3 モードとの関係

モードに `profile` が設定されている場合、`/mode` 変更時に profile も切り替える。ただし、ユーザーが明示的に `/profile` を指定した場合は、現在セッション中はユーザー指定を優先する。

## 7. ツール呼び出し仕様

### 7.1 単一 JSON オブジェクト

LLM は次の形式だけでツール呼び出しを出力する。

```json
{"tool":"builtin.read_file","arguments":{"path":"README.md"}}
```

Runtime はトップレベルの `tool` と `arguments` を検証し、Tool Registry へ渡す。XML風タグと独自token形式は実行しない。

### 7.2 Tool Registry

Tool Registry は以下を統合する。

- built-in tools
- shell tools
- git tools
- MCP tools

ツール名は名前空間を持つ。

```text
builtin.read_file
git.diff
shell.run
mcp.web-search.search
mcp.local-docs.query
```

## 8. ファイルシステムツール

### 8.1 builtin.read_file

- cwd 内のファイルのみ許可
- 最大読み込みサイズを制限
- `.env` などは設定でブロック可能

### 8.2 builtin.write_file

- 既存ファイル上書きは確認必須
- 新規ファイル作成も原則確認
- dry-run 時は DB の `tool_runs` / `audit_logs` に変更案を保存し、必要に応じて `<state-dir>/dry-run/` に出力する。project 配下 `.coderenga/patches` は作成しない

### 8.3 builtin.apply_patch

- unified diff 形式を受け付ける
- 適用前に対象ファイル一覧と差分概要を表示
- 失敗時はファイルを変更しない

## 9. shell 実行設計

### 9.1 ShellPolicy

```go
type ShellLevel string

const (
    ShellAllow   ShellLevel = "allow"
    ShellConfirm ShellLevel = "confirm"
    ShellBlock   ShellLevel = "block"
    ShellUnknown ShellLevel = "unknown"
)
```

### 9.2 判定方式

Shell Policy は生文字列の glob 一致で判定してはならない。`git status*` のような文字列 prefix / glob ルールは禁止する。

`shell.run` は、実行要求を以下の順で正規化してから判定する。

1. `argv` 指定がある場合は、その配列を正規コマンドとして扱う。
2. `command` 文字列指定がある場合は shell 構文を解析し、`;` / `&&` / `||` / `|` / 改行 / サブシェル / command substitution / redirection をコマンドセグメントへ分解する。
3. 各セグメントを argv 化し、コメント、引用、エスケープを正規化する。
4. 各セグメントごとに `block -> confirm -> allow -> unknown` の順で評価する。
5. 全体の判定は最も危険な結果を採用する。優先度は `block > confirm > unknown > allow` とする。
6. unknown は設定の `shell_policy.unknown` に従う。既定値は `confirm` とする。

複合コマンド内に 1 つでも confirm / unknown / block が含まれる場合、全体を allow にしてはならない。

例:

```text
 git status
   -> allow

 git status; rm file
   -> git status = allow
   -> rm file = confirm
   -> command = confirm

 git status && sudo rm -rf /
   -> git status = allow
   -> sudo ... = block
   -> command = block
```

原則として `shell.run` は `exec.Command` 相当で直接実行し、`/bin/sh -c` や `cmd.exe /C` は使わない。shell 構文が必要な場合は `shell_mode: true` を明示し、既定で confirm 以上とする。

### 9.3 unknown の扱い

既定値は `confirm` とする。

```json
{
  "shell_policy": {
    "unknown": "confirm"
  }
}
```

設定可能値:

| 値 | 挙動 |
|---|---|
| confirm | ユーザーに確認する |
| block | 実行禁止 |
| allow | 確認なしで許可。非推奨 |

### 9.4 確認 UI

```text
Command requires confirmation:
  npm install
Reason:
  matched confirm pattern: npm install*
Run? [y/N]
```

unknown の場合:

```text
Command is not classified:
  pnpm lint
Policy:
  unknown = confirm
Run once? [y/N]
```

## 10. MCP 詳細設計

### 10.1 対応 transport

- stdio
- HTTP/SSE

### 10.2 stdio

CodeRenga が MCP server process を起動し、stdin/stdout で JSON-RPC を行う。

```text
CodeRenga
  -> exec.Command(command, args...)
  -> stdin/stdout pipe
  -> initialize
  -> tools/list
  -> tools/call
```

### 10.3 HTTP/SSE

HTTP/SSE transport では、SSE で server events を購読し、HTTP POST で JSON-RPC message を送信する。

```text
CodeRenga
  -> GET /mcp or configured SSE endpoint
  -> receive endpoint/event stream
  -> POST JSON-RPC requests
```

### 10.4 MCP Tool Bridge

MCP から取得した tool は、Tool Registry に以下の名前で登録する。

```text
mcp.<server-name>.<tool-name>
```

例:

```text
mcp.web-search.search
mcp.local-docs.query
```

### 10.5 MCP 権限

モードごとに MCP 使用可否を制御する。

```json
{
  "permissions": {
    "mcp": true,
    "mcp_servers": ["web-search", "local-docs"]
  }
}
```

MCP tool も通常 tool と同様に承認ポリシーを適用できる。

## 11. スラッシュコマンド

```text
/help                     ヘルプ表示
/exit                     終了
/mode <name>              モード切り替え
/modes                    モード一覧
/profile <name>           LLM profile 切り替え
/model <name>             現在 profile の model を一時変更
/status                   状態表示
/diff                     git diff 表示
/plan                     現在の計画表示
/compact                  会話圧縮
/prompts                  読み込まれているプロンプト一覧
/reload-prompts           プロンプト再読み込み
/mcp list                 MCP server 一覧
/mcp tools                MCP tool 一覧
/shell-policy             shell policy 表示
/session list              セッション一覧
/session resume <id>       セッション再開
/session search <keyword>  セッション検索
/summary show              active summary 表示
/db status                 DB状態表示
```









