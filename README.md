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

## アプリケーションの実行

* `config.json` と `api.json` を編集後、実行ファイルを起動。
* デフォルトで [http://localhost:8009/](http://localhost:8009/) にアクセスするとサンプルが表示されます。 Windows MacOS Linuxで実行可能です。 各自でビルドいただくか、[リリース](https://github.com/NyanQL/NyanPUI/releases)からダウンロードしてください。

## JavaScript 実行 (Goja) 環境で使用できる変数と関数

* リクエストパラメータ: `nyanAllParams`
* テンプレート HTML: `nyanHtmlCode`
* コンソール出力: `console.log()`
* Cookie 操作: `nyanGetCookie()` / `nyanSetCookie()`
* localStorage 操作: `nyanGetItem()` / `nyanSetItem()`
* 外部 APIの呼び出し : `nyanGetAPI()` / `nyanJsonAPI()`
* ホスト側でコマンドを実行し、結果を取得する: `nyanHostExec()`
* ファイル読み込み: `nyanGetFile()`

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

###  9. **nyanPlate(data, htmlCode)**

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

## ワイヤーフレームデザインプレビューについて
以下のファイルをプロジェクトに含めることで、ワイヤーフレーム用のプレビュー機能を利用できます：
* **CSS**: `html/css/wf_style.css` にワイヤーフレーム用のスタイルを定義
* **HTML**: `html/wf.html` にサンプルレイアウトを記述

これらを配置した状態でサーバーを起動すると、デフォルトで以下の URL からプレビューが表示されます：

> [http://localhost:8009/wf](http://localhost:8009/wf)

