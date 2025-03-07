package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/dop251/goja"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/natefinch/lumberjack"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
)

// Config は設定データを表します。
type Config struct {
	Name              string    `json:"name"`
	Profile           string    `json:"profile"`
	Version           string    `json:"version"`
	Port              int       `json:"port"`
	CertFile          string    `json:"certPath"`
	KeyFile           string    `json:"keyPath"`
	JavaScriptInclude []string  `json:"javascript_include"`
	Log               LogConfig `json:"log"`
}

// LogConfig はログ設定を表します。
type LogConfig struct {
	Filename      string `json:"Filename"`
	MaxSize       int    `json:"MaxSize"`
	MaxBackups    int    `json:"MaxBackups"`
	MaxAge        int    `json:"MaxAge"`
	Compress      bool   `json:"Compress"`
	EnableLogging bool   `json:"EnableLogging"`
}

// ResponseData はAPIのレスポンスデータを表します。
type ResponseData struct {
	Success bool        `json:"success"`
	Error   *ErrorData  `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// ErrorData はエラーデータを表します。
type ErrorData struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// EndpointConfig はエンドポイントの設定を表します。
type EndpointConfig struct {
	Script      string `json:"script"`
	HTML        string `json:"html"`
	Description string `json:"description"`
	Push        string `json:"push,omitempty"`
}

type APIConfig map[string]EndpointConfig

type NyanResponse struct {
	Name    string             `json:"name"`
	Profile string             `json:"profile"`
	Version string             `json:"version"`
	Apis    map[string]ApiData `json:"apis"`
}

type ApiData struct {
	Description string `json:"description"`
	Push        string `json:"push,omitempty"`
}

type ExecResult struct {
	Success  bool   `json:"success"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// このシステムの設定
var globalConfig Config

// api.jsonから取得する設定
var apiConfig APIConfig

// ストレージ
var storage = make(map[string]string)

var ginContext *gin.Context

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 必要に応じて、オリジンチェックを行う
	},
}

var wsConnections = struct {
	sync.RWMutex
	conns map[string][]*websocket.Conn
}{
	conns: make(map[string][]*websocket.Conn),
}

// main はメイン関数です。
func main() {
	// 実行ファイルのディレクトリを取得
	exePath, err := os.Executable()
	if err != nil {
		log.Fatal("Failed to get executable path:", err)
	}
	exeDir := filepath.Dir(exePath)

	// システム設定をロード
	configPath := resolvePath(exeDir, "config.json")
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatal("Error loading config:", err)
	}
	globalConfig = config

	// ログ設定を初期化
	if globalConfig.Log.EnableLogging {
		// ファイルへのログ出力のみ行う
		initLogger(globalConfig.Log, exeDir)
		// Gin のログ設定もファイルへのみ出力するようにする
		logFile := resolvePath(exeDir, globalConfig.Log.Filename)
		f, err := os.Create(logFile)
		if err != nil {
			log.Printf("Failed to create log file: %v", err)
		} else {
			gin.DefaultWriter = f
		}
	} else {
		// ログをターミナル（標準出力）のみに出力する
		log.SetOutput(os.Stdout)
		gin.DefaultWriter = os.Stdout
	}

	// API設定をロード
	apiConfigPath := resolvePath(exeDir, "api.json")
	if err := loadAPIConfig(apiConfigPath); err != nil {
		log.Fatal("Error loading API configuration:", err)
	}

	gin.DisableConsoleColor()
	r := gin.Default()
	r.SetTrustedProxies(nil)
	r.Use(CORSMiddleware())
	r.StaticFile("/favicon.ico", resolvePath(exeDir, "./html/favicon.ico"))
	r.Static("/css", resolvePath(exeDir, "./html/css"))
	r.Static("/images", resolvePath(exeDir, "./html/images"))
	r.Static("/js", resolvePath(exeDir, "./html/js"))

	r.GET("/nyan", handleNyan)

	// 各APIエンドポイントを設定
	for endpoint := range apiConfig {
		config := apiConfig[endpoint] // ループ変数をローカル変数にコピー
		r.Any("/"+endpoint, func(c *gin.Context) {
			handleAPIRequestOrWebSocket(c, config)
		})
	}

	r.Any("/", func(c *gin.Context) {
		// クエリパラメータ "api" をチェック
		apiName := c.Query("api")
		if apiName != "" {
			if config, ok := apiConfig[apiName]; ok {
				handleAPIRequestOrWebSocket(c, config)
				return
			} else {
				c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
				return
			}
		}
		// "api" パラメータがなければ、デフォルトで "html" を使用
		handleAPIRequestOrWebSocket(c, apiConfig["html"])
	})

	// HTTPSサーバーを起動するかどうかを判断
	certFile := resolvePath(exeDir, globalConfig.CertFile)
	keyFile := resolvePath(exeDir, globalConfig.KeyFile)
	if globalConfig.CertFile != "" && globalConfig.KeyFile != "" {
		log.Printf("Starting HTTPS server at %d", globalConfig.Port)
		err = r.RunTLS(fmt.Sprintf(":%d", globalConfig.Port), certFile, keyFile)
		if err != nil {
			log.Fatal("Failed to start HTTPS server:", err)
		}
	} else {
		log.Printf("Starting HTTP server at %d", globalConfig.Port)
		err = r.Run(fmt.Sprintf(":%d", globalConfig.Port))
		if err != nil {
			log.Fatal("Failed to start HTTP server:", err)
		}
	}
}

// resolvePath は絶対パスを返すユーティリティ関数です。
func resolvePath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

// loadConfig は設定ファイルを読み込みます。
func loadConfig(filename string) (Config, error) {
	var config Config

	// 設定ファイルを読み込む
	data, err := os.ReadFile(filename)
	if err != nil {
		return config, err
	}

	// 設定ファイルの内容をConfig構造体にパースする
	if err := json.Unmarshal(data, &config); err != nil {
		return config, err
	}
	return config, nil
}

// apiの設定を読み込みます。
func loadAPIConfig(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &apiConfig); err != nil {
		return err
	}
	return nil
}

// handleAPIRequestOrWebSocket はAPIリクエストまたはWebSocketリクエストを処理します。
func handleAPIRequestOrWebSocket(c *gin.Context, config EndpointConfig) {
	if websocket.IsWebSocketUpgrade(c.Request) {
		handleWebSocket(c, config)
	} else {
		handleAPIRequest(c, config)
	}
}

// handleAPIRequest はAPIリクエストを処理します。
func handleAPIRequest(c *gin.Context, config EndpointConfig) {
	// HTTP/2サーバープッシュの処理は削除

	// 実行ファイルのディレクトリを取得
	exePath, err := os.Executable()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get executable path"})
		return
	}
	exeDir := filepath.Dir(exePath)

	ginContext = c
	defer func() { ginContext = nil }()

	// リクエストのコンテンツタイプを取得
	contentType := c.ContentType()

	// リクエストパラメータの収集
	allParams := make(map[string]interface{})
	if contentType == "application/json" {
		var requestData map[string]interface{}
		if err := c.BindJSON(&requestData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON data"})
			return
		}
		for k, v := range requestData {
			allParams[k] = v
		}
	}

	// URLクエリパラメータとPOSTフォームパラメータを追加
	c.Request.ParseForm()
	for k, v := range c.Request.PostForm {
		allParams[k] = v[0]
	}
	for k, v := range c.Request.URL.Query() {
		allParams[k] = v[0]
	}

	if allParams["api"] == nil {
		if c.Request.URL.Path != "/" {
			allParams["api"] = c.Request.URL.Path
		} else {
			allParams["api"] = "html"
		}
	}

	// スクリプトとHTMLファイルのパスを取得
	scriptPath := resolvePath(exeDir, config.Script)
	htmlPath := resolvePath(exeDir, config.HTML)

	// scriptが空の場合、HTMLファイルの内容をそのまま返す
	if config.Script == "" {
		htmlContent, err := os.ReadFile(htmlPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load HTML file"})
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", htmlContent)
		return
	}

	// JavaScriptを実行し、結果を取得
	result, err := runJavaScript(scriptPath, htmlPath, allParams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// HTML出力として結果を返す
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(result))

	// push 設定がある場合、対象のWebSocket接続に対してプッシュ
	// API リクエスト完了後の push 処理
	if config.Push != "" {
		// push 先の EndpointConfig を取得
		pushConfig, ok := apiConfig[config.Push]
		if !ok {
			log.Printf("Push target %s not found in apiConfig", config.Push)
		} else {
			exePath, err := os.Executable()
			if err != nil {
				log.Printf("Failed to get executable path for push: %v", err)
				return
			}
			exeDir := filepath.Dir(exePath)

			// scriptPath, htmlPath を pushConfig から解決
			scriptPath := resolvePath(exeDir, pushConfig.Script)
			htmlPath := resolvePath(exeDir, pushConfig.HTML)

			// allParams をどうするか検討：同じパラメータを使うなら reuse する、push 用に変えたいなら新しく定義する
			// ここでは同じ allParams を使う例
			pushResult := ""
			if pushConfig.Script == "" {
				// script が空の場合は HTML ファイルをそのまま送る（従来通り）
				content, err := os.ReadFile(htmlPath)
				if err != nil {
					log.Printf("Failed to read push HTML file %s: %v", htmlPath, err)
				} else {
					pushResult = string(content)
				}
			} else {
				// script がある場合は実行し、その結果を push
				r, err := runJavaScript(scriptPath, htmlPath, allParams)
				if err != nil {
					log.Printf("Failed to run push script: %v", err)
				} else {
					pushResult = r
				}
			}

			if pushResult != "" {
				wsConnections.RLock()
				pushConns := wsConnections.conns[config.Push]
				wsConnections.RUnlock()

				for _, conn := range pushConns {
					if err := conn.WriteMessage(websocket.TextMessage, []byte(pushResult)); err != nil {
						log.Printf("Error pushing message to %s: %v", config.Push, err)
					} else {
						log.Printf("Push message sent to %s", config.Push)
					}
				}
			}
		}
	}

}

// handleWebSocket はWebSocketリクエストを処理します。
func handleWebSocket(c *gin.Context, config EndpointConfig) {
	endpoint := c.Request.URL.Path[1:] // 例: "/html2" -> "html2"
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to set websocket upgrade: %v", err)
		sendHTMLErrorResponse(c.Writer, "WebSocket upgrade failed")
		return
	}
	// 登録処理
	wsConnections.Lock()
	wsConnections.conns[endpoint] = append(wsConnections.conns[endpoint], conn)
	wsConnections.Unlock()

	// 接続終了時に削除する
	defer func() {
		wsConnections.Lock()
		conns := wsConnections.conns[endpoint]
		for i, c := range conns {
			if c == conn {
				wsConnections.conns[endpoint] = append(conns[:i], conns[i+1:]...)
				break
			}
		}
		wsConnections.Unlock()
		conn.Close()
	}()

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Error reading message: %v", err)
			sendWebSocketHTMLError(conn, messageType, "Error reading message")
			break
		}
		log.Printf("Received message on %s: %s", endpoint, message)

		// 受信したメッセージを JSON としてパース
		var req map[string]string
		if err := json.Unmarshal(message, &req); err != nil {
			log.Printf("Invalid JSON received: %v", err)
			// JSON パースに失敗した場合はエコーするか、エラーメッセージを返す
			conn.WriteMessage(messageType, []byte("Invalid JSON"))
			continue
		}

		// "api" キーがあるかチェック
		if apiName, ok := req["api"]; ok {
			// apiConfig から対象の設定を取得
			apiCfg, found := apiConfig[apiName]
			if !found {
				errMsg := fmt.Sprintf("API %s not found", apiName)
				conn.WriteMessage(messageType, []byte(errMsg))
				continue
			}
			// 例として、スクリプトが空の場合は HTML ファイルの内容を返す実装
			exePath, err := os.Executable()
			if err != nil {
				conn.WriteMessage(messageType, []byte("Server error"))
				continue
			}
			exeDir := filepath.Dir(exePath)
			htmlPath := resolvePath(exeDir, apiCfg.HTML)
			content, err := os.ReadFile(htmlPath)
			if err != nil {
				errMsg := fmt.Sprintf("Failed to read HTML file for API %s: %v", apiName, err)
				conn.WriteMessage(messageType, []byte(errMsg))
				continue
			}
			// 取得した内容を返信
			if err := conn.WriteMessage(websocket.TextMessage, content); err != nil {
				log.Printf("Error writing message for API %s: %v", apiName, err)
			}
		} else {
			// "api" キーが無い場合はエコーするか、適宜処理を追加
			if err := conn.WriteMessage(messageType, message); err != nil {
				log.Printf("Error writing echo message: %v", err)
			}
		}
	}
}


// sendWebSocketHTMLError はWebSocket接続にHTML形式のエラーメッセージを送信します。
func sendWebSocketHTMLError(conn *websocket.Conn, messageType int, errorMessage string) {
	errorHTML := fmt.Sprintf("<html><body><h1>Error</h1><p>%s</p></body></html>", errorMessage)
	if err := conn.WriteMessage(messageType, []byte(errorHTML)); err != nil {
		log.Printf("Error writing error message: %v", err)
	}
}

// sendHTMLErrorResponse はWebSocketアップグレードの際に発生したエラーをHTMLでクライアントに送信します。
func sendHTMLErrorResponse(w http.ResponseWriter, errorMessage string) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusInternalServerError)
	errorHTML := fmt.Sprintf("<html><body><h1>Error</h1><p>%s</p></body></html>", errorMessage)
	w.Write([]byte(errorHTML))
}

// runJavaScript はJavaScriptを実行します。
func runJavaScript(scriptPath string, htmlPath string, allParams map[string]interface{}) (string, error) {
	// 実行ファイルのディレクトリを取得
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("Failed to get executable path: %v", err)
	}
	exeDir := filepath.Dir(exePath)

	// スクリプトとHTMLファイルのパスを解決
	scriptPath = resolvePath(exeDir, scriptPath)
	htmlPath = resolvePath(exeDir, htmlPath)

	// goja ランタイムのセットアップ（リクエストごとに新しいランタイムを作る）
	runtime := setupGojaRuntime()

	// ライブラリの JavaScript ファイルを読み込み
	var jsLibCode string
	for _, includePath := range globalConfig.JavaScriptInclude {
		includePath = resolvePath(exeDir, includePath)
		code, err := os.ReadFile(includePath)
		log.Print("Include file:", includePath)
		if err != nil {
			return "", fmt.Errorf("failed to read included JS file %s: %v", includePath, err)
		}
		jsLibCode += string(code) + "\n"
	}

	// HTMLファイルを読み込み
	htmlCodeBytes, err := os.ReadFile(htmlPath)
	if err != nil {
		log.Printf("Failed to load HTML file at path: %s, error: %v", htmlPath, err)
		return "", fmt.Errorf("failed to load HTML file: %v", err)
	}
	escapedHTML := strconv.Quote(string(htmlCodeBytes))
	htmlJS := fmt.Sprintf("const nyanHtmlCode = %s;\n", escapedHTML)

	// リクエストパラメータをJSON文字列に変換してJavaScript変数として設定
	allParamsJSON, err := json.Marshal(allParams)
	if err != nil {
		return "", err
	}
	//変数に格納
	paramsJS := fmt.Sprintf("const nyanAllParams = %s;\n", allParamsJSON)
	// JavaScriptファイル本体を読み込み
	jsCodeBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read JavaScript file: %v", err)
	}

	// 全体の JavaScript コードを結合
	fullJSCode := jsLibCode + htmlJS + paramsJS + string(jsCodeBytes)

	// スクリプトを実行
	value, err := runtime.RunString(fullJSCode)
	if err != nil {
		return "", err
	}

	// 結果を文字列として取得
	return value.String(), nil
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// loadHTMLFile は指定されたHTMLファイルを読み込み、その内容を文字列として返します。
func loadHTMLFile(filePath string) (string, error) {
	htmlBytes, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(htmlBytes), nil
}

func getAPI(url, username, password string) (string, error) {
	// HTTPクライアントの生成
	client := &http.Client{}

	// リクエストの生成
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	// BASIC認証ヘッダーの設定
	if username != "" {
		req.SetBasicAuth(username, password)
	}

	// リクエストの送信
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request: %v", err)
	}
	defer resp.Body.Close()

	// レスポンスの読み取り
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response: %v", err)
	}
	return string(body), nil
}

// POSTリクエストを行うGo関数
func jsonAPI(url string, jsonData []byte, username, password string, headers map[string]string) (string, error) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}

	// BASIC認証のセットアップ（usernameが空でなければ）
	if username != "" {
		basicAuth := username + ":" + password
		basicAuthEncoded := base64.StdEncoding.EncodeToString([]byte(basicAuth))
		req.Header.Set("Authorization", "Basic "+basicAuthEncoded)
	}

	req.Header.Set("Content-Type", "application/json")

	// 追加のヘッダーが指定されていれば設定（複数指定可能）
	if headers != nil {
		for key, value := range headers {
			req.Header.Set(key, value)
		}
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ログの設定を有効化する。
func initLogger(logConfig LogConfig, baseDir string) {
	logFile := resolvePath(baseDir, logConfig.Filename)
	if logConfig.EnableLogging {
		log.SetOutput(&lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    logConfig.MaxSize,    // megabytes
			MaxBackups: logConfig.MaxBackups, // number of backups
			MaxAge:     logConfig.MaxAge,     // days
			Compress:   logConfig.Compress,
		})
	} else {
		// ログを無効にする場合は、標準出力を無効にする（例: io.Discard へ出力する）
		log.SetOutput(io.Discard)
	}
}

// setupGojaRuntime は goja のランタイムをセットアップします。
func setupGojaRuntime() *goja.Runtime {
	vm := goja.New()

	// getAPI 関数の登録
	vm.Set("nyanGetAPI", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 3 {
			return vm.ToValue("")
		}
		url := call.Argument(0).String()
		username := call.Argument(1).String()
		password := call.Argument(2).String()

		result, err := getAPI(url, username, password)
		if err != nil {
			log.Println("getAPI error:", err)
			return vm.ToValue("")
		}
		return vm.ToValue(result)
	})

	// jsonAPI 関数の登録
	vm.Set("nyanJsonAPI", func(call goja.FunctionCall) goja.Value {
		url := call.Argument(0).String()
		jsonData := call.Argument(1).String()
		username := call.Argument(2).String()
		password := call.Argument(3).String()

		// 第5引数：ヘッダー情報（オブジェクトまたはJSON文字列）
		var headers map[string]string
		if len(call.Arguments) >= 5 {
			// まずは、GojaのExportを使って直接オブジェクトとして取り出す
			if obj, ok := call.Argument(4).Export().(map[string]interface{}); ok {
				headers = make(map[string]string)
				for key, value := range obj {
					if s, ok := value.(string); ok {
						headers[key] = s
					} else {
						// 文字列以外なら fmt.Sprintで文字列化
						headers[key] = fmt.Sprint(value)
					}
				}
			} else {
				// オブジェクトとして取得できなければ、JSON文字列として処理する
				headerJSON := call.Argument(4).String()
				if err := json.Unmarshal([]byte(headerJSON), &headers); err != nil {
					panic(vm.ToValue("Invalid header JSON: " + err.Error()))
				}
			}
		}

		result, err := jsonAPI(url, []byte(jsonData), username, password, headers)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return vm.ToValue(result)
	})

	// getCookie, setCookie, setItem, getItem も同様に登録する
	vm.Set("nyanGetCookie", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue("")
		}
		cookieName := call.Argument(0).String()
		if ginContext != nil {
			cookieValue, err := ginContext.Cookie(cookieName)
			if err != nil {
				log.Printf("Error retrieving cookie: %v", err)
				return vm.ToValue("")
			}
			return vm.ToValue(cookieValue)
		}
		return vm.ToValue("")
	})

	vm.Set("nyanSetCookie", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue(nil)
		}
		cookieName := call.Argument(0).String()
		cookieValue := call.Argument(1).String()
		if ginContext != nil {
			ginContext.SetCookie(cookieName, cookieValue, 3600, "/", "", false, true)
			log.Printf("Set-Cookie: %s=%s", cookieName, cookieValue)
		} else {
			log.Println("ginContext is not set")
		}
		return vm.ToValue(nil)
	})

	vm.Set("nyanSetItem", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue(nil)
		}
		key := call.Argument(0).String()
		value := call.Argument(1).String()
		storage[key] = value
		return vm.ToValue(nil)
	})

	vm.Set("nyanGetItem", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue(nil)
		}
		key := call.Argument(0).String()
		if val, ok := storage[key]; ok {
			return vm.ToValue(val)
		}
		return vm.ToValue(nil)
	})

	vm.Set("nyanGetFile", newNyanGetFile(vm))

	// console.log の登録
	console := map[string]func(...interface{}){
		"log": func(args ...interface{}) {
			log.Println(args...)
		},
	}
	vm.Set("console", console)

	vm.Set("nyanHostExec", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue(`{"success":false,"exitCode":0,"stdout":"","stderr":"No command provided"}`)
		}
		cmdLine := call.Argument(0).String()
		outJSON := execGoja(cmdLine)
		return vm.ToValue(outJSON)
	})


	return vm
}

// execGoja は OS コマンドを実行して結果を JSON 文字列で返す例
func execGoja(commandLine string) string {
	var cmdStr string
	var args []string

	if runtime.GOOS == "windows" {
		cmdStr = "cmd"
		args = []string{"/c", commandLine}
	} else {
		cmdStr = "sh"
		args = []string{"-c", commandLine}
	}
	result, err := runCommand(cmdStr, args...)
	if err != nil {
		// エラーが発生しても ExecResult 自体は返す
		// エラー詳細は result.Stderr や ExitCode に含まれる
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// runCommand は OS コマンドを実行し、ExecResult を返す
func runCommand(command string, args ...string) (*ExecResult, error) {
	cmd := exec.Command(command, args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(&stdoutBuf, stdoutPipe)
	}()
	go func() {
		defer wg.Done()
		io.Copy(&stderrBuf, stderrPipe)
	}()
	wg.Wait()

	execErr := cmd.Wait()

	// CP932 -> UTF-8 変換（Windows なら）
	stdoutStr := stdoutBuf.String()
	stderrStr := stderrBuf.String()
	if runtime.GOOS == "windows" {
		if converted, err := cp932ToUTF8(stdoutBuf.Bytes()); err == nil {
			stdoutStr = converted
		}
		if converted, err := cp932ToUTF8(stderrBuf.Bytes()); err == nil {
			stderrStr = converted
		}
	}

	result := &ExecResult{
		Success:  true,
		ExitCode: 0,
		Stdout:   stdoutStr,
		Stderr:   stderrStr,
	}
	if execErr != nil {
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				result.ExitCode = status.ExitStatus()
			}
		}
		result.Success = false
		return result, execErr
	}
	return result, nil
}


// cp932ToUTF8 は、CP932（Shift-JIS）でエンコードされたデータをUTF-8に変換します。
func cp932ToUTF8(data []byte) (string, error) {
	reader := transform.NewReader(bytes.NewReader(data), japanese.ShiftJIS.NewDecoder())
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// handleNyan は /nyan へのリクエストを処理します。
func handleNyan(c *gin.Context) {
	apis := make(map[string]ApiData)
	for apiName, cfg := range apiConfig {
		apis[apiName] = ApiData{
			Description: cfg.Description,
			Push:        cfg.Push,
		}
	}

	response := NyanResponse{
		Name:    globalConfig.Name,
		Profile: globalConfig.Profile,
		Version: globalConfig.Version,
		Apis:    apis,
	}

	c.JSON(http.StatusOK, response)
}

// newNyanGetFile は、渡された vm をクロージャーにキャプチャして nyanGetFile を返します。
func newNyanGetFile(vm *goja.Runtime) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		// 引数のチェック
		if len(call.Arguments) < 1 {
			// vm を使ってエラーオブジェクトを生成する
			panic(vm.NewTypeError("nyanGetFileには1つの引数（ファイルパス）が必要です"))
		}
		relativePath := call.Arguments[0].String()

		// 実行中のバイナリのパスを取得し、ディレクトリ部分を取得
		exePath, err := os.Executable()
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		exeDir := filepath.Dir(exePath)

		// バイナリディレクトリからの相対パスを結合してフルパスを作成
		fullPath := filepath.Join(exeDir, relativePath)

		// ファイルを読み込み
		content, err := ioutil.ReadFile(fullPath)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}

		// 読み込んだ内容を文字列として返す
		return vm.ToValue(string(content))
	}
}