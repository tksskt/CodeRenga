# CodeRenga Markdown 分割版 v0.8

このディレクトリは、Markdown ソースを 1 ファイル 600 行以内に収めるために分割した版です。v0.8 では DOCX/PDF は生成・同梱しません。

## ファイル構成

- `name_and_concept.md` - CodeRenga の名称由来、製品コンセプト、タグライン
- `basic_design.md` - 基本設計書
- `detail/01_core_config_prompt_modes.md` - アーキテクチャ、設定、プロンプト、モード、LLM、基本ツール
- `detail/02_session_storage_operations.md` - SQLite Storage、dry-run、実装フェーズ、非機能要件
- `detail/03_tool_extension_design.md` - ツール拡張、Plugin Tool、MCP統合、Policy、監査
- `detail/04_embedded_init_split_config_tool_calls.md` - 埋め込み初期化、分割設定、JSON Tool Call
- `implementation_status.md` - v0.8 実装フェーズ対応状況

## 方針

- Markdown は 1 ファイル 600 行以内を維持する。
- DOCX/PDF は生成しない。必要になった場合のみ別タスクとして生成する。
- 詳細設計を追加する場合は `detail/04_*.md` のように章単位で分割する。




