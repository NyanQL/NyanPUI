# NyanPUI

NyanPUI(にゃんぷい)は、GoLangで作られたサーバーサイドレンダリングを行うプレゼンテーションフレームワークです。 リクエストに対してJavaScriptを実行してHTMLを出力します。

* **JavaScript エンジン**: Goja (ECMAScript 5.1 準拠) – [https://github.com/dop251/goja](https://github.com/dop251/goja)
* **双方向通信**: gorilla/websocket による WebSocket
* **プッシュ通知**: 特定エンドポイントの処理結果を WebSocket で配信

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
    "javascript/nyanPlateToJson.js"
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

* **script**: 実行する JavaScript ファイル（空文字列なら HTML のみ返却）
* **html**: HTML ファイルパス
* **description**: 説明文
* **push**: WebSocket で配信するエンドポイント名

省略可能なフィールド: `script`, `push`。

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

#### 動作確認（NyanPUI 自身に接続）

リポジトリ同梱の `api.json` には、NyanPUI 自身の `/push/receive` に接続するサンプル（`ws_client/self_push_receive`）があります。

1. `./NyanPUI` を起動（ログに `Starting WebSocket client ws_client/self_push_receive -> ws://127.0.0.1:8009/push/receive` が出ます）
2. ブラウザで `http://localhost:8009/push/request` を開く（push が飛びます）
3. ターミナルに `ws_client ... received ...` が出ればOK

ポートを変更している場合は `api.json` の `connectURL` を合わせてください。

## アプリケーションの実行

* `config.json` と `api.json` を編集後、実行ファイルを起動。
* デフォルトで [http://localhost:8009/](http://localhost:8009/) にアクセスするとサンプルが表示されます。 Windows MacOS Linuxで実行可能です。 各自でビルドいただくか、[リリース](https://github.com/NyanQL/NyanPUI/releases)からダウンロードしてください。

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
* 外部 APIの呼び出し : `nyanGetAPI()` / `nyanJsonAPI()`
* ホスト側でコマンドを実行し、結果を取得する: `nyanHostExec()`
* ファイル読み込み: `nyanGetFile()`
* バイナリをBase64で取得: `nyanReadFileB64()`

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

### 6. **nyanGetAPI / nyanJsonAPI**
外部 API を呼び出します。 GET リクエストは nyanGetAPI、 POST リクエストは nyanJsonAPI を使用します。
引数はリクエスト先URL, 送信データ, Basic認証ユーザー名, Basic認証パスワードの順です。
```javascript
var sendData = { key: "value" };
// GET リクエスト
var response = nyanGetAPI("https://api.example.com/data", sendData, "nyan" , "password");
console.log("GET Response: " + response);
// JSONをPOSTして リクエスト
var postData = { key: "value" };
var jsonResponse = nyanJsonAPI("https://api.example.com/update", postData , "nyan" , "password");
console.log("POST Response: " + jsonResponse);
```

headerを追加したい場合は、オプションを追加してください。
```javascript
const headers = {
  "Content-Type": "application/json",
  "X-Custom-Header": "CustomValue"
};
var response = nyanGetAPI("https://api.example.com/data", sendData, "nyan" , "password", headers);
```

### 7. **nyanHostExec**
ホスト側でコマンドを実行し、結果を取得します。
```javascript
var result = nyanHostExec("ls -la");
console.log("Command Result: " + result);
```
結果は次のようになります。上記例ですと、下記の stdoutにlsコマンドの結果が格納されます。
```json
{
  "stdout": "コマンドの標準出力",
  "stderr": "コマンドの標準エラー出力",
  "exitCode": 0
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
ファイルのパスはカレントディレクトリからの相対パスでも指定できます。
```javascript
var b64 = nyanReadFileB64("./html/images/nyan.png");
console.log(b64);
```

### 10. **nyanPlate(data, htmlCode)**

テンプレート内に `data-nyan*` 属性を記述し、`nyanPlate(data, htmlCode)` で動的置換します。
詳細については [nyanPlate.js](javascript%2Flib%2FnyanPlate.js) の文頭にコメントで記載していますので
そちらを参照してください。
## WebSocket サンプル
WebSocket による双方向通信とプッシュ通知のサンプルを同梱しています。
* フロント: `http://localhost:8009/test`
* プッシュ: `http://localhost:8009/push/request` → `ws://localhost:8009/push/receive`

## JSON-RPC 対応
JSON-RPC 2.0 API を実装しています。（Batch は未実装）。
/nyan-rpc エンドポイントに POST リクエストを送ると、JSON-RPC 形式でレスポンスが返ります。
```json
{
  "jsonrpc": "2.0",
  "method": "api名",
  "params": "生成された HTML",
  "id": 1
}
```

## 予約語
`api` と `nyan` で始まる文字列を避けてください。

---

## JavaScriptのレスポンス形式（拡張）
JavaScript が文字列を返した場合は従来どおり HTML として返します。
オブジェクトを返すと、HTTPレスポンスを制御できます。

```javascript
({
  status: 200, // 省略可（デフォルト200）
  headers: { "Cache-Control": "no-store" }, // 省略可
  contentType: "application/json; charset=utf-8", // 省略可
  body: { ok: true, items: [1, 2, 3] } // object/arrayはJSON化、stringはそのまま
});
```

バイナリを返す場合は `body.encoding = "base64"` を指定します。

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

---

## ワイヤーフレームデザインプレビューについて
以下のファイルをプロジェクトに含めることで、ワイヤーフレーム用のプレビュー機能を利用できます：
* **CSS**: `html/css/wf_style.css` にワイヤーフレーム用のスタイルを定義
* **HTML**: `html/wf.html` にサンプルレイアウトを記述

これらを配置した状態でサーバーを起動すると、デフォルトで以下の URL からプレビューが表示されます：

> [http://localhost:8009/wf](http://localhost:8009/wf)
