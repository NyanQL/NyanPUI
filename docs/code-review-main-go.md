# main.go コードレビュー

- 対象: [main.go](../main.go)（5032行）
- ブランチ: `feature/v0.0.15`
- 日付: 2026-09-09
- `go vet ./...`: クリーン

## 総評

MCP/OAuth・cronスケジュール・WebSocketクライアント・ホットリロード・スナップショット方式のAPI設定管理など、設計はかなり作り込まれている。特に **APIConfigSnapshot による不変スナップショット化**、**OAuth状態ファイルのパストラバーサル・シンボリックリンク対策**（main.go:4867）、**argon2idの並列度制限**、**JSON重複キー検証** は堅牢。

一方で、Goのグローバル変数まわりに **並行性の重大なバグ** が複数ある。

---

## 🔴 Critical（データ競合・クラッシュ）

### 1. `ginContext` グローバル変数の共有 — main.go:626

```go
var ginContext *gin.Context
```

gin はリクエストごとに別 goroutine で処理するが、`ginContext` は package グローバル。main.go:953、main.go:1656、main.go:1717、main.go:2602 で `ginContext = c` → `defer ginContext = nil` としており、**同時リクエストが互いの context を上書き**する。

- 影響: `nyanGetCookie`/`nyanSetCookie`（main.go:2186）が **別リクエストの Cookie を操作**、あるいは `defer` で nil 化されて途中で nil 参照。
- データ競合（`-race` で検出可能）かつ機能的に正しくない。
- 対策: `ginContext` を廃止し、`*gin.Context`（または必要データ）を `runJavaScriptValueWithSnapshot` の引数として明示的に渡すか、goja VM のフィールドに紐付ける。

### 2. `storage` マップに mutex なし — main.go:624

```go
var storage = make(map[string]string)
```

`nyanSetItem`/`nyanGetItem`（main.go:2212-2231）が複数リクエスト goroutine から並行 read/write する。Go では **並行 map 書き込みは fatal panic でプロセス即死**する。`sync.RWMutex` か `sync.Map` が必須。

---

## 🟠 High（堅牢性・DoS）

### 3. goja 実行にタイムアウトなし — main.go:1266

`runtime.RunString(fullJSCode)` に interrupt/timeout がない。無限ループを含むスクリプトが1つあると、その goroutine が永久にブロックする。MCP は同時実行数を制限しているが（main.go:4166）、**実行時間の上限がない**ため枠を食い潰して DoS になり得る。`vm.Interrupt()` + `time.AfterFunc` の導入を推奨。

### 4. HTTP クライアントにタイムアウトなし — main.go:2034, main.go:2085

```go
client := &http.Client{}
```

`getAPI`/`jsonAPI` は無タイムアウト。応答しない上流があるとリクエスト goroutine が無限待機する。`Timeout` を設定すること（併せて `ioutil.ReadAll` は非推奨→`io.ReadAll`）。

### 5. WebSocket の Origin 無検証 — main.go:631

```go
CheckOrigin: func(r *http.Request) bool { return true }
```

全オリジンからの WS 接続を許可（CSWSH のリスク）。MCP 側は `allowedOrigins` を厳格検証しているのに、通常の WS エンドポイントは素通しで非対称。

---

## 🟡 Medium（設定・パフォーマンス）

### 6. CORS: `*` + credentials の組み合わせ — main.go:1985-1986

```go
c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
```

仕様上ブラウザはワイルドカード + credentials を拒否するため、意図した動作にならず、かつセキュリティ的にも危険な指定。オリジンをエコーバックする方式に統一するか、credentials を外すべき（MCP ハンドラは正しくエコーバックしている）。

### 7. リクエストごとの全ファイル再読込 — main.go:1224-1263

毎リクエストで `JavaScriptInclude` 全ファイル + HTML + スクリプトを `os.ReadFile` して文字列連結・再コンパイルしている。高頻度エンドポイントではディスク I/O が支配的になる。ホットリロードのハッシュで無効化するキャッシュを検討。

### 8. リクエストごとのログスパム — main.go:1227

```go
log.Print("Include file:", includePath)
```

全 include を毎リクエストで出力。debug レベル化か削除を。

### 9. `os.Executable()` の多重呼び出し

main.go:946, 1207, 1078, 2662, 2729, 2482 など多数でリクエスト毎に呼んでいる。起動時に一度解決してグローバル/引数で共有すれば syscall とエラー処理の重複を削減できる。

---

## 🔵 Low（一貫性・可読性）

### 10. `nyanReadFileB64` のベースパスが不整合 — main.go:2587

`os.Getwd()` を基準にしているが、`nyanGetFile`/`nyanSaveFile`/`nyanDeleteFile` は exe ディレクトリ基準。同じ相対パスでも解決先が変わり混乱の元。統一を。

### 11. `nyanSetCookie` の Secure=false 固定 — main.go:2204

HTTPS 運用でも `secure=false`・maxAge 3600 固定。TLS 有効時は Secure を立てる/引数化を検討。

### 12. `execGoja` の空 if ブロック — main.go:2340-2343

```go
if err != nil {
    // コメントのみ
}
```

デッドコード。lint 的にも削除推奨。

### 13. `collectRequestParams` — 多値と ParseForm エラーを黙殺 — main.go:1286-1292

`value[0]` のみ採用で多値パラメータ喪失、`ParseForm()` のエラーも無視。意図的なら OK だが要確認。

### 14. ファイル分割

5032行の単一 `main.go`。MCP・OAuth・cron・WS クライアント・設定ロードは責務が明確に分かれているので、パッケージ/ファイル分割で可読性が大きく向上する。

---

## まとめ

最優先は **#1（ginContext）と #2（storage）のデータ競合**。この2つは `-race` を付けたテスト、あるいは本番の同時アクセスで実際にクラッシュ/誤動作を起こす。次いで #3・#4 の無タイムアウトによる DoS 耐性。

修正着手時は、まず `go test -race ./...` で #1/#2 を再現させてから直すのが確実。
