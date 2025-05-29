# NyanPUI

NyanPUI(にゃんぷい)は、GoLangで作られたサーバーサイドレンダリングを行うプレゼンテーションフレームワークです。
リクエストに対してJavaScriptを実行してHTMLを出力します。

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
* デフォルトで [http://localhost:8009/](http://localhost:8009/) にアクセスするとサンプルが表示されます。
Windows MacOS Linuxで実行可能です。
各自でビルドいただくか、[リリース](https://github.com/NyanQL/NyanPUI/releases)からダウンロードしてください

## JavaScript 実行 (Goja)

* リクエストパラメータ: `nyanAllParams`
* テンプレート HTML: `nyanHtmlCode`
* コンソール出力: `console.log`
* Cookie 操作: `nyanGetCookie` / `nyanSetCookie`
* localStorage 操作: `nyanGetItem` / `nyanSetItem`
* 外部 API: `nyanGetAPI` / `nyanJsonAPI`
* ホストコマンド実行: `nyanHostExec`
* ファイル読み込み: `nyanGetFile`

## NyanPlate (HTML テンプレート)

テンプレート内に `data-nyan*` 属性を記述し、`nyanPlate(data, htmlCode)` で動的置換します。

### 主なカスタム属性

```html
<h1 data-nyanString="title" data-nyanStyle="title_style" data-nyanClass="className">Default</h1>
<tbody data-nyanLoop="items">
  <tr>
    <td data-nyanString="name">Product</td>
    <td data-nyanString="price">0</td>
  </tr>
</tbody>
```

* **data-nyanString**: テキスト置換
* **data-nyanHtml**: HTML 部品挿入
* **data-nyanLoop**: 配列展開
* **data-nyanClass** / **data-nyanStyle**: class/style 動的変更

## HTML → JSON 変換: nyanPlateToJson

`javascript/nyanPlateToJson.js` を `config.json` の `javascript_include` に追加してください。

```javascript
// HTML 文字列を取得
var html = nyanGetFile("./output.html");
// 変換実行
var result = nyanPlateToJson(html);
console.log(JSON.stringify(result, null, 2));
```

### ES5.1 互換版サンプル

```javascript
function nyanPlateToJson(htmlString) {
  /*...（上で提供したES5版の全文をここにペースト）...*/
}
```

## WebSocket サンプル

* フロント: `http://localhost:8009/test`
* プッシュ: `http://localhost:8009/push/request` → `ws://localhost:8009/push/receive`

## JSON-RPC 対応

`/nyan-rpc` で JSON-RPC 2.0 API を提供（Batch は未実装）。

```json
{
  "jsonrpc": "2.0",
  "method": "api名",
  "params": "生成された HTML",
  "id": 1
}
```

## サーバー情報取得

* `/nyan`: サーバー情報
* `/nyan/{API名}`: API 情報

## 予約語

`nyanAllParams` に格納されたキー名は、`api`/`nyan` で始まる文字列を避けてください。
