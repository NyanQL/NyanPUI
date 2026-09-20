# NyanPUI

NyanPUI(にゃんぷい)は、GoLangで作られたサーバーサイドレンダリングを行うプレゼンテーションフレームワークです。 リクエストに対してJavaScriptを実行してHTMLを出力します。

* **JavaScript エンジン**: Goja (ECMAScript 5.1 準拠) – [https://github.com/dop251/goja](https://github.com/dop251/goja)
* **双方向通信**: gorilla/websocket による WebSocket
* **プッシュ通知**: 特定エンドポイントの処理結果を WebSocket で配信
* **定期実行**: `type: "schedule"` による cron 形式の JavaScript ジョブ
* **MCP**: Streamable HTTP、Tool公開、OAuth 2.0 Authorization Code + PKCEに対応
* **CORS**: 通常APIは従来どおり全Originを許可し、MCPは設定したOriginだけを許可

## ライセンス

MIT ライセンスです。詳細は [LICENSE.md](LICENSE.md) を参照してください。

## 動作環境

* OS: Windows / macOS / Linux
* 必要に応じて TLS 証明書（.crt）と秘密鍵（.key）を用意してください。

## ディレクトリ構成

```plaintext
.
├── config.json          # システム設定
├── api.json             # API エンドポイント定義
├── html/                # フロントエンドリソース
│   ├── css              # CSS
│   ├── js               # JavaScript
│   └── images           # 画像
├── javascript/          # Goja 用スクリプト
├── main.go              # エントリーポイント
├── logs/                # ログファイル出力先
├── README.md            # 本ファイル
└── NyanPUI              # 実行ファイル
```

## 設定ファイル

NyanPUI は起動時に `api.json` と `config.json` の読み込みパスを指定できます。
指定がない場合は、従来通り実行ファイルと同じディレクトリにある `api.json` / `config.json` を読み込みます。

優先順位は次の通りです。

1. CLI オプション（`--api`, `--config`）
2. 環境変数（`NYAN_API_PATH`, `NYAN_CONFIG_PATH`）
3. 実行ファイルと同じディレクトリのデフォルトファイル

```sh
./NyanPUI
./NyanPUI --api /path/to/api.json --config /path/to/config.json
NYAN_API_PATH=/path/to/api.json NYAN_CONFIG_PATH=/path/to/config.json ./NyanPUI
```

CLI オプションと環境変数には絶対パス、または NyanPUI を起動したカレントディレクトリからの相対パスを指定できます。指定したファイルが存在しない場合、またはディレクトリを指定した場合は起動時にエラーになります。

各 `api.json` 内の `script` / `html` / `paramCheck` / `outCheck` と、`type: "public"` / `type: "include"` の `path` の相対パスは、その定義を書いた `api.json` が置かれているディレクトリから解決されます。MCP endpointはMCP API名から自動的に決まります。
`config.json` 内の `certPath` / `keyPath` / `javascript_include` / `log.Filename` の相対パスは、`config.json` が置かれているディレクトリから解決されます。

起動時のログには、読み込んだ `config.json` / `api.json` の絶対パスと、その指定元（`--api`, `--config`, 環境変数, default）が出力されます。`/css`, `/images`, `/js`, `/favicon.ico` の組み込み静的ファイルは、設定ファイルの場所に関係なく実行ファイルと同じディレクトリの `html/` 配下から配信されます。

パス解決の基準は次の通りです。

| 指定箇所 | 相対パスの基準 |
| --- | --- |
| `--api`, `--config` | NyanPUI を起動したカレントディレクトリ |
| `NYAN_API_PATH`, `NYAN_CONFIG_PATH` | NyanPUI を起動したカレントディレクトリ |
| デフォルトの `api.json`, `config.json` | 実行ファイルと同じディレクトリ |
| `api.json` の `script`, `html`, `paramCheck`, `outCheck`、`public` / `include` の `path` | その定義を書いた `api.json` が置かれているディレクトリ |
| JavaScript の `nyanGetFile`, `nyanSaveFile`, `nyanDeleteFile`, `nyanReadFileB64` | 最上位の API 定義ファイルが置かれているディレクトリ |
| `api.json` の `type: "mcp"` | `path` は指定せず、API名をendpointに使用 |
| `config.json` の `certPath`, `keyPath`, `javascript_include`, `log.Filename` | `config.json` が置かれているディレクトリ |
| `api.json` の `connectURL` | URL 文字列として扱うため相対パス解決なし |
| `/css`, `/images`, `/js`, `/favicon.ico` | 実行ファイルと同じディレクトリの `html/` 配下 |

最上位の API 定義ファイルとは、起動時に `--api`、`NYAN_API_PATH`、またはデフォルトで最初に読み込むファイルです。ファイル名が `api.json` 以外でも同じ扱いです。JavaScript の上記4関数は、多段 `include` のどの API から実行してもこのディレクトリを基準にします。実行ファイルの配置場所やカレントディレクトリには依存せず、絶対パスはそのまま使用します。

従来の `nyanGetFile` / `nyanSaveFile` / `nyanDeleteFile` は実行ファイルのあるディレクトリ、`nyanReadFileB64` はカレントディレクトリを相対パスの基準としていました。最上位の API 定義ファイルと異なる場所を基準にしていたスクリプトは、同じファイルを指すよう相対パスを調整するか、絶対パスを指定してください。

### config.json

```json
{
  "name": "API サーバー名",
  "profile": "説明文",
  "version": "バージョン",
  "port": 8009,
  "certPath": "path/to/cert.crt",
  "keyPath": "path/to/key.key",
  "javascript_include": [
    "javascript/lib/nyanPlateToJson.js"
  ],
  "APIHotReload": {
    "Enabled": true,
    "Interval": "1s"
  },
  "log": {
    "Filename": "./logs/nyanpui.log",
    "MaxSize": 5,
    "MaxBackups": 3,
    "MaxAge": 7,
    "Compress": true,
    "EnableLogging": false,
    "Level": "info"
  }
}
```

### api.json のホットリロード

ルートの `api.json` と、そこからincludeされたすべての `api.json` は既定で1秒ごとに確認され、いずれかの内容が変化した場合にルートから再読み込みされます。`APIHotReload.Enabled` を `false` にすると無効化できます。`Interval` は `500ms`、`1s`、`1m`、`24h` などのGo duration形式で指定し、省略時は `1s` です。0以下または解析できない値は起動エラーになります。

再読み込みではincludeグラフ全体と、すべての `schedule` / `ws_client` 定義を事前検証します。正常な候補だけが一括で公開され、不正な内容の場合は直前の正常な定義を維持します。存在しないinclude候補も監視され、ファイルを作成・修正すると自動復旧します。同じ不正内容のログは状態が変わるまで重複出力しません。

追加、変更、削除は通常API、HTML API、public API、`/nyan`、JSON-RPC、`nyanCallMe`、WebSocket受信処理、pushへ次の処理から反映されます。`schedule` と `ws_client` も動的に開始、更新、停止します。既存のWebSocketサーバー接続は設定変更だけでは切断されません。`ws_client` はscriptまたはdescriptionだけの変更では接続を維持し、`connectURL` の変更時だけ接続先を切り替えます。

### ログ設定（例）

```json
"log": {
  "Filename": "./logs/nyanpui.log",
  "MaxSize": 5,
  "MaxBackups": 3,
  "MaxAge": 7,
  "Compress": true,
  "EnableLogging": false,
  "Level": "info"
}
```

* **Filename**: ログ出力先パス
* **MaxSize**: 1 ファイルあたり最大サイズ (MB)
* **MaxBackups**: ローテーション保持数
* **MaxAge**: 保持日数
* **Compress**: gzip 圧縮 (true/false)
* **EnableLogging**: `true` はファイルへの追記、`false` は標準エラーへの出力（ログ自体は無効化しません）
* **Level**: `debug` / `info` / `warn` / `error`。省略時は `info` で、指定以上の重大度を出力します。不正な値では起動を中止します。変更後は再起動が必要です。

Nyan8・NyanQLと同じく、ログは1行1件のJSON形式で出力します。`time`、`level`、`msg`（処理名）と、処理に応じた項目を記録します。起動時の設定読み込みエラーも標準エラーへ出力し、標準出力はログには使いません。MCPのstdioモードでは標準出力をJSON-RPC応答専用に保ちます。ファイル出力時はアプリケーション・エラーのログが同じローテーション設定を使用し、再起動時にも既存ログを保持します。起動ログは `starting` イベントに `binary_version`、`go_version`、`config_version` を、MCP開始・終了ログは `api` にサーバーの定義名を記録します。

通常の `info` では、起動、設定変更、ジョブ完了、接続状態、警告、エラーを記録します。ジョブ結果は本文ではなく `result_bytes`（結果文字列のバイト数）を記録します。WebSocket接続先は資格情報・パス・クエリ・フラグメントを除いたschemeとhostだけを記録します。リクエスト・WebSocket・Pushの本文、API設定全体、JavaScript全文は自動出力しません。

Nyan8・NyanQLと同じく、全リクエストのURL・ステータス・処理時間を記録するHTTPアクセスログは出力しません。panic時もリクエストのダンプは出力しません。TLSハンドシェイク失敗などHTTPサーバー内部のエラーは、Nyan8と同じ `http_server_error` イベントとしてJSONで記録し、詳細文字列は `debug` 時だけ出力します。

```json
{"time":"2026-09-09T12:00:00+09:00","level":"INFO","msg":"schedule_completed","job":"daily_update","result_bytes":128}
```

エラーは処理名と型、取得できる場合はWebSocket終了コードなどを記録します。`debug` では受信メッセージやPushのバイト数、ジョブの次回実行時刻に加え、**エラー詳細文字列とJavaScriptの `console.log(...)`** を出力します。エラー詳細・consoleメッセージはそれぞれ4096バイトまでとし、改行はJSON内でエスケープします。これらの詳細にはパラメータや認証情報が含まれ得るため、調査時に限って `debug` を使用してください。

## API 定義ファイル (api.json)

各キーがエンドポイント名になります。

```json
{
  "html": {
    "script": "./javascript/html.js",
    "html": "./html/index.html",
    "description": "トップページ"
  },
  "test": {
    "html": "./html/test.html",
    "description": "テストページ",
    "push": "notify"
  }
}
```

* **type**: 種別。`api`（または省略）は通常 API、`public` は静的ファイル公開、`ws_client` は WebSocket クライアント、`schedule` は定期実行ジョブ、`include` は別API定義の読込、`mcp` はMCP Server
* **script**: 実行する JavaScript ファイル（空文字列なら HTML のみ返却）
* **html**: HTML ファイルパス
* **path**: `type: "public"` では公開するフォルダ、`type: "include"` では読込先JSON。`type: "mcp"` では指定不可
* **trigger**: `type: "schedule"` で使う実行トリガー
* **paramCheck**: API 実行前に実行する JavaScript ファイル
* **outCheck**: API 出力前に実行する JavaScript ファイル
* **title**: APIの表示名。MCP Toolとして公開する場合はToolのtitleになり、省略時はAPI名を使用
* **description**: 説明文
* **push**: WebSocket で配信するエンドポイント名

省略可能なフィールド: `type`, `script`, `html`, `path`, `connectURL`, `trigger`, `paramCheck`, `outCheck`, `title`, `description`, `push`。MCP専用フィールドは後述します。

`paramCheck` は `paramcheck` / `check`、`outCheck` は `outcheck` の別名でも読み込めます。README では `paramCheck` / `outCheck` を推奨表記とします。

## MCP ServerとOAuth

`type: "mcp"` を複数定義でき、既存のNyanPUI APIをToolとして共有できます。transportは定義ごとに `streamable_http` または `stdio` のどちらか1つです。Streamable HTTPはstatelessで、MCP protocol version `2025-11-25` と `2025-06-18`、`initialize`、`ping`、`tools/list`、`tools/call` に対応しています。

現在の接続確認用MCP URLは次のとおりです。

```text
https://nyanpui.stamps.necomori.asia/server_mcp_http
```

ChatGPTへはこのURLを登録します。認証画面ではJavaScript hook側で認証するOAuthユーザーを使用します。ユーザーは利用前に別途用意してください。現在のscopeは `nyanpui:read` です。

### MCP設定例

公開環境の `api.vps.json`、OAuth hook、状態ファイル、Ansible設定は環境ごとの運用ファイルとしてGit管理しません。次は構造を示す例です。

```json
{
  "sample/json": {
    "type": "api",
    "script": "./javascript/sample_json.js",
    "paramCheck": "./javascript/mcp_sample_input.js",
    "outCheck": "./javascript/mcp_sample_output.js",
    "title": "疎通確認データを取得",
    "description": "固定JSONを返します。",
    "securitySchemes": [
      {"type": "oauth2", "scopes": ["nyanpui:read"]}
    ],
    "annotations": {
      "readOnlyHint": true,
      "destructiveHint": false,
      "openWorldHint": false
    }
  },
  ".well-known/oauth-authorization-server": {
    "type": "api",
    "description": "Authorization Server Metadata"
  },
  ".well-known/oauth-protected-resource/server_mcp_http": {
    "type": "api",
    "description": "Protected Resource Metadata"
  },
  "oauth/authorize": {
    "type": "api",
    "script": "./runtime/oauth_policy.js"
  },
  "oauth/token": {
    "type": "api",
    "script": "./runtime/oauth_policy.js"
  },
  "oauth/register": {
    "type": "api",
    "script": "./runtime/oauth_policy.js"
  },
  "oauth/verify_access": {
    "type": "api",
    "script": "./runtime/oauth_policy.js",
    "scopes": ["nyanpui:read"]
  },
  "server_mcp_http": {
    "type": "mcp",
    "transport": "streamable_http",
    "protocolVersions": ["2025-11-25", "2025-06-18"],
    "allowedOrigins": ["https://chatgpt.com", "https://platform.openai.com"],
    "redirectURIAllowedPrefixes": ["https://chatgpt.com/connector/oauth/"],
    "rateLimit": {"requests": 120, "window": "1m"},
    "maxConcurrent": 8,
    "oauth": {
      "authorizationServerMetadata": ".well-known/oauth-authorization-server",
      "protectedResourceMetadata": ".well-known/oauth-protected-resource/server_mcp_http",
      "authorize": "oauth/authorize",
      "token": "oauth/token",
      "register": "oauth/register",
      "verifyAccess": "oauth/verify_access"
    },
    "tools": ["sample/json"]
  },
  "server_mcp_stdio": {
    "type": "mcp",
    "transport": "stdio",
    "tools": ["sample/json"]
  }
}
```

### MCPサーバー定義フィールド

`type: "mcp"` の定義で使用できるフィールドは次のとおりです。ここに通常API用の `script`、`path`、`resource` などを指定すると起動時にエラーになります。

| フィールド | 必須 | 内容 |
| --- | --- | --- |
| `type` | 必須 | `mcp` を指定 |
| `transport` | 必須 | `streamable_http` または `stdio` |
| `protocolVersions` | 省略可 | 対応するMCP protocol versionの配列。指定可能なのは `2025-11-25` と `2025-06-18`。省略時は両方に対応 |
| `allowedOrigins` | Streamable HTTPで必須 | HTTPS originの配列。schemeとhostだけを指定し、path、query、fragmentは含めない |
| `redirectURIAllowedPrefixes` | OAuth使用時に必須 | 許可するHTTPS redirect URI prefixの配列。各prefixは `/` で終える |
| `rateLimit` | 省略可 | 接続元ごとの呼出し制限。`requests`は1〜10000、`window`は1秒〜24時間のGo duration形式 |
| `maxConcurrent` | 省略可 | Toolの同時実行数。1〜256。省略時または0は16 |
| `oauth` | 省略可 | OAuthで使用する通常APIの参照。詳細は後述 |
| `tools` | 必須 | Toolとして公開する通常API名の配列。1件以上必要 |
| `instructions` | 省略可 | MCPの `initialize` 応答に含めるサーバー利用説明 |

`streamable_http` のendpointはMCP API名から自動生成されます。`stdio` では `allowedOrigins` は不要で、OAuthは使用できません。

### MCP Toolとして参照する通常API

`tools` に指定できるのは、`type: "api"` またはtypeを省略した通常APIです。参照先には実在する `script` が必要で、Toolの情報は次のフィールドから生成されます。

| 通常APIのフィールド | MCP Toolでの用途 |
| --- | --- |
| API名 | Toolの `name`。`title` 省略時のtitleにも使用 |
| `title` | Toolの `title` |
| `description` | Toolの `description` |
| `paramCheck` | 入力JSON Schema。省略時は空のobject schema |
| `outCheck` | 出力JSON Schema |
| `securitySchemes` | OAuthなど、Toolが必要とするsecurity scheme |
| `annotations` | `readOnlyHint`、`destructiveHint`、`idempotentHint`、`openWorldHint` などのTool annotation |
| `script` | `tools/call` で実行するJavaScript |

OAuthを有効にしたMCPで公開する各Toolは、`securitySchemes` の定義に1件以上の `scopes` が必要です。指定したscopeは、後述する `verifyAccess` APIの `scopes` に含まれている必要があります。

### OAuth設定フィールド

`oauth` を指定する場合は `transport: "streamable_http"` が必要です。各値には役割を担当する通常API名を指定し、同じAPIを複数の役割で共有することはできません。Metadata用の2つを除き、参照先には実在する `script` が必要です。

| フィールド | 必須 | 内容 |
| --- | --- | --- |
| `authorizationServerMetadata` | 必須 | Authorization Server Metadataを返すAPI |
| `protectedResourceMetadata` | 必須 | Protected Resource Metadataを返すAPI |
| `authorize` | 必須 | 認可endpointのAPI |
| `token` | 必須 | token endpointのAPI |
| `register` | 必須 | Dynamic Client Registration API |
| `verifyAccess` | 必須 | access tokenを検証するAPI。この通常APIの `scopes` にMCPで許可する重複のないscopeを1件以上指定 |

HTTP endpointは `/server_mcp_http` になり、`/?api=server_mcp_http` でも呼び出せます。公開URLはrequestのschemeとHostから生成するため、固定domain、`path`、`resource` は設定しません。Toolの名前、説明、schema、security scheme、annotation、実行scriptは参照先の通常APIから解決されます。

stdioは次のように起動します。stdoutはJSON-RPC専用で、HTTP listener、OAuth、background処理、hot reloadは起動しません。

```sh
./NyanPUI --mcp-server server_mcp_stdio --api /absolute/path/to/api.json --config /absolute/path/to/config.json
```

### OAuthの責務と状態保存

Goの `main.go` はMCP/OAuthのHTTP受付、request由来URL生成、JavaScript hook呼出し、hookの結果に基づく処理の許可・拒否と応答を担当します。安全な状態ファイル操作やパスワードハッシュなどの汎用ヘルパーも提供します。ユーザー認証、PKCE検証、認可コードの一回限り消費、access tokenの発行・検証は参照先APIのJavaScript側の責務です。JavaScriptファイル名は固定されません。

状態保存rootは `config.json` の `oauth_state_directory` で指定します。実際の保存先はその下のMCP API名ごとに分離されます。未指定時はMCP定義元の `oauth-state/MCP API名` です。

`nyanOAuthList` は、書き込み中断で残った `.nyanpui-oauth-*.tmp` を一覧から除外します。除外するのは通常ファイルで、既存の権限検査に合格するものだけです。シンボリックリンク、ディレクトリ、不適切な権限のファイルは引き続きエラーになります。この命名規則に一致しない `.json` 以外のファイルもエラーになります。一時ファイル自体は削除しません。

NyanPUIには組み込みのOAuthユーザー管理APIはありません。ユーザーの作成・更新・管理が必要な場合は、利用者がJavaScriptなどで実装し、その処理へのアクセス制御も行ってください。ローカルサンプルのOAuth hookは状態ファイルに保存されたユーザーを認証するため、利用前にhookが扱う形式のユーザーデータを別途用意する必要があります。既存のユーザーデータは引き続き利用できます。

旧設定から移行する場合は、`config.json` の `BasicAuth`、MCP定義の `oauth.adminUser`、その参照先の管理API定義を削除してください。`oauth.adminUser` が残ったAPI設定は読み込みエラーになります。管理認証用の `nyanOAuthAdminAuthorized` と、hookへの `operator_username`・`operator_password`・`admin_user_endpoint` の受け渡しも廃止しています。

OAuth discovery endpointは次のとおりです。

| endpoint | 用途 |
| --- | --- |
| `/.well-known/oauth-protected-resource/server_mcp_http` | MCPのProtected Resource Metadata |
| `/.well-known/oauth-authorization-server` | Authorization Server Metadata |
| `/oauth/register` | Dynamic Client Registration |
| `/oauth/authorize` | ログイン、同意、認可コード発行 |
| `/oauth/token` | PKCE検証とaccess token発行 |

### api.jsonの分割と多段include

`type: "include"` を使うとAPI定義を複数ファイルに分割できます。includeのキーがマウント名となり、子ファイルのAPIは `/` 区切りの完全名で公開されます。

```json
{
  "health": {"script": "./javascript/health.js"},
  "sub": {"type": "include", "path": "./sub/api.json"}
}
```

子の `sub/api.json` に `getItem` と、さらに `admin` includeがある場合、API名は `sub/getItem`、`sub/admin/...` となります。完全名はHTTP、WebSocket、JSON-RPC、`nyanCallMe`、push、public、schedule、ws_clientで共通です。

include定義に指定できるフィールドは`type`と`path`だけです。循環参照、展開後の重複名、重複JSONキー、マウント名の衝突や `/` を含む不正なマウント名は読み込みエラーになります。includeされない既存API名に `/` を含める書き方は引き続き利用できます。

通常 API は `GET`, `POST`, `PUT`, `DELETE` などのメソッドを受け付けます。`Content-Type: application/json` の JSON body、フォーム値、URL クエリを `nyanAllParams` にまとめて渡します。`api` が未指定の場合は、エンドポイントパスまたは `html` が入ります。

### public フォルダ公開（`type: "public"`）

`type: "public"` を指定すると、`path` のフォルダ配下にあるファイルをそのまま配信します。`path` は `api.json` が置かれているディレクトリからの相対パス、または絶対パスで指定できます。

```json
{
  "public": {
    "type": "public",
    "path": "./public",
    "description": "public フォルダ"
  }
}
```

この例では `./public/app.js` を `http://localhost:8009/public/app.js` で取得できます。リクエスト先が実在するファイルではない場合は 404 を返します。フォルダへのアクセスでは `index.html` を探さず、ディレクトリ一覧も表示しません。

### 実行前チェック（`paramCheck`）

`paramCheck` を指定すると、通常 API の `script` 実行前、または `type: "public"` のファイル配信前に JavaScript を実行できます。

```json
{
  "private-files": {
    "type": "public",
    "path": "./public/private",
    "paramCheck": "./javascript/check_login.js",
    "description": "認証付きファイル"
  }
}
```

通常 API にも同じように指定できます。

```json
{
  "private-api": {
    "script": "./javascript/private_api.js",
    "html": "./html/private.html",
    "paramCheck": "./javascript/check_login.js",
    "description": "認証付きAPI"
  }
}
```

`paramCheck` は次の JSON 形式を返します。

```js
return {
  success: true,
  status: 200,
  result: {}
};
```

`success: true` かつ `status: 200` の場合だけ次の処理へ進みます。それ以外は、通常のHTTP呼び出しでは `paramCheck` の結果を JSON として返し、HTTP ステータスも `status` の値になります。JSON-RPC呼び出し時は、後述のJSON-RPCエラー形式で返します。

```js
return {
  success: false,
  status: 401,
  result: {
    message: "login required"
  }
};
```

`nyan_mode=checkOnly` を指定した場合は `paramCheck` だけを実行し、成功時も本処理へ進まずチェック結果を返します。`paramCheck` 未設定の場合は次を返します。

```json
{
  "success": true,
  "status": 200,
  "result": null
}
```

`type: "public"` の `paramCheck` では、リクエストされた公開ファイルの情報を参照できます。

```js
var endpoint = nyanAllParams.nyan_public_endpoint; // 例: "public"
var path = nyanAllParams.nyan_public_path;         // 例: "docs/a.txt"
```

絶対パスは渡しません。認証・認可判定には公開フォルダ内の相対パスを使ってください。

### 出力前チェック（`outCheck`）

`outCheck` を指定すると、通常 API の本体実行後、または `type: "public"` のファイル送信前に JavaScript を実行できます。`outCheck` が成功した場合は本体の実行結果をそのまま出力し、失敗した場合は `outCheck` の結果を JSON として出力します。JSON-RPC呼び出し時は、後述のJSON-RPCエラー形式で返します。

```json
{
  "checked-api": {
    "script": "./javascript/main.js",
    "outCheck": "./javascript/out_check.js",
    "description": "出力前チェック付きAPI"
  }
}
```

`type: "public"` にも指定できます。この場合、ファイル送信前にファイル内容を `outCheck` へ渡して検査します。

```json
{
  "public": {
    "type": "public",
    "path": "./public",
    "paramCheck": "./javascript/check_login.js",
    "outCheck": "./javascript/out_check.js",
    "description": "チェック付き public フォルダ"
  }
}
```

`outCheck` は `paramCheck` と同じ JSON 形式を返します。

```js
return {
  success: true,
  status: 200,
  result: {}
};
```

`success: true` かつ `status: 200` の場合だけ本体の実行結果をそのまま出力します。それ以外は、通常のHTTP呼び出しでは `outCheck` の結果を JSON として返し、HTTP ステータスも `status` の値になります。

本体の実行結果は `nyanAllParams.nyan_output` で参照できます。

```js
if (nyanAllParams.nyan_output.body.indexOf("expected") >= 0) {
  return {
    success: true,
    status: 200,
    result: {}
  };
}

return {
  success: false,
  status: 409,
  result: {
    message: "output mismatch"
  }
};
```

`nyan_output` には `status`, `contentType`, `headers`, `body`, `bodyBase64`, `bodyLength` が入ります。互換用に `nyan_output_status`, `nyan_output_content_type`, `nyan_output_body`, `nyan_output_body_base64` も利用できます。

`type: "public"` の `outCheck` でも、リクエストされた公開ファイル名を参照できます。

```js
var path = nyanAllParams.nyan_public_path;

if (path === "test.txt" && nyanAllParams.nyan_output.body === "expected") {
  return {
    success: true,
    status: 200,
    result: {}
  };
}

return {
  success: false,
  status: 409,
  result: {
    message: "file output mismatch",
    path: path
  }
};
```

### `/nyan`と入出力スキーマ

`GET /nyan`または`GET /nyan/`は通常APIだけを一覧表示します。`public`、`schedule`、`ws_client`は含まれません。`GET /nyan/{API名}`では通常APIの詳細と`inputSchema`、`outputSchema`、`schemaSource`を返します。includeされたAPIも`/nyan/sub/admin/getItem`のような完全名で取得できます。

入力スキーマは`paramCheck`のトップレベルに`nyanInputSchema`、出力スキーマは`outCheck`に`nyanOutputSchema`として宣言します。

```js
const nyanInputSchema = {
  type: "object",
  properties: {id: {type: "integer"}},
  required: ["id"],
  additionalProperties: false
};
```

```js
const nyanOutputSchema = {
  type: "object",
  properties: {status: {const: 200}},
  required: ["status"]
};
```

入力は`nyanInputSchema`、本体scriptの旧形式`nyanAcceptedParams`、空スキーマの順で解決します。出力は`nyanOutputSchema`、空スキーマの順です。legacy入力を利用した場合は互換性のため`nyanAcceptedParams`も返します。`nyanOutputColumns`は公開しません。

スキーマはJavaScriptを実行せずASTから静的に読み取ります。オブジェクト、配列、文字列、数値、真偽値、`null`を利用できます。関数呼び出し、spread、識別子参照などの動的な定義はスキーマ解決エラーになります。スキーマファイルは詳細リクエストごとに読み直されます。

公開したJSON Schemaによるリクエスト・レスポンスの自動検証は行いません。実際の検証は従来どおり`paramCheck`と`outCheck`が担当します。

### WebSocket レシーバー（`type: "ws_client"`）

`type: "ws_client"` を指定すると NyanPUI 自身が WebSocket クライアントになり、起動時に常時接続します（HTTP エンドポイントとしては登録されません）。

```json
{
  "receiver_main": {
    "type": "ws_client",
    "connectURL": "ws://127.0.0.1:8000/ws",
    "script": "./javascript/ws/receiver_main.js",
    "description": "WebSocket 受信処理"
  }
}
```

`connectURL` は `env:WS_URL` のように環境変数からも指定できます。
`script` の相対パスは `api.json` が置かれているディレクトリから解決されます。`connectURL` はファイルパスではないため、相対パス解決の対象ではありません。

受信したメッセージは `script` に渡され、次の値を `nyanAllParams` で参照できます。`script` の戻り値が空でない場合は、接続先 WebSocket にテキストメッセージとして返信します。

起動時はすぐに接続を試みます。再接続の待ち時間は1秒から始まり、接続失敗が続くと2秒、4秒、8秒、16秒、最大30秒へ増えます。接続に成功した後の切断では、過去の失敗回数にかかわらず1秒から再接続を試みます。

| キー | 内容 |
| --- | --- |
| `ws_client` | ws_client 名 |
| `ws_message_type` | `text`, `binary`, `close`, `ping`, `pong` など |
| `ws_message_text` | 受信内容の文字列 |
| `ws_message_base64` | バイナリ受信時の Base64 文字列 |
| `ws_message_json` | テキスト受信内容が JSON として読めた場合の値 |
| `ws_connect_url` | 接続先 URL |
| `ws_description` | `api.json` の説明文 |

#### 動作確認（NyanPUI 自身に接続）

リポジトリ同梱の `api.json` には、NyanPUI 自身の `/push/receive` に接続するサンプル（`ws_client/self_push_receive`）があります。

1. `log.Level` を `debug` にして `./NyanPUI` を起動（`ws_client_starting` イベントの `client` が `ws_client/self_push_receive`、`origin` が `ws://127.0.0.1:8009` になります）
2. ブラウザで `http://localhost:8009/push/request` を開く（push が飛びます）
3. 設定したログ出力先に `ws_client_message_received` イベントが出ればOK

ポートを変更している場合は `api.json` の `connectURL` を合わせてください。

### 定期実行ジョブ（`type: "schedule"`）

`type: "schedule"` を指定すると、NyanPUI の起動時に定期実行ジョブとして登録されます。HTTP エンドポイント、`/nyan` の API 一覧、JSON-RPC には公開されません。

```json
{
  "schedule_debug_every_minute": {
    "type": "schedule",
    "script": "./javascript/schedule_debug.js",
    "trigger": {
      "type": "cron",
      "value": "* * * * *"
    },
    "description": "schedule の動作確認用。1分ごとにログへ実行時刻を出力します。"
  }
}
```

現在サポートしている `trigger.type` は `cron` です。`trigger.value` は `分 時 日 月 曜日` の 5 フィールドで指定します。
`script` の相対パスは `api.json` が置かれているディレクトリから解決されます。

例:

```text
* * * * *          # 毎分
*/15 9-10 * * 1-5 # 平日 9:00-10:59 の15分ごと
0 10 * * *         # 毎日 10:00
```

schedule の `script` では通常 API と同じ Goja 環境を使えます。加えて `nyanAllParams` に次の値が入ります。

| キー | 内容 |
| --- | --- |
| `nyan_job_name` | ジョブ名 |
| `nyan_schedule_trigger_type` | トリガー種別。現在は `cron` |
| `nyan_schedule_trigger` | cron 式 |
| `nyan_schedule_time` | 実行予定時刻 |
| `nyan_schedule_description` | `api.json` の説明文 |

同梱の `api.json` には動作確認用の `schedule_debug_every_minute` を追加しています。起動すると1分ごとに `javascript/schedule_debug.js` が実行され、`info` では `schedule_completed` イベントを記録します。`console.log` による実行時刻の詳細は `debug` 時だけ `script_console` イベントとして出力されます。

## アプリケーションの実行

* `config.json` と `api.json` を編集後、実行ファイルを起動。設定ファイルを別の場所に置く場合は `--api` / `--config`、または `NYAN_API_PATH` / `NYAN_CONFIG_PATH` で指定します。
* デフォルトで [http://localhost:8009/](http://localhost:8009/) にアクセスするとサンプルが表示されます。 Windows MacOS Linuxで実行可能です。 各自でビルドいただくか、[リリース](https://github.com/NyanQL/NyanPUI/releases)からダウンロードしてください。
* `/nyan` にアクセスすると通常APIの一覧を、`/nyan/{API名}`では入出力スキーマを含む詳細をJSONで取得できます。
* `/css`, `/images`, `/js`, `/favicon.ico` は `html/` 配下の静的ファイルとして配信されます。

## ビルド

ビルドには `go.mod` に記載された Go バージョンを使用してください。

### macOS

`dyld: missing LC_UUID load command` で起動時に abort する場合は、外部リンカでビルドしてください。

`go build -ldflags="-linkmode=external" -o NyanPUI main.go`

ビルドしたバイナリのバージョンをログに出したい場合は、`-X main.buildVersion=...` を指定します。

`go build -ldflags="-linkmode=external -X main.buildVersion=v1.2.3" -o NyanPUI main.go`

### Windows / Linux

`go build -o NyanPUI main.go`

## JavaScript 実行 (Goja) 環境で使用できる変数と関数

* リクエストパラメータ: `nyanAllParams`
* テンプレート HTML: `nyanHtmlCode`
* コンソール出力: `console.log()`
* リクエストヘッダーの取得: `nyanGetRequestHeaders()`
* Cookie 操作: `nyanGetCookie()` / `nyanSetCookie()`
* プロセス内の共有ストレージ操作: `nyanGetItem()` / `nyanSetItem()` / `nyanRemoveItem()`
* 外部 APIの呼び出し : `nyanGetAPI()` / `nyanJsonAPI()` / `nyanCallAPI()`
* ホスト側でコマンドを実行し、結果を取得する: `nyanHostExec()`
* ファイル読み込み: `nyanGetFile()`
* ファイルのatomic保存・削除: `nyanSaveFile()` / `nyanDeleteFile()`
* バイナリをBase64で取得: `nyanReadFileB64()`
* OAuth等で使う乱数・hash・Base64: `nyanRandomBase64URL()` / `nyanSHA256Base64URL()` / `nyanBase64Decode()`
* 自身のAPIを内部実行: `nyanCallMe()`

それぞれの使い方は次のとおりです。
### 1. **nyanAllParams**
GET/POST/JSON 受信パラメータをまとめたオブジェクトです。
console.log で内容を確認できます。
```javascript 
console.log(nyanAllParams);
```

### 2. **nyanHtmlCode**
nyanHtmlCode には `api.json` で指定した HTML ファイルの内容が文字列として格納されています。
HTMLコードを加工して出力する場合にはこちらの変数を利用してください。

### 3. **console.log()**
console.log は `log.Level` が `debug` のときだけ、JSONログの `script_console` イベントとして内容を出力します。
出力先は config.json の `log.EnableLogging` で制御し、`true` ならログファイル、`false` なら標準エラーへ出力します。
```javascript
console.log("Hello, NyanPUI!");
```

### 4. **nyanGetCookie / nyanSetCookie**
cookie の取得と設定ができます。
```javascript
// Cookie の取得
var cookieValue = nyanGetCookie("cookieName");
console.log("Cookie Value: " + cookieValue);
// Cookie の設定
nyanSetCookie("cookieName", "cookieValue");
```
### 5. **nyanGetItem / nyanSetItem / nyanRemoveItem**

NyanPUIプロセス内のメモリにキーと値を保存します。同じプロセスの各API・JavaScript実行で共有され、API設定の再読込でも保持されます。プロセスを再起動すると消えます。

`nyanSetItem(key, value)` はキーと値を文字列に変換して保存し、同じキーがあれば上書きします。`nyanGetItem(key)` は保存した文字列を返し、キーが存在しなければ `null` を返します。`nyanRemoveItem(key)` は文字列に変換したキーのデータを削除します。存在しないキーの削除や、引数なしの呼び出しでは何もしません。`nyanSetItem` と `nyanRemoveItem` の戻り値は `null` です。

```javascript
// 値を保存
nyanSetItem("itemKey", "itemValue");
// 値を取得
var itemValue = nyanGetItem("itemKey");
console.log("Item Value: " + itemValue);
// 不要になったデータを削除
nyanRemoveItem("itemKey");
console.log(nyanGetItem("itemKey")); // null
```

自動削除や有効期限、件数・サイズの上限はありません。動的にキーを増やす場合は、不要になった時点でJavaScript側から削除してください。空文字を保存してもキー自体は削除されません。

### 6. **nyanGetAPI / nyanJsonAPI / nyanCallAPI**
外部 API を呼び出します。

* `nyanGetAPI(url, username, password)` は GET リクエストを送信します。
* `nyanJsonAPI(url, jsonData, username, password, headers)` は JSON を POST します。
* `nyanCallAPI(url, jsonData, username, password, headers)` は `nyanJsonAPI()` のラッパーで、引数と挙動は同じです。

`jsonData` には JSON 文字列を渡してください。JavaScript オブジェクトを送る場合は `JSON.stringify()` してください。
`headers` は省略可能で、オブジェクトまたは JSON 文字列で指定できます。

```javascript
// GET リクエスト
var response = nyanGetAPI("https://api.example.com/data", "nyan", "password");
console.log("GET Response: " + response);

// JSONをPOSTして リクエスト
var postData = JSON.stringify({ key: "value" });
var jsonResponse = nyanJsonAPI("https://api.example.com/update", postData, "nyan", "password");
var jsonResponse2 = nyanCallAPI("https://api.example.com/update", postData, "nyan", "password");
console.log("POST Response: " + jsonResponse);
```

header を追加したい場合は第5引数に指定してください。
```javascript
const headers = {
  "Content-Type": "application/json",
  "X-Custom-Header": "CustomValue"
};
var response = nyanCallAPI("https://api.example.com/data", postData, "nyan", "password", headers);
```

### 7. **nyanHostExec**
ホスト側でコマンドを実行し、結果を取得します。
```javascript
var result = JSON.parse(nyanHostExec("ls -la"));
console.log("Command Result: " + result.stdout);
```
戻り値は JSON 文字列です。上記例では `stdout` に ls コマンドの結果が格納されます。
```json
{
  "success": true,
  "exit_code": 0,
  "stdout": "コマンドの標準出力",
  "stderr": "コマンドの標準エラー出力"
}
```

### 8. **nyanGetFile**
ファイルを読み込み、内容を文字列として取得します。
ファイルのパスは最上位の API 定義ファイルがあるディレクトリからの相対パス、または絶対パスで指定できます。
指定したファイルが存在しない場合はnullが返ります。
```javascript
var fileContent = nyanGetFile("./path/to/file.txt");
console.log("File Content: " + fileContent);
```

### 9. **nyanSaveFile / nyanDeleteFile**

テキストファイルを保存・削除します。相対パスは最上位の API 定義ファイルがあるディレクトリを基準にします。絶対パスも指定できます。

```javascript
nyanSaveFile("./state/example.json", JSON.stringify({ok: true}));
nyanDeleteFile("./state/example.json");
```

`nyanSaveFile` は親ディレクトリをmode 0750で作成し、同じディレクトリの一時ファイルへmode 0600で書き込んだ後、atomic renameします。`nyanDeleteFile` は対象が存在しない場合も成功扱いです。その他のI/OエラーはJavaScript例外になります。

### 10. **nyanRandomBase64URL / nyanSHA256Base64URL / nyanBase64Decode**

OAuth、PKCE、key-valueファイル名などに必要な値を生成・変換します。

```javascript
var token = nyanRandomBase64URL(32);
var digest = nyanSHA256Base64URL("value-to-hash");
var plain = nyanBase64Decode("dXNlcjpwYXNzd29yZA==");
```

`nyanRandomBase64URL` の引数は乱数のbyte数で、1から1024まで指定できます。省略時は32です。戻り値とSHA-256 digestはpaddingなしBase64URLです。`nyanBase64Decode` は標準Base64文字列をdecodeします。

### 11. **nyanReadFileB64**
バイナリファイルをBase64文字列として取得します。
ファイルのパスは最上位の API 定義ファイルがあるディレクトリからの相対パス、または絶対パスで指定できます。存在しない場合は JavaScript 例外になります。
```javascript
var b64 = nyanReadFileB64("./html/images/nyan.png");
console.log(b64);
```

### 12. **nyanCallMe(data)**
同一 NyanPUI プロセス内で、自身の API を直接実行します。  
`nyanGetAPI` / `nyanJsonAPI` と異なり HTTP/HTTPS を経由しないため、証明書設定や `port` に依存しません。

```javascript
const result = nyanCallMe({ api: "sample/json" });
console.log(result);
```

#### 挙動
* `data.api` で呼び出し先 API 名を指定します。未指定時は現在処理中の API 名（HTTP エンドポイント実行時）を使用します。
* 引数オブジェクトは呼び出し先 API の `nyanAllParams` に渡されます（`api` は呼び出し先名で上書き）。
* 呼び出し先の戻り値が JSON 文字列の場合は自動でオブジェクト化されます。
* `type: "ws_client"` のエンドポイントは `nyanCallMe` では呼び出せません。
* `type: "schedule"` のエンドポイントは `nyanCallMe` では呼び出せません。
* 現在処理中 API 名を解決できない場合（例: `ws_client` から `api` 未指定で呼ぶ場合）は例外になります。
* 失敗時は JavaScript 側で例外になります。

### 13. **nyanPlate(data, htmlCode)**

テンプレート内に `data-nyan*` 属性を記述し、`nyanPlate(data, htmlCode)` で動的置換します。
詳細については [nyanPlate.js](javascript%2Flib%2FnyanPlate.js) の文頭にコメントで記載していますので
そちらを参照してください。

### 14. **nyanGetRequestHeaders()**

Nyan8と同じ形式で、現在のHTTPリクエストのヘッダーをオブジェクトとして取得します。通常API、JSON-RPC、`paramCheck` / `outCheck` など、HTTPリクエストに伴うJavaScriptから使用できます。`nyanCallMe()` の呼び出し先にも元のリクエスト情報を引き継ぎます。

```javascript
const headers = nyanGetRequestHeaders();
const origin = headers["Origin"] || "";
const userAgent = headers["User-Agent"] || "";
```

ヘッダー名は `Origin`、`User-Agent` などの表記に正規化されます。値は文字列で、同名ヘッダーの複数の値はカンマで連結します。取得するのは実際の受信ヘッダーで、クエリーやJSONのパラメーターからは補いません。返されたオブジェクトを書き換えても元のヘッダーは変わりません。`Host` は含みません。

`schedule`、`ws_client`、MCPのstdioなど、HTTPリクエストを伴わない実行では空のオブジェクト `{}` を返します。WebSocket接続時と接続後のチェック処理では、接続時のHTTPリクエストヘッダーを取得します。

## WebSocket サンプル
WebSocket による双方向通信とプッシュ通知のサンプルを同梱しています。
* フロント: `http://localhost:8009/push/test`
* プッシュ: `http://localhost:8009/push/request` → `ws://localhost:8009/push/receive`

通常HTTPのAPIに `push` を設定すると、`paramCheck`・`outCheck` を通過し、HTTPステータスが200〜399の応答を返した後に、指定先の内容をWebSocket接続へ配信します。文字列・オブジェクト・空の応答・HTMLのみのAPIで共通です。応答の内容や形式は変わりません。

HTTP 4xx・5xxの応答、チェック拒否、スクリプト例外、HTML読み込みや応答変換の失敗、`nyan_mode=checkOnly` の場合はPushを実行しません。本体スクリプトが明示的に `status: 500` などを返した場合も対象です。

### JavaScriptによる接続前のOriginチェック

WebSocketの接続先APIに `paramCheck` を設定すると、接続確立前にチェックを実行します。次のスクリプトでは、指定したOriginのみ許可し、Originがない場合も拒否します。許可する値と、Originがない場合の扱いはJavaScript側で決めてください。

```javascript
// api.jsonの接続先APIで、このファイルをparamCheckに指定します。
const origin = nyanGetRequestHeaders()["Origin"] || "";
const allowed = origin === "https://app.example.com";
({
  success: allowed,
  status: allowed ? 200 : 403,
  result: allowed ? null : { message: "Origin not allowed" }
});
```

拒否するとHTTP 403を返し、WebSocket接続は確立しません。このチェックは同じAPIへの通常HTTPリクエストにも適用されます。チェックを設定しなければ、NyanPUIはWebSocket接続に対するOrigin制限を自動では行いません。

## JSON-RPC 対応
JSON-RPC 2.0 API を実装しています。（Batch は未実装）。
/nyan-rpc エンドポイントに POST リクエストを送ると、JSON-RPC 形式でレスポンスが返ります。
```json
{
  "jsonrpc": "2.0",
  "method": "api名",
  "params": {
    "foo": "bar"
  },
  "id": 1
}
```

`method` には `api.json` の API 名を指定します。JSON-RPCから呼べるのは `type: "api"` またはtypeを省略した通常APIで、`script` が必須です。`ws_client`、`public`、`schedule`、`mcp` など、それ以外の種別は呼び出せません。

また、いずれかのMCP定義の `oauth` から参照されるAPIは、通常APIとして定義されていてもJSON-RPCからは呼び出せません。対象は `authorizationServerMetadata`、`protectedResourceMetadata`、`authorize`、`token`、`register`、`verifyAccess` の全役割です。対象外のAPIにはHTTP 200でJSON-RPCエラー `-32601`（`Method not found`）を返し、チェック・本体・Pushを実行しません。この制限はホットリロード後の定義にも適用されます。

成功時の `result` は JavaScript の戻り値を文字列化した値です。

JSON-RPCでも `paramCheck` と `outCheck` を実行します。チェックが `success: true` かつ `status: 200` を満たさなかった場合、HTTP 200で次のJSON-RPCエラーを返します。`id` はリクエストの値を保持し、`error.code` は `-32000`、`error.message` は `paramCheck rejected` または `outCheck rejected` です。元のチェック結果は `error.data` に保持します。

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32000,
    "message": "paramCheck rejected",
    "data": {
      "success": false,
      "status": 403,
      "result": "denied"
    }
  },
  "id": 1
}
```

`paramCheck` の拒否時は本体とPushを実行しません。`outCheck` の拒否時は実行済みの本体結果を返さず、Pushも実行しません。チェックスクリプトの例外や不正な戻り値も同じエラー形式となり、`error.data.status` は `500`、`error.data.result.message` に詳細が入ります。

`nyan_mode=checkOnly` では本体とPushを実行せず、成功時はHTTP 200でチェック結果オブジェクトをJSON-RPCの `result` に格納します。`paramCheck` 未設定時の `result` は `{"success":true,"status":200,"result":null}` です。チェック拒否時は上記のエラー形式になります。

---

## JavaScriptのレスポンス形式（拡張）
JavaScript が文字列を返した場合は従来どおり `text/html; charset=utf-8` として返します。`null` や `undefined` の場合は空ボディになります。
オブジェクトを返すと、HTTPレスポンスを制御できます。

```javascript
({
  status: 200, // 省略可（デフォルト200）
  headers: { "Cache-Control": "no-store" }, // 省略可
  contentType: "application/json; charset=utf-8", // 省略可
  body: { ok: true, items: [1, 2, 3] } // object/arrayはJSON化、stringはそのまま
});
```

`body` に配列やオブジェクトを指定した場合は JSON として返します。バイナリを返す場合は `body.encoding = "base64"` を指定します。

```javascript
const data = nyanReadFileB64("./html/images/nyan.png");
({
  status: 200,
  contentType: "image/png",
  body: { encoding: "base64", data: data }
});
```

### サンプルAPI
`api.json` には以下のサンプルを追加しています。

- `http://localhost:8009/sample/json`
- `http://localhost:8009/sample/png`
- `schedule_debug_every_minute`（HTTP では公開されない定期実行ジョブ）

---

## ワイヤーフレームデザインプレビューについて
以下のファイルをプロジェクトに含めることで、ワイヤーフレーム用のプレビュー機能を利用できます：
* **CSS**: `html/css/wf_style.css` にワイヤーフレーム用のスタイルを定義
* **HTML**: `html/wf_html.html` にサンプルレイアウトを記述

これらを配置した状態でサーバーを起動すると、デフォルトで以下の URL からプレビューが表示されます：

> [http://localhost:8009/wf](http://localhost:8009/wf)


## 予約語
エンドポイント名や変数名など、`nyan` で始まる名前は予約語になりますので使用しないでください。`nyan`, `nyan-rpc` は固定ルートで使われます。`api` は `nyanAllParams` で呼び出し先 API 名に使う予約パラメータです。
