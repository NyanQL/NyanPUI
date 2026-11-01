# v0.0.15: MCP公開とAPI定義の動的更新、実行・ログ処理の改善

## 概要

既存の通常APIをMCP Toolとして公開できるようにし、Streamable HTTPとstdioの両方に対応します。API定義の多段includeとホットリロードを追加し、稼働中のAPI・静的ファイル配信・スケジュール・WebSocketクライアントへ設定変更を反映できるようにします。

あわせて、リクエスト間の情報混在、通常HTTPの応答形式によるPushの実行漏れ、JSON-RPCの公開対象と応答形式を改善します。ログをNyan8・NyanQLと同じJSON形式に統一し、依存関係と既存の回帰テストを更新します。

## 主な変更

### MCPサーバーとOAuth連携

- `type: "mcp"` を追加し、既存APIを `tools` から参照して公開できるようにします。複数のMCPサーバー定義と、`streamable_http` / `stdio` に対応します。
- MCP protocol version `2025-11-25` / `2025-06-18`、`initialize`・`ping`・`tools/list`・`tools/call` をサポートします。Toolの説明、annotations、security schemes、入出力スキーマをAPI定義から取得します。
- `paramCheck` / `outCheck` の静的なスキーマ定義を抽出し、MCP Toolの引数と結果をJSON Schemaで検証します。MCPではチェックスクリプト自体やPushを実行せず、Toolの実行結果を `content` / `structuredContent` として返します。
- Streamable HTTPではOrigin、Content-Type / Accept、protocol version、予約パラメータなどを検証し、MCP専用のペイロード上限・接続元別レート制限・同時実行制御を設けます。設定変更で不要になった同時実行制御のデータは、使用中の処理が終了してから回収します。
- OAuthのMetadata生成、認可・トークン・登録・アクセストークン検証APIへのJavaScript hook呼び出しを追加します。認証、PKCE、認可コードの消費、トークンの発行・検証、ユーザー管理のポリシーはJavaScript側が担当します。
- OAuth向けの状態ファイル操作とArgon2idの補助関数を提供します。状態ファイル一覧ではNyanPUIが作成する一時ファイルだけを除外し、Argon2idは生成用の標準設定と検証可能な設定を分離して、将来の設定変更後も既存ハッシュを検証できる構造にします。
- `--mcp-server` によるstdio専用起動を追加します。このモードでは標準出力をJSON-RPC応答専用にし、HTTP listener、OAuth、バックグラウンド処理、ホットリロードを起動しません。

### API定義の分割・再読み込み・スキーマ公開

- `type: "include"` による多段のAPI定義読み込みに対応します。マウント名をAPI名に付加し、各定義内のスクリプト・HTML・チェック・public・includeのパスを、その定義ファイルの場所から解決します。
- includeの循環参照、API名の衝突、重複JSONキー、不正なMCP・schedule・ws_client定義などを検証します。起動時は定義全体を検証し、更新時に不正な変更を検出した場合は直前の正常な設定を維持します。
- 1回のリクエストでは同じ設定スナップショットを使用します。HTTP、public、WebSocket受信、JSON-RPC、内部呼び出し、Pushに加えて、schedule / ws_clientの追加・更新・削除を反映します。
- ws_clientのscriptやdescriptionだけの変更では接続を維持します。また、接続成功後に再接続待ち時間を1秒へ戻します。
- `/nyan/{API名}` による詳細取得を追加します。`nyanInputSchema` / `nyanOutputSchema` をチェックスクリプトのASTから取得し、旧 `nyanAcceptedParams` も入力スキーマとして扱います。

### JSON-RPC・通常HTTP・WebSocketの実行処理

- `/nyan-rpc` から実行できる対象を、スクリプトを持つ通常APIに限定します。MCPのOAuth役割に参照されるAPIは公開対象から除外します。
- JSON-RPCでも `outCheck` を実行し、チェック拒否をHTTP 200のJSON-RPCエラー `-32000` として返します。リクエストの `id` と元のチェック結果を保持し、チェック結果は `error.data` に格納します。
- JSON-RPCの成功時は、オブジェクト・配列・数値・真偽値の型を保持します。文字列は自動解析せず、`null` / `undefined` は明示的な `result: null` として返します。JSON変換や応答変換に失敗した場合は `-32603` を返し、Pushを実行しません。
- 通常HTTPでは、文字列・オブジェクト・空応答・HTMLのみのいずれも、チェック通過後の応答ステータスが200〜399ならPushを実行します。4xx/5xx、チェック拒否、実行・応答変換の失敗、`checkOnly` では実行しません。
- WebSocket接続確立前に接続先APIの `paramCheck` を実行し、受信したAPI要求にも入出力チェックを適用します。`nyanGetRequestHeaders()` を追加し、JavaScript側でOriginなどを判定できるようにします。
- HTTPコンテキストをJavaScriptの実行単位で渡し、Cookieや内部呼び出しで別リクエストの情報を参照する競合を解消します。WebSocketへの応答とPushの書き込みも接続ごとに直列化します。

### JavaScript補助関数とファイル操作

- `nyanSaveFile` / `nyanDeleteFile`、乱数・SHA-256・Base64の補助関数を追加します。ファイル保存は同じディレクトリの一時ファイルからatomic renameする方式です。
- `nyanGetFile` / `nyanSaveFile` / `nyanDeleteFile` / `nyanReadFileB64` の相対パスを、最上位のAPI定義ファイルがあるディレクトリに統一します。多段includeの呼び出し元によらず同じ基準を使い、絶対パスも使用できます。
- プロセス内の共有メモリを明示的に削除する `nyanRemoveItem` を追加します。保持期間や削除の判断はJavaScript側で行います。
- READMEに、API定義・JavaScriptを信頼して実行する前提とファイル権限の管理を明記します。相対パスの基準はアクセス範囲の制限ではなく、OSの権限と実行環境の範囲でコマンド実行やファイル操作を行います。

### ログ・依存関係・保守性

- ログを1行1件のJSON形式に統一し、`time`・`level`・`msg` と処理別の情報を記録します。`log.Level` に `debug` / `info` / `warn` / `error` を追加します。
- `log.EnableLogging: true` ではファイルへの追記とローテーション、`false` では標準エラーへの出力を行います。起動失敗、panic、TLSなどのHTTP内部エラーも同じログ方針で扱います。
- 通常のログではエラー型などを記録し、エラー詳細やJavaScriptの `console.log` はdebug時だけ出力します。詳細は各4096バイトまでとし、接続URLから資格情報・パス・クエリを除きます。
- 本体の既定バージョンを `v0.0.15`、Go指定を `1.26.6` に更新します。`config.json` の `version` はアプリ側のバージョンとして扱います。
- `golang.org/x/crypto` を `v0.14.0` から `v0.56.0` に更新し、Dependabotが要求する `v0.52.0` 以上を満たします。`x/net`・`x/sys`・`x/text`・protobufなども更新し、JSON Schema検証ライブラリを追加、未使用のv8go依存を削除します。
- 未使用の旧ハンドラーや重複した経路を削除し、テストを現在の実行経路に合わせます。MCPの入口判定で不要だったソートと、ログの原因エラー取得の重複も整理します。

### ワイヤーフレームサンプルの削除

- `html/css/wf_style.css` と `html/wf_html.html` を削除します。
- 同梱の `api.json` から `wf` 定義を削除し、READMEのプレビュー説明も取り除きます。

## 互換性と移行時の確認事項

| 対象 | 変更と確認事項 |
| --- | --- |
| API定義のホットリロード | `APIHotReload` を省略した場合も有効、確認間隔は1秒です。無効にする場合は `Enabled: false` を指定します。JavaScriptは実行ごとに読み込むため、この指定でスクリプトの内容が固定されるわけではありません。 |
| 起動時の設定検証 | 不正なschedule / ws_client定義も起動エラーになります。一部のバックグラウンド処理だけ失敗したままHTTPを起動する動作から変更されます。 |
| 既存のファイル読み込み | `nyanGetFile` は実行ファイル基準、`nyanReadFileB64` はカレントディレクトリ基準から、最上位API定義のディレクトリ基準へ変わります。配置が異なる場合は相対パスを調整するか、絶対パスを指定してください。 |
| JSON-RPCの成功結果 | 数値・真偽値・配列・オブジェクトなどが文字列からJSON本来の型に変わります。`JSON.stringify(...)` を返す既存スクリプトの文字列は維持され、クライアントの `JSON.parse(response.result)` もその構成では引き続き使用できます。 |
| JSON-RPCとHTTP応答用オブジェクト | `{status, headers, contentType, body}` を返す場合、JSON-RPCではオブジェクト全体が `result` になります。`body` の抽出や、そのstatus・headersのHTTP応答への適用は行いません。 |
| JSON-RPCの呼び出しとチェック | 公開対象外のAPIは `-32601` になります。`outCheck` が実行されるようになり、チェック拒否はHTTPステータスだけでなくJSON-RPCの `error` / `error.data` で判定する必要があります。 |
| ログ収集 | 出力形式がJSONに変わります。`EnableLogging: false` はログ無効化ではなく標準エラー出力です。エラー詳細や `console.log` を確認する場合は `Level: "debug"` を指定します。 |
| API一覧と予約パラメータ | `/nyan` は通常APIを列挙し、public / ws_clientは一覧から除外されます。`mcp_principal` / `mcp_tool` は予約名となり、外部リクエストでの指定を拒否します。 |
| ワイヤーフレームサンプル | 同梱サンプルからCSS・HTML・`/wf` のAPI定義を削除します。既存ページで `wf_style.css` や `/wf` へのリンクを利用している場合は、スタイルや参照先を更新してください。 |

MCP/OAuthの設定方法とJavaScript hookの仕様はREADMEに記載しています。認証・トークン検証・ユーザー管理は、利用するアプリケーション側で実装します。

## 検証

検証環境はGo `1.26.6`、macOS / arm64です。以下のコード検証は `656d5ef` で成功しており、比較対象のHEAD `6876cb5` までGoコード・テスト・依存関係の変更はありません。その後の変更はREADMEの更新とワイヤーフレームサンプルの削除です。依存関係の整合性も確認済みです。

| 確認内容 | 結果 |
| --- | --- |
| `go test -race -shuffle=on -count=1 -timeout=90s ./...` | 成功 |
| `go vet ./...` | 成功 |
| `go build -o /private/tmp/nyanpui-jsonrpc-typed-results .` | 成功 |
| `go mod verify` | `all modules verified` |
| ワイヤーフレームサンプル削除 | `api.json` のJSON構文、`wf` 以外の定義が維持されていること、実装・設定・READMEに削除済みサンプルへの参照が残っていないことを確認 |

`main_test.go` は、API定義の読み込み・更新、設定の世代整合性、通常HTTPのチェックとPush、JSON-RPCの公開範囲と型保持、MCPのHTTP/stdio、OAuth hookと状態保存、Argon2の既存ハッシュ互換性、WebSocketの並行送信、共有ストレージ、ログの秘匿・ローテーション・実プロセス出力などを拡充しています。ローカルの未管理サンプルへの依存をなくし、一時ファイルと実際の実行経路で確認します。

## 比較対象

- 作成日時：2026-09-20 18:02:20 JST
- 更新日時：2026-09-20 18:35:21 JST
- マージ先：`main` — `5c3f3ce94eab54aeed6202193d2dd8c094b912c1`
- マージ元：`feature/v0.0.15` — `6876cb56091afbd6f55c155928c75dfbb782e32d`
- 比較方法：`git diff main...HEAD`
- 差分規模：9ファイル、7,443行追加・1,276行削除
- 対象ファイル：`README.md`、`api.json`、`config.json`、`go.mod`、`go.sum`、`html/css/wf_style.css`（削除）、`html/wf_html.html`（削除）、`main.go`、`main_test.go`

上記はコミット済みの実装差分です。このPR文書自体や、作業ディレクトリ内の未管理ファイルは差分規模に含めていません。
