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
<html lang="ja">
<head>
    <title>NyanPUI Wireframe</title>
    <link href="/css/wf_style.css" rel="stylesheet" />
</head>
<body>

<header class="wf-header">Site Logo / Header</header>

<div class="wf-search">
    <input type="text" placeholder="キーワードを入力">
    <button class="wf-button" type="button">検索</button>
</div>

<nav class="wf-nav">
    <div class="wf-nav-item"><div class="wf-image">Image1</div>Nav 1</div>
    <div class="wf-nav-item"><div class="wf-image">Image2</div>Nav 2</div>
    <div class="wf-nav-item"><div class="wf-image">Image3</div>Nav 3</div>
</nav>

<section class="wf-hero">
    <div class="wf-image">Hello Image</div>
    <div class="wf-text"></div>
    <div class="wf-text short"></div>
</section>

<section class="wf-content">
    <div class="wf-box"><div class="wf-image">Image 1</div>Feature 1</div>
    <div class="wf-box">Feature 2</div>
    <div class="wf-box">Feature 3</div>
</section>

<section class="wf-form">
    <h2 class="wf-form-title">お問い合わせフォーム</h2>
    <input type="text" name="name" placeholder="お名前">
    <input type="email" name="email" placeholder="メールアドレス">
    <div><label class="checkbox"><input type="checkbox" name="subscribe"> チェックボックス</label></div>
    <div><label class="radio"><input type="radio" name="gender" value="1"> 男性</label>
         <label class="radio"><input type="radio" name="gender" value="2"> 女性</label></div>
    <div class="mt_32"><textarea name="message" placeholder="ご意見・ご要望"></textarea></div>
    <div class="right"><button class="wf-button">buttonタグのボタン</button></div>
    <div class="right"><a href="#" class="wf-button">aタグのボタン</a></div>
</section>

<form>
    <div class="wf-upload-box">
        <div class="wf-upload">
            <label for="fileUpload" class="wf-file-label">ファイルを選択</label>
            <input type="file" id="fileUpload">
            <a class="wf-button">アップロード</a>
        </div>
    </div>
</form>

<div class="wf-pager">
    <a href="#" class="wf-page">前へ</a>
    <a href="#" class="wf-page">1</a>
    <a href="#" class="wf-page">2</a>
    <a href="#" class="wf-page">3</a>
    <a href="#" class="wf-page">次へ</a>
</div>

<div class="wf-table-box">
    <table>
        <thead>
            <tr><th>項目1</th><th>項目2</th><th>項目3</th></tr>
        </thead>
        <tbody>
            <tr><td>xxxx</td><td>xxxxxx</td><td>xxxx</td></tr>
            <tr><td>xxxx</td><td>xxxxxx</td><td>xxxx</td></tr>
            <tr><td>xxxx</td><td>xxxxxx</td><td>xxxx</td></tr>
        </tbody>
    </table>
</div>

<footer class="wf-footer">footer</footer>
</body>
</html>
```

### ワイヤーフレーム表示の使い方

1. **エンドポイントを追加**
   `api.json` に以下のように `"wf"` エントリを追加してください：

   ```json
   {
     "wf": {
       "html": "./html/wf.html",
       "description": "ワイヤーフレームプレビュー"
     }
   }
   ```

    * `wf.html` は上記サンプル HTML を保存したファイルです。

2. **CSS を読み込む**
   `wf.html` の `<head>` に以下を記述して、先ほど設置した `wf_style.css` を読み込みます：

   ```html
   <link href="/css/wf_style.css" rel="stylesheet" />
   ```

3. **ブラウザで確認**
   サーバーを再起動した後、以下にアクセスして表示を確認できます：

   > [http://localhost:8009/wf](http://localhost:8009/wf)

以上で、ワイヤーフレームデザインのプレビュー機能が動作します。

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

## 予約語

`nyanAllParams` に格納されたキー名は、`api`/`nyan` で始まる文字列を避けてください。

---

## ワイヤーフレームデザインプレビュー

以下のファイルをプロジェクトに含めることで、ワイヤーフレーム用のプレビュー機能を利用できます：

* **CSS**: `html/css/wf_style.css` にワイヤーフレーム用のスタイルを定義
* **HTML**: `html/wf.html` にサンプルレイアウトを記述

これらを配置した状態でサーバーを起動すると、デフォルトで以下の URL からプレビューが表示されます：

> [http://localhost:8009/wf](http://localhost:8009/wf)

