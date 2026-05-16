# NyanPUI

NyanPUI(にゃんぷい)は、GoLangで作られたサーバーサイドレンダリングを行うプレゼンテーションフレームワークです。 リクエストに対してJavaScriptを実行してHTMLを出力します。

* **JavaScript エンジン**: Goja (ECMAScript 5.1 準拠) – [https://github.com/dop251/goja](https://github.com/dop251/goja)
* **双方向通信**: gorilla/websocket による WebSocket
* **プッシュ通知**: 特定エンドポイントの処理結果を WebSocket で配信
* **定期実行**: `type: "schedule"` による cron 形式の JavaScript ジョブ
* **CORS**: `Access-Control-Allow-Origin: *` を付与

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
└── NyanPUI_XXX          # 実行ファイル（XXX は OS 名）
```

## 設定ファイル

NyanPUI は起動時に `api.json` と `config.json` の読み込みパスを指定できます。
指定がない場合は、従来通り実行ファイルと同じディレクトリにある `api.json` / `config.json` を読み込みます。

優先順位は次の通りです。

1. CLI オプション
2. 環境変数
3. 実行ファイルと同じディレクトリのデフォルトファイル

```sh
./NyanPUI_Mac
./NyanPUI_Mac --api /path/to/api.json --config /path/to/config.json
NYAN_API_PATH=/path/to/api.json NYAN_CONFIG_PATH=/path/to/config.json ./NyanPUI_Mac
```

`api.json` 内の `script` / `html` / `path` / `paramCheck` / `outCheck` の相対パスは、`api.json` が置かれているディレクトリから解決されます。
`config.json` 内の `certPath` / `keyPath` / `javascript_include` / `log.Filename` の相対パスは、`config.json` が置かれているディレクトリから解決されます。

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
  "log": {
    "Filename": "./logs/nyanpui.log",
    "MaxSize": 5,
    "MaxBackups": 3,
    "MaxAge": 7,
    "Compress": true,
    "EnableLogging": false
  }
}
```

### ログ設定（例）

```json
"log": {
  "Filename": "./logs/nyanpui.log",
  "MaxSize": 5,
  "MaxBackups": 3,
  "MaxAge": 7,
  "Compress": true,
  "EnableLogging": false
}
```

* **Filename**: ログ出力先パス
* **MaxSize**: 1 ファイルあたり最大サイズ (MB)
* **MaxBackups**: ローテーション保持数
* **MaxAge**: 保持日数
* **Compress**: gzip 圧縮 (true/false)
* **EnableLogging**: ログ出力をファイルに書くか（false でターミナル出力）

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

* **type**: 種別。省略時は通常 API、`public` は静的ファイル公開、`ws_client` は WebSocket クライアント、`schedule` は定期実行ジョブ
* **script**: 実行する JavaScript ファイル（空文字列なら HTML のみ返却）
* **html**: HTML ファイルパス
* **path**: `type: "public"` で公開するフォルダパス
* **trigger**: `type: "schedule"` で使う実行トリガー
* **paramCheck**: API 実行前に実行する JavaScript ファイル
* **outCheck**: API 出力前に実行する JavaScript ファイル
* **description**: 説明文
* **push**: WebSocket で配信するエンドポイント名

省略可能なフィールド: `type`, `script`, `html`, `path`, `connectURL`, `trigger`, `paramCheck`, `outCheck`, `description`, `push`。

`paramCheck` は `paramcheck`、`outCheck` は `outcheck` の小文字表記でも読み込めます。README では `paramCheck` / `outCheck` を推奨表記とします。

通常 API は `GET`, `POST`, `PUT`, `DELETE` などのメソッドを受け付けます。`Content-Type: application/json` の JSON body、フォーム値、URL クエリを `nyanAllParams` にまとめて渡します。`api` が未指定の場合は、エンドポイントパスまたは `html` が入ります。

### public フォルダ公開（`type: "public"`）

`type: "public"` を指定すると、`path` のフォルダ配下にあるファイルをそのまま配信します。`path` は実行ファイルのあるディレクトリからの相対パス、または絶対パスで指定できます。

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

`success: true` かつ `status: 200` の場合だけ次の処理へ進みます。それ以外は `paramCheck` の結果を JSON として返します。HTTP ステータスも `status` の値になります。

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

`outCheck` を指定すると、通常 API の本体実行後、または `type: "public"` のファイル送信前に JavaScript を実行できます。`outCheck` が成功した場合は本体の実行結果をそのまま出力し、失敗した場合は `outCheck` の結果を JSON として出力します。

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

`success: true` かつ `status: 200` の場合だけ本体の実行結果をそのまま出力します。それ以外は `outCheck` の結果を JSON として返します。HTTP ステータスも `status` の値になります。

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

受信したメッセージは `script` に渡され、次の値を `nyanAllParams` で参照できます。`script` の戻り値が空でない場合は、接続先 WebSocket にテキストメッセージとして返信します。切断時は 1 秒から最大 30 秒までの指数バックオフで再接続します。

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

1. `./NyanPUI` を起動（ログに `Starting WebSocket client ws_client/self_push_receive -> ws://127.0.0.1:8009/push/receive` が出ます）
2. ブラウザで `http://localhost:8009/push/request` を開く（push が飛びます）
3. ターミナルに `ws_client ... received ...` が出ればOK

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

同梱の `api.json` には動作確認用の `schedule_debug_every_minute` を追加しています。起動すると1分ごとに `javascript/schedule_debug.js` が実行され、ログへ実行時刻が出力されます。

## アプリケーションの実行

* `config.json` と `api.json` を編集後、実行ファイルを起動。
* デフォルトで [http://localhost:8009/](http://localhost:8009/) にアクセスするとサンプルが表示されます。 Windows MacOS Linuxで実行可能です。 各自でビルドいただくか、[リリース](https://github.com/NyanQL/NyanPUI/releases)からダウンロードしてください。
* `/nyan` にアクセスすると、`type: "schedule"` 以外の API 一覧を JSON で取得できます。
* `/css`, `/images`, `/js`, `/favicon.ico` は `html/` 配下の静的ファイルとして配信されます。

## ビルド

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
* Cookie 操作: `nyanGetCookie()` / `nyanSetCookie()`
* localStorage 操作: `nyanGetItem()` / `nyanSetItem()`
* 外部 APIの呼び出し : `nyanGetAPI()` / `nyanJsonAPI()` / `nyanCallAPI()`
* ホスト側でコマンドを実行し、結果を取得する: `nyanHostExec()`
* ファイル読み込み: `nyanGetFile()`
* バイナリをBase64で取得: `nyanReadFileB64()`
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
console.log はコンソールもしくはログファイルへ内容が出力されます。
どちらに表示されるかは config.json の `log.EnableLogging` で制御します。
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
### 5. **nyanGetItem / nyanSetItem**
ローカルストレージを操作制御します。
```javascript
// ローカルストレージから値を取得
var itemValue = nyanGetItem("itemKey");
console.log("Item Value: " + itemValue);
// ローカルストレージに値を設定
nyanSetItem("itemKey", "itemValue");
```

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
ファイルのパスは実行ファイル(NyanPUI)からの相対パスでも指定できます。
指定したファイルが存在しない場合はnullが返ります。
```javascript
var fileContent = nyanGetFile("./path/to/file.txt");
console.log("File Content: " + fileContent);
```

### 9. **nyanReadFileB64**
バイナリファイルをBase64文字列として取得します。
ファイルのパスはカレントディレクトリからの相対パス、または絶対パスで指定できます。存在しない場合は JavaScript 例外になります。
```javascript
var b64 = nyanReadFileB64("./html/images/nyan.png");
console.log(b64);
```

### 10. **nyanCallMe(data)**
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

### 11. **nyanPlate(data, htmlCode)**

テンプレート内に `data-nyan*` 属性を記述し、`nyanPlate(data, htmlCode)` で動的置換します。
詳細については [nyanPlate.js](javascript%2Flib%2FnyanPlate.js) の文頭にコメントで記載していますので
そちらを参照してください。
## WebSocket サンプル
WebSocket による双方向通信とプッシュ通知のサンプルを同梱しています。
* フロント: `http://localhost:8009/push/test`
* プッシュ: `http://localhost:8009/push/request` → `ws://localhost:8009/push/receive`

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

`method` には `api.json` の API 名を指定します。JSON-RPC では `script` が必須です。`type: "schedule"` は呼び出せません。成功時の `result` は JavaScript の戻り値を文字列化した値です。

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
