# NyanPUI
NyanPUI(にゃんぷい)は、GoLangで作られたサーバーサイドJavaScript実行環境で、HTMLを出力します。
NyanPUIではjavascriptの実行エンジンは Goja( https://github.com/dop251/goja ) を採用しています。 
これはECMAScript 5.1 準拠のJavaScriptインタープリターです。
HTTP/HTTPS サーバーとして動作し、下記の機能を提供します。

API エンドポイントの定義（api.json）
JavaScript 実行環境（goja を利用）
WebSocket による双方向通信（gorilla/websocket を利用）
サーバーサイドでのプッシュ通知（特定のエンドポイントに対する HTML やスクリプトの結果を WebSocket で配信）

# ライセンス
MITライセンスです。 詳細は[LICENSE.md](LICENSE.md)を参照してください。

# 動作環境
OS: Windows / macOS / Linux など
必要に応じて TLS 証明書 (.crt) と秘密鍵 (.key) を用意してください。

# 構成
```
.
├── config.json          # システム設定
├── api.json             # エンドポイント設定
├── html/
  ├──html/css            # CSS/JSなどのフロントエンドリソース
  ├──html/js             # CSS/JSなどのフロントエンドリソース
  ├──html/images         # CSS/JSなどのフロントエンドリソース
├── javascript/          # goja で読み込むスクリプト(別の場所にもできます)
├── main.go              # エントリーポイント 実行する場合は実行ファイルの方を使用してください。
├── logs/                # ログファイルの出力先 (初回起動時に作成)
└── README.md            # 本ファイル
└── NyanPUI_XXX          #実行ファイル XXXの部分は実行させる環境のOSによって変わります。 
```

# 設定ファイル
## config.json

```json
{
  "name": "このAPIサーバの名前",
  "profile": "このAPIサーバの説明",
  "version": "このAPIサーバのversion",
  "port": "このAPIサーバの利用するポート番号を数値で記入してください。",
  "certPath": "TLS 証明書 (.crt) 指定があればhttpsで起動します。",
  "keyPath": "秘密鍵 (.key)",
  "javascript_include": [
    "goja で実行される際に一緒に読み込む JavaScript ファイルのパス一です。",
    "全体で共通で使う処理を記述したものをここで指定してください。",
    "複数ファイル設定できます。"
  ],
  "log": {
    "Filename": "ログファイルを保存する場所へのパス",
    "MaxSize": 5,
    "MaxBackups": 3,
    "MaxAge": 7,
    "Compress": true,
    "EnableLogging": false
  }
}

```

## log : ログファイルの設定
config.jsonより抜粋

```
"log": {
    "Filename": "./logs/nyanpui.log",
    "MaxSize": 5,
    "MaxBackups": 3,
    "MaxAge": 7,
    "Compress": true,
    "EnableLogging": false
  }
```

* Filename : ログファイルの出力先
* MaxSize : 1つのログファイルあたりの最大サイズ (MB)
* MaxBackups : ローテーションで保持するログファイル数
* MaxAge : ログファイルを保持する日数
* Compress : ログファイルを gzip 圧縮するか
* EnableLogging : ログ出力を有効化するかどうか (falseにするとターミナルに出力されます)

# API 定義ファイル (api.json)
API エンドポイントの定義は、api.json に記述します。以下は、サンプルの api.json です。
各キー (例: "html", "html2") : エンドポイント名。 /html や /html2 でアクセス可能になります。
また、?api=html のように URL以外で GET POST Jsonにて指定が可能です。

* script : JavaScript (goja で実行) のファイルパス。空文字列ならスクリプトを実行せず HTML を返すだけ
* html : HTML ファイルのパス
* description : API の説明
* push : push 先のエンドポイント名（API リクエスト完了後、WebSocket でメッセージを送信する先を指定）

pushとscriptは省略できます。省略した場合は、HTML ファイルのみを返します。

api.json(例)

```json
{
  "html": {
    "script": "./javascript/html.js",
    "html": "./html/index.html",
    "description": "トップページ"
   
  },
  "html2": {
    "html": "./html/html2.html",
    "description": "htmlファイル",
    "push": "test"
  },
  "test": {
    "html": "./html/test.html",
    "description": "テストページ"
  }
}
```

# アプリケーションの実行方法
まず、config.jsonとapi.jsonを編集してください。
改変せずに実行した場合には http://localhost:8009/ にアクセスするとサンプルページが表示されます。

## Windowsの場合
NyanPUI_Win.exe をダブルクリックして起動します。

## Macの場合
Nyan8_Macをダブルクリックするか、ターミナルで以下のコマンドを実行して起動します。

```
./NyanPUI_Mac
```

## Linuxの場合
ターミナルで以下のコマンドを実行して起動します。

```
./NyanPUI_Linux_x64
```

# JavaScript 実行 (goja)
* エンドポイントに script が設定されている場合、runJavaScript 関数を通じて goja でスクリプトを実行します。
* html ファイルの内容は nyanHtmlCode という変数として JavaScript 内で参照可能です。
* リクエストパラメータ (POST / Query / json) は nyanAllParams オブジェクトとしてスクリプトに渡されます。

# GojaでうごくJavascriptのサンプル
## postやgetやjsonなどで受信されたパラメータの取得
nyanAllParamsに格納されています。

```javascript
console.log(nyanAllParams);
```

## console.log が使えます
ログファイルの設定を有効にしている場合はログファイルに出力されます。
無効にしている場合はターミナルに出力されます。

```javascript
console.log("Hello, NyanPUI!");
```

## Cookieの操作
cookieの設定が可能です。

```javascript
nyanSetCookie("nyanpui", "kawaii");
```

cookieの取得が可能です。

```javascript
console.log(nyanGetCookie("nyanpui"));
```

## localStorageの操作
localStorageの設定が可能です。

```javascript
nyanSetItem("nyanpui", "kawaii");
```

localStorageの取得が可能です。

```javascript
nyanGetItem("nyanpui");
```

## 外部APIの利用
外部APIの利用が可能です。
取得したデータはJSON.parseでパースしてください。

getでの取得の場合

```javascript
//apiURL: apiのURL
//apiUser basic認証のID
//apiPass basic認証のパスワード
console.log(nyanGetAPI(apiURL,apiUser,apiPass));
```

jsonでの取得の場合

```javascript
//apiのURL  apiURL
//basic認証のID  apiUser
//basic認証のパスワード apiPass
//javascript内でデータとして扱う場合、JSON.parse()で文字列から変換をする必要があります。
const data = {
            api: "create_user",
            username: nyanAllParams.username,
            password: nyanAllParams.password,
            email: nyanAllParams.email,
            salt: saltKey
        };
const result = nyanJsonAPI(
        apiURL,
        JSON.stringify(data),
        apiUser,
        apiPass
    );
const resultData = JSON.parse(result);
```


## HTMLファイルに書かれているコードの操作
nyanHtmlCodeに格納されていますので、文字置き換えや連結等に利用してください。

```javascript
console.log(nyanHtmlCode);
```

## hostでのコマンド実行と結果の取得
hostでのコマンド実行が可能です。

```javascript
console.log(nyanHostExec("ls"));
```

実行結果は次のような構成になって取得できます。

```json
{"success":true,"exit_code":0,"stdout":"コマンドの実行結果","stderr":""}
```

* success : コマンドの実行が成功したかどうか
* exit_code : コマンドの終了コード
* stdout : 標準出力
* stderr : 標準エラー出力

## ファイルの読み込み

ファイルの読み込みができます。

```js
let text = nyanGetFile("ファイルのパス");
let data = JSON.parse(text);
```


# HTMLファイルとの連携について
## にゃんぷれ (NyanPlate)
にゃんぷれは、HTML テンプレート内のカスタムデータ属性を使って JSON データに基づいてコンテンツをレンダリングします。
独自のカスタムデータ属性を使って HTML テンプレート内の文字列や属性を動的に置換し、JSON データに基づいてコンテンツをレンダリングできます。

## 特徴
### 動的文字列置換
要素内のテキスト（またはコメント内のテキスト）を `data-nyanString="key"` 属性に基づいて JSON データの該当キーの値に置き換えます。

### HTML部品の動的挿入
HTMLを `data-nyanHTML="key"` 属性に基づいて JSON データの該当キーの値に置き換えます。
ファイルを読み込み、nyanPlateでHTMLを生成したものを設置する場合などに利用します。

### ループ処理
`data-nyanLoop="key"` 属性を持つ要素内のテンプレート部分を、JSON 配列の各アイテムごとに展開します。
各ループ項目では、個別のデータコンテキストに基づいて文字列、クラス、スタイルの置換が行われます。

### クラス・スタイルの動的変更
`data-nyanClass="key"` および `data-nyanStyle="key"` 属性を使い、要素の class や style 属性を JSON データの値で置換します。
ループ内のテンプレートとグローバルな置換処理の干渉を防ぐため、ループ展開後に不要な属性を削除する工夫を施しています。

### 仕組みの概要
テンプレート記述
HTML 内に以下のようなカスタムデータ属性を記述します。

```html
<h1 data-nyanString="title" data-nyanStyle="title_style" data-nyanClass="className">Default Title</h1>

<table>
  <thead>
    <tr>
      <th>商品名</th>
      <th>価格</th>
    </tr>
  </thead>
  <tbody data-nyanLoop="items">
    <tr>
      <td data-nyanString="title" data-nyanClass="className">Default Product</td>
      <td data-nyanString="price">0</td>
    </tr>
  </tbody>
</table>
```

### JSON データ定義
テンプレート内の各キーに対応するデータを JSON で定義します。

```javascript
const data = {
    title: "タイトル",
    title_style: "text-align: center; color: blue;",
    className: "featured",
    items: [
            { title: "りんご", price: "100", className: "apple" },
            { title: "みかん", price: "50", className: "orange" },
            { title: "バナナ", price: "80", className: "banana" }
        ]
    };
```

### 実行方法
メイン関数 nyanPlate(data, htmlCode) を呼び出すことで、指定したテンプレート（またはデフォルトテンプレート）内の置換処理が順次実行され、最終的な HTML が生成されます。
htmlCodeを省略すると、nyanHtmlCode (api.jsonで指定されたHTMLファイルの中身) が使われます。

```javascript
const finalHtml = nyanPlate(data);
console.log(finalHtml);
```

## にゃんぷれ (NyanPlate) で作成されたHTMLのJSON化
### 1. 関数を用意
javascript/lib/nyanPlateToJson.jsをconfig.jsonの `javascript_include` で指定し、利用可能な状態にしてください。
HTML文字列は nyanGetFile("ファイルのパス") や nyanGetAPIを使ってHTMLを取得することもできます。

```javascript
// HTML文字列を用意
var html = '<html> ... あなたのHTML ... </html>';

// nyanPlateToJsonを呼び出す
var result = nyanPlateToJson(html);

// JSONを表示
console.log(JSON.stringify(result, null, 2));
```

(例) 
入力HTML
```html
<h1 data-nyanDoneNyanString="title" style="color: blue;" data-nyanDoneStyle="title_style" class="orange" data-nyanDoneClass="className">タイトル</h1>
<table data-nyanDoneNyanHtml="data">
  <tbody data-nyanLoop="items">
    <tr>
      <td data-nyanDoneNyanString="name">りんご</td>
    </tr>
    <tr>
      <td data-nyanDoneNyanString="name">バナナ</td>
    </tr>
  </tbody>
</table>
```
出力されるJSON
```json
{
  "title": "タイトル",
  "title_style": "color: blue;",
  "className": "orange",
  "data": {
    "items": [
      { "name": "りんご" },
      { "name": "バナナ" }
    ]
  }
}
```



# NyanPUIでのWebSocketの使い方
サンプルプログラムを用意しました。
WebSocketを使うことで、サーバーからクライアントにメッセージを送信することができます。

* http://localhost:8009/test にアクセスするとフロントエンドでWebSocketを使ってサーバーからメッセージを受け取ります。
* 上記を開いた状態で 別のタブ等から http://localhost:8009/push/request にアクセスすると受信したメッセージが表示されることを確認できます。

サンプルプログラムのしくみ
* http://localhost:8009/push/request にアクセスするとサーバーからwebsocketで ws://localhost:8009/push/receive にメッセージを送信します。
* http://localhost:8009/test に書かれているjavascriptは上記に接続していてサーバからのpushを受け取る処理が書かれています。

# JSON-RPC対応について
http(s)://{hostname}:{port}/nyan-rpc にアクセスすると、JSON-RPCのAPIを利用することができます。
NyanPUIはJSON-RPC 2.0に準拠したAPIを提供しています。 ただし、現在 6.Batch については未実装です。

http(s)://{hostname}:{port}/nyan-rpc にアクセスすると、JSON-RPCのAPIを利用することができます。
JSON-RPC 2.0の仕様については、[こちら](https://www.jsonrpc.org/specification)を参照してください。
以下のようなJSON-RPCリクエストを送信することで、APIを呼び出すことができます。

```json
{
  "jsonrpc": "2.0",
  "method": "api名",
  "params": "にゃんぷいが生成したHTMLテキスト",
  "id": 1
}
```


# このAPIサーバの情報を取得する場合
http(s)://{hostname}:{port}/nyan にアクセスすると、このAPIサーバの情報を取得することができます。
http(s)://{hostname}:{port}/nyan/{API名} にアクセスすると、APIの情報を取得することができます。


# 予約語について
リクエストされた値はnyanAllParamsに格納されています。
apiとnyanから始まるものは予約語となります。 パラメータなどで使用しないようご注意ください。 NyanQLとその仲間の共通ルールです。

