# CodeRenga

[English](README.md) | 日本語

CodeRenga は、`docs/` にある v0.8 設計をもとに実装された、軽量な Go 製 CLI コーディングエージェントです。

## 名前とコンセプト

CodeRenga という名前は、日本の連歌に由来します。連歌は一人の詩人だけで完成するものではありません。参加者は前の句を受け取り、その文脈を保ちながら次の句をつないでいきます。

CodeRenga は、その考え方をソフトウェア開発に応用します。クラウド LLM がアーキテクチャや方向性を考え、ローカル LLM がその意図を受け取って実装し、ツール群が差分、実行、検証の流れをつなぎます。すべてを一つの AI に任せるのではなく、複数の知能と実行環境を連携させ、一句ずつコードを形にしていくための仕組みです。

Cloud LLM thinks. Local LLM builds. CodeRenga links the verses.

## 開発

ローカルでは Go 1.26.4 を使用し、`go.mod` では `go 1.25.0` を宣言しています。各スクリプトは `.local/go/bin` を優先して使用し、モジュールキャッシュとビルドキャッシュは `.local/cache/` 配下に保持します。

```sh
make setup
make fmt
make lint
make test
make build
```

ビルドされたバイナリは `.local/bin/coderenga.exe` に出力されます。初期化テンプレートは実行ファイルに埋め込まれているため、実行時に外部の `templates` ディレクトリは不要です。

## Windows アプリケーションアイコン

Windows アイコンのソースは `assets/CodeRenga.ico` です。固定バージョンの `rsrc v0.10.2` により `cmd/coderenga/rsrc_windows_amd64.syso` が生成され、Go が Windows amd64 向け実行ファイルに自動でリンクします。

アイコンを変更した後は、リソースを再生成してください。

```sh
make windows-resource
# または
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/generate-windows-resources.ps1
```

生成済みの `.syso` はリポジトリにコミットされています。そのため、直接ビルドしてもアイコンは含まれます。

```sh
go build -o coderenga.exe ./cmd/coderenga
```

`_windows_amd64.syso` という suffix により、このリソースは Linux や macOS 向けビルドには含まれません。

## 使い方

```powershell
.\.local\bin\coderenga.exe --help
.\.local\bin\coderenga.exe --init
.\.local\bin\coderenga.exe --cwd . "inspect this repository"
.\.local\bin\coderenga.exe --cwd . --no-persist
.\.local\bin\coderenga.exe --mode coder --non-interactive "implement the requested change"
```

`coderenga.d/` をバイナリの隣に作成するのは `--init` のみです。明示的に `--state-dir` を指定した場合は、SQLite の状態データベースが作成されることがあります。`--no-persist` を指定した場合は、常にインメモリ SQLite が使用されます。

`--init` は `coderenga.d/` 配下に分割されたランタイム設定を作成します。作成される主なファイルは `config.json`、`llm.json`、`mcp.json`、`tools.json`、外部プロンプト、モード定義、`coderenga.db` です。

ランタイムは以下をサポートします。

* OpenAI 互換のストリーミング / 非ストリーミング chat completions
* SQLite によるセッション管理とコンパクション
* 完全修飾名を持つ built-in / shell / git / MCP / plugin ツール
* ポリシー集約
* cwd サンドボックス
* dry-run
* MCP stdio および HTTP/SSE
* plugin の soft / hard サンドボックス要件

## REPL コマンド

主な REPL コマンドは以下の通りです。

```text
/mode <name>              /profile <name>          /model <name>
/prompts                  /reload-prompts          /status
/db status                /session list            /session resume <id>
/session search <text>    /compact light|normal|hard
/mcp list                 /mcp tools               /tools [namespace]
/tool info <name>         /tool enable <name>      /tool disable <name>
/tool reload              /tool-policy             /exit
```

ツール実行では、以下のような完全修飾名を使用します。

```text
builtin.read_file
shell.run
git.diff
mcp.<server>.<tool>
plugin.<name>
```

ポリシー判定は次の優先順位で集約されます。

```text
block > confirm > unknown > allow
```

下位レイヤーが、上位レイヤーのより厳しい判定を弱めることはできません。

ツール呼び出しは、`tool` と `arguments` を含む一つの JSON オブジェクトとして扱われます。XML 風のタグは実行されません。ツール結果は、モデルが最終回答を生成するまでモデルへ返されます。

`--dry-run` では読み取り専用ツールは実行されますが、ファイル書き込み、パッチ適用、シェルコマンド、plugin、MCP 呼び出しは実行されず、実行予定として報告されます。

dry-run のツール結果では `executed=false` が明示されます。モデルがそれと矛盾する主張をした場合、その内容は最終回答として表示されません。

挨拶にはツールを使わずに応答します。連続して同一のツール呼び出しが発生した場合は、ツール名、引数、前回の結果を表示して停止します。8 ターンの上限に達した場合は、呼び出し履歴を報告します。

`--no-persist` はインメモリ SQLite のみを使用し、設定されたデータベースファイルには触れません。

初期モードでは、以下の書き込みポリシーが設定されています。

```text
coder              write:allow
debug              write:confirm
architect/reviewer write:false
```

ファイルを変更するツールは、引き続き cwd サンドボックスと `tools.json` によって制約されます。

`--non-interactive` は許可済みの操作を実行しますが、確認が必要な操作はプロンプト表示や自動承認を行わずに失敗します。

## ライセンス

MIT License です。詳細は [LICENSE](LICENSE) を参照してください。