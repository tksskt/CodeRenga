# CodeRenga Usage Review Fix Plan 2026-06-27

## 目的

Codex から CodeRenga を実際に呼び出して修正計画に沿った実装を進めた際に発生した問題を整理し、CodeRenga 自体の改善計画として実装単位へ落とし込む。

今回の観察対象は、設計レビュー修正計画の Step 1 / Step 2 を CodeRenga に実装させた実行ログ相当の振る舞いである。

## 実際に発生した問題

### 1. ツール呼び出し形式が安定しない

CodeRenga は system prompt で JSON tool call を要求していても、時々 XML 風 tool tag や、JSON の前後に説明文を混ぜた出力を返した。

影響:

- Runtime が `legacy tag formats are not supported` として拒否する。
- ファイル編集まで進まず、同じ作業を複数回再投入する必要がある。
- 非対話実行の worker としての信頼性が下がる。

改善方針:

- tool call protocol を prompt だけに依存させず、モデル出力の検証エラーを次 turn の明示的な修正指示として返す。
- JSON tool call 以外を出した場合は、通常の最終回答として扱う前に repair turn を 1 回だけ挟む。
- 修復 turn でも失敗した場合は、失敗理由、直前出力、期待形式を含むエラーを返す。

### 2. 実装依頼を通常会話として扱うことがある

`implement`, `edit`, `add tests`, 対象ファイル、期待挙動を含む instruction を渡しても、CodeRenga が「何を実装しますか」と返すことがあった。

影響:

- 自動 worker として呼び出した場合に無駄な turn が発生する。
- `--stdin` 経由の長い指示が、具体的な実装タスクとして扱われないことがある。
- 親エージェントが追加 prompt 調整を繰り返す必要がある。

改善方針:

- instruction classifier を追加し、実装タスク、レビュー、通常会話を起動直後に分類する。
- 実装タスクに分類された場合、最初の応答は原則として `builtin.read_file` / `builtin.search_text` / `builtin.list_files` のいずれかの tool call に限定する。
- 対象ファイルや期待挙動が instruction に含まれる場合、「何をしますか」という質問を禁止する。

### 3. 同じ read_file を繰り返して停止する

対象ファイルを読んだ後、同じ `builtin.read_file` を連続で要求し、Runtime の repeated tool call guard で停止した。

影響:

- 読み取りまでは成功しているのに編集へ進めない。
- guard 自体は妥当だが、モデルへ「次に何をすべきか」が十分に返っていない。

改善方針:

- repeated tool call 検出時、単に停止するだけでなく、直前結果の要約と「次は別 tool を使うか最終回答せよ」という recovery prompt を 1 回返す。
- 同一 tool / 同一 arguments の再実行は、明示的な `force` 理由がない限り禁止する。
- tool result を短縮しすぎて、モデルが未読と誤認しないよう、読了済み marker を context に残す。

### 4. ドキュメント編集で失敗率が高い

Go コードとテストの編集は比較的成功したが、設計書や README の更新では旧 tool tag 出力や通常会話化が目立った。

影響:

- 設計優先ワークフローなのに、実装とドキュメントの同期が残りやすい。
- 親エージェントが手作業で文書更新するか、未完了リスクとして残す必要がある。

改善方針:

- `--mode documenter` または文書更新向け mode を追加する。
- Markdown 編集時の標準手順を mode prompt に持たせる。
- 既存ファイルの一部置換が必要な場合、`builtin.apply_patch` を優先させる。

### 5. ローカル prompt 調整が必要だった

CodeRenga を実装 worker として動かすため、ローカルの system prompt / coder mode prompt を強める必要があった。

影響:

- 初期テンプレートのままだと worker 用途で再現性が低い。
- ユーザー環境ごとの prompt 差分に依存しやすい。

改善方針:

- coder mode の初期 prompt に、非対話 worker としての実行規則を明記する。
- tool call 形式、実装タスクの開始条件、同一 tool 再実行禁止をテンプレートへ反映する。
- prompt 変更が実行結果へ影響するため、session fingerprint の対象に prompt hash を含める。

### 6. shell を含む危険文字列の instruction 取り扱いが難しい

`| sh`, `rm -rf /`, command substitution などを含む安全テスト指示は、PowerShell の引数として直接渡すとホスト shell 側で解釈される危険があった。

影響:

- 親エージェントが CodeRenga を呼び出す際に quote / escape を誤ると、CodeRenga ではなくホスト shell が先に解釈する。
- shell safety の検証指示自体が実行環境上のリスクになる。

改善方針:

- 長文 instruction や shell 構文を含む instruction は `--stdin` を推奨し、README と設計書に明記する。
- `--instruction-file <path>` のような instruction file 指定を追加し、shell quoting 依存を下げる。
- 実行ログには instruction の渡し方も記録する。

## 修正計画

### 基本方針

公開されているコーディングエージェントの実装を見ると、安定している実装は prompt だけでモデルを制御していない。LLM 出力の parse、validate、repair、policy、execution、observation、loop detection を Runtime 側の制御層として持っている。

CodeRenga も同じ方向へ寄せる。個別の prompt 強化を先に増やすのではなく、まず Runtime orchestration を次の形へ分解する。

```text
LLM output
  -> parse
  -> validate
  -> repair / recover if invalid
  -> classify action
  -> policy / inspection
  -> approval
  -> execute
  -> observation
  -> loop / stuck detection
  -> next turn
```

新しい責務分割の目安:

```text
internal/runtime/
  loop.go
  recovery.go
  transcript.go
  classifier.go
  tool_visibility.go

internal/policy/
  engine.go
  inspector.go
  safety.go
  repetition.go
```

参考にする実装パターン:

- Aider: malformed response を reflection / repair turn で修復する。
- OpenHands: action / observation 履歴と stuck detector を持つ。
- Cline: tool parameter error や連続 mistake を tool result として戻す。
- Goose: permission / security / repetition を inspector chain として扱う。
- Continue: tool call status と mode を分離する。
- Codex CLI: shell / patch / sandbox safety を runtime 側で判定する。
- SWE-agent / mini-swe-agent: replay 可能な action / observation transcript を重視する。

### Step 1: Runtime Recovery Layer

Status: implemented in the first slice on 2026-06-27.

Implemented scope:

- `MalformedToolCallError` for malformed tool-call parser failures.
- One-shot repair turn for malformed tool calls.
- One-shot task-start recovery when a concrete repository task receives a greeting or clarification stall instead of a tool call.
- Tests for malformed repair success, malformed repair failure, task-start recovery success, greeting pass-through, and task-start recovery failure.

Remaining scope is architectural extraction only: later steps may move this logic into `recovery.go` and connect it to transcript / stuck detection.

対象:

- `internal/runtime.RunInstruction`
- `internal/runtime/recovery.go`
- `internal/tools.ParseCalls`
- tool loop tests

実装:

- Runtime loop に recovery layer を追加する。
- ParseCalls が legacy tag / prose mixed JSON / malformed JSON を返した場合、即時終了せず、構造化された `MalformedToolCallError` として扱う。
- recovery layer は最大 1 回、期待 JSON 形式、失敗理由、直前出力の短縮版を含む修復 turn を LLM に返す。
- 実装タスクなのに greeting や追加質問で止まった場合も、recovery layer が 1 回だけ「読み取り tool から開始せよ」と返す。
- recovery 後も失敗した場合は、失敗理由と直前出力を含む明示エラーで停止する。

テスト:

- XML 風 tag 出力後、次 turn の正しい JSON tool call が実行される。
- JSON 前後に prose が混じった場合、修復 turn が入る。
- 実装指示に対して greeting が返った場合、修復 turn が入り tool call へ進む。
- 修復 turn でも不正な場合はエラーになる。
- 通常の最終回答は修復対象にしない。

### Step 2: Action / Observation Transcript

対象:

- `internal/runtime/transcript.go`
- `internal/storage`
- tool loop tests

実装:

- LLM 出力、tool call、tool result、policy decision、approval、error、recovery を action / observation として正規化する。
- 既存の message / tool_runs に加えて、Runtime 内部で replay 可能な transcript を保持する。
- transcript entry には turn、action kind、tool name、arguments hash、result summary、error kind、policy level を含める。
- 永続化は既存 DB を使う。初期段階では in-memory transcript だけで loop detection と final error reporting に利用してもよい。

テスト:

- tool call と result が transcript に記録される。
- parse error と recovery turn が transcript に記録される。
- final error に直近の action / observation summary が含まれる。

### Step 3: Loop / Stuck Detector

対象:

- `internal/runtime/recovery.go`
- `internal/policy/repetition.go`
- tool loop tests

実装:

- OpenHands 型の stuck detection を小さく導入する。
- 同一 tool / 同一 arguments / 同一 result の連続、同一 assistant text の連続、同一 error の連続を検出する。
- 初回検出では即時エラーにせず、1 回だけ recovery turn を返す。
- recovery turn には「直前 action は完了済み」「次は別 tool、編集 tool、または最終回答を出す」という制約を含める。
- recovery 後も同じ loop pattern が続く場合はエラーで停止する。

テスト:

- `read_file A` の連続後、次 turn の `write_file A` が実行される。
- recovery 後も `read_file A` ならエラーになる。
- 同じ assistant text の連続が loop として検出される。
- 同じ tool error の連続が loop として検出される。

### Step 4: ToolCallStatus と Tool Visibility

対象:

- `internal/runtime/tool_visibility.go`
- `internal/tools`
- `internal/runtime/system_prompt.go`
- runtime tests

実装:

- Continue / Cline 型の tool call state を導入する。
- 状態は `generated`, `validated`, `blocked`, `awaiting_approval`, `running`, `done`, `failed`, `canceled` を基本とする。
- system prompt の Available tools は、Registry に存在し、enabled で、mode / policy により block されていない tool のみを表示する。
- 表示上の available tool と実行時に許可される tool の不一致をなくす。

テスト:

- `tool_deny: builtin.write_file` の mode では system prompt から `builtin.write_file` が消える。
- disabled tool は system prompt に出ない。
- block された tool を LLM が要求した場合、ToolCallStatus が `blocked` になる。

### Step 5: Policy Inspector Chain

対象:

- `internal/policy/engine.go`
- `internal/policy/inspector.go`
- `internal/policy/safety.go`
- `internal/runtime`
- existing policy tests

実装:

- Goose / Codex CLI 型の policy inspector chain を導入する。
- mode policy、tool_policy、shell_policy、plugin manifest policy、available_modes、sandbox requirement、repetition、safety check を inspector として分離する。
- 最終判定は `Reject > AskUser > Unknown > AutoApprove` に寄せる。外部表示は既存互換として `block > confirm > unknown > allow` を維持してよい。
- 各 inspector は decision と reason を返す。
- `/tool-policy` と audit log に reason chain を出せるようにする。

テスト:

- mode が block した場合、tool_policy allow では緩和されない。
- shell_policy block は shell_mode true でも緩和されない。
- plugin available_modes の不一致は Reject になる。
- decision reason chain が取得できる。

### Step 6: SafetyCheck と Shell / Patch Safety

対象:

- `internal/policy/safety.go`
- `internal/tools/shell`
- `internal/tools/builtin`
- runtime tests

実装:

- Codex CLI 型の `SafetyCheck` を導入する。
- 内部判定は `AutoApprove`, `AskUser`, `Reject` とし、既存の allow / confirm / block へ bridge する。
- writable root 外の write / patch は Reject にする。
- sandbox が利用できない dangerous operation は AutoApprove しない。
- shell.run は argv direct execution を標準とし、shell_mode は AskUser 以上にする。
- stdout / stderr output cap、timeout、cancellation を明示的な safety policy に含める。

テスト:

- writable root 外への patch は Reject。
- shell_mode true は AskUser 以上。
- block rule は shell_mode true でも Reject。
- output cap を超える shell output は切り詰められる。

### Step 7: Instruction Classifier

対象:

- `internal/runtime/classifier.go`
- runtime tests

実装:

- instruction を `implementation`, `review`, `document`, `conversation`, `unknown` に分類する軽量 classifier を追加する。
- 日本語と英語の主要語を対象にする。
- classifier は tool 実行を直接決めない。Runtime Recovery Layer が「実装タスクなのに tool call が出ない」ケースを判断するための入力として使う。

テスト:

- 「実装して」「修正して」「テストを追加して」は implementation。
- 「レビューして」は review。
- 「設計書を更新して」「README を直して」は document。
- greeting は conversation。

### Step 8: Worker Prompt Template Hardening

対象:

- `templates/coderenga.d/prompts/default.md`
- `templates/coderenga.d/modes/coder.md`
- template tests

実装:

- Runtime 側の recovery / classifier / policy を補助する最小限の prompt 強化を行う。
- JSON tool call 形式を厳格に明記する。
- 実装タスクでは最初に read/list/search のいずれかを使うことを明記する。
- 同一 tool / 同一 arguments の連続実行を避けることを明記する。
- 具体的な実装範囲が与えられている場合、追加質問せず作業開始することを明記する。

テスト:

- テンプレートに tool call protocol と implementation-start rule が含まれる。
- coder mode に worker 用の実行規則が含まれる。

### Step 9: Instruction File Input

対象:

- CLI options
- app runner
- README / docs

実装:

- `--instruction-file <path>` を追加する。
- `--stdin` と同様に、shell quoting に依存せず長文 instruction を渡せるようにする。
- `--instruction-file` と positional instruction が両方ある場合は結合順を明示する。

テスト:

- instruction file の内容で RunInstruction が呼ばれる。
- 存在しない file は明示エラーになる。
- stdin / positional instruction との結合順が安定する。

### Step 10: Documenter Mode

対象:

- `templates/coderenga.d/modes/documenter.md`
- prompt manager tests
- docs

実装:

- Markdown / README / 設計書更新用の `documenter` mode を追加する。
- `write: allow`, `shell: allow_readonly`, `tool_allow` は read/list/search/write/apply_patch/git.diff 程度に限定する。
- 文書更新時は既存文脈を読み、局所 patch を優先することを mode prompt に明記する。

テスト:

- `--init` テンプレートに documenter mode が含まれる。
- documenter mode の tool_allow が読み込まれる。

## 優先順位

1. Step 1 Runtime Recovery Layer
2. Step 2 Action / Observation Transcript
3. Step 3 Loop / Stuck Detector
4. Step 4 ToolCallStatus と Tool Visibility
5. Step 5 Policy Inspector Chain
6. Step 6 SafetyCheck と Shell / Patch Safety
7. Step 7 Instruction Classifier
8. Step 8 Worker Prompt Template Hardening
9. Step 9 Instruction File Input
10. Step 10 Documenter Mode

理由:

- 公開実装では、安定性の核は prompt ではなく runtime recovery / transcript / stuck detection にある。
- recovery と transcript がないまま policy や prompt を増やすと、失敗理由を追跡できず、同じ問題が再発する。
- Tool visibility と policy inspector chain は、mode 別 tool 制限と plugin / MCP 拡張の土台になる。
- Instruction classifier と prompt hardening は runtime 制御層を補助する位置づけに下げる。
- instruction file input と documenter mode は重要だが、まず制御層が安定してから導入する。

## 完了条件

- Runtime loop が parse / validate / repair / policy / execute / observation / stuck detection の段階を持つ。
- action / observation transcript から直近の失敗理由を説明できる。
- 実装タスクを渡したとき、CodeRenga が greeting や追加質問で止まらず、読み取り tool call から開始する。
- 不正 tool call 形式を 1 回は自動修復できる。
- 同一 read_file の繰り返しから 1 回は復帰できる。
- system prompt の Available tools と実行時 policy が一致する。
- policy decision に reason chain が残る。
- shell / patch safety が `AutoApprove / AskUser / Reject` として判定される。
- Markdown / README 更新を専用 mode で実行できる。
- shell 構文を含む危険な検証指示を instruction file 経由で安全に渡せる。
- 上記の挙動が自動テストで確認できる。

## Implementation status 2026-06-27

Implemented through Step 10 as a first working slice.

- Step 1 Runtime Recovery Layer: implemented malformed tool-call repair and task-start recovery.
- Step 2 Action / Observation Transcript: added in-memory runtime transcript entries for LLM output, parse errors, recoveries, tool calls, and tool results.
- Step 3 Loop / Stuck Detector: added one-shot repeated tool-call recovery before failing on repeated loops.
- Step 4 ToolCallStatus and Tool Visibility: added runtime tool-call status records and filtered system prompt available tools by enabled state and mode block policy.
- Step 5 Policy Inspector Chain: added `internal/policy` engine, inspector interface, decision aggregation, and reason chain tests.
- Step 6 SafetyCheck and Shell / Patch Safety: added `SafetyCheck` plus shell output capping and tests.
- Step 7 Instruction Classifier: added lightweight concrete-task and stall detection used by task-start recovery.
- Step 8 Worker Prompt Template Hardening: updated default and coder mode templates with stricter worker/tool-call rules.
- Step 9 Instruction File Input: added `--instruction-file` parsing and app-runner integration.
- Step 10 Documenter Mode: added embedded `documenter` mode and template/init tests.

Notes:

- This is the first integrated slice. Later refactoring can move inline runtime helpers into `recovery.go`, `classifier.go`, and `tool_visibility.go` without changing behavior.
- The policy package is present as a foundation; deeper integration into every executor decision can be done in the next hardening pass.
## Follow-up completion 2026-06-27

The items called out as incomplete after the first slice were addressed:

- Design examples in `docs/detail/01_core_config_prompt_modes.md` and `docs/detail/03_tool_extension_design.md` now use canonical `tool_policy` / `shell_policy` rather than legacy `policies`.
- `Registry.Replace` no longer overwrites an existing tool name and now returns a duplicate error.
- Sessions are created with config and prompt fingerprints, and resume warns when the current fingerprints differ from the stored session values.
- Messages store `token_estimate`, and auto compaction considers `trigger_context_ratio` using uncompacted token estimates before falling back to turn-count compaction.
- `internal/policy` is connected to the real executor decision path as a policy engine layer with SafetyCheck integration.

Remaining architectural cleanup:

- The recovery / classifier / visibility helpers can still be split into their planned files (`recovery.go`, `classifier.go`, `tool_visibility.go`) without behavior changes.
- Policy reason chains are computed in the executor path; a later UI pass can expose the full chain through `/tool-policy` output.