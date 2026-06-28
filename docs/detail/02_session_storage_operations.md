# CodeRenga 詳細設計書 v0.8 - 02 Session / Storage / Operations

## 12. セッション管理と SQLite Storage

セッションは JSONL ファイルではなく `<binary-dir>/coderenga.d/coderenga.db` に保存する。通常起動で `coderenga.d/` が存在しない場合は DB を作成せず、永続化なしで動作する。`--init` 時に `coderenga.db` を作成し、schema migration を実行する。

保存対象:

- sessions: セッション単位のメタデータ
- messages: user / assistant / tool / summary の全メッセージ
- tool_runs: Built-in / MCP / Plugin Tool の実行履歴
- shell_runs: shell.run の実行履歴
- summaries: 圧縮サマリ
- checkpoints: 圧縮や重要操作時点のスナップショット
- mcp_tools_cache: MCP tool schema のキャッシュ
- audit_logs: 承認、拒否、ブロック、エラーなどの監査イベント

プロンプトファイルや config が変更された場合、再開時に fingerprint 差分を検出して警告を表示する。config fingerprint は構造と非 secret 値を対象にし、`apiKey`、`token`、`secret`、`password` などの secret 値は値そのものではなく presence と field name のみを反映する。

### 12.1 DB 配置

```text
<binary-dir>/
  coderenga
  coderenga.d/
    config.json
    prompts/
      default.md
      compact.md
    modes/
      architect.md
      coder.md
      reviewer.md
      debug.md
    tools.json
    mcp.json
    coderenga.db
```

`--state-dir <dir>` が指定された場合は `<dir>/coderenga.db` を使用する。`--no-persist` の場合は既存の永続DBを開かず、in-memory SQLiteを使用する。実行前後で永続DBのLengthとLastWriteTimeを変更しない。

### 12.2 SQLite driver

Go の SQLite driver は、単体バイナリ配布とクロスビルド容易性を優先し、CGO 不要の `modernc.org/sqlite` を第一候補とする。CGO 前提の `github.com/mattn/go-sqlite3` は互換実装候補に留める。

### 12.3 schema 概要

```sql
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  project_path TEXT NOT NULL,
  project_hash TEXT NOT NULL,
  title TEXT,
  active_mode TEXT,
  active_profile TEXT,
  status TEXT NOT NULL,
  config_fingerprint TEXT,
  prompt_fingerprint TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content TEXT NOT NULL,
  token_estimate INTEGER,
  compacted INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  FOREIGN KEY(session_id) REFERENCES sessions(id)
);

CREATE TABLE tool_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  message_id INTEGER,
  tool_name TEXT NOT NULL,
  namespace TEXT NOT NULL,
  arguments_json TEXT,
  result_summary TEXT,
  result_full TEXT,
  status TEXT NOT NULL,
  policy_decision TEXT,
  approved INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  FOREIGN KEY(session_id) REFERENCES sessions(id),
  FOREIGN KEY(message_id) REFERENCES messages(id)
);

CREATE TABLE shell_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  command TEXT NOT NULL,
  cwd TEXT NOT NULL,
  exit_code INTEGER,
  stdout_summary TEXT,
  stderr_summary TEXT,
  stdout_full TEXT,
  stderr_full TEXT,
  policy_level TEXT NOT NULL,
  approved_by_user INTEGER NOT NULL DEFAULT 0,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  FOREIGN KEY(session_id) REFERENCES sessions(id)
);

CREATE TABLE summaries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  level TEXT NOT NULL,
  content TEXT NOT NULL,
  source_from_message_id INTEGER,
  source_to_message_id INTEGER,
  active INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  FOREIGN KEY(session_id) REFERENCES sessions(id)
);

CREATE TABLE checkpoints (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  summary_id INTEGER,
  git_head TEXT,
  changed_files_json TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY(session_id) REFERENCES sessions(id),
  FOREIGN KEY(summary_id) REFERENCES summaries(id)
);

CREATE TABLE mcp_tools_cache (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  server_name TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  schema_json TEXT,
  description TEXT,
  updated_at TEXT NOT NULL,
  UNIQUE(server_name, tool_name)
);

CREATE TABLE audit_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT,
  event_type TEXT NOT NULL,
  event_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);
```

### 12.4 Context Builder

DB には全履歴を保存するが、LLM へは全履歴を渡さない。毎 turn の context は以下の順で構築する。

```text
1. system prompt / mode prompt
2. project instructions
3. summaries.active = 1 の active summary
4. messages から直近 keep_recent_turns 分
5. 現在の user instruction
```

### 12.5 圧縮

圧縮は古い messages を削除せず、`summaries` に構造化要約を追加する。圧縮後、対象 message は `compacted = 1` として扱い、通常 context からは除外する。`/compact light|normal|hard` により手動圧縮できる。自動圧縮は `config.json` の `compact.level` を使用する。未指定時は `normal` を使うが、未知の level は起動時設定エラーにする。level は `light` / `normal` / `hard` の固定3段階のみで、custom level はサポートしない。`compact.levels` は固定3段階それぞれの `target_tokens` 上書きにだけ使い、未知キーは設定エラーにする。各 level の `target_tokens` は context window ではなく、要約生成プロンプトまたは圧縮戦略へ渡す目標長として扱う。

圧縮トリガー:

- context 使用率が `trigger_context_ratio` を超過
- turn 数が `trigger_turns` を超過
- ユーザーが `/compact` を実行

### 12.6 検索と再開

DB 化により以下をサポートする。

```text
/session list
/session resume <id>
/session search <keyword>
/summary show
/db status
```

## 13. dry-run

`--dry-run` 時はファイルを書き換えない。

- `builtin.write_file` / `builtin.apply_patch` の変更案は DB の `tool_runs` / `audit_logs` に保存し、必要に応じて `<state-dir>/dry-run/` にファイル出力する
- project 配下 `.coderenga/patches` は明示指定がない限り作成しない
- shell 実行は原則禁止、または read-only allow のみ許可

## 14. 実装フェーズ

### Phase 1: CLI と config

- 引数 parsing
- config 読み込みとマージ
- profile 読み込み
- mode 読み込み

### Phase 1.5: SQLite Storage

- `coderenga --init` 時の `coderenga.db` 作成
- schema migration
- sessions / messages / summaries / tool_runs / shell_runs / audit_logs DAO 実装
- `--no-persist` 時の in-memory DB または永続化無効動作
- `/db status`, `/session list`, `/session resume`, `/session search`

### Phase 2: LLM 接続

- OpenAI 互換 Chat Completions
- streaming SSE
- strict JSON Tool Call parser

### Phase 3: Prompt Builder

- 外部 system prompt 読み込み
- AGENTS.md / CODERENGA.md 読み込み
- mode prompt 読み込み
- `/reload-prompts`

### Phase 4: Built-in tools

- builtin.read_file
- builtin.write_file
- builtin.apply_patch
- builtin.list_files
- builtin.search_text
- git.status
- git.diff

### Phase 5: shell policy

- allow / confirm / block / unknown
- config パターン判定
- approval UI
- timeout

### Phase 6: MCP

- stdio transport
- HTTP/SSE transport
- tools/list
- tools/call
- Tool Registry bridge

### Phase 7: REPL 拡張

- `/mode`
- `/profile`
- `/model`
- `/prompts`
- `/mcp tools`
- `/shell-policy`

## 15. 主要な非機能要件

- 起動が速いこと
- 単体バイナリで配布できること
- ローカルLLMでも破綻しにくいこと
- ファイル破壊を防ぐこと
- shell 実行をユーザーが制御できること
- プロンプトとモードをユーザーが自由に差し替えられること
- MCP サーバー追加時に本体の再ビルドが不要であること
