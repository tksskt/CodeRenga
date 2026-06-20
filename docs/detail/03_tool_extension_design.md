Exit code: 0
Wall time: 0.4 seconds
Output:
Exit code: 0
Wall time: 0.4 seconds
Output:
# CodeRenga 詳細設計書 v0.8 - 03 Tool Extension Design

## 16. ツール拡張詳細設計

### 16.1 設計方針

CodeRenga のツール実行はすべて Runtime 内の Tool Registry に集約する。LLM はローカルファイル、shell、MCP、Plugin Tool を直接実行しない。LLM は `{"tool":"<fully-qualified-name>","arguments":{...}}` という単一JSONオブジェクトで要求を出し、Runtime が登録済みツール、ポリシー、モード制限、ユーザー確認、timeout、出力制限を適用する。

### 16.2 パッケージ構成

```text
internal/
  tools/
    registry.go
    types.go
    parser.go
    executor.go
    policy.go
    result.go

    builtin/
      read_file.go
      write_file.go
      apply_patch.go
      list_files.go
      search_text.go

    shell/
      run_shell.go
      command_policy.go

    git/
      status.go
      diff.go
      show.go

    mcpbridge/
      bridge.go
      mapper.go

    plugin/
      loader.go
      manifest.go
      executor.go
      json_stdio.go
      arg_expand.go
```

### 16.3 Tool インターフェース

```go
type Tool interface {
    Name() string
    Description() string
    Schema() JSONSchema
    Policy() ToolPolicy
    AvailableModes() []string
    Execute(ctx context.Context, req ToolRequest) (ToolResult, error)
}

type ToolRequest struct {
    Name      string         `json:"name"`
    Arguments map[string]any `json:"arguments"`
    Context   ToolContext    `json:"context"`
}

type ToolContext struct {
    CWD       string `json:"cwd"`
    Mode      string `json:"mode"`
    SessionID string `json:"session_id"`
}

type ToolResult struct {
    OK       bool           `json:"ok"`
    Content  string         `json:"content,omitempty"`
    Error    string         `json:"error,omitempty"`
    Metadata map[string]any `json:"metadata,omitempty"`
}
```

### 16.4 Tool Registry

Tool Registry は以下の責務を持つ。

- Built-in Tool の登録
- Plugin Tool の読み込みと登録
- MCP Tool の動的登録
- ツール名の衝突検出
- 現在の mode に応じた利用可能ツール一覧の生成
- LLM に渡すツール説明テキストの生成
- 実行時の完全修飾名解決

登録順は以下とする。

1. Built-in Tool
2. User Plugin Tool
3. MCP Tool

同名衝突が発生した場合は上書きしない。完全修飾名が重複した場合は起動時警告を出し、後から読み込まれたツールを無効化する。

### 16.5 名前空間

内部ツール名は完全修飾名を必須とする。

| 名前空間 | 用途 | 例 |
|---|---|---|
| `builtin.*` | ファイル操作などの内蔵ツール | `builtin.read_file` |
| `shell.*` | shell 実行 | `shell.run` |
| `git.*` | Git 補助 | `git.diff` |
| `mcp.<server>.*` | MCP サーバ由来ツール | `mcp.web.search` |
| `plugin.*` | ユーザー追加ツール | `plugin.docker_ps` |

短縮名は UI 表示用としてのみ扱う。実行解決時には完全修飾名を使う。

### 16.6 Plugin Tool 設定

Plugin Tool は以下の2方式で読み込める。

#### 16.6.1 tools.json 方式

```json
{
  "version": 1,
  "policies": {
    "plugin.docker_ps": "allow"
  },
  "plugins": {
    "docker_ps": {
      "description": "List Docker containers.",
      "command": "/home/tks/coderenga-tools/docker_ps",
      "input_mode": "json_stdin",
      "args_schema": {
        "type": "object",
        "properties": {
          "all": {"type": "boolean", "description": "Include stopped containers."}
        }
      },
      "policy": "allow",
      "timeout_sec": 10,
      "available_modes": ["debug", "coder"]
    }
  }
}
```

#### 16.6.2 plugin directory 方式

```text
<binary-dir>/coderenga.d/plugins/
  docker_ps/
    tool.json
    docker_ps
```

`tool.json`:

```json
{
  "name": "plugin.docker_ps",
  "description": "List Docker containers.",
  "command": "./docker_ps",
  "input_mode": "json_stdin",
  "args_schema": {
    "type": "object",
    "properties": {
      "all": { "type": "boolean" }
    }
  },
  "policy": "allow",
  "timeout_sec": 10,
  "available_modes": ["debug", "coder"]
}
```

### 16.7 Plugin Tool 入出力

標準方式は JSON stdin / JSON stdout とする。

CodeRenga から Plugin Tool への入力:

```json
{
  "arguments": {
    "all": true
  },
  "context": {
    "cwd": "/home/tks/project",
    "mode": "debug",
    "session_id": "20260619-xxxx"
  }
}
```

Plugin Tool から CodeRenga への出力:

```json
{
  "ok": true,
  "content": "CONTAINER ID   IMAGE   STATUS\n...",
  "metadata": {
    "truncated": false
  }
}
```

失敗時:

```json
{
  "ok": false,
  "error": "Docker daemon is not running."
}
```

### 16.8 input_mode

| input_mode | 内容 | 用途 |
|---|---|---|
| `json_stdin` | JSON を stdin に渡す | 推奨方式 |
| `args` | JSON arguments を CLI 引数へ展開 | 単純な shell script |
| `env` | arguments を環境変数として渡す | 既存 CLI 連携 |

`json_stdin` を標準とし、`args` と `env` は軽量な互換用途に限定する。

### 16.9 Plugin Tool Policy

Plugin Tool は shell command policy とは別に Tool Policy を持つ。ただし実行ファイルとしての危険性があるため、最終的には共通 Policy Engine で評価する。

```json
{
  "tool_policy": {
    "unknown": "confirm",
    "allow": [
      "builtin.read_file",
      "builtin.search_text",
      "git.status",
      "git.diff",
      "plugin.docker_ps"
    ],
    "confirm": [
      "builtin.write_file",
      "builtin.apply_patch",
      "shell.run",
      "plugin.restart_service"
    ],
    "block": [
      "plugin.wipe_disk"
    ]
  }
}
```

評価順は以下とする。

1. mode の allow / deny
2. tool_policy
3. shell_policy。対象が `shell.run` の場合のみ
4. Tool manifest の policy
5. unknown fallback

判定の集約規則は、評価順の「後勝ち」ではなく、すべての層から得られた判定のうち最大危険度を採用する。危険度の順序は以下とする。

```text
block > confirm > unknown > allow
```

下位層は上位層の判定を緩和できない。たとえば mode が `block` を返した場合、tool_policy や Tool manifest が `allow` を返しても最終判定は `block` とする。tool_policy が `confirm` を返し、manifest が `allow` を返した場合も、最終判定は `confirm` とする。

`shell.run` では、shell_policy がセグメントごとの最大危険度を返し、その結果も同じ集約規則に参加する。Plugin Tool では、mode、tool_policy、manifest、sandbox 要件、unknown fallback の最大危険度を採用する。

#### Plugin sandbox の基本方針

Plugin Tool は任意実行ファイルであり、CodeRenga のプロセス外で動作する。そのため、Policy 判定、cwd 設定、環境変数制限だけでは、plugin が cwd 外のファイルを直接操作することを完全には防げない。

本設計では、Plugin Tool の filesystem 制御を以下の 3 層に分ける。

| 層 | 内容 | cwd sandbox 保証 |
|---|---|---|
| `builtin` | CodeRenga 内蔵ツール | 強制可能 |
| `plugin_soft` | cwd 設定、環境変数 allowlist、引数制限のみ | 保証しない |
| `plugin_hard` | OS sandbox backend による filesystem access 制限 | backend 次第で保証可能 |

Plugin Tool で cwd sandbox を保証したい場合は `sandbox.required = true` を設定する。利用可能な OS sandbox backend がない場合、CodeRenga は当該 plugin 実行を拒否する。

```json
{
  "name": "plugin.query_logs",
  "command": "./query_logs",
  "policy": "confirm",
  "sandbox": {
    "required": true,
    "filesystem": "cwd_readonly",
    "network": "deny",
    "env": {
      "mode": "allowlist",
      "allow": ["PATH", "CR_CWD", "CR_SESSION_ID"]
    }
  }
}
```

MVP では、hard sandbox backend が未実装または利用不可の環境では `sandbox.required = true` の plugin を実行しない。`sandbox.required = false` の plugin は信頼済み外部実行ファイルとして扱い、既定で confirm 以上にする。

実行時の最低制約:

```text
- working directory は対象 cwd に固定する
- 環境変数は allowlist 方式で渡す
- shell 経由で起動せず、直接 exec する
- stdin は JSON payload のみを渡す
- timeout を必須にする
- stdout / stderr の最大サイズを制限する
- network / filesystem 制御は hard sandbox backend がある場合のみ保証する
```

### 16.10 モード別ツール制限

ユーザー定義モードは、利用可能ツールを制限できる。

```json
{
  "modes": {
    "architect": {
      "prompt_file": "<binary-dir>/coderenga.d/modes/architect.md",
      "tools": {
        "allow": [
          "builtin.read_file",
          "builtin.list_files",
          "builtin.search_text",
          "git.status",
          "git.diff"
        ],
        "deny": [
          "builtin.write_file",
          "builtin.apply_patch",
          "shell.run"
        ]
      }
    },
    "debug": {
      "prompt_file": "<binary-dir>/coderenga.d/modes/debug.md",
      "tools": {
        "allow": [
          "builtin.*",
          "git.*",
          "shell.run",
          "plugin.query_logs"
        ]
      }
    }
  }
}
```

### 16.11 MCP Tool との統合

MCP Tool は MCP サーバから取得した tool schema を CodeRenga の Tool Registry へ変換して登録する。

```text
MCP server: web
MCP tool: search
CodeRenga tool name: mcp.web.search
```

MCP Tool も `tool_policy` と mode restriction の対象とする。これにより、MCP の外部ツールであっても確認、ブロック、ログ、timeout、出力制限を統一的に扱える。

### 16.12 実行ライフサイクル

```text
LLM output
  -> ToolCall Parser
  -> Tool Registry
  -> Mode Restriction
  -> Policy Engine
  -> User Confirmation if needed
  -> Tool Executor
  -> Output Truncation
  -> Tool Result
  -> LLM context
```

### 16.13 監査ログ

すべてのツール実行監査ログは SQLite DB `coderenga.db` の `audit_logs` table に記録する。JSONL 監査ログは標準保存先として使用しない。

記録対象:

```text
- tool_requested
- policy_decided
- user_approved / user_denied
- tool_started
- tool_finished
- tool_failed
- tool_blocked
```

代表的な `event_json`:

```json
{
  "time": "2026-06-19T20:00:00+09:00",
  "session_id": "20260619-xxxx",
  "mode": "debug",
  "tool": "plugin.docker_ps",
  "policy": "allow",
  "approved": true,
  "duration_ms": 120,
  "ok": true
}
```

ログには secret や巨大出力を保存しない。必要に応じて出力は切り詰め、機密パターンはマスクする。
巨大な stdout / stderr / tool output を DB に直接保存しない設定の場合のみ、`<state-dir>/blobs/` に blob を退避し、DB には blob path と hash を保存する。
`--no-persist` または未初期化時は永続監査ログを作成しない。

### 16.14 CLI / REPL コマンド

```text
/tools
/tools builtin
/tools plugin
/tools mcp
/tool info plugin.docker_ps
/tool enable plugin.docker_ps
/tool disable plugin.docker_ps
/tool reload
/tool-policy
```

`/tool reload` は Plugin Tool manifest と MCP Tool 一覧を再読み込みする。

### 16.15 初期実装範囲

v0.1 初期実装に含める。

- Tool Registry
- Built-in Tool
- MCP Tool bridge
- User Plugin Tool の tools.json 方式
- JSON stdin / JSON stdout
- Tool Policy
- mode restriction
- 監査ログ

v0.2 以降で拡張する。

- plugin directory / tool.json manifest
- args / env input_mode
- plugin enable / disable 管理
- plugin package import / export




