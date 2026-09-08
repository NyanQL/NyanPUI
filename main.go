package main

import (
	"bufio"
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/token"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/natefinch/lumberjack"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/crypto/argon2"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// Config は設定データを表します。
type Config struct {
	Name              string             `json:"name"`
	Profile           string             `json:"profile"`
	Version           string             `json:"version"`
	Port              int                `json:"port"`
	CertFile          string             `json:"certPath"`
	KeyFile           string             `json:"keyPath"`
	BasicAuth         BasicAuthConfig    `json:"BasicAuth"`
	JavaScriptInclude []string           `json:"javascript_include"`
	Log               LogConfig          `json:"log"`
	APIHotReload      APIHotReloadConfig `json:"APIHotReload"`
	OAuthStateRoot    string             `json:"oauth_state_directory"`
}

type BasicAuthConfig struct {
	Username string `json:"Username"`
	Password string `json:"Password"`
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
	Type                       string                   `json:"type,omitempty"`
	Script                     string                   `json:"script"`
	HTML                       string                   `json:"html"`
	Path                       string                   `json:"path,omitempty"`
	ParamCheck                 string                   `json:"paramCheck,omitempty"`
	OutCheck                   string                   `json:"outCheck,omitempty"`
	ConnectURL                 string                   `json:"connectURL,omitempty"`
	Trigger                    TriggerConfig            `json:"trigger,omitempty"`
	Description                string                   `json:"description"`
	Title                      string                   `json:"title,omitempty"`
	Push                       string                   `json:"push,omitempty"`
	Transport                  string                   `json:"transport,omitempty"`
	ProtocolVersions           []string                 `json:"protocolVersions,omitempty"`
	Resource                   string                   `json:"resource,omitempty"`
	AllowedOrigins             []string                 `json:"allowedOrigins,omitempty"`
	RateLimit                  *MCPRateLimit            `json:"rateLimit,omitempty"`
	MaxConcurrent              int                      `json:"maxConcurrent,omitempty"`
	OAuth                      MCPOAuthHooks            `json:"oauth,omitempty"`
	Tools                      []MCPToolConfig          `json:"tools,omitempty"`
	Instructions               string                   `json:"instructions,omitempty"`
	RedirectURIAllowedPrefixes []string                 `json:"redirectURIAllowedPrefixes,omitempty"`
	SecuritySchemes            []map[string]interface{} `json:"securitySchemes,omitempty"`
	Scopes                     []string                 `json:"scopes,omitempty"`
	Annotations                map[string]interface{}   `json:"annotations,omitempty"`
	legacyTransports           bool
	unknownFields              []string
	presentFields              map[string]bool
}

type MCPRateLimit struct {
	Requests int    `json:"requests"`
	Window   string `json:"window"`
}

type MCPOAuthHooks struct {
	AuthorizationServerMetadata  string   `json:"authorizationServerMetadata,omitempty"`
	ProtectedResourceMetadataAPI string   `json:"protectedResourceMetadata,omitempty"`
	Authorize                    string   `json:"authorize,omitempty"`
	Token                        string   `json:"token,omitempty"`
	Register                     string   `json:"register,omitempty"`
	AdminUser                    string   `json:"adminUser,omitempty"`
	VerifyAccess                 string   `json:"verifyAccess,omitempty"`
	Scopes                       []string `json:"scopes,omitempty"`
	RedirectURIAllowedPrefixes   []string `json:"redirectURIAllowedPrefixes,omitempty"`
}

func (oauth *MCPOAuthHooks) UnmarshalJSON(data []byte) error {
	type alias MCPOAuthHooks
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	allowed := map[string]bool{"authorizationServerMetadata": true, "protectedResourceMetadata": true, "authorize": true, "token": true, "register": true, "adminUser": true, "verifyAccess": true}
	for key := range object {
		if !allowed[key] {
			return fmt.Errorf("unknown OAuth field %s", key)
		}
	}
	*oauth = MCPOAuthHooks(decoded)
	return nil
}

type MCPToolConfig struct {
	Name            string                   `json:"name"`
	API             string                   `json:"api"`
	Title           string                   `json:"title"`
	Description     string                   `json:"description"`
	InputSchema     map[string]interface{}   `json:"inputSchema"`
	OutputSchema    map[string]interface{}   `json:"outputSchema"`
	SecuritySchemes []map[string]interface{} `json:"securitySchemes,omitempty"`
	Annotations     map[string]interface{}   `json:"annotations,omitempty"`
	legacyObject    bool
}

func (tool *MCPToolConfig) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		name = strings.TrimSpace(name)
		tool.Name, tool.API = name, name
		return nil
	}
	type alias MCPToolConfig
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*tool = MCPToolConfig(decoded)
	tool.legacyObject = true
	return nil
}

type TriggerConfig struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func (e *EndpointConfig) UnmarshalJSON(data []byte) error {
	type endpointConfigAlias EndpointConfig
	var raw struct {
		endpointConfigAlias
		ParamCheckLower string          `json:"paramcheck"`
		Check           string          `json:"check"`
		OutCheckLower   string          `json:"outcheck"`
		Transports      json.RawMessage `json:"transports"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*e = EndpointConfig(raw.endpointConfigAlias)
	if strings.TrimSpace(e.ParamCheck) == "" {
		e.ParamCheck = raw.ParamCheckLower
	}
	if strings.TrimSpace(e.ParamCheck) == "" {
		e.ParamCheck = raw.Check
	}
	if strings.TrimSpace(e.OutCheck) == "" {
		e.OutCheck = raw.OutCheckLower
	}
	e.legacyTransports = len(raw.Transports) != 0
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err == nil {
		e.presentFields = make(map[string]bool, len(object))
		for key := range object {
			e.presentFields[key] = true
		}
		allowed := map[string]bool{"type": true, "script": true, "html": true, "path": true, "paramCheck": true, "paramcheck": true, "check": true, "outCheck": true, "outcheck": true, "connectURL": true, "trigger": true, "description": true, "title": true, "push": true, "transport": true, "protocolVersions": true, "allowedOrigins": true, "rateLimit": true, "maxConcurrent": true, "oauth": true, "tools": true, "instructions": true, "redirectURIAllowedPrefixes": true, "securitySchemes": true, "scopes": true, "annotations": true, "transports": true}
		for key := range object {
			if !allowed[key] {
				e.unknownFields = append(e.unknownFields, key)
			}
		}
		sort.Strings(e.unknownFields)
	}
	return nil
}

type APIConfig map[string]EndpointConfig

const apiTypeInclude = "include"

// APIConfigSnapshot is one immutable, fully validated generation of api.json.
// Published snapshots are never mutated after publication.
type APIConfigSnapshot struct {
	RootPath   string
	Config     APIConfig
	Sources    map[string]string
	FileStates map[string]APIFileState
	Schedules  map[string]scheduleJobConfig
	WSClients  map[string]wsClientConfig
}

type APIFileState struct {
	Exists bool
	Hash   [sha256.Size]byte
}

type apiConfigLoadResult struct {
	Snapshot *APIConfigSnapshot
	Hash     [sha256.Size]byte
}

const (
	schemaSourceParamCheck   = "paramCheck"
	schemaSourceOutCheck     = "outCheck"
	schemaSourceScriptLegacy = "scriptLegacy"
	schemaSourceUnknown      = "unknown"
	maxMCPToolResultBytes    = 2 << 20
	maxMCPResponseBytes      = 4 << 20
	mcpProtocol20250618      = "2025-06-18"
	mcpProtocol20251125      = "2025-11-25"
)

func validateMCPConfiguration(config APIConfig) error {
	oauthOwners := map[string]string{}
	for name, endpoint := range config {
		if strings.TrimSpace(endpoint.Type) != apiTypeMCP {
			continue
		}
		if _, err := canonicalAPIEndpointPath(name); err != nil {
			return fmt.Errorf("MCP endpoint %s has an invalid API name: %w", name, err)
		}
		if endpoint.legacyTransports {
			return fmt.Errorf("MCP endpoint %s uses removed field transports", name)
		}
		if len(endpoint.unknownFields) != 0 {
			return fmt.Errorf("MCP endpoint %s contains unknown field %s", name, endpoint.unknownFields[0])
		}
		mcpFields := map[string]bool{"type": true, "transport": true, "protocolVersions": true, "allowedOrigins": true, "redirectURIAllowedPrefixes": true, "rateLimit": true, "maxConcurrent": true, "oauth": true, "tools": true, "instructions": true}
		for field := range endpoint.presentFields {
			if !mcpFields[field] {
				return fmt.Errorf("MCP endpoint %s contains unsupported field %s", name, field)
			}
		}
		if strings.TrimSpace(endpoint.Path) != "" || strings.TrimSpace(endpoint.Resource) != "" {
			return fmt.Errorf("MCP endpoint %s must not define path or resource", name)
		}
		if endpoint.Transport != "streamable_http" && endpoint.Transport != "stdio" {
			return fmt.Errorf("MCP endpoint %s must define transport as streamable_http or stdio", name)
		}
		if endpoint.Transport == "streamable_http" && len(endpoint.AllowedOrigins) == 0 {
			return fmt.Errorf("MCP endpoint %s must define allowedOrigins", name)
		}
		if endpoint.Transport == "streamable_http" {
			seenOrigins := map[string]bool{}
			for _, origin := range endpoint.AllowedOrigins {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
					return fmt.Errorf("MCP endpoint %s has invalid allowedOrigins", name)
				}
				canonical := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
				if seenOrigins[canonical] {
					return fmt.Errorf("MCP endpoint %s has duplicate allowedOrigins", name)
				}
				seenOrigins[canonical] = true
			}
		}
		if len(endpoint.ProtocolVersions) == 0 {
			endpoint.ProtocolVersions = []string{mcpProtocol20251125, mcpProtocol20250618}
		}
		versions := map[string]struct{}{}
		for _, version := range endpoint.ProtocolVersions {
			if version != mcpProtocol20250618 && version != mcpProtocol20251125 {
				return fmt.Errorf("MCP endpoint %s contains unsupported protocolVersion %s", name, version)
			}
			if _, exists := versions[version]; exists {
				return fmt.Errorf("MCP endpoint %s contains duplicate protocolVersion %s", name, version)
			}
			versions[version] = struct{}{}
		}
		if endpoint.RateLimit != nil {
			window, err := time.ParseDuration(endpoint.RateLimit.Window)
			if endpoint.RateLimit.Requests < 1 || endpoint.RateLimit.Requests > 10000 || err != nil || window < time.Second || window > 24*time.Hour {
				return fmt.Errorf("MCP endpoint %s has an invalid rateLimit", name)
			}
		}
		if endpoint.MaxConcurrent < 0 || endpoint.MaxConcurrent > 256 {
			return fmt.Errorf("MCP endpoint %s has an invalid maxConcurrent", name)
		}
		if len(endpoint.Tools) == 0 {
			return fmt.Errorf("MCP endpoint %s must define at least one Tool", name)
		}
		toolNames := map[string]struct{}{}
		for index, tool := range endpoint.Tools {
			if tool.legacyObject {
				return fmt.Errorf("MCP endpoint %s tools must contain API names, not Tool objects", name)
			}
			if tool.Name == "" || tool.API == "" {
				return fmt.Errorf("MCP endpoint %s contains an incomplete Tool", name)
			}
			if _, exists := toolNames[tool.Name]; exists {
				return fmt.Errorf("MCP endpoint %s contains duplicate Tool %s", name, tool.Name)
			}
			toolNames[tool.Name] = struct{}{}
			backing, ok := config[tool.API]
			if !ok {
				return fmt.Errorf("MCP Tool %s references unknown API %s", tool.Name, tool.API)
			}
			if strings.TrimSpace(backing.Type) != "" && strings.TrimSpace(backing.Type) != "api" {
				return fmt.Errorf("MCP Tool %s must reference a normal API", tool.Name)
			}
			info, err := os.Stat(backing.Script)
			if strings.TrimSpace(backing.Script) == "" || err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("MCP Tool %s script is not a regular file", tool.Name)
			}
			schema, err := resolveAPISchema(backing)
			if err != nil {
				return fmt.Errorf("MCP Tool %s schema: %w", tool.Name, err)
			}
			if schema.Input == nil {
				schema.Input = map[string]interface{}{"type": "object"}
			}
			tool.Title, tool.Description = backing.Title, backing.Description
			if tool.Title == "" {
				tool.Title = tool.Name
			}
			tool.InputSchema, tool.OutputSchema = schema.Input, schema.Output
			tool.SecuritySchemes, tool.Annotations = backing.SecuritySchemes, backing.Annotations
			if _, err := compileMCPJSONSchema(tool.InputSchema); err != nil {
				return fmt.Errorf("MCP Tool %s has invalid inputSchema: %w", tool.Name, err)
			}
			if tool.OutputSchema != nil {
				if _, err := compileMCPJSONSchema(tool.OutputSchema); err != nil {
					return fmt.Errorf("MCP Tool %s has invalid outputSchema: %w", tool.Name, err)
				}
			}
			endpoint.Tools[index] = tool
		}
		if mcpOAuthConfigured(endpoint.OAuth) {
			if endpoint.Transport != "streamable_http" {
				return fmt.Errorf("MCP endpoint %s OAuth requires streamable_http", name)
			}
			if len(endpoint.RedirectURIAllowedPrefixes) == 0 {
				return fmt.Errorf("MCP endpoint %s OAuth requires redirectURIAllowedPrefixes", name)
			}
			refs := []struct {
				role, api string
				optional  bool
			}{{"authorizationServerMetadata", endpoint.OAuth.AuthorizationServerMetadata, false}, {"protectedResourceMetadata", endpoint.OAuth.ProtectedResourceMetadataAPI, false}, {"authorize", endpoint.OAuth.Authorize, false}, {"token", endpoint.OAuth.Token, false}, {"register", endpoint.OAuth.Register, false}, {"verifyAccess", endpoint.OAuth.VerifyAccess, false}, {"adminUser", endpoint.OAuth.AdminUser, true}}
			seen := map[string]bool{}
			for _, ref := range refs {
				if ref.api == "" && ref.optional {
					continue
				}
				if ref.api == "" {
					return fmt.Errorf("MCP endpoint %s OAuth requires %s", name, ref.role)
				}
				api, ok := config[ref.api]
				if !ok || (api.Type != "" && api.Type != "api") {
					return fmt.Errorf("MCP endpoint %s OAuth %s references invalid API %s", name, ref.role, ref.api)
				}
				if seen[ref.api] {
					return fmt.Errorf("MCP endpoint %s OAuth roles must reference distinct APIs", name)
				}
				seen[ref.api] = true
				if owner := oauthOwners[ref.api]; owner != "" && owner != name {
					return fmt.Errorf("OAuth API %s is owned by multiple MCP endpoints", ref.api)
				}
				oauthOwners[ref.api] = name
				if ref.role != "authorizationServerMetadata" && ref.role != "protectedResourceMetadata" {
					info, err := os.Stat(api.Script)
					if api.Script == "" || err != nil || !info.Mode().IsRegular() {
						return fmt.Errorf("MCP endpoint %s OAuth %s script is not a regular file", name, ref.role)
					}
				}
			}
			verify := config[endpoint.OAuth.VerifyAccess]
			if err := validateUniqueNonemptyStrings(verify.Scopes); err != nil || len(verify.Scopes) == 0 {
				return fmt.Errorf("MCP endpoint %s verifyAccess must define valid scopes", name)
			}
			endpoint.OAuth.Scopes = append([]string(nil), verify.Scopes...)
			endpoint.OAuth.RedirectURIAllowedPrefixes = append([]string(nil), endpoint.RedirectURIAllowedPrefixes...)
			for _, prefix := range endpoint.RedirectURIAllowedPrefixes {
				u, err := url.Parse(prefix)
				if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" || !strings.HasSuffix(u.Path, "/") || u.EscapedPath() != u.Path {
					return fmt.Errorf("MCP endpoint %s has unsafe redirect URI prefix", name)
				}
			}
			allowedScopes := map[string]bool{}
			for _, scope := range verify.Scopes {
				allowedScopes[scope] = true
			}
			for _, tool := range endpoint.Tools {
				required := mcpToolScopes(tool)
				if len(required) == 0 {
					return fmt.Errorf("MCP Tool %s OAuth securitySchemes must define scopes", tool.Name)
				}
				for _, scope := range required {
					if !allowedScopes[scope] {
						return fmt.Errorf("MCP Tool %s requires unsupported scope %s", tool.Name, scope)
					}
				}
			}
		}
		config[name] = endpoint
	}
	return nil
}

func validateUniqueNonemptyStrings(values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return fmt.Errorf("values must be non-empty and unique")
		}
		seen[value] = true
	}
	return nil
}

func mcpOAuthConfigured(oauth MCPOAuthHooks) bool {
	return oauth.AuthorizationServerMetadata != "" || oauth.ProtectedResourceMetadataAPI != "" || oauth.Authorize != "" || oauth.Token != "" || oauth.Register != "" || oauth.AdminUser != "" || oauth.VerifyAccess != ""
}

func canonicalAPIEndpointPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.ContainsAny(name, "\\?#\r\n\t") {
		return "", fmt.Errorf("invalid API name")
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid API name")
		}
	}
	path := "/" + name
	if (&url.URL{Path: path}).EscapedPath() != path {
		return "", fmt.Errorf("invalid API name")
	}
	switch path {
	case "/", "/favicon.ico", "/nyan", "/nyan-rpc":
		return "", fmt.Errorf("reserved API name")
	}
	return path, nil
}

type rejectingMCPJSONSchemaLoader struct{}

func (rejectingMCPJSONSchemaLoader) Load(location string) (interface{}, error) {
	return nil, fmt.Errorf("external JSON Schema resource is not allowed: %s", location)
}

func compileMCPJSONSchema(schema map[string]interface{}) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(rejectingMCPJSONSchemaLoader{})
	const location = "urn:nyanpui:mcp-schema"
	if err := compiler.AddResource(location, schema); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

func validateMCPJSONSchemaValue(schema map[string]interface{}, value interface{}) error {
	compiled, err := compileMCPJSONSchema(schema)
	if err != nil {
		return err
	}
	return compiled.Validate(value)
}

type APISchema struct {
	Input        map[string]interface{}
	Output       map[string]interface{}
	InputSource  string
	OutputSource string
}

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
var buildVersion = "v0.0.14"

type serviceFilePath struct {
	Path   string
	Source string
}

type serviceFilePaths struct {
	API    serviceFilePath
	Config serviceFilePath
}

type startupOptions struct {
	Paths     serviceFilePaths
	MCPServer string
}

// api.jsonから取得する設定
var (
	apiConfigMu        sync.RWMutex
	apiConfig          APIConfig // compatibility alias; always matches apiSnapshot.Config
	apiSnapshot        *APIConfigSnapshot
	backgroundRuntimes *backgroundRuntimeManager
)

type mcpRateBucket struct {
	StartedAt time.Time
	Window    time.Duration
	Count     int
}

var mcpRateBuckets = struct {
	sync.Mutex
	Buckets     map[string]mcpRateBucket
	LastCleanup time.Time
}{Buckets: make(map[string]mcpRateBucket)}

var mcpConcurrencyLimiters = struct {
	sync.Mutex
	Limiters map[string]chan struct{}
}{Limiters: make(map[string]chan struct{})}

// ストレージ
var storage = make(map[string]string)
var storageMu sync.RWMutex

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 必要に応じて、オリジンチェックを行う
	},
}

var wsConnections = struct {
	sync.RWMutex
	conns map[string][]*serverWebSocket
}{
	conns: make(map[string][]*serverWebSocket),
}

type serverWebSocket struct {
	*websocket.Conn
	writeMu sync.Mutex
}

func (conn *serverWebSocket) WriteMessage(messageType int, data []byte) error {
	conn.writeMu.Lock()
	defer conn.writeMu.Unlock()
	return conn.Conn.WriteMessage(messageType, data)
}

// main はメイン関数です。
func main() {
	// 実行ファイルのディレクトリを取得
	exePath, err := os.Executable()
	if err != nil {
		log.Fatal("Failed to get executable path:", err)
	}
	exeDir := filepath.Dir(exePath)

	options, err := resolveStartupOptions(exeDir, os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	paths := options.Paths
	mcpStdio := options.MCPServer != ""

	// システム設定をロード
	config, err := loadConfig(paths.Config.Path)
	if err != nil {
		log.Fatal("Error loading config:", err)
	}
	configBaseDir := filepath.Dir(paths.Config.Path)
	apiBaseDir := filepath.Dir(paths.API.Path)
	adjustConfigPaths(configBaseDir, &config)
	globalConfig = config
	if mcpStdio {
		log.SetOutput(os.Stderr)
		gin.DefaultWriter = os.Stderr
	}

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
	} else if mcpStdio {
		log.SetOutput(os.Stderr)
		gin.DefaultWriter = os.Stderr
	} else {
		// ログをターミナル（標準出力）のみに出力する
		log.SetOutput(os.Stdout)
		gin.DefaultWriter = os.Stdout
	}

	log.Printf("Binary version: %s", buildVersion)
	log.Printf("Config file: %s (source: %s)", paths.Config.Path, paths.Config.Source)
	log.Printf("API file: %s (source: %s)", paths.API.Path, paths.API.Source)
	log.Printf("Config version: %s", globalConfig.Version)

	// API設定をロードし、background定義を公開前に全件検証する。
	initialConfig, err := readAPIConfigGraph(paths.API.Path, apiBaseDir)
	if err != nil {
		log.Fatal("Error loading API configuration:", err)
	}
	publishAPISnapshot(initialConfig.Snapshot)
	if mcpStdio {
		mcp, selectErr := selectMCPStdioServer(initialConfig.Snapshot, options.MCPServer)
		if selectErr != nil {
			log.Fatal(selectErr)
		}
		if serveErr := serveMCPStdio(os.Stdin, os.Stdout, initialConfig.Snapshot, mcp); serveErr != nil {
			log.Fatal(serveErr)
		}
		return
	}
	apiHotReloadInterval, err := parseAPIHotReloadInterval(config.APIHotReload.Interval)
	if err != nil {
		log.Fatalf("Invalid APIHotReload.Interval %q: %v", config.APIHotReload.Interval, err)
	}
	backgroundRuntimes = newBackgroundRuntimeManager()
	backgroundRuntimes.reconcile(initialConfig.Snapshot.Schedules, initialConfig.Snapshot.WSClients)
	if config.APIHotReload.Enabled {
		log.Printf("API hot reload enabled: interval=%s", apiHotReloadInterval)
		go watchAPIConfigGraph(paths.API.Path, apiBaseDir, apiHotReloadInterval, initialConfig.Snapshot.FileStates)
	} else {
		log.Printf("API hot reload disabled")
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
	r.GET("/nyan/*apiName", handleNyanDetail)
	r.POST("/nyan-rpc", handleJSONRPC)

	r.Any("/", func(c *gin.Context) {
		if dispatchMCPOrOAuth(c) {
			return
		}
		// クエリパラメータ "api" をチェック
		apiName := c.Query("api")
		if apiName != "" {
			if handleAPIRequestOrWebSocket(c, apiName) {
				return
			} else {
				c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
				return
			}
		}
		// "api" パラメータがなければ、デフォルトで "html" を使用
		if !handleAPIRequestOrWebSocket(c, "html") {
			c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
		}
	})

	r.NoRoute(func(c *gin.Context) {
		if dispatchMCPOrOAuth(c) {
			return
		}
		if dispatchDynamicEndpoint(c) {
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "API not found"})
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

func resolvePathFromBase(baseDir, pathValue string) string {
	if strings.TrimSpace(pathValue) == "" || filepath.IsAbs(pathValue) {
		return pathValue
	}
	return filepath.Join(baseDir, pathValue)
}

func resolveServiceFilePaths(execDir string, args []string) (serviceFilePaths, error) {
	options, err := resolveStartupOptions(execDir, args)
	return options.Paths, err
}

func resolveStartupOptions(execDir string, args []string) (startupOptions, error) {
	flags := flag.NewFlagSet("NyanPUI", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	apiFlag := flags.String("api", "", "path to api.json")
	configFlag := flags.String("config", "", "path to config.json")
	mcpServerFlag := flags.String("mcp-server", "", "MCP API name selected for stdio mode")
	if err := flags.Parse(args); err != nil {
		return startupOptions{}, err
	}
	if flags.NArg() != 0 {
		return startupOptions{}, fmt.Errorf("unexpected command arguments: %s", strings.Join(flags.Args(), " "))
	}

	apiPath, apiSource := chooseServiceFilePath(*apiFlag, "NYAN_API_PATH", filepath.Join(execDir, "api.json"), "--api")
	configPath, configSource := chooseServiceFilePath(*configFlag, "NYAN_CONFIG_PATH", filepath.Join(execDir, "config.json"), "--config")

	resolvedAPIPath, err := resolveExistingServiceFilePath(apiPath, "api", apiSource)
	if err != nil {
		return startupOptions{}, err
	}
	resolvedConfigPath, err := resolveExistingServiceFilePath(configPath, "config", configSource)
	if err != nil {
		return startupOptions{}, err
	}

	return startupOptions{Paths: serviceFilePaths{
		API:    serviceFilePath{Path: resolvedAPIPath, Source: apiSource},
		Config: serviceFilePath{Path: resolvedConfigPath, Source: configSource},
	}, MCPServer: strings.TrimSpace(*mcpServerFlag)}, nil
}

func chooseServiceFilePath(cliValue, envName, defaultPath, cliSource string) (string, string) {
	if strings.TrimSpace(cliValue) != "" {
		return cliValue, cliSource
	}
	if envValue := strings.TrimSpace(os.Getenv(envName)); envValue != "" {
		return envValue, envName
	}
	return defaultPath, "default"
}

func resolveExistingServiceFilePath(pathValue, label, source string) (string, error) {
	resolvedPath, err := filepath.Abs(pathValue)
	if err != nil {
		return "", fmt.Errorf("%s file path could not be resolved: %s (source: %s): %w", label, pathValue, source, err)
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s file not found: %s (source: %s)", label, resolvedPath, source)
		}
		return "", fmt.Errorf("%s file cannot be accessed: %s (source: %s): %w", label, resolvedPath, source, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s file is a directory: %s (source: %s)", label, resolvedPath, source)
	}
	return resolvedPath, nil
}

func adjustConfigPaths(configBaseDir string, config *Config) {
	config.CertFile = resolvePathFromBase(configBaseDir, config.CertFile)
	config.KeyFile = resolvePathFromBase(configBaseDir, config.KeyFile)
	config.Log.Filename = resolvePathFromBase(configBaseDir, config.Log.Filename)
	config.OAuthStateRoot = resolvePathFromBase(configBaseDir, config.OAuthStateRoot)
	for i, includePath := range config.JavaScriptInclude {
		config.JavaScriptInclude[i] = resolvePathFromBase(configBaseDir, includePath)
	}
}

// loadConfig は設定ファイルを読み込みます。
func loadConfig(filename string) (Config, error) {
	var config Config
	applyConfigDefaults(&config)

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
func loadAPIConfig(filePath string, apiBaseDir string) error {
	loaded, err := readAPIConfigGraph(filePath, apiBaseDir)
	if err != nil {
		return err
	}
	publishAPISnapshot(loaded.Snapshot)
	return nil
}

func adjustAPIConfigPaths(config APIConfig, apiBaseDir string) {
	for apiKey, endpoint := range config {
		endpoint.Script = resolvePathFromBase(apiBaseDir, endpoint.Script)
		endpoint.HTML = resolvePathFromBase(apiBaseDir, endpoint.HTML)
		if strings.TrimSpace(endpoint.Type) != apiTypeMCP {
			endpoint.Path = resolvePathFromBase(apiBaseDir, endpoint.Path)
		}
		endpoint.ParamCheck = resolvePathFromBase(apiBaseDir, endpoint.ParamCheck)
		endpoint.OutCheck = resolvePathFromBase(apiBaseDir, endpoint.OutCheck)
		config[apiKey] = endpoint
	}
}

// handleAPIRequestOrWebSocket はAPIリクエストまたはWebSocketリクエストを処理します。
func handleAPIRequestOrWebSocket(c *gin.Context, apiName string) bool {
	snapshot := currentAPISnapshot()
	if snapshot == nil {
		return false
	}
	config, ok := snapshot.Config[apiName]
	if !ok || !isRequestAPI(config) {
		return false
	}
	if websocket.IsWebSocketUpgrade(c.Request) {
		handleWebSocketWithSnapshot(c, snapshot, apiName, config)
	} else {
		handleAPIRequestWithSnapshot(c, snapshot, config)
	}
	return true
}

// handleAPIRequest はAPIリクエストを処理します。
func handleAPIRequest(c *gin.Context, config EndpointConfig) {
	handleAPIRequestWithSnapshot(c, currentAPISnapshot(), config)
}

func handleAPIRequestWithSnapshot(c *gin.Context, snapshot *APIConfigSnapshot, config EndpointConfig) {
	// HTTP/2サーバープッシュの処理は削除

	// 実行ファイルのディレクトリを取得
	exePath, err := os.Executable()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get executable path"})
		return
	}
	exeDir := filepath.Dir(exePath)

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

	if allowed, handled := runParamCheckWithSnapshot(c, snapshot, config, exeDir, htmlPath, allParams); handled {
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
		if handled := runOutCheckWithSnapshot(c, snapshot, config, exeDir, htmlPath, allParams, response); handled {
			return
		}
		writeAPIResponse(c, response)
		return
	}

	// JavaScriptを実行し、結果を取得
	resultValue, err := runJavaScriptValueWithContext(snapshot, c, scriptPath, htmlPath, allParams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response, handledJSResponse, err := responseFromJSValue(resultValue)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if handled := runOutCheckWithSnapshot(c, snapshot, config, exeDir, htmlPath, allParams, response); handled {
		return
	}

	writeAPIResponse(c, response)
	if handledJSResponse {
		return
	}

	// push 設定がある場合、対象のWebSocket接続に対してプッシュ
	// API リクエスト完了後の push 処理
	performPushWithContext(snapshot, c, config, allParams)

}

// handleWebSocket はWebSocketリクエストを処理します。
func handleWebSocket(c *gin.Context, endpoint string, config EndpointConfig) {
	handleWebSocketWithSnapshot(c, currentAPISnapshot(), endpoint, config)
}

func handleWebSocketWithSnapshot(c *gin.Context, snapshot *APIConfigSnapshot, endpoint string, config EndpointConfig) {
	allParams, err := collectRequestParams(c, endpoint)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request parameters"})
		return
	}
	exePath, err := os.Executable()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	exeDir := filepath.Dir(exePath)
	if allowed, handled := runParamCheckWithSnapshot(c, snapshot, config, exeDir, config.HTML, allParams); handled || !allowed {
		return
	}
	rawConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to set websocket upgrade: %v", err)
		return
	}
	conn := &serverWebSocket{Conn: rawConn}
	wsConnections.Lock()
	wsConnections.conns[endpoint] = append(wsConnections.conns[endpoint], conn)
	wsConnections.Unlock()
	defer func() {
		wsConnections.Lock()
		conns := wsConnections.conns[endpoint]
		for i, candidate := range conns {
			if candidate == conn {
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
			break
		}
		var req map[string]string
		if err := json.Unmarshal(message, &req); err != nil {
			_ = conn.WriteMessage(messageType, []byte("Invalid JSON"))
			continue
		}
		apiName, requested := req["api"]
		if !requested {
			if err := conn.WriteMessage(messageType, message); err != nil {
				break
			}
			continue
		}
		snapshot := currentAPISnapshot()
		if snapshot == nil {
			_ = conn.WriteMessage(messageType, []byte("API configuration is not loaded"))
			continue
		}
		apiCfg, found := snapshot.Config[apiName]
		if !found || !isRequestAPI(apiCfg) {
			_ = conn.WriteMessage(messageType, []byte(fmt.Sprintf("API %s not found", apiName)))
			continue
		}
		params := cloneParams(allParams)
		for key, value := range req {
			params[key] = value
		}
		if err := validateExternalRequestParams(params); err != nil {
			_ = conn.WriteMessage(messageType, []byte("Invalid request parameters"))
			continue
		}
		htmlPath := resolvePathFromBase(exeDir, apiCfg.HTML)
		if !checkWebSocketScript(conn, snapshot, c, apiCfg.ParamCheck, exeDir, htmlPath, params, "paramCheck") {
			continue
		}
		if isCheckOnlyMode(params) {
			encoded, _ := json.Marshal(ParamCheckResponse{Success: true, Status: http.StatusOK})
			_ = conn.WriteMessage(websocket.TextMessage, encoded)
			continue
		}
		content, err := os.ReadFile(htmlPath)
		if err != nil {
			_ = conn.WriteMessage(messageType, []byte(fmt.Sprintf("Failed to read HTML file for API %s: %v", apiName, err)))
			continue
		}
		response := APIResponse{Status: http.StatusOK, ContentType: "text/html; charset=utf-8", Headers: map[string]string{}, Body: content}
		if !checkWebSocketScript(conn, snapshot, c, apiCfg.OutCheck, exeDir, htmlPath, outputCheckParams(params, response), "outCheck") {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, content); err != nil {
			break
		}
	}
}

func checkWebSocketScript(conn *serverWebSocket, snapshot *APIConfigSnapshot, requestContext *gin.Context, checkPath, exeDir, htmlPath string, params map[string]interface{}, checkName string) bool {
	if strings.TrimSpace(checkPath) == "" {
		return true
	}
	value, err := runJavaScriptValueWithContext(snapshot, requestContext, resolvePath(exeDir, checkPath), htmlPath, params)
	response := ParamCheckResponse{}
	if err == nil {
		response, err = parseCheckResponse(value, checkName)
	}
	if err != nil {
		response = newParamCheckError(http.StatusInternalServerError, err.Error())
	}
	if response.Success && response.Status == http.StatusOK {
		return true
	}
	encoded, _ := json.Marshal(response)
	_ = conn.WriteMessage(websocket.TextMessage, encoded)
	return false
}

// sendWebSocketHTMLError はWebSocket接続にHTML形式のエラーメッセージを送信します。
func sendWebSocketHTMLError(conn *serverWebSocket, messageType int, errorMessage string) {
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
	return runJavaScriptWithSnapshot(currentAPISnapshot(), scriptPath, htmlPath, allParams)
}

func runJavaScriptWithSnapshot(snapshot *APIConfigSnapshot, scriptPath string, htmlPath string, allParams map[string]interface{}) (string, error) {
	value, err := runJavaScriptValueWithSnapshot(snapshot, scriptPath, htmlPath, allParams)
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
	return callNyanAPIFromVMWithSnapshot(currentAPISnapshot(), apiName, allParams)
}

func callNyanAPIFromVMWithSnapshot(snapshot *APIConfigSnapshot, apiName string, allParams map[string]interface{}) (interface{}, error) {
	return callNyanAPIFromVMWithContext(snapshot, nil, apiName, allParams)
}

func callNyanAPIFromVMWithContext(snapshot *APIConfigSnapshot, requestContext *gin.Context, apiName string, allParams map[string]interface{}) (interface{}, error) {
	if strings.TrimSpace(apiName) == "" {
		return nil, fmt.Errorf("api name is required")
	}

	if snapshot == nil {
		return nil, fmt.Errorf("API configuration is not loaded")
	}
	apiCfg, found := snapshot.Config[apiName]
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

	resultValue, err := runJavaScriptValueWithContext(snapshot, requestContext, apiCfg.Script, apiCfg.HTML, params)
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
	return runJavaScriptValueWithSnapshot(currentAPISnapshot(), scriptPath, htmlPath, allParams)
}

func runJavaScriptValueWithSnapshot(snapshot *APIConfigSnapshot, scriptPath string, htmlPath string, allParams map[string]interface{}) (goja.Value, error) {
	return runJavaScriptValueWithContext(snapshot, nil, scriptPath, htmlPath, allParams)
}

func runJavaScriptValueWithContext(snapshot *APIConfigSnapshot, requestContext *gin.Context, scriptPath string, htmlPath string, allParams map[string]interface{}) (goja.Value, error) {
	// 実行ファイルのディレクトリを取得
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("Failed to get executable path: %v", err)
	}
	exeDir := filepath.Dir(exePath)

	// スクリプトのパスを解決
	scriptPath = resolvePath(exeDir, scriptPath)

	// goja ランタイムのセットアップ（リクエストごとに新しいランタイムを作る）
	runtime := setupGojaRuntimeWithContext(snapshot, requestContext)
	if stateRoot, ok := allParams["state_directory"].(string); ok && stateRoot != "" {
		setupOAuthStateRuntime(runtime, stateRoot)
	}

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

	if err := c.Request.ParseForm(); err != nil {
		return nil, err
	}
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

	if err := validateExternalRequestParams(allParams); err != nil {
		return nil, err
	}
	return allParams, nil
}

func validateExternalRequestParams(params map[string]interface{}) error {
	for _, key := range []string{"mcp_principal", "mcp_tool"} {
		if _, exists := params[key]; exists {
			return fmt.Errorf("request contains reserved parameter %s", key)
		}
	}
	return nil
}

func runParamCheck(c *gin.Context, config EndpointConfig, exeDir string, htmlPath string, allParams map[string]interface{}) (bool, bool) {
	return runParamCheckWithSnapshot(c, currentAPISnapshot(), config, exeDir, htmlPath, allParams)
}

func runParamCheckWithSnapshot(c *gin.Context, snapshot *APIConfigSnapshot, config EndpointConfig, exeDir string, htmlPath string, allParams map[string]interface{}) (bool, bool) {
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

	resultValue, err := runJavaScriptValueWithContext(snapshot, c, resolvePath(exeDir, paramCheckPath), htmlPath, allParams)
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
	return runOutCheckWithSnapshot(c, currentAPISnapshot(), config, exeDir, htmlPath, allParams, response)
}

func runOutCheckWithSnapshot(c *gin.Context, snapshot *APIConfigSnapshot, config EndpointConfig, exeDir string, htmlPath string, allParams map[string]interface{}, response APIResponse) bool {
	outCheckPath := strings.TrimSpace(config.OutCheck)
	if outCheckPath == "" {
		return false
	}

	checkParams := outputCheckParams(allParams, response)

	resultValue, err := runJavaScriptValueWithContext(snapshot, c, resolvePath(exeDir, outCheckPath), htmlPath, checkParams)
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

func outputCheckParams(allParams map[string]interface{}, response APIResponse) map[string]interface{} {
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

	return checkParams
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
	apiTypeMCP      = "mcp"
)

func isRequestAPI(config EndpointConfig) bool {
	switch strings.TrimSpace(config.Type) {
	case apiTypeWSClient, apiTypePublic, apiTypeSchedule, apiTypeMCP:
		return false
	default:
		return true
	}
}

// dispatchDynamicEndpoint resolves the request against one immutable API snapshot.
func dispatchDynamicEndpoint(c *gin.Context) bool {
	requestPath := strings.TrimPrefix(c.Request.URL.Path, "/")
	snapshot := currentAPISnapshot()
	if snapshot == nil {
		return false
	}
	config := snapshot.Config
	if endpoint, ok := config[requestPath]; ok && isRequestAPI(endpoint) {
		if websocket.IsWebSocketUpgrade(c.Request) {
			handleWebSocketWithSnapshot(c, snapshot, requestPath, endpoint)
		} else {
			handleAPIRequestWithSnapshot(c, snapshot, endpoint)
		}
		return true
	}

	endpointName := ""
	var endpoint EndpointConfig
	for name, candidate := range config {
		if strings.TrimSpace(candidate.Type) != apiTypePublic {
			continue
		}
		cleanName := strings.Trim(strings.TrimSpace(name), "/")
		if requestPath == cleanName || strings.HasPrefix(requestPath, cleanName+"/") {
			if len(cleanName) > len(endpointName) {
				endpointName, endpoint = cleanName, candidate
			}
		}
	}
	if endpointName == "" {
		return false
	}
	servePublicEndpointWithSnapshot(c, snapshot, endpointName, endpoint)
	return true
}

func servePublicEndpoint(c *gin.Context, endpoint string, config EndpointConfig) {
	servePublicEndpointWithSnapshot(c, currentAPISnapshot(), endpoint, config)
}

func servePublicEndpointWithSnapshot(c *gin.Context, snapshot *APIConfigSnapshot, endpoint string, config EndpointConfig) {
	publicPath := strings.TrimSpace(config.Path)
	if publicPath == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "public path is missing"})
		return
	}
	requestPath := strings.TrimPrefix(c.Request.URL.Path, "/")
	requestedPath := strings.TrimPrefix(strings.TrimPrefix(requestPath, endpoint), "/")
	allParams, err := collectRequestParams(c, endpoint)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON data"})
		return
	}
	allParams["nyan_public_endpoint"] = endpoint
	allParams["nyan_public_path"] = requestedPath
	if allowed, handled := runParamCheckWithSnapshot(c, snapshot, config, filepath.Dir(publicPath), "", allParams); handled || !allowed {
		return
	}
	if requestedPath == "" || !filepath.IsLocal(requestedPath) {
		c.Status(http.StatusNotFound)
		return
	}
	filePath := filepath.Join(publicPath, requestedPath)
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		if err != nil && !os.IsNotExist(err) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read public file"})
		} else {
			c.Status(http.StatusNotFound)
		}
		return
	}
	if strings.TrimSpace(config.OutCheck) != "" {
		content, err := os.ReadFile(filePath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read public file"})
			return
		}
		response := APIResponse{Status: http.StatusOK, ContentType: http.DetectContentType(content), Headers: map[string]string{}, Body: content}
		if runOutCheckWithSnapshot(c, snapshot, config, filepath.Dir(publicPath), "", allParams, response) {
			return
		}
	}
	c.File(filePath)
}

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
		if isMCPOrOAuthHTTPRequest(c.Request) {
			c.Next()
			return
		}
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func isMCPOrOAuthHTTPRequest(request *http.Request) bool {
	snapshot := currentAPISnapshot()
	if snapshot == nil || request == nil || request.URL == nil {
		return false
	}
	name := strings.Trim(request.URL.Path, "/")
	if request.URL.Path == "/" {
		name = request.URL.Query().Get("api")
	}
	for _, mcpName := range sortedMCPNames(snapshot.Config) {
		mcp := snapshot.Config[mcpName]
		if mcp.Transport != "streamable_http" {
			continue
		}
		if name == mcpName {
			return true
		}
		if _, ok := mcpOAuthRoleForAPI(mcp, name); ok {
			return true
		}
	}
	return false
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

	body, err := io.ReadAll(resp.Body)
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
	return setupGojaRuntimeWithSnapshot(currentAPISnapshot())
}

func setupGojaRuntimeWithSnapshot(snapshot *APIConfigSnapshot) *goja.Runtime {
	return setupGojaRuntimeWithContext(snapshot, nil)
}

func setupGojaRuntimeWithContext(snapshot *APIConfigSnapshot, requestContext *gin.Context) *goja.Runtime {
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
		if requestContext != nil {
			cookieValue, err := requestContext.Cookie(cookieName)
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
		if requestContext != nil {
			requestContext.SetCookie(cookieName, cookieValue, 3600, "/", "", requestContext.Request.TLS != nil, true)
		} else {
			log.Println("HTTP request context is not set")
		}
		return vm.ToValue(nil)
	})

	vm.Set("nyanSetItem", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			return vm.ToValue(nil)
		}
		key := call.Argument(0).String()
		value := call.Argument(1).String()
		storageMu.Lock()
		storage[key] = value
		storageMu.Unlock()
		return vm.ToValue(nil)
	})

	vm.Set("nyanGetItem", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return vm.ToValue(nil)
		}
		key := call.Argument(0).String()
		storageMu.RLock()
		val, ok := storage[key]
		storageMu.RUnlock()
		if ok {
			return vm.ToValue(val)
		}
		return vm.ToValue(nil)
	})

	vm.Set("nyanGetFile", nyanGetFile(vm))
	vm.Set("nyanSaveFile", nyanSaveFile(vm))
	vm.Set("nyanDeleteFile", nyanDeleteFile(vm))
	vm.Set("nyanReadFileB64", nyanReadFileB64(vm))
	vm.Set("nyanRandomBase64URL", func(call goja.FunctionCall) goja.Value {
		count := 32
		if len(call.Arguments) > 0 {
			count = int(call.Arguments[0].ToInteger())
		}
		if count < 1 || count > 1024 {
			panic(vm.NewTypeError("random byte count is outside the allowed range"))
		}
		buffer := make([]byte, count)
		if _, err := io.ReadFull(cryptorand.Reader, buffer); err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return vm.ToValue(base64.RawURLEncoding.EncodeToString(buffer))
	})
	vm.Set("nyanSHA256Base64URL", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(vm.NewTypeError("nyanSHA256Base64URL requires a string"))
		}
		digest := sha256.Sum256([]byte(call.Arguments[0].String()))
		return vm.ToValue(base64.RawURLEncoding.EncodeToString(digest[:]))
	})
	vm.Set("nyanBase64Decode", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(vm.NewTypeError("nyanBase64Decode requires a string"))
		}
		decoded, err := base64.StdEncoding.DecodeString(call.Arguments[0].String())
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return vm.ToValue(string(decoded))
	})
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
			apiName = resolveCurrentAPINameFromContext(requestContext)
		}
		if strings.TrimSpace(apiName) == "" {
			panic(vm.ToValue("nyanCallMe: api is required"))
		}

		result, err := callNyanAPIFromVMWithContext(snapshot, requestContext, apiName, params)
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
	// Command failures are represented by ExecResult, including stderr and exit code.
	result, _ := runCommand(cmdStr, args...)
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
	for apiName, cfg := range currentAPIConfig() {
		if !isRequestAPI(cfg) {
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

// handleNyanDetail publishes documentation schemas for one normal API.
func handleNyanDetail(c *gin.Context) {
	apiName := strings.TrimPrefix(c.Param("apiName"), "/")
	if apiName == "" {
		handleNyan(c)
		return
	}
	config, exists := currentAPIConfig()[apiName]
	if !exists || !isRequestAPI(config) {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("API not found: %s", apiName)})
		return
	}
	schema, err := resolveAPISchema(config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to resolve API schemas: %s", apiName), "detail": err.Error()})
		return
	}
	result := gin.H{
		"api": apiName, "type": "api", "description": config.Description,
		"inputSchema": schema.Input, "outputSchema": schema.Output,
		"schemaSource": gin.H{"input": schema.InputSource, "output": schema.OutputSource},
	}
	if schema.InputSource == schemaSourceScriptLegacy {
		if params, found, readErr := readStaticLegacyAcceptedParams(config.Script); readErr == nil && found {
			result["nyanAcceptedParams"] = params
		}
	}
	c.JSON(http.StatusOK, result)
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
		fullPath := relativePath
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(exeDir, fullPath)
		}

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

func nyanSaveFile(vm *goja.Runtime) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewTypeError("nyanSaveFileにはファイルパスと内容が必要です"))
		}
		exePath, err := os.Executable()
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		path := call.Arguments[0].String()
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(exePath), path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			panic(vm.ToValue(err.Error()))
		}
		temporary, err := os.CreateTemp(filepath.Dir(path), ".nyan-save-*")
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		temporaryPath := temporary.Name()
		cleanup := func() { _ = temporary.Close(); _ = os.Remove(temporaryPath) }
		if err := temporary.Chmod(0600); err != nil {
			cleanup()
			panic(vm.ToValue(err.Error()))
		}
		if _, err := temporary.Write([]byte(call.Arguments[1].String())); err != nil {
			cleanup()
			panic(vm.ToValue(err.Error()))
		}
		if err := temporary.Sync(); err != nil {
			cleanup()
			panic(vm.ToValue(err.Error()))
		}
		if err := temporary.Close(); err != nil {
			_ = os.Remove(temporaryPath)
			panic(vm.ToValue(err.Error()))
		}
		if err := os.Rename(temporaryPath, path); err != nil {
			_ = os.Remove(temporaryPath)
			panic(vm.ToValue(err.Error()))
		}
		return vm.ToValue(true)
	}
}

func nyanDeleteFile(vm *goja.Runtime) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(vm.NewTypeError("nyanDeleteFile requires a file path"))
		}
		exePath, err := os.Executable()
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		path := call.Arguments[0].String()
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(exePath), path)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			panic(vm.ToValue(err.Error()))
		}
		return vm.ToValue(true)
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
	snapshot := currentAPISnapshot()
	if snapshot == nil {
		respondJSONRPCError(c, rpcReq.ID, -32603, "API configuration is not loaded", nil)
		return
	}
	config, exists := snapshot.Config[rpcReq.Method]
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
	if err := c.Request.ParseForm(); err != nil {
		respondJSONRPCError(c, rpcReq.ID, -32602, "Invalid params", nil)
		return
	}
	for k, v := range c.Request.PostForm {
		allParams[k] = v[0]
	}
	for k, v := range c.Request.URL.Query() {
		allParams[k] = v[0]
	}
	if err := validateExternalRequestParams(allParams); err != nil {
		respondJSONRPCError(c, rpcReq.ID, -32602, "Invalid params", err.Error())
		return
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

	if allowed, handled := runParamCheckWithSnapshot(c, snapshot, config, exeDir, htmlPath, allParams); handled {
		return
	} else if !allowed {
		return
	}

	// 7) メインのスクリプト実行（runJavaScript は既存関数）
	resultValue, err := runJavaScriptValueWithContext(snapshot, c, scriptPath, htmlPath, allParams)
	if err != nil {
		respondJSONRPCError(c, rpcReq.ID, -32603, "Script execution error", err.Error())
		return
	}
	response, _, err := responseFromJSValue(resultValue)
	if err != nil {
		respondJSONRPCError(c, rpcReq.ID, -32603, "Invalid script response", err.Error())
		return
	}
	if runOutCheckWithSnapshot(c, snapshot, config, exeDir, htmlPath, allParams, response) {
		return
	}

	// 8) Push 処理（必要な場合）
	performPushWithContext(snapshot, c, config, allParams)

	// 10) JSON-RPC 成功レスポンスを構築して返却
	rpcResp := JSONRPCResponse{
		JSONRPC: "2.0",
		Result:  resultValue.String(),
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
	performPushWithSnapshot(currentAPISnapshot(), config, allParams)
}

func performPushWithSnapshot(snapshot *APIConfigSnapshot, config EndpointConfig, allParams map[string]interface{}) {
	performPushWithContext(snapshot, nil, config, allParams)
}

func performPushWithContext(snapshot *APIConfigSnapshot, requestContext *gin.Context, config EndpointConfig, allParams map[string]interface{}) {
	if config.Push == "" {
		return
	}
	if snapshot == nil {
		return
	}
	pushConfig, ok := snapshot.Config[config.Push]
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
	htmlPath := resolvePathFromBase(exeDir, pushConfig.HTML)
	var pushResult string
	if pushConfig.Script == "" {
		content, err := os.ReadFile(htmlPath)
		if err != nil {
			log.Printf("Failed to read push HTML file %s: %v", htmlPath, err)
			return
		}
		pushResult = string(content)
	} else {
		result, err := runJavaScriptValueWithContext(snapshot, requestContext, scriptPath, htmlPath, allParams)
		if err != nil {
			log.Printf("Failed to run push script: %v", err)
			return
		}
		pushResult = result.String()
	}
	if pushResult != "" {
		wsConnections.RLock()
		pushConns := append([]*serverWebSocket(nil), wsConnections.conns[config.Push]...)
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

func parseStaticJavaScriptValue(filename, source string) (interface{}, error) {
	program, err := parser.ParseFile(nil, filename, "("+source+");", 0)
	if err != nil {
		return nil, fmt.Errorf("parse static JavaScript value: %w", err)
	}
	if len(program.Body) != 1 {
		return nil, fmt.Errorf("parse static JavaScript value: expected one expression")
	}
	statement, ok := program.Body[0].(*ast.ExpressionStatement)
	if !ok {
		return nil, fmt.Errorf("parse static JavaScript value: expected an expression, got %T", program.Body[0])
	}
	return convertStaticJavaScriptValue(statement.Expression, "$")
}

func convertStaticJavaScriptValue(expression ast.Expression, path string) (interface{}, error) {
	switch value := expression.(type) {
	case *ast.ObjectLiteral:
		result := make(map[string]interface{}, len(value.Value))
		for _, rawProperty := range value.Value {
			property, ok := rawProperty.(*ast.PropertyKeyed)
			if !ok {
				return nil, fmt.Errorf("static JavaScript value at %s: unsupported property %T", path, rawProperty)
			}
			if property.Computed || property.Kind != ast.PropertyKindValue {
				return nil, fmt.Errorf("static JavaScript value at %s: dynamic properties are not supported", path)
			}
			keyLiteral, ok := property.Key.(*ast.StringLiteral)
			if !ok {
				return nil, fmt.Errorf("static JavaScript value at %s: property names must be strings", path)
			}
			key := keyLiteral.Value.String()
			if _, exists := result[key]; exists {
				return nil, fmt.Errorf("static JavaScript value at %s: duplicate property %q", path, key)
			}
			converted, err := convertStaticJavaScriptValue(property.Value, path+"."+key)
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	case *ast.ArrayLiteral:
		result := make([]interface{}, len(value.Value))
		for index, item := range value.Value {
			if item == nil {
				return nil, fmt.Errorf("static JavaScript value at %s[%d]: array holes are not supported", path, index)
			}
			converted, err := convertStaticJavaScriptValue(item, fmt.Sprintf("%s[%d]", path, index))
			if err != nil {
				return nil, err
			}
			result[index] = converted
		}
		return result, nil
	case *ast.StringLiteral:
		return value.Value.String(), nil
	case *ast.NumberLiteral:
		return staticJavaScriptNumber(value.Value, path)
	case *ast.BooleanLiteral:
		return value.Value, nil
	case *ast.NullLiteral:
		return nil, nil
	case *ast.UnaryExpression:
		if value.Postfix || (value.Operator != token.MINUS && value.Operator != token.PLUS) {
			return nil, fmt.Errorf("static JavaScript value at %s: unary operator is not supported", path)
		}
		numberLiteral, ok := value.Operand.(*ast.NumberLiteral)
		if !ok {
			return nil, fmt.Errorf("static JavaScript value at %s: unary requires a numeric literal", path)
		}
		number, err := staticJavaScriptNumber(numberLiteral.Value, path)
		if err != nil || value.Operator == token.PLUS {
			return number, err
		}
		switch number := number.(type) {
		case int64:
			return -number, nil
		case float64:
			return -number, nil
		}
	}
	return nil, fmt.Errorf("static JavaScript value at %s: expressions of type %T are not supported", path, expression)
}

func staticJavaScriptNumber(value interface{}, path string) (interface{}, error) {
	switch number := value.(type) {
	case int64:
		return number, nil
	case float64:
		if math.IsInf(number, 0) || math.IsNaN(number) {
			return nil, fmt.Errorf("static JavaScript value at %s: non-finite number", path)
		}
		return number, nil
	default:
		return nil, fmt.Errorf("static JavaScript value at %s: numeric value %T is not JSON-compatible", path, value)
	}
}

func extractStaticJavaScriptConstant(filename string, source []byte, constantName string) (interface{}, bool, error) {
	program, err := parser.ParseFile(nil, filename, source, 0)
	if err != nil {
		return nil, false, fmt.Errorf("parse JavaScript file %s: %w", filename, err)
	}
	var initializer ast.Expression
	for _, statement := range program.Body {
		switch declaration := statement.(type) {
		case *ast.LexicalDeclaration:
			for _, binding := range declaration.List {
				identifier, ok := binding.Target.(*ast.Identifier)
				if !ok || identifier.Name.String() != constantName {
					continue
				}
				if declaration.Token != token.CONST {
					return nil, false, fmt.Errorf("JavaScript file %s: %s must be declared with const", filename, constantName)
				}
				if initializer != nil {
					return nil, false, fmt.Errorf("JavaScript file %s: duplicate declaration of %s", filename, constantName)
				}
				if binding.Initializer == nil {
					return nil, false, fmt.Errorf("JavaScript file %s: %s has no initializer", filename, constantName)
				}
				initializer = binding.Initializer
			}
		case *ast.VariableStatement:
			for _, binding := range declaration.List {
				if identifier, ok := binding.Target.(*ast.Identifier); ok && identifier.Name.String() == constantName {
					return nil, false, fmt.Errorf("JavaScript file %s: %s must be declared with const", filename, constantName)
				}
			}
		}
	}
	if initializer == nil {
		return nil, false, nil
	}
	converted, err := convertStaticJavaScriptValue(initializer, constantName)
	if err != nil {
		return nil, false, fmt.Errorf("JavaScript file %s: %w", filename, err)
	}
	return converted, true, nil
}

func readStaticJavaScriptObjectConstant(filePath, constantName string) (map[string]interface{}, bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false, err
	}
	value, found, err := extractStaticJavaScriptConstant(filePath, data, constantName)
	if err != nil || !found {
		return nil, found, err
	}
	object, ok := value.(map[string]interface{})
	if !ok {
		return nil, false, fmt.Errorf("JavaScript file %s: %s must be a static object literal", filePath, constantName)
	}
	return object, true, nil
}

func readOptionalStaticJavaScriptObjectConstant(filePath, constantName string) (map[string]interface{}, bool, error) {
	if _, err := os.Stat(filePath); err != nil {
		return nil, false, nil
	}
	return readStaticJavaScriptObjectConstant(filePath, constantName)
}

func readStaticLegacyAcceptedParams(filePath string) (map[string]interface{}, bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false, err
	}
	value, found, err := extractStaticJavaScriptConstant(filePath, data, "nyanAcceptedParams")
	if err != nil || !found {
		return nil, found, err
	}
	params, ok := value.(map[string]interface{})
	if !ok {
		return nil, false, fmt.Errorf("nyanAcceptedParams must be a static object literal")
	}
	return params, true, nil
}

func resolveAPISchema(config EndpointConfig) (APISchema, error) {
	resolved := APISchema{Input: map[string]interface{}{}, Output: map[string]interface{}{}, InputSource: schemaSourceUnknown, OutputSource: schemaSourceUnknown}
	if config.ParamCheck != "" {
		input, found, err := readOptionalStaticJavaScriptObjectConstant(config.ParamCheck, "nyanInputSchema")
		if err != nil {
			return APISchema{}, fmt.Errorf("input schema from paramCheck: %w", err)
		}
		if found {
			resolved.Input, resolved.InputSource = input, schemaSourceParamCheck
		}
	}
	if config.OutCheck != "" {
		output, found, err := readOptionalStaticJavaScriptObjectConstant(config.OutCheck, "nyanOutputSchema")
		if err != nil {
			return APISchema{}, fmt.Errorf("output schema from outCheck: %w", err)
		}
		if found {
			resolved.Output, resolved.OutputSource = output, schemaSourceOutCheck
		}
	}
	if config.Script != "" && resolved.InputSource == schemaSourceUnknown {
		params, found, err := readStaticLegacyAcceptedParams(config.Script)
		if err == nil && found {
			resolved.Input, resolved.InputSource = legacyInputSchema(params), schemaSourceScriptLegacy
		}
	}
	return resolved, nil
}

func legacyInputSchema(params map[string]interface{}) map[string]interface{} {
	properties := make(map[string]interface{}, len(params))
	for name, value := range params {
		properties[name] = legacyValueSchema(value)
	}
	return map[string]interface{}{"type": "object", "properties": properties, "additionalProperties": true}
}

func legacyValueSchema(value interface{}) map[string]interface{} {
	schema := map[string]interface{}{}
	switch value := value.(type) {
	case string:
		schema["type"] = "string"
	case bool:
		schema["type"] = "boolean"
	case int64, int, int32:
		schema["type"] = "integer"
	case float64:
		if math.Trunc(value) == value {
			schema["type"] = "integer"
		} else {
			schema["type"] = "number"
		}
	case map[string]interface{}:
		properties := map[string]interface{}{}
		for name, item := range value {
			properties[name] = legacyValueSchema(item)
		}
		schema["type"], schema["properties"], schema["additionalProperties"] = "object", properties, true
	case []interface{}:
		schema["type"] = "array"
		if len(value) > 0 {
			schema["items"] = legacyValueSchema(value[0])
		} else {
			schema["items"] = map[string]interface{}{}
		}
	default:
		return schema
	}
	schema["examples"] = []interface{}{value}
	return schema
}

const defaultAPIHotReloadCheckInterval = time.Second

type APIHotReloadConfig struct {
	Enabled  bool   `json:"Enabled"`
	Interval string `json:"Interval"`
}

func applyConfigDefaults(target *Config) {
	target.APIHotReload.Enabled = true
	target.APIHotReload.Interval = defaultAPIHotReloadCheckInterval.String()
}

func parseAPIHotReloadInterval(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultAPIHotReloadCheckInterval, nil
	}
	interval, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if interval <= 0 {
		return 0, fmt.Errorf("must be greater than zero")
	}
	return interval, nil
}

func decodeAPIConfig(data []byte, apiBaseDir string) (APIConfig, error) {
	if err := validateNoDuplicateJSONKeys(data); err != nil {
		return nil, fmt.Errorf("decode api JSON: %w", err)
	}
	var config APIConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("decode api JSON: %w", err)
	}
	if config == nil {
		return nil, fmt.Errorf("decode api JSON: top-level value must be an object")
	}
	adjustAPIConfigPaths(config, apiBaseDir)
	if err := validateMCPConfiguration(config); err != nil {
		return nil, err
	}
	return config, nil
}

// readAPIConfigFile remains as the single-file compatibility entry point used
// by existing callers and tests. Production loading uses readAPIConfigGraph.
func readAPIConfigFile(path, apiBaseDir string) (APIConfig, [sha256.Size]byte, error) {
	loaded, err := readAPIConfigGraph(path, apiBaseDir)
	if err != nil {
		return nil, [sha256.Size]byte{}, fmt.Errorf("read api file: %w", err)
	}
	return loaded.Snapshot.Config, loaded.Hash, nil
}

func currentAPISnapshot() *APIConfigSnapshot {
	apiConfigMu.RLock()
	snapshot := apiSnapshot
	apiConfigMu.RUnlock()
	return snapshot
}

func currentAPIConfig() APIConfig {
	apiConfigMu.RLock()
	config := apiConfig
	apiConfigMu.RUnlock()
	return config
}

func setAPIConfig(config APIConfig) {
	if config == nil {
		publishAPISnapshot(nil)
		return
	}
	schedules, _ := buildScheduleJobConfigs(config)
	wsClients, _ := buildWSClientConfigs(config)
	publishAPISnapshot(&APIConfigSnapshot{Config: config, Sources: map[string]string{}, FileStates: map[string]APIFileState{}, Schedules: schedules, WSClients: wsClients})
}

func publishAPISnapshot(snapshot *APIConfigSnapshot) {
	apiConfigMu.Lock()
	apiSnapshot = snapshot
	if snapshot == nil {
		apiConfig = nil
	} else {
		apiConfig = snapshot.Config
	}
	apiConfigMu.Unlock()
}

func reloadAPIConfigIfChanged(path, apiBaseDir string, lastHash [sha256.Size]byte) ([sha256.Size]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return lastHash, false, fmt.Errorf("read api file: %w", err)
	}
	hash := sha256.Sum256(data)
	if hash == lastHash {
		return lastHash, false, nil
	}
	candidate, err := decodeAPIConfig(data, apiBaseDir)
	if err != nil {
		return hash, false, err
	}
	if reflect.DeepEqual(currentAPIConfig(), candidate) {
		return hash, false, nil
	}
	schedules, err := buildScheduleJobConfigs(candidate)
	if err != nil {
		return hash, false, err
	}
	wsClients, err := buildWSClientConfigs(candidate)
	if err != nil {
		return hash, false, err
	}
	setAPIConfig(candidate)
	if backgroundRuntimes != nil {
		backgroundRuntimes.reconcile(schedules, wsClients)
	}
	return hash, true, nil
}

func watchAPIConfig(path, apiBaseDir string, interval time.Duration, initialHash [sha256.Size]byte) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastHash := initialHash
	lastError := ""
	for range ticker.C {
		hash, reloaded, err := reloadAPIConfigIfChanged(path, apiBaseDir, lastHash)
		lastHash = hash
		if err != nil {
			if err.Error() != lastError {
				log.Printf("API hot reload failed: %v; current API configuration remains active", err)
			}
			lastError = err.Error()
			continue
		}
		lastError = ""
		if reloaded {
			log.Printf("API hot reload succeeded: api_count=%d", len(currentAPIConfig()))
		}
	}
}

type apiIncludeDefinition struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type apiGraphLoader struct {
	definitions APIConfig
	sources     map[string]string
	states      map[string]APIFileState
}

func readAPIConfigGraph(rootPath, apiBaseDir string) (*apiConfigLoadResult, error) {
	rootPath = resolvePathFromBase(apiBaseDir, rootPath)
	rootPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	loader := &apiGraphLoader{definitions: APIConfig{}, sources: map[string]string{}, states: map[string]APIFileState{}}
	result := &apiConfigLoadResult{Snapshot: &APIConfigSnapshot{RootPath: rootPath, Config: loader.definitions, Sources: loader.sources, FileStates: loader.states}}
	rootData, rootIdentity, err := loader.readFile(rootPath)
	if err != nil {
		return result, err
	}
	result.Hash = sha256.Sum256(rootData)
	if err := loader.loadFile(rootPath, rootIdentity, rootData, "", nil); err != nil {
		return result, err
	}
	if err := validateMCPConfiguration(loader.definitions); err != nil {
		return result, err
	}
	schedules, err := buildScheduleJobConfigs(loader.definitions)
	if err != nil {
		return result, err
	}
	wsClients, err := buildWSClientConfigs(loader.definitions)
	if err != nil {
		return result, err
	}
	if err := verifyAPIFileStates(loader.states); err != nil {
		return result, err
	}
	snapshot := &APIConfigSnapshot{RootPath: rootPath, Config: loader.definitions, Sources: loader.sources, FileStates: loader.states, Schedules: schedules, WSClients: wsClients}
	result.Snapshot = snapshot
	return result, nil
}

func (loader *apiGraphLoader) readFile(path string) ([]byte, string, error) {
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, "", err
	}
	identity := absPath
	if canonical, canonicalErr := filepath.EvalSymlinks(absPath); canonicalErr == nil {
		identity = canonical
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		loader.states[identity] = APIFileState{Exists: false}
		return nil, identity, fmt.Errorf("read api file %s: %w", absPath, err)
	}
	loader.states[identity] = APIFileState{Exists: true, Hash: sha256.Sum256(data)}
	return data, identity, nil
}

func (loader *apiGraphLoader) loadFile(path, identity string, data []byte, prefix string, stack []string) error {
	for _, active := range stack {
		if active == identity {
			return fmt.Errorf("include cycle detected at %s", path)
		}
	}
	if err := validateNoDuplicateJSONKeys(data); err != nil {
		return fmt.Errorf("decode api JSON %s: %w", path, err)
	}
	var definitions map[string]json.RawMessage
	if err := json.Unmarshal(data, &definitions); err != nil {
		return fmt.Errorf("decode api JSON %s: %w", path, err)
	}
	if definitions == nil {
		return fmt.Errorf("decode api JSON %s: top-level value must be an object", path)
	}
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	mounts := map[string]struct{}{}
	for _, name := range names {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(definitions[name], &object); err != nil || object == nil {
			if err == nil {
				err = fmt.Errorf("definition must be an object")
			}
			return fmt.Errorf("decode API definition %q in %s: %w", name, path, err)
		}
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(definitions[name], &header); err != nil {
			return fmt.Errorf("decode API definition %q in %s: %w", name, path, err)
		}
		if strings.TrimSpace(header.Type) == apiTypeInclude {
			if err := validateIncludeMountName(name); err != nil {
				return err
			}
			mounts[name] = struct{}{}
		}
	}
	for mount := range mounts {
		for _, name := range names {
			if name != mount && strings.HasPrefix(name, mount+"/") {
				return fmt.Errorf("include mount %q conflicts with API name %q in %s", mount, name, path)
			}
		}
	}
	for _, name := range names {
		raw := definitions[name]
		var header struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &header)
		if strings.TrimSpace(header.Type) == apiTypeInclude {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				return err
			}
			for field := range fields {
				if field != "type" && field != "path" {
					return fmt.Errorf("include %q: unsupported field %q; only type and path are allowed", name, field)
				}
			}
			var include apiIncludeDefinition
			if err := json.Unmarshal(raw, &include); err != nil {
				return fmt.Errorf("include %q: %w", name, err)
			}
			if strings.TrimSpace(include.Path) == "" {
				return fmt.Errorf("include %q: path is empty", name)
			}
			childPath := resolvePathFromBase(filepath.Dir(path), include.Path)
			childData, childIdentity, err := loader.readFile(childPath)
			if err != nil {
				return fmt.Errorf("include %q in %s: %w", name, path, err)
			}
			if err := loader.loadFile(childPath, childIdentity, childData, joinAPIName(prefix, name), append(stack, identity)); err != nil {
				return err
			}
			continue
		}
		var endpoint EndpointConfig
		if err := json.Unmarshal(raw, &endpoint); err != nil {
			return fmt.Errorf("decode API definition %q in %s: %w", name, path, err)
		}
		endpointConfig := APIConfig{name: endpoint}
		adjustAPIConfigPaths(endpointConfig, filepath.Dir(path))
		fullName := joinAPIName(prefix, name)
		if _, exists := loader.definitions[fullName]; exists {
			return fmt.Errorf("duplicate expanded API name %q", fullName)
		}
		loader.definitions[fullName] = endpointConfig[name]
		loader.sources[fullName] = path
	}
	return nil
}

func validateIncludeMountName(name string) error {
	if name == "" || name == "." || name == ".." || strings.TrimSpace(name) != name || strings.Contains(name, "/") {
		return fmt.Errorf("invalid include mount name %q", name)
	}
	return nil
}

func joinAPIName(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

func validateNoDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := consumeJSONValue(decoder, "$", nil); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple top-level JSON values")
		}
		return err
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder, path string, first json.Token) error {
	token := first
	var err error
	if token == nil {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("invalid object key at %s", path)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate key %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder, path+"."+key, nil); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		index := 0
		for decoder.More() {
			if err := consumeJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index), nil); err != nil {
				return err
			}
			index++
		}
		_, err = decoder.Token()
		return err
	default:
		return nil
	}
}

func observeAPIFileStates(states map[string]APIFileState) map[string]APIFileState {
	observed := make(map[string]APIFileState, len(states))
	for path := range states {
		data, err := os.ReadFile(path)
		if err != nil {
			observed[path] = APIFileState{Exists: false}
		} else {
			observed[path] = APIFileState{Exists: true, Hash: sha256.Sum256(data)}
		}
	}
	return observed
}

func verifyAPIFileStates(expected map[string]APIFileState) error {
	observed := observeAPIFileStates(expected)
	if !reflect.DeepEqual(observed, expected) {
		return fmt.Errorf("api files changed while configuration was being loaded")
	}
	return nil
}

func reloadAPIConfigGraphIfChanged(path, apiBaseDir string, watched map[string]APIFileState) (map[string]APIFileState, bool, error) {
	observed := observeAPIFileStates(watched)
	if reflect.DeepEqual(observed, watched) {
		return watched, false, nil
	}
	loaded, err := readAPIConfigGraph(path, apiBaseDir)
	if err != nil {
		if loaded != nil && loaded.Snapshot != nil {
			for filePath, state := range loaded.Snapshot.FileStates {
				observed[filePath] = state
			}
		}
		return observed, false, err
	}
	current := currentAPISnapshot()
	if current != nil && reflect.DeepEqual(current.Config, loaded.Snapshot.Config) && reflect.DeepEqual(current.Sources, loaded.Snapshot.Sources) {
		return loaded.Snapshot.FileStates, false, nil
	}
	publishAPISnapshot(loaded.Snapshot)
	if backgroundRuntimes != nil {
		backgroundRuntimes.reconcile(loaded.Snapshot.Schedules, loaded.Snapshot.WSClients)
	}
	return loaded.Snapshot.FileStates, true, nil
}

func watchAPIConfigGraph(path, apiBaseDir string, interval time.Duration, initialStates map[string]APIFileState) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	watched := initialStates
	lastError := ""
	for range ticker.C {
		states, reloaded, err := reloadAPIConfigGraphIfChanged(path, apiBaseDir, watched)
		watched = states
		if err != nil {
			if err.Error() != lastError {
				log.Printf("API hot reload failed: %v; current API configuration remains active", err)
			}
			lastError = err.Error()
			continue
		}
		lastError = ""
		if reloaded {
			log.Printf("API hot reload succeeded: api_count=%d", len(currentAPIConfig()))
		}
	}
}

type backgroundRuntimeManager struct {
	mu        sync.Mutex
	schedules map[string]*scheduleRuntime
	wsClients map[string]*wsClientRuntime
}

type scheduleRuntime struct {
	mu      sync.Mutex
	desired *scheduleJobConfig
	wake    chan struct{}
	done    chan struct{}
	stopped bool
}

type wsClientRuntime struct {
	mu         sync.Mutex
	desired    *wsClientConfig
	wake       chan struct{}
	done       chan struct{}
	stopped    bool
	conn       *websocket.Conn
	dialCancel context.CancelFunc
}

func newBackgroundRuntimeManager() *backgroundRuntimeManager {
	return &backgroundRuntimeManager{schedules: make(map[string]*scheduleRuntime), wsClients: make(map[string]*wsClientRuntime)}
}

func (manager *backgroundRuntimeManager) reconcile(schedules map[string]scheduleJobConfig, wsClients map[string]wsClientConfig) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for name, runtime := range manager.schedules {
		if _, exists := schedules[name]; !exists {
			if _, changed := runtime.update(nil); changed {
				log.Printf("Stopping schedule job %s", name)
			}
		}
	}
	for name, cfg := range schedules {
		if runtime, exists := manager.schedules[name]; exists {
			if accepted, changed := runtime.update(&cfg); accepted {
				if changed {
					log.Printf("Updated schedule job %s with cron %q", name, cfg.trigger.Value)
				}
				continue
			}
		}
		runtime := newScheduleRuntime(cfg)
		manager.schedules[name] = runtime
		log.Printf("Starting schedule job %s with cron %q", name, cfg.trigger.Value)
		go manager.runSchedule(name, runtime)
	}
	for name, runtime := range manager.wsClients {
		if _, exists := wsClients[name]; !exists {
			if _, changed, _ := runtime.update(nil); changed {
				log.Printf("Stopping WebSocket client %s", name)
			}
		}
	}
	for name, cfg := range wsClients {
		if runtime, exists := manager.wsClients[name]; exists {
			if accepted, changed, reconnect := runtime.update(&cfg); accepted {
				if changed {
					log.Printf("Updated WebSocket client %s reconnect=%t", name, reconnect)
				}
				continue
			}
		}
		runtime := newWSClientRuntime(cfg)
		manager.wsClients[name] = runtime
		log.Printf("Starting WebSocket client %s -> %s", name, cfg.connectURL)
		go manager.runWSClient(name, runtime)
	}
}

func (manager *backgroundRuntimeManager) runSchedule(name string, runtime *scheduleRuntime) {
	runtime.run()
	manager.mu.Lock()
	if manager.schedules[name] == runtime {
		delete(manager.schedules, name)
	}
	manager.mu.Unlock()
}

func (manager *backgroundRuntimeManager) runWSClient(name string, runtime *wsClientRuntime) {
	runtime.run()
	manager.mu.Lock()
	if manager.wsClients[name] == runtime {
		delete(manager.wsClients, name)
	}
	manager.mu.Unlock()
}

func buildScheduleJobConfigs(config APIConfig) (map[string]scheduleJobConfig, error) {
	result := make(map[string]scheduleJobConfig)
	for name, endpoint := range config {
		if strings.TrimSpace(endpoint.Type) != apiTypeSchedule {
			continue
		}
		if strings.TrimSpace(endpoint.Script) == "" {
			return nil, fmt.Errorf("schedule %s: script is missing", name)
		}
		trigger := endpoint.Trigger
		trigger.Type, trigger.Value = strings.TrimSpace(trigger.Type), strings.TrimSpace(trigger.Value)
		if trigger.Type != "cron" {
			return nil, fmt.Errorf("schedule %s: unsupported trigger type %q", name, trigger.Type)
		}
		schedule, err := parseCronSchedule(trigger.Value)
		if err != nil {
			return nil, fmt.Errorf("schedule %s: invalid cron trigger %q: %w", name, trigger.Value, err)
		}
		result[name] = scheduleJobConfig{name: name, scriptPath: endpoint.Script, trigger: trigger, description: endpoint.Description, schedule: schedule}
	}
	return result, nil
}

func buildWSClientConfigs(config APIConfig) (map[string]wsClientConfig, error) {
	result := make(map[string]wsClientConfig)
	for name, endpoint := range config {
		if strings.TrimSpace(endpoint.Type) != apiTypeWSClient {
			continue
		}
		if strings.TrimSpace(endpoint.Script) == "" {
			return nil, fmt.Errorf("ws_client %s: script is missing", name)
		}
		connectURL, err := resolveConnectURL(endpoint.ConnectURL)
		if err != nil {
			return nil, fmt.Errorf("ws_client %s: %w", name, err)
		}
		result[name] = wsClientConfig{name: name, scriptPath: endpoint.Script, connectURL: connectURL, description: endpoint.Description}
	}
	return result, nil
}

func signalRuntime(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func newScheduleRuntime(cfg scheduleJobConfig) *scheduleRuntime {
	copy := cfg
	return &scheduleRuntime{desired: &copy, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func sameScheduleTiming(a, b scheduleJobConfig) bool {
	return a.name == b.name && a.scriptPath == b.scriptPath && a.trigger == b.trigger
}
func sameScheduleConfig(a, b scheduleJobConfig) bool {
	return sameScheduleTiming(a, b) && a.description == b.description
}

func (runtime *scheduleRuntime) update(cfg *scheduleJobConfig) (bool, bool) {
	runtime.mu.Lock()
	if runtime.stopped {
		runtime.mu.Unlock()
		return false, false
	}
	if cfg == nil {
		if runtime.desired == nil {
			runtime.mu.Unlock()
			return true, false
		}
		runtime.desired = nil
		runtime.mu.Unlock()
		signalRuntime(runtime.wake)
		return true, true
	}
	if runtime.desired != nil && sameScheduleConfig(*runtime.desired, *cfg) {
		runtime.mu.Unlock()
		return true, false
	}
	wake := runtime.desired == nil || !sameScheduleTiming(*runtime.desired, *cfg)
	copy := *cfg
	runtime.desired = &copy
	runtime.mu.Unlock()
	if wake {
		signalRuntime(runtime.wake)
	}
	return true, true
}

func (runtime *scheduleRuntime) current(stop bool) (scheduleJobConfig, bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.desired == nil {
		if stop {
			runtime.stopped = true
		}
		return scheduleJobConfig{}, false
	}
	return *runtime.desired, true
}

func (runtime *scheduleRuntime) run() {
	defer close(runtime.done)
	for {
		cfg, active := runtime.current(true)
		if !active {
			return
		}
		next := cfg.schedule.next(time.Now())
		if next.IsZero() {
			<-runtime.wake
			continue
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-runtime.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			continue
		case <-timer.C:
		}
		latest, active := runtime.current(false)
		if !active || !sameScheduleTiming(cfg, latest) {
			continue
		}
		params := map[string]interface{}{"api": latest.name, "nyan_job_name": latest.name, "nyan_schedule_trigger_type": latest.trigger.Type, "nyan_schedule_trigger": latest.trigger.Value, "nyan_schedule_time": next.Format(time.RFC3339), "nyan_schedule_description": latest.description}
		if result, err := runJavaScript(latest.scriptPath, "", params); err != nil {
			log.Printf("Schedule job %s failed: %v", latest.name, err)
		} else {
			log.Printf("Schedule job %s completed: %s", latest.name, result)
		}
	}
}

func newWSClientRuntime(cfg wsClientConfig) *wsClientRuntime {
	copy := cfg
	return &wsClientRuntime{desired: &copy, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func (runtime *wsClientRuntime) update(cfg *wsClientConfig) (bool, bool, bool) {
	runtime.mu.Lock()
	if runtime.stopped {
		runtime.mu.Unlock()
		return false, false, false
	}
	if cfg == nil {
		if runtime.desired == nil {
			runtime.mu.Unlock()
			return true, false, false
		}
		runtime.desired = nil
		conn, cancel := runtime.conn, runtime.dialCancel
		runtime.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if conn != nil {
			_ = conn.Close()
		}
		signalRuntime(runtime.wake)
		return true, true, false
	}
	if runtime.desired != nil && *runtime.desired == *cfg {
		runtime.mu.Unlock()
		return true, false, false
	}
	reconnect := runtime.desired == nil || runtime.desired.connectURL != cfg.connectURL
	copy := *cfg
	runtime.desired = &copy
	conn, cancel := runtime.conn, runtime.dialCancel
	runtime.mu.Unlock()
	if reconnect {
		if cancel != nil {
			cancel()
		}
		if conn != nil {
			_ = conn.Close()
		}
		signalRuntime(runtime.wake)
	}
	return true, true, reconnect
}

func (runtime *wsClientRuntime) current(stop bool) (wsClientConfig, bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.desired == nil {
		if stop {
			runtime.stopped = true
		}
		return wsClientConfig{}, false
	}
	return *runtime.desired, true
}

func (runtime *wsClientRuntime) beginDial(cfg wsClientConfig) (context.Context, context.CancelFunc, bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.stopped || runtime.desired == nil || runtime.desired.connectURL != cfg.connectURL {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	runtime.dialCancel = cancel
	return ctx, cancel, true
}

func (runtime *wsClientRuntime) finishDial(cancel context.CancelFunc) {
	runtime.mu.Lock()
	runtime.dialCancel = nil
	runtime.mu.Unlock()
	cancel()
}
func (runtime *wsClientRuntime) acceptConnection(conn *websocket.Conn, url string) bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.stopped || runtime.desired == nil || runtime.desired.connectURL != url {
		return false
	}
	runtime.conn = conn
	return true
}
func (runtime *wsClientRuntime) clearConnection(conn *websocket.Conn) {
	runtime.mu.Lock()
	if runtime.conn == conn {
		runtime.conn = nil
	}
	runtime.mu.Unlock()
}

func (runtime *wsClientRuntime) run() {
	defer close(runtime.done)
	backoff := time.Second
	for {
		cfg, active := runtime.current(true)
		if !active {
			return
		}
		err := runtime.connectAndListen(cfg)
		latest, active := runtime.current(true)
		if !active {
			return
		}
		if latest.connectURL != cfg.connectURL {
			select {
			case <-runtime.wake:
			default:
			}
			backoff = time.Second
			continue
		}
		if err != nil {
			log.Printf("WebSocket client %s disconnected: %v", cfg.name, err)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-runtime.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			backoff = time.Second
		case <-timer.C:
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (runtime *wsClientRuntime) connectAndListen(cfg wsClientConfig) error {
	ctx, cancel, ok := runtime.beginDial(cfg)
	if !ok {
		return nil
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, cfg.connectURL, nil)
	runtime.finishDial(cancel)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	if !runtime.acceptConnection(conn, cfg.connectURL) {
		_ = conn.Close()
		return nil
	}
	defer runtime.clearConnection(conn)
	defer conn.Close()
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read error: %w", err)
		}
		latest, active := runtime.current(false)
		if !active || latest.connectURL != cfg.connectURL {
			return nil
		}
		params := map[string]interface{}{"api": latest.name, "ws_client": latest.name, "ws_message_type": websocketMessageTypeLabel(msgType), "ws_message_text": string(data), "ws_connect_url": latest.connectURL, "ws_description": latest.description}
		if msgType == websocket.BinaryMessage {
			params["ws_message_base64"] = base64.StdEncoding.EncodeToString(data)
		}
		if msgType == websocket.TextMessage {
			var decoded interface{}
			if json.Unmarshal(data, &decoded) == nil {
				params["ws_message_json"] = decoded
			}
		}
		result, err := runJavaScript(latest.scriptPath, "", params)
		if err != nil {
			log.Printf("ws_client %s script error: %v", latest.name, err)
			continue
		}
		if result = strings.TrimSpace(result); result != "" {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(result)); err != nil {
				return fmt.Errorf("send error: %w", err)
			}
		}
	}
}

// MCP/OAuth execution is intentionally delegated to JavaScript. Go only
// transports request data to the configured hook and serializes its result.
type mcpRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpRuntimeURLs struct{ Origin, Resource, Issuer, AuthorizationServerMetadata, ProtectedResourceMetadata, AuthorizationEndpoint, TokenEndpoint, RegistrationEndpoint, AdminUserEndpoint string }

var mcpDNSLabelPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

func deriveMCPRuntimeURLs(request *http.Request, name string, mcp EndpointConfig) (mcpRuntimeURLs, error) {
	if request == nil || request.URL == nil {
		return mcpRuntimeURLs{}, fmt.Errorf("request URL unavailable")
	}
	scheme := strings.ToLower(request.URL.Scheme)
	if scheme == "" {
		if request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	if scheme != "http" && scheme != "https" {
		return mcpRuntimeURLs{}, fmt.Errorf("invalid scheme")
	}
	parsed, err := url.Parse("//" + strings.TrimSpace(request.Host))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" {
		return mcpRuntimeURLs{}, fmt.Errorf("invalid host")
	}
	hostname := parsed.Hostname()
	if net.ParseIP(hostname) == nil {
		for _, label := range strings.Split(hostname, ".") {
			if !mcpDNSLabelPattern.MatchString(label) {
				return mcpRuntimeURLs{}, fmt.Errorf("invalid host")
			}
		}
	}
	if port := parsed.Port(); port != "" {
		number, e := strconv.Atoi(port)
		if e != nil || number < 1 || number > 65535 {
			return mcpRuntimeURLs{}, fmt.Errorf("invalid port")
		}
	}
	authority := parsed.Host
	origin := scheme + "://" + authority
	apiURL := func(api string) string {
		if api == "" {
			return ""
		}
		path, e := canonicalAPIEndpointPath(api)
		if e != nil {
			return ""
		}
		return origin + path
	}
	path, _ := canonicalAPIEndpointPath(name)
	return mcpRuntimeURLs{Origin: origin, Resource: origin + path, Issuer: origin, AuthorizationServerMetadata: apiURL(mcp.OAuth.AuthorizationServerMetadata), ProtectedResourceMetadata: apiURL(mcp.OAuth.ProtectedResourceMetadataAPI), AuthorizationEndpoint: apiURL(mcp.OAuth.Authorize), TokenEndpoint: apiURL(mcp.OAuth.Token), RegistrationEndpoint: apiURL(mcp.OAuth.Register), AdminUserEndpoint: apiURL(mcp.OAuth.AdminUser)}, nil
}

func dispatchMCPOrOAuth(c *gin.Context) bool {
	snapshot := currentAPISnapshot()
	if snapshot == nil {
		return false
	}
	apiName := strings.Trim(strings.TrimSpace(c.Request.URL.Path), "/")
	if c.Request.URL.Path == "/" {
		apiName = strings.TrimSpace(c.Query("api"))
	}
	for _, name := range sortedMCPNames(snapshot.Config) {
		mcp := snapshot.Config[name]
		if mcp.Transport != "streamable_http" {
			continue
		}
		if apiName == name {
			handleMCPHTTP(c, snapshot, name, mcp)
			return true
		}
		if role, ok := mcpOAuthRoleForAPI(mcp, apiName); ok {
			handleOAuthJavaScript(c, snapshot, name, mcp, role)
			return true
		}
	}
	return false
}

func sortedMCPNames(config APIConfig) []string {
	names := []string{}
	for name, e := range config {
		if e.Type == apiTypeMCP {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func mcpOAuthRoleForAPI(mcp EndpointConfig, apiName string) (string, bool) {
	refs := []struct{ api, role string }{{mcp.OAuth.AuthorizationServerMetadata, "authorizationServerMetadata"}, {mcp.OAuth.ProtectedResourceMetadataAPI, "protectedResourceMetadata"}, {mcp.OAuth.Authorize, "oauthAuthorize"}, {mcp.OAuth.Token, "oauthToken"}, {mcp.OAuth.Register, "oauthRegister"}, {mcp.OAuth.AdminUser, "oauthAdminUser"}, {mcp.OAuth.VerifyAccess, "oauthValidateAccessToken"}}
	for _, ref := range refs {
		if ref.api != "" && ref.api == apiName {
			return ref.role, true
		}
	}
	return "", false
}

func handleMCPHTTP(c *gin.Context, snapshot *APIConfigSnapshot, endpointName string, mcp EndpointConfig) {
	runtimeURLs, err := deriveMCPRuntimeURLs(c.Request, endpointName, mcp)
	if err != nil {
		c.JSON(http.StatusMisdirectedRequest, gin.H{"error": "invalid Host"})
		return
	}
	if !mcpOriginAllowed(c.Request.Header.Get("Origin"), mcp, runtimeURLs.Origin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "origin is not allowed"})
		return
	}
	if origin := strings.TrimSpace(c.Request.Header.Get("Origin")); origin != "" {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Accept, Authorization, MCP-Protocol-Version, MCP-Session-Id, Last-Event-ID")
		c.Header("Access-Control-Allow-Methods", "POST, OPTIONS")
		c.Header("Access-Control-Expose-Headers", "WWW-Authenticate, MCP-Protocol-Version, Retry-After")
	}
	if c.Request.Method == http.MethodOptions {
		c.Status(http.StatusNoContent)
		return
	}
	if c.Request.Method != http.MethodPost {
		c.Header("Allow", "POST, OPTIONS")
		c.Status(http.StatusMethodNotAllowed)
		return
	}
	if allowed, retryAfter := mcpRateLimitAllows(endpointName, mcp.RateLimit, c.Request.RemoteAddr, time.Now()); !allowed {
		c.Header("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retryAfter.Seconds())))))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
		return
	}
	release, acquired := acquireMCPExecutionSlot(endpointName, mcpMaxConcurrent(mcp))
	if !acquired {
		c.Header("Retry-After", "1")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "MCP server is busy"})
		return
	}
	defer release()
	contentType, _, contentTypeErr := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if contentTypeErr != nil || contentType != "application/json" {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "Content-Type must be application/json"})
		return
	}
	if !strings.Contains(c.GetHeader("Accept"), "application/json") || !strings.Contains(c.GetHeader("Accept"), "text/event-stream") {
		c.JSON(http.StatusNotAcceptable, gin.H{"error": "Accept must include application/json and text/event-stream"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body is too large"})
		return
	}
	if len(bytes.TrimSpace(body)) == 0 || bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
		mcpWriteError(c, nil, -32600, "Invalid Request")
		return
	}
	if err := validateNoDuplicateJSONKeys(body); err != nil {
		mcpWriteError(c, nil, -32600, "Invalid Request")
		return
	}
	var request mcpRPCRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF || request.JSONRPC != "2.0" || request.Method == "" || !validMCPRequestID(request.ID) {
		mcpWriteError(c, nil, -32700, "Parse error")
		return
	}
	if request.Method == "initialize" {
		var params map[string]interface{}
		_ = json.Unmarshal(request.Params, &params)
		version, _ := params["protocolVersion"].(string)
		if version == "" {
			mcpWriteError(c, request.ID, -32602, "protocolVersion is required")
			return
		}
		if len(mcp.ProtocolVersions) > 0 {
			supported := false
			for _, candidate := range mcp.ProtocolVersions {
				if candidate == version {
					supported = true
					break
				}
			}
			if !supported {
				mcpWriteError(c, request.ID, -32602, "unsupported protocolVersion")
				return
			}
		}
		c.Header("MCP-Protocol-Version", version)
		mcpWriteResult(c, request.ID, mcpInitializeResult(mcp, version))
		return
	}
	protocolVersion := c.GetHeader("MCP-Protocol-Version")
	if !mcpProtocolVersionAllowed(protocolVersion, mcp.ProtocolVersions) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "MCP-Protocol-Version is missing or unsupported"})
		return
	}
	if len(bytes.TrimSpace(request.ID)) == 0 {
		c.Status(http.StatusAccepted)
		return
	}
	switch request.Method {
	case "ping":
		mcpWriteResult(c, request.ID, map[string]interface{}{})
	case "tools/list":
		mcpWriteResult(c, request.ID, map[string]interface{}{"tools": mcpToolListForTransport(mcp, true)})
	case "tools/call":
		handleMCPToolCall(c, snapshot, endpointName, mcp, runtimeURLs, request)
	default:
		mcpWriteError(c, request.ID, -32601, "Method not found")
	}
}

func mcpProtocolVersionAllowed(version string, allowed []string) bool {
	if strings.TrimSpace(version) == "" {
		return false
	}
	for _, candidate := range allowed {
		if version == candidate {
			return true
		}
	}
	return false
}

func mcpRateLimitAllows(endpointName string, limit *MCPRateLimit, remoteAddress string, now time.Time) (bool, time.Duration) {
	if limit == nil {
		return true, 0
	}
	window, err := time.ParseDuration(limit.Window)
	if err != nil || window <= 0 || limit.Requests <= 0 {
		return false, time.Second
	}
	host := remoteAddress
	if parsed, _, err := net.SplitHostPort(remoteAddress); err == nil {
		host = parsed
	}
	key := endpointName + "\x00" + host + "\x00" + strconv.Itoa(limit.Requests) + "\x00" + window.String()
	mcpRateBuckets.Lock()
	defer mcpRateBuckets.Unlock()
	if mcpRateBuckets.LastCleanup.IsZero() || now.Sub(mcpRateBuckets.LastCleanup) >= time.Minute {
		for bucketKey, bucket := range mcpRateBuckets.Buckets {
			if now.Sub(bucket.StartedAt) >= bucket.Window*2 {
				delete(mcpRateBuckets.Buckets, bucketKey)
			}
		}
		mcpRateBuckets.LastCleanup = now
	}
	bucket, exists := mcpRateBuckets.Buckets[key]
	if !exists || now.Before(bucket.StartedAt) || now.Sub(bucket.StartedAt) >= window {
		mcpRateBuckets.Buckets[key] = mcpRateBucket{StartedAt: now, Window: window, Count: 1}
		return true, 0
	}
	if bucket.Count >= limit.Requests {
		return false, window - now.Sub(bucket.StartedAt)
	}
	bucket.Count++
	mcpRateBuckets.Buckets[key] = bucket
	return true, 0
}

func mcpMaxConcurrent(endpoint EndpointConfig) int {
	if endpoint.MaxConcurrent > 0 {
		return endpoint.MaxConcurrent
	}
	return 16
}

func acquireMCPExecutionSlot(endpointName string, limit int) (func(), bool) {
	key := endpointName + "\x00" + strconv.Itoa(limit)
	mcpConcurrencyLimiters.Lock()
	limiter := mcpConcurrencyLimiters.Limiters[key]
	if limiter == nil {
		limiter = make(chan struct{}, limit)
		mcpConcurrencyLimiters.Limiters[key] = limiter
	}
	mcpConcurrencyLimiters.Unlock()
	select {
	case limiter <- struct{}{}:
		return func() { <-limiter }, true
	default:
		return func() {}, false
	}
}

func handleMCPToolCall(c *gin.Context, snapshot *APIConfigSnapshot, endpointName string, mcp EndpointConfig, runtimeURLs mcpRuntimeURLs, request mcpRPCRequest) {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil || params.Name == "" {
		mcpWriteError(c, request.ID, -32602, "Invalid params")
		return
	}
	var tool MCPToolConfig
	for _, candidate := range mcp.Tools {
		if candidate.Name == params.Name {
			tool = candidate
			break
		}
	}
	if tool.Name == "" {
		mcpWriteError(c, request.ID, -32602, "Unknown tool")
		return
	}
	principal, authenticated := interface{}(map[string]interface{}{"anonymous": true, "transport": "streamable_http"}), true
	if mcpOAuthConfigured(mcp.OAuth) {
		principal, authenticated = invokeOAuthHook(c, snapshot, endpointName, mcp, runtimeURLs, "oauthValidateAccessToken", map[string]interface{}{"authorization": c.GetHeader("Authorization"), "tool": tool.Name, "required_scopes": mcpToolScopes(tool)})
	}
	if !authenticated {
		challenge := fmt.Sprintf(`Bearer resource_metadata="%s", scope="%s"`, runtimeURLs.ProtectedResourceMetadata, strings.Join(mcpToolScopes(tool), " "))
		c.Header("WWW-Authenticate", challenge)
		mcpWriteHTTPResult(c, request.ID, http.StatusUnauthorized, map[string]interface{}{"content": []map[string]interface{}{{"type": "text", "text": "Authentication required."}}, "isError": true, "_meta": map[string]interface{}{"mcp/www_authenticate": []string{challenge}}})
		return
	}
	if params.Arguments == nil {
		params.Arguments = map[string]interface{}{}
	}
	for key := range params.Arguments {
		if key == "api" || key == "mcp_principal" || key == "mcp_tool" || strings.HasPrefix(key, "_headers") || strings.HasPrefix(key, "_remote") {
			mcpWriteError(c, request.ID, -32602, "Tool arguments contain a reserved parameter")
			return
		}
	}
	if err := validateMCPJSONSchemaValue(tool.InputSchema, params.Arguments); err != nil {
		mcpWriteError(c, request.ID, -32602, "Tool arguments do not match inputSchema")
		return
	}
	payload, toolError := executeMCPToolWithContext(snapshot, c, tool, params.Arguments, principal)
	if toolError != "" {
		mcpWriteHTTPResult(c, request.ID, http.StatusOK, map[string]interface{}{"content": []map[string]interface{}{{"type": "text", "text": toolError}}, "isError": true})
		return
	}
	mcpWriteResult(c, request.ID, payload)
}

func mcpToolScopes(tool MCPToolConfig) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, scheme := range tool.SecuritySchemes {
		raw, ok := scheme["scopes"].([]interface{})
		if ok {
			for _, item := range raw {
				value, ok := item.(string)
				if ok && !seen[value] {
					seen[value] = true
					result = append(result, value)
				}
			}
		}
		if typed, ok := scheme["scopes"].([]string); ok {
			for _, value := range typed {
				if !seen[value] {
					seen[value] = true
					result = append(result, value)
				}
			}
		}
	}
	return result
}

func invokeOAuthHook(c *gin.Context, snapshot *APIConfigSnapshot, endpointName string, mcp EndpointConfig, runtimeURLs mcpRuntimeURLs, hookName string, extra map[string]interface{}) (interface{}, bool) {
	apiName := map[string]string{"oauthRegister": mcp.OAuth.Register, "oauthAuthorize": mcp.OAuth.Authorize, "oauthToken": mcp.OAuth.Token, "oauthAdminUser": mcp.OAuth.AdminUser, "oauthValidateAccessToken": mcp.OAuth.VerifyAccess}[hookName]
	backing, exists := snapshot.Config[apiName]
	hookPath := strings.TrimSpace(backing.Script)
	if !exists || hookPath == "" {
		return nil, false
	}
	apiPath, pathErr := canonicalAPIEndpointPath(apiName)
	if pathErr != nil {
		return nil, false
	}
	params := map[string]interface{}{"oauth_hook": hookName, "oauth_api": apiName, "method": c.Request.Method, "request_path": c.Request.URL.Path, "path": apiPath, "endpoint": endpointName, "mcp_api_name": endpointName, "issuer": runtimeURLs.Issuer, "resource": runtimeURLs.Resource, "authorization_server_metadata_url": runtimeURLs.AuthorizationServerMetadata, "protected_resource_metadata_url": runtimeURLs.ProtectedResourceMetadata, "authorization_endpoint": runtimeURLs.AuthorizationEndpoint, "token_endpoint": runtimeURLs.TokenEndpoint, "registration_endpoint": runtimeURLs.RegistrationEndpoint, "admin_user_endpoint": runtimeURLs.AdminUserEndpoint, "scopes": mcp.OAuth.Scopes, "redirect_uri_allowed_prefixes": mcp.RedirectURIAllowedPrefixes, "state_directory": mcpOAuthStateDirectory(snapshot, endpointName), "operator_username": globalConfig.BasicAuth.Username, "operator_password": globalConfig.BasicAuth.Password, "authorization": c.GetHeader("Authorization")}
	for key, value := range extra {
		params[key] = value
	}
	if c.Request.URL != nil {
		params["query"] = c.Request.URL.Query()
	}
	params["headers"] = c.Request.Header
	cookies := map[string]string{}
	for _, cookie := range c.Request.Cookies() {
		cookies[cookie.Name] = cookie.Value
	}
	params["cookies"] = cookies
	if c.Request.Method == http.MethodPost {
		contentType := c.GetHeader("Content-Type")
		if strings.HasPrefix(contentType, "application/json") {
			data, readErr := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20+1))
			if readErr == nil && len(data) <= 1<<20 {
				var body interface{}
				if json.Unmarshal(data, &body) == nil {
					params["body"] = body
				}
				c.Request.Body = io.NopCloser(bytes.NewReader(data))
			}
		} else {
			_ = c.Request.ParseForm()
			params["form"] = c.Request.PostForm
		}
	}
	value, err := runJavaScriptValueWithContext(snapshot, c, hookPath, "", params)
	if err != nil || value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, false
	}
	exported := value.Export()
	if result, ok := exported.(map[string]interface{}); ok {
		if allowed, exists := result["allowed"].(bool); exists {
			return result["principal"], allowed
		}
		if authenticated, exists := result["authenticated"].(bool); exists {
			return result["principal"], authenticated
		}
	}
	if hookName == "oauthValidateAccessToken" {
		return nil, false
	}
	return exported, true
}

func handleOAuthJavaScript(c *gin.Context, snapshot *APIConfigSnapshot, endpointName string, mcp EndpointConfig, role string) {
	runtimeURLs, err := deriveMCPRuntimeURLs(c.Request, endpointName, mcp)
	if err != nil {
		c.JSON(http.StatusMisdirectedRequest, gin.H{"error": "invalid Host"})
		return
	}
	if !mcpOriginAllowed(c.GetHeader("Origin"), mcp, runtimeURLs.Origin) {
		c.JSON(http.StatusForbidden, gin.H{"error": "origin is not allowed"})
		return
	}
	if role == "authorizationServerMetadata" || role == "protectedResourceMetadata" {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodOptions {
			c.Header("Allow", "GET, OPTIONS")
			c.Status(http.StatusMethodNotAllowed)
			return
		}
		if c.Request.Method == http.MethodOptions {
			c.Status(http.StatusNoContent)
			return
		}
		if role == "authorizationServerMetadata" {
			c.JSON(http.StatusOK, gin.H{"issuer": runtimeURLs.Issuer, "authorization_endpoint": runtimeURLs.AuthorizationEndpoint, "token_endpoint": runtimeURLs.TokenEndpoint, "registration_endpoint": runtimeURLs.RegistrationEndpoint, "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": mcp.OAuth.Scopes})
			return
		}
		c.JSON(http.StatusOK, gin.H{"resource": runtimeURLs.Resource, "authorization_servers": []string{runtimeURLs.Issuer}, "scopes_supported": mcp.OAuth.Scopes})
		return
	}
	hookName := role
	if hookName == "oauthValidateAccessToken" || hookName == "" {
		c.Status(http.StatusNotFound)
		return
	}
	value, ok := invokeOAuthHook(c, snapshot, endpointName, mcp, runtimeURLs, hookName, map[string]interface{}{})
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "OAuth hook denied the request"})
		return
	}
	if response, isResponse := value.(map[string]interface{}); isResponse {
		if rawStatus, exists := response["status"]; exists {
			status, _ := parseStatusCode(rawStatus)
			contentType, _ := response["contentType"].(string)
			body, _ := jsBodyToBytes(response["body"])
			if headers, ok := response["headers"].(map[string]interface{}); ok {
				for key, value := range headers {
					c.Header(key, fmt.Sprint(value))
				}
			}
			if contentType == "" {
				contentType = "application/json; charset=utf-8"
			}
			c.Data(status, contentType, body)
			return
		}
	}
	c.JSON(http.StatusOK, value)
}

func mcpOriginAllowed(origin string, mcp EndpointConfig, requestOrigin string) bool {
	if origin == "" {
		return true
	}
	for _, allowed := range mcp.AllowedOrigins {
		if strings.TrimSuffix(origin, "/") == strings.TrimSuffix(allowed, "/") {
			return true
		}
	}
	return strings.EqualFold(strings.TrimSuffix(origin, "/"), strings.TrimSuffix(requestOrigin, "/"))
}

func mcpOAuthStateDirectory(snapshot *APIConfigSnapshot, name string) string {
	root := strings.TrimSpace(globalConfig.OAuthStateRoot)
	if root == "" && snapshot != nil {
		source := snapshot.Sources[name]
		if source == "" {
			source = snapshot.RootPath
		}
		root = filepath.Join(filepath.Dir(source), "oauth-state")
	}
	return filepath.Join(root, filepath.FromSlash(name))
}

func mcpWriteResult(c *gin.Context, id json.RawMessage, result interface{}) {
	mcpWriteHTTPResult(c, id, http.StatusOK, map[string]interface{}{"jsonrpc": "2.0", "id": rawMCPID(id), "result": result})
}

func mcpWriteError(c *gin.Context, id json.RawMessage, code int, message string) {
	mcpWriteHTTPResult(c, id, http.StatusOK, map[string]interface{}{"jsonrpc": "2.0", "id": rawMCPID(id), "error": map[string]interface{}{"code": code, "message": message}})
}

func mcpWriteHTTPResult(c *gin.Context, id json.RawMessage, status int, payload interface{}) {
	object, isObject := payload.(map[string]interface{})
	if !isObject || object["jsonrpc"] == nil {
		payload = map[string]interface{}{"jsonrpc": "2.0", "id": rawMCPID(id), "result": payload}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		status = http.StatusInternalServerError
		encoded = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"MCP response could not be serialized"}}`)
	} else if len(encoded) > maxMCPResponseBytes {
		status = http.StatusInternalServerError
		encoded = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"MCP response is too large"}}`)
	}
	c.Data(status, "application/json; charset=utf-8", encoded)
}

func rawMCPID(id json.RawMessage) interface{} {
	if len(id) == 0 {
		return nil
	}
	return id
}

type mcpStdioLifecycle int

const (
	mcpStdioCreated mcpStdioLifecycle = iota
	mcpStdioWaitingForInitialized
	mcpStdioReady
)

type mcpStdioProtocolResponse struct {
	Payload interface{}
	Respond bool
}

func selectMCPStdioServer(snapshot *APIConfigSnapshot, name string) (EndpointConfig, error) {
	if snapshot == nil {
		return EndpointConfig{}, fmt.Errorf("API snapshot is unavailable")
	}
	mcp, ok := snapshot.Config[name]
	if !ok || mcp.Type != apiTypeMCP {
		return EndpointConfig{}, fmt.Errorf("MCP API %q was not found", name)
	}
	if mcp.Transport != "stdio" {
		return EndpointConfig{}, fmt.Errorf("MCP API %q does not use stdio", name)
	}
	return mcp, nil
}

func serveMCPStdio(input io.Reader, output io.Writer, snapshot *APIConfigSnapshot, mcp EndpointConfig) error {
	if input == nil || output == nil || snapshot == nil {
		return fmt.Errorf("stdio MCP input, output, and snapshot are required")
	}
	if mcp.Transport != "stdio" {
		return fmt.Errorf("selected MCP does not use stdio")
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), (1<<20)+1)
	state := mcpStdioCreated
	for scanner.Scan() {
		message := append([]byte(nil), scanner.Bytes()...)
		if len(message) > 1<<20 {
			return fmt.Errorf("stdio MCP message exceeds 1 MiB")
		}
		response := handleMCPStdioMessage(snapshot, mcp, &state, message)
		if response.Respond {
			if err := writeMCPStdioMessage(output, response.Payload); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stdio MCP input failed: %w", err)
	}
	return nil
}

func handleMCPStdioMessage(snapshot *APIConfigSnapshot, mcp EndpointConfig, state *mcpStdioLifecycle, message []byte) mcpStdioProtocolResponse {
	request, code, msg := decodeMCPStdioRequest(message)
	if code != 0 {
		return mcpStdioError(nil, code, msg)
	}
	notification := len(request.ID) == 0
	if request.Method == "notifications/initialized" {
		if !notification {
			return mcpStdioError(request.ID, -32600, "notifications/initialized must be a notification")
		}
		if *state == mcpStdioWaitingForInitialized && mcpParamsAreObjectOrEmpty(request.Params) {
			*state = mcpStdioReady
		}
		return mcpStdioProtocolResponse{}
	}
	if notification {
		return mcpStdioProtocolResponse{}
	}
	if request.Method == "initialize" {
		if *state != mcpStdioCreated {
			return mcpStdioError(request.ID, -32600, "MCP server is already initialized")
		}
		var params struct {
			ProtocolVersion string                 `json:"protocolVersion"`
			Capabilities    map[string]interface{} `json:"capabilities"`
			ClientInfo      map[string]interface{} `json:"clientInfo"`
			Meta            map[string]interface{} `json:"_meta"`
		}
		if !decodeMCPParams(request.Params, &params) || !mcpProtocolVersionAllowed(params.ProtocolVersion, mcp.ProtocolVersions) {
			return mcpStdioError(request.ID, -32602, "unsupported or missing protocolVersion")
		}
		*state = mcpStdioWaitingForInitialized
		return mcpStdioResult(request.ID, mcpInitializeResult(mcp, params.ProtocolVersion))
	}
	if *state != mcpStdioReady {
		return mcpStdioError(request.ID, -32002, "MCP server is not initialized")
	}
	switch request.Method {
	case "ping":
		if !mcpParamsAreObjectOrEmpty(request.Params) {
			return mcpStdioError(request.ID, -32602, "Invalid params")
		}
		return mcpStdioResult(request.ID, map[string]interface{}{})
	case "tools/list":
		if !mcpParamsAreObjectOrEmpty(request.Params) {
			return mcpStdioError(request.ID, -32602, "Invalid params")
		}
		return mcpStdioResult(request.ID, map[string]interface{}{"tools": mcpToolListForTransport(mcp, false)})
	case "tools/call":
		return handleMCPStdioToolCall(snapshot, mcp, request)
	default:
		return mcpStdioError(request.ID, -32601, "Method not found")
	}
}

func decodeMCPStdioRequest(message []byte) (mcpRPCRequest, int, string) {
	trimmed := bytes.TrimSpace(message)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return mcpRPCRequest{}, -32700, "Parse error"
	}
	if trimmed[0] != '{' {
		return mcpRPCRequest{}, -32600, "Invalid Request"
	}
	if validateNoDuplicateJSONKeys(trimmed) != nil {
		return mcpRPCRequest{}, -32600, "Invalid Request"
	}
	var request mcpRPCRequest
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if decoder.Decode(&request) != nil || request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" || !validMCPRequestID(request.ID) {
		return mcpRPCRequest{}, -32600, "Invalid Request"
	}
	return request, 0, ""
}
func validMCPRequestID(id json.RawMessage) bool {
	if len(id) == 0 {
		return true
	}
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(id))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	switch value.(type) {
	case string, json.Number, nil:
		return true
	}
	return false
}
func decodeMCPParams(raw json.RawMessage, target interface{}) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	if validateNoDuplicateJSONKeys(raw) != nil {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil
}
func mcpParamsAreObjectOrEmpty(raw json.RawMessage) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	var value map[string]interface{}
	return json.Unmarshal(raw, &value) == nil
}
func mcpInitializeResult(mcp EndpointConfig, version string) map[string]interface{} {
	return map[string]interface{}{"protocolVersion": version, "capabilities": map[string]interface{}{"tools": map[string]interface{}{"listChanged": false}}, "serverInfo": map[string]interface{}{"name": globalConfig.Name, "version": globalConfig.Version}, "instructions": mcp.Instructions}
}
func mcpToolListForTransport(mcp EndpointConfig, httpSecurity bool) []map[string]interface{} {
	tools := make([]map[string]interface{}, 0, len(mcp.Tools))
	for _, tool := range mcp.Tools {
		entry := map[string]interface{}{"name": tool.Name, "title": tool.Title, "description": tool.Description, "inputSchema": tool.InputSchema}
		if tool.OutputSchema != nil {
			entry["outputSchema"] = tool.OutputSchema
		}
		if httpSecurity && len(tool.SecuritySchemes) != 0 {
			entry["securitySchemes"] = tool.SecuritySchemes
			entry["_meta"] = map[string]interface{}{"securitySchemes": tool.SecuritySchemes}
		}
		if tool.Annotations != nil {
			entry["annotations"] = tool.Annotations
		}
		tools = append(tools, entry)
	}
	return tools
}
func handleMCPStdioToolCall(snapshot *APIConfigSnapshot, mcp EndpointConfig, request mcpRPCRequest) mcpStdioProtocolResponse {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if !decodeMCPParams(request.Params, &params) || params.Name == "" {
		return mcpStdioError(request.ID, -32602, "Invalid params")
	}
	var tool *MCPToolConfig
	for index := range mcp.Tools {
		if mcp.Tools[index].Name == params.Name {
			tool = &mcp.Tools[index]
			break
		}
	}
	if tool == nil {
		return mcpStdioError(request.ID, -32602, "Unknown tool")
	}
	scopes := mcpToolScopes(*tool)
	principal := map[string]interface{}{"user_id": "local-process", "username": "local-process", "client_id": "stdio", "transport": "stdio", "scope": strings.Join(scopes, " "), "scopes": scopes}
	payload, toolError := executeMCPTool(snapshot, *tool, params.Arguments, principal)
	if toolError != "" {
		return mcpStdioResult(request.ID, map[string]interface{}{"content": []map[string]interface{}{{"type": "text", "text": toolError}}, "isError": true})
	}
	return mcpStdioResult(request.ID, payload)
}
func executeMCPTool(snapshot *APIConfigSnapshot, tool MCPToolConfig, arguments map[string]interface{}, principal interface{}) (map[string]interface{}, string) {
	return executeMCPToolWithContext(snapshot, nil, tool, arguments, principal)
}

func executeMCPToolWithContext(snapshot *APIConfigSnapshot, requestContext *gin.Context, tool MCPToolConfig, arguments map[string]interface{}, principal interface{}) (map[string]interface{}, string) {
	if arguments == nil {
		arguments = map[string]interface{}{}
	}
	if validateMCPJSONSchemaValue(tool.InputSchema, arguments) != nil {
		return nil, "Tool arguments do not match inputSchema."
	}
	args := map[string]interface{}{}
	for key, value := range arguments {
		if key == "api" || key == "mcp_principal" || key == "mcp_tool" || strings.HasPrefix(key, "_headers") || strings.HasPrefix(key, "_remote") {
			return nil, "Tool arguments contain a reserved parameter."
		}
		args[key] = value
	}
	args["api"], args["mcp_principal"], args["mcp_tool"] = tool.API, principal, tool.Name
	backing, ok := snapshot.Config[tool.API]
	if !ok {
		return nil, "Tool backing API is unavailable."
	}
	value, err := runJavaScriptValueWithContext(snapshot, requestContext, backing.Script, backing.HTML, args)
	if err != nil {
		return nil, "Tool execution failed."
	}
	response, handled, err := responseFromJSValue(value)
	if err != nil {
		return nil, "Tool returned invalid JSON."
	}
	body := response.Body
	if exported, ok := value.Export().(map[string]interface{}); ok {
		_, hasBody := exported["body"]
		_, hasStatus := exported["status"]
		_, hasContentType := exported["contentType"]
		_, hasHeaders := exported["headers"]
		handled = hasBody || hasStatus || hasContentType || hasHeaders
	}
	if !handled {
		body, _ = json.Marshal(value.Export())
	}
	if len(body) > maxMCPToolResultBytes {
		return nil, "Tool result is too large."
	}
	var structured interface{}
	if json.Unmarshal(body, &structured) != nil {
		return nil, "Tool returned invalid JSON."
	}
	if response.Status < http.StatusBadRequest && tool.OutputSchema != nil && validateMCPJSONSchemaValue(tool.OutputSchema, structured) != nil {
		return nil, "Tool result does not match outputSchema."
	}
	return map[string]interface{}{"content": []map[string]interface{}{{"type": "text", "text": string(body)}}, "structuredContent": structured, "isError": response.Status >= http.StatusBadRequest}, ""
}
func mcpStdioResult(id json.RawMessage, result interface{}) mcpStdioProtocolResponse {
	return mcpStdioProtocolResponse{Respond: true, Payload: map[string]interface{}{"jsonrpc": "2.0", "id": rawMCPID(id), "result": result}}
}
func mcpStdioError(id json.RawMessage, code int, message string) mcpStdioProtocolResponse {
	return mcpStdioProtocolResponse{Respond: true, Payload: map[string]interface{}{"jsonrpc": "2.0", "id": rawMCPID(id), "error": map[string]interface{}{"code": code, "message": message}}}
}
func writeMCPStdioMessage(output io.Writer, payload interface{}) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(encoded) > maxMCPResponseBytes {
		return fmt.Errorf("stdio MCP response exceeds 4 MiB")
	}
	encoded = append(encoded, '\n')
	written, err := output.Write(encoded)
	if err != nil {
		return err
	}
	if written != len(encoded) {
		return io.ErrShortWrite
	}
	return nil
}

var oauthStateMu sync.Mutex
var oauthStateNamespacePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var oauthArgon2Slots = make(chan struct{}, 2)

const (
	argon2Memory      = 64 * 1024
	argon2Iterations  = 3
	argon2Parallelism = 2
	argon2SaltLength  = 16
	argon2KeyLength   = 32
)

func setupOAuthStateRuntime(vm *goja.Runtime, root string) {
	vm.Set("nyanOAuthRead", func(key string) string {
		value, err := oauthReadState(root, key)
		if os.IsNotExist(err) {
			return ""
		}
		if err != nil {
			panic(vm.ToValue("OAuth state read failed"))
		}
		return value
	})
	vm.Set("nyanOAuthWrite", func(key, value string) bool {
		if err := oauthWriteState(root, key, value); err != nil {
			panic(vm.ToValue("OAuth state write failed"))
		}
		return true
	})
	vm.Set("nyanOAuthDelete", func(key string) bool {
		err := oauthDeleteState(root, key)
		if err != nil && !os.IsNotExist(err) {
			panic(vm.ToValue("OAuth state delete failed"))
		}
		return true
	})
	vm.Set("nyanOAuthConsume", func(key string) string {
		value, err := oauthConsumeState(root, key)
		if os.IsNotExist(err) {
			return ""
		}
		if err != nil {
			panic(vm.ToValue("OAuth state consume failed"))
		}
		return value
	})
	vm.Set("nyanOAuthList", func(namespace string) []string {
		values, err := oauthListState(root, namespace)
		if err != nil {
			panic(vm.ToValue("OAuth state list failed"))
		}
		return values
	})
	vm.Set("nyanArgon2idHash", func(password string) string {
		value, err := argon2idHash(password)
		if err != nil {
			panic(vm.ToValue("password hashing failed"))
		}
		return value
	})
	vm.Set("nyanArgon2idVerify", argon2idVerify)
	vm.Set("nyanOAuthAdminAuthorized", oauthAdminAuthorized)
}

func oauthAdminAuthorized(authorization string) bool {
	if !strings.HasPrefix(authorization, "Basic ") {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(authorization, "Basic ")))
	if err != nil {
		return false
	}
	username, password, found := strings.Cut(string(decoded), ":")
	if !found || globalConfig.BasicAuth.Username == "" || globalConfig.BasicAuth.Password == "" {
		return false
	}
	left := sha256.Sum256([]byte(username))
	right := sha256.Sum256([]byte(globalConfig.BasicAuth.Username))
	leftPassword := sha256.Sum256([]byte(password))
	rightPassword := sha256.Sum256([]byte(globalConfig.BasicAuth.Password))
	return subtle.ConstantTimeCompare(left[:], right[:]) == 1 && subtle.ConstantTimeCompare(leftPassword[:], rightPassword[:]) == 1
}
func argon2idHash(password string) (string, error) {
	if len(password) < 1 || len(password) > 4096 {
		return "", fmt.Errorf("invalid password")
	}
	salt := make([]byte, argon2SaltLength)
	if _, err := cryptorand.Read(salt); err != nil {
		return "", err
	}
	oauthArgon2Slots <- struct{}{}
	defer func() { <-oauthArgon2Slots }()
	hash := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argon2Memory, argon2Iterations, argon2Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}
func argon2idVerify(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || len(password) > 4096 {
		return false
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil || memory != argon2Memory || iterations != argon2Iterations || parallelism != argon2Parallelism {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[4])
	expected, e2 := base64.RawStdEncoding.DecodeString(parts[5])
	if e1 != nil || e2 != nil || len(salt) != argon2SaltLength || len(expected) != argon2KeyLength {
		return false
	}
	oauthArgon2Slots <- struct{}{}
	defer func() { <-oauthArgon2Slots }()
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
func resolveOAuthStatePath(root, key string, create bool) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("OAuth state root must be absolute")
	}
	key = filepath.Clean(filepath.FromSlash(strings.TrimSpace(key)))
	if !filepath.IsLocal(key) || filepath.Ext(key) != ".json" {
		return "", fmt.Errorf("invalid OAuth state key")
	}
	if err := ensureOAuthStateDirectory(root, create); err != nil {
		return "", err
	}
	path := filepath.Join(root, key)
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		return "", fmt.Errorf("OAuth state key escapes root")
	}
	parent := filepath.Dir(path)
	if create {
		if err := os.MkdirAll(parent, 0700); err != nil {
			return "", err
		}
	}
	for current := parent; current != filepath.Dir(root) && strings.HasPrefix(current, root); current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || oauthPermissionsTooBroad(info.Mode(), 0077) {
			return "", fmt.Errorf("unsafe OAuth state directory")
		}
		if current == root {
			break
		}
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("OAuth state file is symlink")
	}
	return path, nil
}
func ensureOAuthStateDirectory(root string, create bool) error {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) && create {
		if err = os.MkdirAll(root, 0700); err != nil {
			return err
		}
		info, err = os.Lstat(root)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || oauthPermissionsTooBroad(info.Mode(), 0077) {
		return fmt.Errorf("unsafe OAuth state root")
	}
	return nil
}
func oauthPermissionsTooBroad(mode os.FileMode, mask os.FileMode) bool {
	return runtime.GOOS != "windows" && mode.Perm()&mask != 0
}
func oauthReadState(root, key string) (string, error) {
	oauthStateMu.Lock()
	defer oauthStateMu.Unlock()
	return oauthReadStateLocked(root, key)
}
func oauthReadStateLocked(root, key string) (string, error) {
	path, err := resolveOAuthStatePath(root, key, false)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || oauthPermissionsTooBroad(info.Mode(), 0077) {
		return "", fmt.Errorf("unsafe OAuth state file")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 1<<20 || !json.Valid(data) || validateNoDuplicateJSONKeys(data) != nil {
		return "", fmt.Errorf("invalid OAuth state file")
	}
	return string(data), nil
}
func oauthWriteState(root, key, value string) error {
	if len(value) > 1<<20 || !json.Valid([]byte(value)) || validateNoDuplicateJSONKeys([]byte(value)) != nil {
		return fmt.Errorf("OAuth state must be valid JSON")
	}
	oauthStateMu.Lock()
	defer oauthStateMu.Unlock()
	path, err := resolveOAuthStatePath(root, key, true)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".nyanpui-oauth-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err = temp.Chmod(0600); err == nil {
		_, err = io.WriteString(temp, value)
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tempPath, path)
	}
	return err
}
func oauthDeleteState(root, key string) error {
	oauthStateMu.Lock()
	defer oauthStateMu.Unlock()
	path, err := resolveOAuthStatePath(root, key, false)
	if err != nil {
		return err
	}
	return os.Remove(path)
}
func oauthConsumeState(root, key string) (string, error) {
	oauthStateMu.Lock()
	defer oauthStateMu.Unlock()
	value, err := oauthReadStateLocked(root, key)
	if err != nil {
		return "", err
	}
	path, err := resolveOAuthStatePath(root, key, false)
	if err != nil {
		return "", err
	}
	if err = os.Remove(path); err != nil {
		return "", err
	}
	return value, nil
}
func oauthListState(root, namespace string) ([]string, error) {
	oauthStateMu.Lock()
	defer oauthStateMu.Unlock()
	if !oauthStateNamespacePattern.MatchString(namespace) {
		return nil, fmt.Errorf("invalid namespace")
	}
	directory := filepath.Join(root, namespace)
	if _, err := resolveOAuthStatePath(root, namespace+"/placeholder.json", false); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	keys := []string{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || oauthPermissionsTooBroad(info.Mode(), 0077) || filepath.Ext(entry.Name()) != ".json" {
			return nil, fmt.Errorf("unsafe state entry")
		}
		keys = append(keys, namespace+"/"+entry.Name())
	}
	sort.Strings(keys)
	return keys, nil
}
