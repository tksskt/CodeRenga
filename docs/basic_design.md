Exit code: 0
Wall time: 0.3 seconds
Output:
Exit code: 0
Wall time: 0.2 seconds
Output:
# CodeRenga 基本設計書 v0.8

## 1. 目的

CodeRenga は、ローカル LLM を主対象とした軽量なコーディングエージェントである。Go 製の単体 CLI バイナリとして提供し、ローカルファイルを直接操作しながら、設計、調査、実装、レビュー、テストを支援する。

本改訂では、v0.6 のレビュー結果を反映し、shell 複合コマンド判定、Plugin sandbox、監査ログ保存先、正規ツール名、dry-run 出力先、版番号の整合性を修正する。

- MCP stdio client 対応
- MCP HTTP/SSE client 対応
- ターミナルコマンドの安全レベル設定化
- ユーザー定義可能なプロンプト構成
- 組み込み固定システムプロンプトの廃止
- AGENTS.md / CODERENGA.md / 任意 md によるプロジェクト指示読み込み
- エージェントモードのユーザー定義化
- 複数モデル、複数プロファイルの切り替え
- Built-in Tool / MCP Tool / User Plugin Tool を統合する Tool Registry
- ユーザーが後付け可能な Plugin Tool
- `tools.json` および plugin directory / `tool.json` manifest によるツール追加
- ツール名前空間、実行ポリシー、モード別利用制限
- デフォルト設定・状態ディレクトリをバイナリ横 `coderenga.d/` とする
- 通常起動時は `coderenga.d/` を自動生成しない
- `--init` 実行時のみ `coderenga.d/` テンプレートを生成する
- バイナリ横配置モードは廃止し、通常動作をバイナリ横配置前提に統一する
- shell command policy は shell 構文を分解し、全セグメントの最大危険度で判定する
- Plugin Tool は任意実行ファイルとして扱い、hard sandbox がない場合に cwd sandbox 保証を主張しない

## 2. 基本方針

CodeRenga は「LLM を信頼しすぎない」設計とする。

- LLM は提案者であり、実行責任者ではない。
- Runtime がファイルアクセス、コマンド実行、MCP 呼び出し、確認、ブロックを制御する。
- ユーザーが最終承認者である。
- すべての危険操作は設定と実行時確認により制御する。
- ユーザーが明示的に初期化しない限り、設定ファイル・状態ファイル・プロジェクト内 `.coderenga/` を自動生成しない。

## 3. 提供形態

- 言語: Go
- UI: CLI / REPL
- 配布: 単体バイナリ
- 初期対象 OS: Linux amd64, Windows amd64
- LLM 接続: OpenAI 互換 Chat Completions API
- ツール呼び出し: `tool` と `arguments` を持つ単一 JSON オブジェクト
- MCP: stdio と HTTP/SSE の両方に初期対応する
- 標準設定配置: バイナリ横 `coderenga.d/`
- 自動展開: なし。`--init` の明示実行時のみテンプレートと `coderenga.db` を生成


## 4. 実行時ファイル配置と初期化方針

CodeRenga は環境汚染を避けるため、通常起動時に設定ファイル、プロンプト、ログ、セッション、キャッシュ、プロジェクト設定を自動生成しない。

デフォルトの設定・状態ディレクトリは、バイナリと同じディレクトリにある `coderenga.d/` とする。これは旧来の `--portable` 相当の考え方を通常動作に統合したものであり、`--portable` オプションは廃止する。

```text
<binary-dir>/
  coderenga
  coderenga.d/
    config.json
    llm.json
    mcp.json
    tools.json
    prompts/
      default.md
      compact.md
    modes/
      architect.md
      coder.md
      reviewer.md
      debug.md
    coderenga.db
```

`plugins/` は User Plugin Tool を追加する場合にユーザーが作成するか、将来の `--init-plugins` 相当の補助コマンドで生成する。セッション、ログ、MCP ツールキャッシュ、圧縮サマリは JSONL やディレクトリ分散ではなく `coderenga.db` に保存する。巨大な tool output をファイル退避する必要がある場合のみ、設定された state directory 配下に blob 用ディレクトリを作成する。

### 4.1 通常起動時

`coderenga.d/config.json` または `coderenga.d/llm.json` が存在しない場合、CodeRenga は自動生成せず起動を停止し、`--init` と `llm.json` の編集を案内する。

```text
coderenga: configuration is not initialized.
Run "coderenga --init" to create coderenga.d, then edit coderenga.d/llm.json.
```

この状態では、`coderenga.db` を作成せず、セッション、ログ、キャッシュは保存しない。必要な一時ファイルは OS の一時ディレクトリに作成し、終了時に削除する。

### 4.2 `--init`

`coderenga --init` を実行した場合のみ、バイナリ横に `coderenga.d/` を生成する。テンプレートは `embed.FS` から展開するため、実行時に外部 `templates/` は不要である。生成対象は分割設定、外部プロンプト、モード定義、SQLite DB `coderenga.db` である。`--init` 時に schema migration を実行し、必要なテーブルを作成する。

### 4.3 `--init-project`

プロジェクト配下の `.coderenga/` は `coderenga --init-project --cwd <project>` を明示実行した場合のみ生成する。通常起動では作成しない。

### 4.4 XDG / HOME 配下

`~/.config/coderenga` や XDG パスはデフォルトでは使用しない。必要な場合は `--config-dir` または `--state-dir` で明示指定する。

## 5. 起動方式

### 5.1 対話モード

```bash
coderenga --cwd /path/to/project
```

REPL を起動し、自然文指示とスラッシュコマンドを受け付ける。

### 5.2 単発実行モード

```bash
coderenga --cwd . "このリポジトリを調査して、改善点を出して"
```

内部で必要なツール呼び出しを複数ターン実行し、最終回答を出して終了する。

### 5.3 stdin 連携

```bash
git diff | coderenga --cwd . --stdin --mode reviewer "この差分をレビューして"
```

標準入力を追加コンテキストとして利用する。

## 6. ローカルファイル操作

CodeRenga は `--cwd` で指定されたプロジェクトルート配下を直接操作できる。

- 読み取り: 確認なしで許可
- 書き込み: 原則確認あり
- パッチ適用: 原則確認あり
- cwd 外アクセス: ブロック
- symlink による cwd 外脱出: ブロック

## 7. ターミナルコマンド実行

ターミナルコマンドは `run_shell` ツールで実行する。安全性のため、コマンドを以下の 3 段階に分類する。

| レベル | 意味 | 例 |
|---|---|---|
| allow | 確認なしで実行可能 | `git status`, `git diff`, `pwd`, `ls`, `rg` |
| confirm | 実行前にユーザー確認が必要 | `npm test`, `go test ./...`, `go build`, `npm install`, `rm` |
| block | 実行禁止 | `sudo`, `dd`, `mkfs`, `curl | sh`, `rm -rf /` |

どのレベルにも属さないコマンドは `unknown` として扱い、実行前にユーザー確認を求める。設定により、unknown を confirm 扱いまたは block 扱いに変更できる。

## 8. コマンド安全ポリシーの設定化

ユーザーは設定ファイルでコマンドパターンを安全レベルに割り当てられる。

```json
{
  "shell_policy": {
    "unknown": "confirm",
    "allow": [
      { "cmd": "git", "args": ["status"], "match": "argv_prefix" },
      { "cmd": "git", "args": ["diff"], "match": "argv_prefix" },
      { "cmd": "pwd", "match": "exact" },
      { "cmd": "ls", "match": "argv_prefix" },
      { "cmd": "rg", "match": "argv_prefix" }
    ],
    "confirm": [
      { "cmd": "npm", "args": ["test"], "match": "argv_prefix" },
      { "cmd": "go", "args": ["test"], "match": "argv_prefix" },
      { "cmd": "go", "args": ["build"], "match": "argv_prefix" },
      { "cmd": "npm", "args": ["install"], "match": "argv_prefix" },
      { "cmd": "rm", "match": "argv_prefix" }
    ],
    "block": [
      { "cmd": "sudo", "match": "argv_prefix" },
      { "cmd": "dd", "match": "argv_prefix" },
      { "cmd": "mkfs", "match": "argv_prefix" },
      { "pattern": "curl_pipe_sh", "match": "compound" },
      { "pattern": "wget_pipe_sh", "match": "compound" },
      { "cmd": "rm", "args": ["-rf", "/"], "match": "argv_prefix" }
    ]
  }
}
```

この設定は起動時に読み込まれ、`shell.run` の実行前に必ず評価される。評価は shell 文字列の glob 判定ではなく、shell 構文をセグメント分解したうえで各セグメントの最大危険度を採用する。

## 9. MCP 対応

CodeRenga はMCP stdioとHTTP/SSE clientに対応する。サーバー定義は `coderenga.d/mcp.json` だけに置く。

```json
{
  "version": 1,
  "servers": {
    "web-search": {"transport":"stdio","command":"node","args":["./mcp-server.js"],"enabled":true},
    "docs": {"transport":"http_sse","url":"http://127.0.0.1:8000/mcp","enabled":true}
  }
}
```

MCP ToolはTool Registryへ登録し、通常Toolと同じPolicy Engineと監査ログの対象にする。

## 10. Tool Registry とツール拡張

CodeRenga のツールはすべて Runtime 側の Tool Registry に登録される。LLM はファイル、ターミナル、MCP、プラグインへ直接アクセスしない。LLM はツール呼び出し要求を出し、CodeRenga Runtime が Policy Engine を通して実行可否を判断する。

### 10.1 ツール種別

| 種別 | 概要 | 例 |
|---|---|---|
| Built-in Tool | CodeRenga 本体に実装される標準ツール | `builtin.read_file`, `builtin.write_file`, `builtin.apply_patch`, `shell.run`, `git.diff` |
| MCP Tool | MCP サーバから動的に取得する外部ツール | `mcp.web.search`, `mcp.context7.get_library_docs` |
| User Plugin Tool | ユーザーが後付け登録する任意の実行ファイルまたはスクリプト | `plugin.docker_ps`, `plugin.query_db_schema` |

ローカルファイル操作は原則として Built-in Tool を優先する。Built-in Tool は CodeRenga Runtime 内で実パス解決と symlink 解決を行い、cwd sandbox を強制する。

MCP filesystem 系ツールや User Plugin Tool は外部プロセスであるため、Policy 判定だけでは cwd sandbox を技術的に保証できない。これらにファイル操作を許可する場合は、OS sandbox backend を有効化するか、ユーザー確認付きの信頼済みツールとして扱う。hard sandbox が要求され、利用可能な backend がない場合、CodeRenga は当該ツール実行を拒否する。

### 10.2 User Plugin Tool

ユーザーは任意の実行ファイルをPlugin Toolとして追加できる。標準インターフェースはJSON stdin / JSON stdoutとし、定義は `coderenga.d/tools.json` のトップレベル `plugins` に置く。

```json
{
  "version": 1,
  "policies": {"plugin.docker_ps":"allow"},
  "plugins": {
    "docker_ps": {
      "description":"List Docker containers.",
      "command":"/home/tks/coderenga-tools/docker_ps",
      "input_mode":"json_stdin",
      "policy":"allow",
      "timeout_sec":10,
      "available_modes":["debug","coder"]
    }
  }
}
```

LLMは `{"tool":"plugin.docker_ps","arguments":{"all":true}}` の形式で要求する。

### 10.3 ツールマニフェスト

複数の自作ツールを管理しやすくするため、plugin directory と `tool.json` manifest をサポートする。

```text
<binary-dir>/coderenga.d/plugins/
  docker_ps/
    tool.json
    docker_ps
  query_db_schema/
    tool.json
    query_db_schema
```

`tool.json` 例:

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

### 10.4 ツール名前空間

同名衝突と権限誤認を避けるため、内部的には名前空間を必須とする。

- `builtin.*`
- `shell.*`
- `git.*`
- `mcp.<server>.*`
- `plugin.*`

LLM に提示する説明では短縮名を併記してもよいが、実行時の解決は完全修飾名で行う。

### 10.5 ツール実行ポリシー

各ツールは `allow / confirm / block / unknown` のいずれかのポリシーで評価される。Plugin Tool と MCP Tool も Built-in Tool と同じ Policy Engine の対象とする。

- `allow`: 確認なしで実行可能
- `confirm`: 実行前にユーザー確認
- `block`: 常に拒否
- `unknown`: 明示設定がないため、既定では確認要求

### 10.6 モード別ツール制限

ユーザー定義エージェントモードごとに利用可能ツールを制限できる。

```json
{
  "modes": {
    "architect": {
      "tools": {
        "allow": ["builtin.read_file", "builtin.search_text", "git.diff"],
        "deny": ["builtin.write_file", "builtin.apply_patch", "shell.run"]
      }
    },
    "debug": {
      "tools": {
        "allow": ["builtin.*", "git.*", "shell.run", "plugin.query_logs"]
      }
    }
  }
}
```

## 11. ツール実行ライフサイクル

1. 設定ファイルを読み込む。
2. Built-in Tool を登録する。
3. User Plugin Tool を `tools.json` と plugin directory から読み込む。
4. MCP stdio / HTTP / SSE サーバへ接続し、MCP Tool を動的登録する。
5. 現在の mode に応じて利用可能ツールを絞る。
6. LLM へ利用可能ツール一覧と説明を渡す。
7. LLM が単一 JSON オブジェクトでツール呼び出しを要求する。
8. Tool Parser が呼び出しを解析する。
9. Tool Registry が対象ツールを解決する。
10. Policy Engine が実行可否を判定する。
11. 必要に応じてユーザー確認を行う。
12. Tool Executor が実行する。
13. 結果を LLM コンテキストへ戻す。

この設計により、ユーザーが後付けしたツールであっても、CodeRenga Runtime の権限管理、確認、ログ、sandbox の制御下で実行される。

## 12. プロンプト方針

固定システムプロンプトはバイナリに組み込まない。

CodeRenga は外部ファイルからプロンプトを読み込む。

- システムプロンプトは実行時に外部Markdownから読む。`--init` 用テンプレートのみ単体配布のためバイナリへ埋め込む。
- ユーザーは system prompt を完全に差し替え可能。
- プロジェクトごとの指示ファイルを読み込める。
- エージェントモードごとの振る舞いも外部ファイルまたは config で定義する。

読み込み候補:

- `<binary-dir>/coderenga.d/prompts/default.md`
- `<binary-dir>/coderenga.d/modes/*.md`
- `.coderenga/system.md`
- `.coderenga/instructions.md`
- `.coderenga/modes/*.md`
- `CODERENGA.md`
- `AGENTS.md`

## 13. エージェントモード

エージェントモードは固定 enum ではなく、ユーザー定義可能なプロファイルとして扱う。

例:

- architect
- coder
- reviewer
- debug
- ask
- migration-planner
- security-reviewer

各モードは以下を定義できる。

- mode 名
- 説明
- 振る舞いプロンプト
- 使用モデル / profile
- 利用可能ツール
- 書き込み可否
- shell 実行可否
- MCP 使用可否
- plan-first 強制の有無


## 14. セッション履歴と SQLite DB

CodeRenga は、セッション履歴を JSONL ファイル中心ではなく SQLite DB に保存する。DB はデフォルトで `<binary-dir>/coderenga.d/coderenga.db` とする。

DB 化する対象:

- セッション情報
- user / assistant / tool / summary message
- ツール実行履歴
- shell 実行履歴
- MCP ツールキャッシュ
- 圧縮サマリ
- チェックポイント
- 監査ログ

DB 化しない対象:

- `config.json`
- `llm.json`
- `prompts/*.md`
- `modes/*.md`
- `tools.json`
- `mcp.json`
- Plugin Tool の実体ファイル

SQLite driver は、単体バイナリ配布を優先し、CGO 不要の `modernc.org/sqlite` を第一候補とする。

### 14.1 コンテキスト構築

DB には全履歴を保存するが、LLM には全履歴を渡さない。Context Builder は以下を組み合わせて LLM 入力を構築する。

```text
1. system prompt / mode prompt
2. project instructions
3. active summary from summaries table
4. recent messages from messages table
5. current user instruction
```

### 14.2 圧縮方針

圧縮は `summaries` table に保存する。raw message は削除しない。

- context 使用率が閾値を超えた場合に自動圧縮
- `/compact`
/session list
/session resume <id>
/session search <keyword>
/db status で手動圧縮
- `light / normal / hard` の圧縮レベルを持つ
- 圧縮プロンプトは `coderenga.d/prompts/compact.md` に置く
- 圧縮用 profile / model を別指定可能

### 14.3 no-persist

`--no-persist` の場合は `coderenga.db` に書き込まない。内部状態管理が必要な場合は in-memory SQLite を使用し、終了時に破棄する。

## 15. 複数モデル切り替え

複数モデルprofileは `coderenga.d/llm.json` に定義し、起動時の `--profile` / `--model` とREPLの `/profile` / `/model` で切り替える。

```json
{
  "version": 1,
  "defaultProfile": "local",
  "profiles": {
    "local": {"baseURL":"http://127.0.0.1:8080/v1","apiKey":"","model":"local-model","temperature":0.2,"maxTokens":4096}
  }
}
```

## 16. スラッシュコマンド

REPL では以下を提供する。

```text
/help
/exit
/mode <name>
/modes
/profile <name>
/model <model-name>
/status
/diff
/plan
/compact
/prompts
/reload-prompts
/mcp list
/mcp tools
/shell-policy
```

## 17. v0.1 スコープ

v0.1 に含めるもの:

- Go 製単体 CLI
- CLI/REPL と単発実行
- OpenAI 互換 API
- ローカルファイル操作
- shell 実行と 3 段階ポリシー
- MCP stdio client
- MCP HTTP/SSE client
- 外部プロンプト読み込み
- AGENTS.md / CODERENGA.md / `.coderenga` 指示読み込み
- ユーザー定義エージェントモード
- 複数モデルプロファイル
- SQLite DB によるセッション、ツール履歴、圧縮サマリ保存

v0.1 で後回しにするもの:

- フル TUI
- VSCode 拡張
- Docker sandbox
- 複数エージェント並列実行
- 高度な RAG





