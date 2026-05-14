package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dop251/goja"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/natefinch/lumberjack"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
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
	Type        string        `json:"type,omitempty"`
	Script      string        `json:"script"`
	HTML        string        `json:"html"`
	Path        string        `json:"path,omitempty"`
	ParamCheck  string        `json:"paramCheck,omitempty"`
	OutCheck    string        `json:"outCheck,omitempty"`
	ConnectURL  string        `json:"connectURL,omitempty"`
	Trigger     TriggerConfig `json:"trigger,omitempty"`
	Description string        `json:"description"`
	Push        string        `json:"push,omitempty"`
}

type TriggerConfig struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (e *EndpointConfig) UnmarshalJSON(data []byte) error {
	type endpointConfigAlias EndpointConfig
	var raw struct {
		endpointConfigAlias
		ParamCheckLower string `json:"paramcheck"`
		OutCheckLower   string `json:"outcheck"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*e = EndpointConfig(raw.endpointConfigAlias)
	if strings.TrimSpace(e.ParamCheck) == "" {
		e.ParamCheck = raw.ParamCheckLower
	}
	if strings.TrimSpace(e.OutCheck) == "" {
		e.OutCheck = raw.OutCheckLower
	}
	return nil
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

type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
	ID      interface{}   `json:"id,omitempty"`
}

type JSONRPCRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params"`
	ID      interface{}            `json:"id"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type ParamCheckResponse struct {
	Success bool        `json:"success"`
	Status  int         `json:"status"`
	Result  interface{} `json:"result"`
}

type APIResponse struct {
	Status      int
	ContentType string
	Headers     map[string]string
	Body        []byte
}

// このシステムの設定
var globalConfig Config

// ビルド時に -ldflags "-X main.buildVersion=..." で上書き可能
var buildVersion = "v0.0.13"

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

	log.Printf("Binary version: %s", buildVersion)
	log.Printf("Config version: %s", globalConfig.Version)

	// API設定をロード
	apiConfigPath := resolvePath(exeDir, "api.json")
	if err := loadAPIConfig(apiConfigPath); err != nil {
		log.Fatal("Error loading API configuration:", err)
	}

	if err := startWebSocketClients(exeDir); err != nil {
		log.Printf("Failed to start WebSocket clients: %v", err)
	}
	if err := startScheduleJobs(exeDir); err != nil {
		log.Printf("Failed to start schedule jobs: %v", err)
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
	r.POST("/nyan-rpc", handleJSONRPC)

	// 各APIエンドポイントを設定
	for endpoint := range apiConfig {
		config := apiConfig[endpoint] // ループ変数をローカル変数にコピー
		switch strings.TrimSpace(config.Type) {
		case apiTypeWSClient:
			continue
		case apiTypeSchedule:
			continue
		case apiTypePublic:
			registerPublicEndpoint(r, endpoint, config, exeDir)
			continue
		}
		r.Any("/"+endpoint, func(c *gin.Context) {
			handleAPIRequestOrWebSocket(c, config)
		})
	}

	r.Any("/", func(c *gin.Context) {
		// クエリパラメータ "api" をチェック
		apiName := c.Query("api")
		if apiName != "" {
			if config, ok := apiConfig[apiName]; ok {
				if strings.TrimSpace(config.Type) == apiTypeSchedule {
					c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
					return
				}
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

	allParams, err := collectRequestParams(c, "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON data"})
		return
	}

	// スクリプトとHTMLファイルのパスを取得
	scriptPath := resolvePath(exeDir, config.Script)
	htmlPath := ""
	if strings.TrimSpace(config.HTML) != "" {
		htmlPath = resolvePath(exeDir, config.HTML)
	}

	if allowed, handled := runParamCheck(c, config, exeDir, htmlPath, allParams); handled {
		return
	} else if !allowed {
		return
	}

	// scriptが空の場合、HTMLファイルの内容をそのまま返す
	if config.Script == "" {
		htmlContent, err := os.ReadFile(htmlPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load HTML file"})
			return
		}
		response := APIResponse{
			Status:      http.StatusOK,
			ContentType: "text/html; charset=utf-8",
			Headers:     map[string]string{},
			Body:        htmlContent,
		}
		if handled := runOutCheck(c, config, exeDir, htmlPath, allParams, response); handled {
			return
		}
		writeAPIResponse(c, response)
		return
	}

	// JavaScriptを実行し、結果を取得
	resultValue, err := runJavaScriptValue(scriptPath, htmlPath, allParams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response, handledJSResponse, err := responseFromJSValue(resultValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if handled := runOutCheck(c, config, exeDir, htmlPath, allParams, response); handled {
		return
	}

	writeAPIResponse(c, response)
	if handledJSResponse {
		return
	}

	// push 設定がある場合、対象のWebSocket接続に対してプッシュ
	// API リクエスト完了後の push 処理
	performPush(config, allParams)

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
	value, err := runJavaScriptValue(scriptPath, htmlPath, allParams)
	if err != nil {
		return "", err
	}
	return value.String(), nil
}

// resolveCurrentAPINameFromContext は現在の HTTP リクエストから API 名を解決します。
func resolveCurrentAPINameFromContext(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}

	if apiName := strings.TrimSpace(c.Query("api")); apiName != "" {
		return apiName
	}

	path := strings.TrimSpace(c.Request.URL.Path)
	if path == "" || path == "/" {
		return "html"
	}
	return strings.TrimPrefix(path, "/")
}

// callNyanAPIFromVM は、JavaScript(VM) から api.json 定義の API を内部実行します。
func callNyanAPIFromVM(apiName string, allParams map[string]interface{}) (interface{}, error) {
	if strings.TrimSpace(apiName) == "" {
		return nil, fmt.Errorf("api name is required")
	}

	apiCfg, found := apiConfig[apiName]
	if !found {
		return nil, fmt.Errorf("API config not found: %s", apiName)
	}
	if strings.TrimSpace(apiCfg.Type) == apiTypeWSClient {
		return nil, fmt.Errorf("API %s is ws_client and cannot be called by nyanCallMe", apiName)
	}
	if strings.TrimSpace(apiCfg.Type) == apiTypeSchedule {
		return nil, fmt.Errorf("API %s is schedule and cannot be called by nyanCallMe", apiName)
	}
	if strings.TrimSpace(apiCfg.Script) == "" {
		return nil, fmt.Errorf("script not found for API %s", apiName)
	}

	params := make(map[string]interface{}, len(allParams)+1)
	for key, value := range allParams {
		params[key] = value
	}
	params["api"] = apiName

	resultValue, err := runJavaScriptValue(apiCfg.Script, apiCfg.HTML, params)
	if err != nil {
		return nil, fmt.Errorf("failed to run API %s: %w", apiName, err)
	}
	if resultValue == nil || goja.IsUndefined(resultValue) || goja.IsNull(resultValue) {
		return nil, nil
	}

	exported := resultValue.Export()
	if asText, ok := exported.(string); ok {
		var parsed interface{}
		if json.Unmarshal([]byte(asText), &parsed) == nil {
			return parsed, nil
		}
	}
	return exported, nil
}

func runJavaScriptValue(scriptPath string, htmlPath string, allParams map[string]interface{}) (goja.Value, error) {
	// 実行ファイルのディレクトリを取得
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("Failed to get executable path: %v", err)
	}
	exeDir := filepath.Dir(exePath)

	// スクリプトのパスを解決
	scriptPath = resolvePath(exeDir, scriptPath)

	// goja ランタイムのセットアップ（リクエストごとに新しいランタイムを作る）
	runtime := setupGojaRuntime()

	// ライブラリの JavaScript ファイルを読み込み
	var jsLibCode string
	for _, includePath := range globalConfig.JavaScriptInclude {
		includePath = resolvePath(exeDir, includePath)
		code, err := os.ReadFile(includePath)
		log.Print("Include file:", includePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read included JS file %s: %v", includePath, err)
		}
		jsLibCode += string(code) + "\n"
	}

	var htmlJS string
	if strings.TrimSpace(htmlPath) == "" {
		htmlJS = "const nyanHtmlCode = \"\";\n"
	} else {
		// HTMLファイルを読み込み
		htmlPath = resolvePath(exeDir, htmlPath)
		htmlCodeBytes, err := os.ReadFile(htmlPath)
		if err != nil {
			log.Printf("Failed to load HTML file at path: %s, error: %v", htmlPath, err)
			return nil, fmt.Errorf("failed to load HTML file: %v", err)
		}
		escapedHTML := strconv.Quote(string(htmlCodeBytes))
		htmlJS = fmt.Sprintf("const nyanHtmlCode = %s;\n", escapedHTML)
	}

	// リクエストパラメータをJSON文字列に変換してJavaScript変数として設定
	allParamsJSON, err := json.Marshal(allParams)
	if err != nil {
		return nil, err
	}
	//変数に格納
	paramsJS := fmt.Sprintf("const nyanAllParams = %s;\n", allParamsJSON)
	// JavaScriptファイル本体を読み込み
	jsCodeBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read JavaScript file: %v", err)
	}

	// 全体の JavaScript コードを結合
	fullJSCode := jsLibCode + htmlJS + paramsJS + string(jsCodeBytes)

	// スクリプトを実行
	value, err := runtime.RunString(fullJSCode)
	if err != nil {
		return nil, err
	}

	return value, nil
}

func collectRequestParams(c *gin.Context, defaultAPI string) (map[string]interface{}, error) {
	allParams := make(map[string]interface{})
	if c.ContentType() == "application/json" {
		var requestData map[string]interface{}
		if err := c.ShouldBindJSON(&requestData); err != nil {
			return nil, err
		}
		for key, value := range requestData {
			allParams[key] = value
		}
	}

	c.Request.ParseForm()
	for key, value := range c.Request.PostForm {
		allParams[key] = value[0]
	}
	for key, value := range c.Request.URL.Query() {
		allParams[key] = value[0]
	}

	if allParams["api"] == nil {
		if strings.TrimSpace(defaultAPI) != "" {
			allParams["api"] = defaultAPI
		} else if c.Request.URL.Path != "/" {
			allParams["api"] = c.Request.URL.Path
		} else {
			allParams["api"] = "html"
		}
	}

	return allParams, nil
}

func runParamCheck(c *gin.Context, config EndpointConfig, exeDir string, htmlPath string, allParams map[string]interface{}) (bool, bool) {
	checkOnly := isCheckOnlyMode(allParams)
	paramCheckPath := strings.TrimSpace(config.ParamCheck)
	if paramCheckPath == "" {
		if checkOnly {
			writeParamCheckResponse(c, ParamCheckResponse{
				Success: true,
				Status:  http.StatusOK,
				Result:  nil,
			})
			return false, true
		}
		return true, false
	}

	c.Writer.Header().Set("Cache-Control", "no-store")
	c.Writer.Header().Set("Pragma", "no-cache")

	resultValue, err := runJavaScriptValue(resolvePath(exeDir, paramCheckPath), htmlPath, allParams)
	if err != nil {
		writeParamCheckResponse(c, newParamCheckError(http.StatusInternalServerError, err.Error()))
		return false, true
	}

	checkResponse, err := parseCheckResponse(resultValue, "paramCheck")
	if err != nil {
		writeParamCheckResponse(c, newParamCheckError(http.StatusInternalServerError, err.Error()))
		return false, true
	}

	allowed := checkResponse.Success && checkResponse.Status == http.StatusOK
	if checkOnly || !allowed {
		writeParamCheckResponse(c, checkResponse)
		return allowed, true
	}

	return true, false
}

func isCheckOnlyMode(allParams map[string]interface{}) bool {
	if allParams == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(fmt.Sprint(allParams["nyan_mode"])), "checkOnly")
}

func runOutCheck(c *gin.Context, config EndpointConfig, exeDir string, htmlPath string, allParams map[string]interface{}, response APIResponse) bool {
	outCheckPath := strings.TrimSpace(config.OutCheck)
	if outCheckPath == "" {
		return false
	}

	checkParams := cloneParams(allParams)
	checkParams["nyan_output"] = map[string]interface{}{
		"status":          response.Status,
		"contentType":     response.ContentType,
		"headers":         response.Headers,
		"body":            string(response.Body),
		"bodyBase64":      base64.StdEncoding.EncodeToString(response.Body),
		"bodyLength":      len(response.Body),
		"bodyLengthBytes": len(response.Body),
	}
	checkParams["nyan_output_status"] = response.Status
	checkParams["nyan_output_content_type"] = response.ContentType
	checkParams["nyan_output_body"] = string(response.Body)
	checkParams["nyan_output_body_base64"] = base64.StdEncoding.EncodeToString(response.Body)

	resultValue, err := runJavaScriptValue(resolvePath(exeDir, outCheckPath), htmlPath, checkParams)
	if err != nil {
		writeParamCheckResponse(c, newParamCheckError(http.StatusInternalServerError, err.Error()))
		return true
	}

	checkResponse, err := parseCheckResponse(resultValue, "outCheck")
	if err != nil {
		writeParamCheckResponse(c, newParamCheckError(http.StatusInternalServerError, err.Error()))
		return true
	}

	if checkResponse.Success && checkResponse.Status == http.StatusOK {
		return false
	}

	writeParamCheckResponse(c, checkResponse)
	return true
}

func cloneParams(params map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(params))
	for key, value := range params {
		cloned[key] = value
	}
	return cloned
}

func parseCheckResponse(value goja.Value, checkName string) (ParamCheckResponse, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return ParamCheckResponse{}, fmt.Errorf("%s must return an object", checkName)
	}

	exported := value.Export()
	respMap, ok := exported.(map[string]interface{})
	if !ok {
		if text, ok := exported.(string); ok {
			if err := json.Unmarshal([]byte(text), &respMap); err != nil {
				return ParamCheckResponse{}, fmt.Errorf("%s string response must be JSON: %w", checkName, err)
			}
		} else {
			return ParamCheckResponse{}, fmt.Errorf("%s must return an object", checkName)
		}
	}

	success, ok := respMap["success"].(bool)
	if !ok {
		return ParamCheckResponse{}, fmt.Errorf("%s response success must be boolean", checkName)
	}

	status, ok := parseStatusCode(respMap["status"])
	if !ok {
		return ParamCheckResponse{}, fmt.Errorf("%s response status must be a number", checkName)
	}
	if status < 100 || status > 599 {
		return ParamCheckResponse{}, fmt.Errorf("%s response status is out of range: %d", checkName, status)
	}

	return ParamCheckResponse{
		Success: success,
		Status:  status,
		Result:  respMap["result"],
	}, nil
}

func newParamCheckError(status int, message string) ParamCheckResponse {
	return ParamCheckResponse{
		Success: false,
		Status:  status,
		Result: map[string]interface{}{
			"message": message,
		},
	}
}

func writeParamCheckResponse(c *gin.Context, resp ParamCheckResponse) {
	status := resp.Status
	if status < 100 || status > 599 {
		status = http.StatusInternalServerError
		resp.Status = status
	}
	c.JSON(status, resp)
}

func responseFromJSValue(value goja.Value) (APIResponse, bool, error) {
	response := APIResponse{
		Status:      http.StatusOK,
		ContentType: "text/html; charset=utf-8",
		Headers:     map[string]string{},
		Body:        []byte{},
	}

	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return response, false, nil
	}

	exported := value.Export()
	respMap, ok := exported.(map[string]interface{})
	if !ok {
		response.Body = []byte(value.String())
		return response, false, nil
	}

	if rawStatus, ok := respMap["status"]; ok {
		if parsed, ok := parseStatusCode(rawStatus); ok {
			response.Status = parsed
		}
	}

	if rawContentType, ok := respMap["contentType"]; ok {
		if s, ok := rawContentType.(string); ok {
			response.ContentType = s
		} else {
			response.ContentType = fmt.Sprint(rawContentType)
		}
	}

	if rawHeaders, ok := respMap["headers"]; ok {
		if headerMap, ok := rawHeaders.(map[string]interface{}); ok {
			for key, value := range headerMap {
				response.Headers[key] = fmt.Sprint(value)
			}
		}
	}

	bodyBytes, err := jsBodyToBytes(respMap["body"])
	if err != nil {
		return response, true, err
	}
	response.Body = bodyBytes
	return response, true, nil
}

func writeAPIResponse(c *gin.Context, response APIResponse) {
	for key, value := range response.Headers {
		c.Writer.Header().Set(key, value)
	}

	if strings.TrimSpace(response.ContentType) == "" {
		response.ContentType = "text/html; charset=utf-8"
	}

	if response.Status < 100 || response.Status > 599 {
		response.Status = http.StatusInternalServerError
	}

	c.Data(response.Status, response.ContentType, response.Body)
}

func parseStatusCode(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func jsBodyToBytes(body interface{}) ([]byte, error) {
	if body == nil {
		return []byte{}, nil
	}

	switch v := body.(type) {
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	case []interface{}:
		return json.Marshal(v)
	case map[string]interface{}:
		if encoding, ok := v["encoding"].(string); ok && strings.EqualFold(encoding, "base64") {
			data, _ := v["data"].(string)
			if strings.TrimSpace(data) == "" {
				return []byte{}, nil
			}
			decoded, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				return nil, fmt.Errorf("invalid base64 body: %w", err)
			}
			return decoded, nil
		}
		return json.Marshal(v)
	default:
		return []byte(fmt.Sprint(v)), nil
	}
}

const (
	apiTypeWSClient = "ws_client"
	apiTypePublic   = "public"
	apiTypeSchedule = "schedule"
)

func registerPublicEndpoint(r *gin.Engine, endpoint string, config EndpointConfig, exeDir string) {
	routePath := "/" + strings.Trim(strings.TrimSpace(endpoint), "/")
	if routePath == "/" {
		log.Printf("public endpoint %q is invalid: endpoint name must not be empty", endpoint)
		return
	}

	publicPath := strings.TrimSpace(config.Path)
	if publicPath == "" {
		log.Printf("public endpoint %s: path is missing", endpoint)
	}

	basePath := resolvePath(exeDir, publicPath)
	handler := func(c *gin.Context) {
		if publicPath == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "public path is missing"})
			return
		}

		requestedPath := strings.TrimPrefix(c.Param("filepath"), "/")
		allParams, err := collectRequestParams(c, endpoint)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON data"})
			return
		}
		allParams["nyan_public_endpoint"] = endpoint
		allParams["nyan_public_path"] = requestedPath

		ginContext = c
		defer func() { ginContext = nil }()

		if allowed, handled := runParamCheck(c, config, exeDir, "", allParams); handled {
			return
		} else if !allowed {
			return
		}

		if requestedPath == "" || !filepath.IsLocal(requestedPath) {
			c.Status(http.StatusNotFound)
			return
		}

		filePath := filepath.Join(basePath, requestedPath)
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				c.Status(http.StatusNotFound)
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read public file"})
			return
		}
		if fileInfo.IsDir() {
			c.Status(http.StatusNotFound)
			return
		}

		if strings.TrimSpace(config.OutCheck) != "" {
			fileContent, err := os.ReadFile(filePath)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read public file"})
				return
			}
			response := APIResponse{
				Status:      http.StatusOK,
				ContentType: http.DetectContentType(fileContent),
				Headers:     map[string]string{},
				Body:        fileContent,
			}
			if handled := runOutCheck(c, config, exeDir, "", allParams, response); handled {
				return
			}
		}

		c.File(filePath)
	}

	r.GET(routePath, handler)
	r.HEAD(routePath, handler)
	r.GET(routePath+"/*filepath", handler)
	r.HEAD(routePath+"/*filepath", handler)
}

type wsClientConfig struct {
	name        string
	scriptPath  string
	connectURL  string
	description string
}

type scheduleJobConfig struct {
	name        string
	scriptPath  string
	trigger     TriggerConfig
	description string
	schedule    cronSchedule
}

type cronSchedule struct {
	minutes     cronField
	hours       cronField
	days        cronField
	months      cronField
	weekdays    cronField
	dayStar     bool
	weekdayStar bool
}

type cronField map[int]bool

func parseCronSchedule(expr string) (cronSchedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return cronSchedule{}, fmt.Errorf("cron expression must have 5 fields")
	}

	minutes, _, err := parseCronField(fields[0], 0, 59, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("minute field: %w", err)
	}
	hours, _, err := parseCronField(fields[1], 0, 23, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("hour field: %w", err)
	}
	days, dayStar, err := parseCronField(fields[2], 1, 31, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("day field: %w", err)
	}
	months, _, err := parseCronField(fields[3], 1, 12, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("month field: %w", err)
	}
	weekdays, weekdayStar, err := parseCronField(fields[4], 0, 7, true)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("weekday field: %w", err)
	}

	return cronSchedule{
		minutes:     minutes,
		hours:       hours,
		days:        days,
		months:      months,
		weekdays:    weekdays,
		dayStar:     dayStar,
		weekdayStar: weekdayStar,
	}, nil
}

func parseCronField(field string, minValue, maxValue int, normalizeSunday bool) (cronField, bool, error) {
	values := make(cronField)
	isStar := field == "*"
	for _, part := range strings.Split(field, ",") {
		if part == "" {
			return nil, false, fmt.Errorf("empty list item")
		}

		step := 1
		base := part
		if strings.Contains(part, "/") {
			stepParts := strings.Split(part, "/")
			if len(stepParts) != 2 || stepParts[0] == "" || stepParts[1] == "" {
				return nil, false, fmt.Errorf("invalid step %q", part)
			}
			base = stepParts[0]
			parsedStep, err := strconv.Atoi(stepParts[1])
			if err != nil || parsedStep <= 0 {
				return nil, false, fmt.Errorf("invalid step %q", part)
			}
			step = parsedStep
		}

		start, end, err := cronRange(base, minValue, maxValue)
		if err != nil {
			return nil, false, err
		}
		for value := start; value <= end; value += step {
			normalized := value
			if normalizeSunday && normalized == 7 {
				normalized = 0
			}
			values[normalized] = true
		}
	}

	return values, isStar, nil
}

func cronRange(base string, minValue, maxValue int) (int, int, error) {
	if base == "*" {
		return minValue, maxValue, nil
	}
	if strings.Contains(base, "-") {
		rangeParts := strings.Split(base, "-")
		if len(rangeParts) != 2 || rangeParts[0] == "" || rangeParts[1] == "" {
			return 0, 0, fmt.Errorf("invalid range %q", base)
		}
		start, err := strconv.Atoi(rangeParts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range start %q", base)
		}
		end, err := strconv.Atoi(rangeParts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range end %q", base)
		}
		if start > end {
			return 0, 0, fmt.Errorf("range start is greater than end %q", base)
		}
		if start < minValue || end > maxValue {
			return 0, 0, fmt.Errorf("range %q is out of bounds %d-%d", base, minValue, maxValue)
		}
		return start, end, nil
	}

	value, err := strconv.Atoi(base)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid value %q", base)
	}
	if value < minValue || value > maxValue {
		return 0, 0, fmt.Errorf("value %q is out of bounds %d-%d", base, minValue, maxValue)
	}
	return value, value, nil
}

func (s cronSchedule) next(after time.Time) time.Time {
	next := after.Truncate(time.Minute).Add(time.Minute)
	limit := next.AddDate(5, 0, 0)
	for next.Before(limit) {
		if s.matches(next) {
			return next
		}
		next = next.Add(time.Minute)
	}
	return time.Time{}
}

func (s cronSchedule) matches(t time.Time) bool {
	weekday := int(t.Weekday())
	dayMatches := s.days[t.Day()]
	weekdayMatches := s.weekdays[weekday]
	switch {
	case !s.dayStar && !s.weekdayStar:
		if !dayMatches && !weekdayMatches {
			return false
		}
	case !dayMatches || !weekdayMatches:
		return false
	}

	return s.minutes[t.Minute()] &&
		s.hours[t.Hour()] &&
		s.months[int(t.Month())]
}

func startScheduleJobs(execDir string) error {
	var firstErr error
	for name, cfg := range apiConfig {
		if strings.TrimSpace(cfg.Type) != apiTypeSchedule {
			continue
		}

		scriptPath := strings.TrimSpace(cfg.Script)
		trigger := cfg.Trigger
		trigger.Type = strings.TrimSpace(trigger.Type)
		trigger.Value = strings.TrimSpace(trigger.Value)

		if scriptPath == "" {
			err := fmt.Errorf("schedule %s: script is missing", name)
			log.Print(err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if trigger.Type != "cron" {
			err := fmt.Errorf("schedule %s: unsupported trigger type %q", name, trigger.Type)
			log.Print(err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		schedule, err := parseCronSchedule(trigger.Value)
		if err != nil {
			err = fmt.Errorf("schedule %s: invalid cron trigger %q: %w", name, trigger.Value, err)
			log.Print(err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		jobCfg := scheduleJobConfig{
			name:        name,
			scriptPath:  resolvePath(execDir, scriptPath),
			trigger:     trigger,
			description: cfg.Description,
			schedule:    schedule,
		}

		log.Printf("Starting schedule job %s with cron %q", jobCfg.name, jobCfg.trigger.Value)
		go runScheduleJob(jobCfg)
	}

	return firstErr
}

func runScheduleJob(cfg scheduleJobConfig) {
	for {
		next := cfg.schedule.next(time.Now())
		if next.IsZero() {
			log.Printf("Schedule job %s has no next run time", cfg.name)
			return
		}

		log.Printf("Schedule job %s next run at %s", cfg.name, next.Format(time.RFC3339))
		timer := time.NewTimer(time.Until(next))
		<-timer.C

		allParams := map[string]interface{}{
			"api":                        cfg.name,
			"nyan_job_name":              cfg.name,
			"nyan_schedule_trigger_type": cfg.trigger.Type,
			"nyan_schedule_trigger":      cfg.trigger.Value,
			"nyan_schedule_time":         next.Format(time.RFC3339),
			"nyan_schedule_description":  cfg.description,
		}
		result, err := runJavaScript(cfg.scriptPath, "", allParams)
		if err != nil {
			log.Printf("Schedule job %s failed: %v", cfg.name, err)
			continue
		}
		log.Printf("Schedule job %s completed: %s", cfg.name, result)
	}
}

// connectURL が env:XXXX 形式なら環境変数 XXXX で解決する。空や未設定はエラー。
func resolveConnectURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("connectURL is empty")
	}
	if strings.HasPrefix(raw, "env:") {
		key := strings.TrimPrefix(raw, "env:")
		if key == "" {
			return "", fmt.Errorf("connectURL env: prefix is empty")
		}
		val := os.Getenv(key)
		if val == "" {
			return "", fmt.Errorf("environment variable %s is empty", key)
		}
		return val, nil
	}
	return raw, nil
}

// startWebSocketClients は api.json に定義された ws_client を起動します。
func startWebSocketClients(execDir string) error {
	var firstErr error
	for name, cfg := range apiConfig {
		if strings.TrimSpace(cfg.Type) != apiTypeWSClient {
			continue
		}

		scriptPath := strings.TrimSpace(cfg.Script)
		connectURLRaw := strings.TrimSpace(cfg.ConnectURL)

		if scriptPath == "" {
			err := fmt.Errorf("ws_client %s: script is missing", name)
			log.Print(err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if connectURLRaw == "" {
			err := fmt.Errorf("ws_client %s: connectURL is missing", name)
			log.Print(err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		connectURL, err := resolveConnectURL(connectURLRaw)
		if err != nil {
			log.Printf("ws_client %s: %v", name, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		wsCfg := wsClientConfig{
			name:        name,
			scriptPath:  scriptPath,
			connectURL:  connectURL,
			description: cfg.Description,
		}

		log.Printf("Starting WebSocket client %s -> %s", wsCfg.name, wsCfg.connectURL)
		go runWebSocketClient(wsCfg)
	}

	return firstErr
}

// 常時接続を維持し、切断時は指数バックオフで再接続します。
func runWebSocketClient(cfg wsClientConfig) {
	backoff := time.Second
	for {
		err := connectAndListenWebSocket(cfg)
		if err != nil {
			log.Printf("WebSocket client %s disconnected: %v", cfg.name, err)
		}

		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func connectAndListenWebSocket(cfg wsClientConfig) error {
	conn, _, err := websocket.DefaultDialer.Dial(cfg.connectURL, nil)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()

	log.Printf("WebSocket client %s connected", cfg.name)

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}
		if msgType == websocket.CloseMessage {
			return fmt.Errorf("close message received: %s", string(data))
		}

		log.Printf("ws_client %s received %s: %s", cfg.name, websocketMessageTypeLabel(msgType), string(data))

		allParams := map[string]interface{}{
			"api":             cfg.name,
			"ws_client":       cfg.name,
			"ws_message_type": websocketMessageTypeLabel(msgType),
			"ws_message_text": string(data),
			"ws_connect_url":  cfg.connectURL,
			"ws_description":  cfg.description,
		}

		if msgType == websocket.BinaryMessage {
			allParams["ws_message_base64"] = base64.StdEncoding.EncodeToString(data)
		}

		if msgType == websocket.TextMessage {
			var decoded interface{}
			if err := json.Unmarshal(data, &decoded); err == nil {
				allParams["ws_message_json"] = decoded
			}
		}

		result, err := runJavaScript(cfg.scriptPath, "", allParams)
		if err != nil {
			log.Printf("ws_client %s script error: %v", cfg.name, err)
			continue
		}

		trimmed := strings.TrimSpace(result)
		if trimmed == "" {
			continue
		}

		if err := conn.WriteMessage(websocket.TextMessage, []byte(trimmed)); err != nil {
			return fmt.Errorf("send error: %w", err)
		}
	}
}

func websocketMessageTypeLabel(t int) string {
	switch t {
	case websocket.TextMessage:
		return "text"
	case websocket.BinaryMessage:
		return "binary"
	case websocket.CloseMessage:
		return "close"
	case websocket.PingMessage:
		return "ping"
	case websocket.PongMessage:
		return "pong"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
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
	jsonAPIFunc := func(call goja.FunctionCall) goja.Value {
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
	}
	vm.Set("nyanJsonAPI", jsonAPIFunc)
	vm.Set("nyanCallAPI", jsonAPIFunc)

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

	vm.Set("nyanGetFile", nyanGetFile(vm))
	vm.Set("nyanReadFileB64", nyanReadFileB64(vm))
	vm.Set("nyanCallMe", func(call goja.FunctionCall) goja.Value {
		apiName := ""
		params := map[string]interface{}{}

		if len(call.Arguments) >= 1 {
			raw := call.Argument(0).Export()
			if raw != nil {
				if asMap, ok := raw.(map[string]interface{}); ok {
					params = asMap
				} else if obj, ok := call.Argument(0).(*goja.Object); ok {
					if exported, ok := obj.Export().(map[string]interface{}); ok {
						params = exported
					}
				}
			}
		}

		if apiValue, ok := params["api"]; ok {
			if asText, ok := apiValue.(string); ok && strings.TrimSpace(asText) != "" {
				apiName = asText
			}
		}
		if strings.TrimSpace(apiName) == "" {
			apiName = resolveCurrentAPINameFromContext(ginContext)
		}
		if strings.TrimSpace(apiName) == "" {
			panic(vm.ToValue("nyanCallMe: api is required"))
		}

		result, err := callNyanAPIFromVM(apiName, params)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		if result == nil {
			return goja.Null()
		}
		return vm.ToValue(result)
	})

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
		if strings.TrimSpace(cfg.Type) == apiTypeSchedule {
			continue
		}
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

func nyanGetFile(vm *goja.Runtime) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		// 引数のチェック
		if len(call.Arguments) < 1 {
			panic(vm.NewTypeError("nyanGetFileには1つの引数（ファイルパス）が必要です"))
		}
		relativePath := call.Arguments[0].String()

		// 実行中のバイナリのディレクトリからの相対パスに解決
		exePath, err := os.Executable()
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		exeDir := filepath.Dir(exePath)
		fullPath := filepath.Join(exeDir, relativePath)

		// ディレクトリ指定なら null
		if fi, err := os.Stat(fullPath); err == nil && fi.IsDir() {
			return goja.Null()
		}

		// 読み込み。存在しないなら null、その他はエラーを投げる
		content, err := os.ReadFile(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				return goja.Null()
			}
			// 権限など他のエラーはJS例外に（従来の動作）
			panic(vm.ToValue(err.Error()))
		}

		// 読み込んだ内容を文字列で返す（バイナリは Base64 を使う nyanReadFileB64 を推奨）
		return vm.ToValue(string(content))
	}
}

func nyanReadFileB64(vm *goja.Runtime) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(vm.NewTypeError("nyanReadFileB64には1つの引数（ファイルパス）が必要です"))
		}
		path := call.Arguments[0].String()

		abs := path
		if !filepath.IsAbs(path) {
			wd, _ := os.Getwd()
			abs = filepath.Join(wd, path)
		}

		content, err := os.ReadFile(abs)
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}

		return vm.ToValue(base64.StdEncoding.EncodeToString(content))
	}
}

func handleJSONRPC(c *gin.Context) {
	log.Print("handleJSONRPC called")
	ginContext = c
	defer func() { ginContext = nil }()

	// 1) リクエストボディを読み込み、JSONRPCRequest にパース
	var rpcReq JSONRPCRequest
	if err := c.ShouldBindJSON(&rpcReq); err != nil {
		respondJSONRPCError(c, nil, -32700, "Parse error", err.Error())
		return
	}
	if rpcReq.JSONRPC != "2.0" {
		respondJSONRPCError(c, rpcReq.ID, -32600, "Invalid Request: 'jsonrpc' must be '2.0'", nil)
		return
	}
	if rpcReq.Method == "" {
		respondJSONRPCError(c, rpcReq.ID, -32601, "Method not found", nil)
		return
	}

	// 2) リクエストパラメータを収集
	allParams := make(map[string]interface{})
	for k, v := range rpcReq.Params {
		allParams[k] = v
	}
	// 既存の実装で "api" を利用している場合、未設定なら method をセット
	if _, ok := allParams["api"]; !ok {
		allParams["api"] = rpcReq.Method
	}

	// 3) api.json から、リクエストされたAPI設定を取得
	config, exists := apiConfig[rpcReq.Method]
	if !exists {
		respondJSONRPCError(c, rpcReq.ID, -32601, fmt.Sprintf("API not found: %s", rpcReq.Method), nil)
		return
	}
	if strings.TrimSpace(config.Type) == apiTypeSchedule {
		respondJSONRPCError(c, rpcReq.ID, -32601, fmt.Sprintf("API not found: %s", rpcReq.Method), nil)
		return
	}

	// 4) JSON-RPC では HTML 出力は想定しないため、script が必須とする
	if config.Script == "" {
		respondJSONRPCError(c, rpcReq.ID, -32603, "No script defined for JSON-RPC API", nil)
		return
	}

	// 5) 必要なら URL や POST のパラメータもマージ（handleAPIRequest と同様）
	c.Request.ParseForm()
	for k, v := range c.Request.PostForm {
		allParams[k] = v[0]
	}
	for k, v := range c.Request.URL.Query() {
		allParams[k] = v[0]
	}

	// 6) 実行ファイルのディレクトリを解決し、スクリプトのパスを決定
	exePath, err := os.Executable()
	if err != nil {
		respondJSONRPCError(c, rpcReq.ID, -32603, "Failed to get executable path", err.Error())
		return
	}
	exeDir := filepath.Dir(exePath)
	scriptPath := resolvePath(exeDir, config.Script)
	htmlPath := ""
	if config.HTML != "" {
		htmlPath = resolvePath(exeDir, config.HTML)
	}

	if allowed, handled := runParamCheck(c, config, exeDir, htmlPath, allParams); handled {
		return
	} else if !allowed {
		return
	}

	// 7) メインのスクリプト実行（runJavaScript は既存関数）
	resultStr, err := runJavaScript(scriptPath, htmlPath, allParams)
	if err != nil {
		respondJSONRPCError(c, rpcReq.ID, -32603, "Script execution error", err.Error())
		return
	}

	// 8) Push 処理（必要な場合）
	performPush(config, allParams)

	// 10) JSON-RPC 成功レスポンスを構築して返却
	rpcResp := JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  resultStr,
		ID:      rpcReq.ID,
	}
	c.JSON(http.StatusOK, rpcResp)
}

func respondJSONRPCError(c *gin.Context, id interface{}, code int, message string, data interface{}) {
	rpcErr := &JSONRPCError{
		Code:    code,
		Message: message,
		Data:    data,
	}
	c.JSON(http.StatusOK, JSONRPCResponse{
		JSONRPC: "2.0",
		Error:   rpcErr,
		ID:      id,
	})
}

// performPush は指定された config に対して push 処理を行います。
func performPush(config EndpointConfig, allParams map[string]interface{}) {
	if config.Push == "" {
		return
	}
	pushConfig, ok := apiConfig[config.Push]
	if !ok {
		log.Printf("Push target %s not found in apiConfig", config.Push)
		return
	}
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get executable path for push: %v", err)
		return
	}
	exeDir := filepath.Dir(exePath)
	scriptPath := resolvePath(exeDir, pushConfig.Script)
	htmlPath := resolvePath(exeDir, pushConfig.HTML)
	var pushResult string
	if pushConfig.Script == "" {
		content, err := os.ReadFile(htmlPath)
		if err != nil {
			log.Printf("Failed to read push HTML file %s: %v", htmlPath, err)
			return
		}
		pushResult = string(content)
	} else {
		result, err := runJavaScript(scriptPath, htmlPath, allParams)
		if err != nil {
			log.Printf("Failed to run push script: %v", err)
			return
		}
		pushResult = result
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
