package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/natefinch/lumberjack"
)

func TestPublicEndpointServesFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	publicDir := filepath.Join(tempDir, "public")
	if err := os.Mkdir(publicDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "app.js"), []byte("console.log('nyan');"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "index.html"), []byte("<h1>index</h1>"), 0644); err != nil {
		t.Fatal(err)
	}

	config := APIConfig{"assets": {
		Type: apiTypePublic,
		Path: "./public",
	}}
	adjustAPIConfigPaths(config, tempDir)
	router := newRequestRegressionRouter(t, config)

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := rec.Body.String(), "console.log('nyan');"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}

	for _, target := range []string{"/assets", "/assets/", "/assets/index-missing.html"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want %d; body=%q", target, rec.Code, http.StatusNotFound, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/app.js?nyan_mode=checkOnly", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	assertParamCheckResponse(t, rec.Body.Bytes(), true, http.StatusOK)
}

func TestPublicEndpointRequiresPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := APIConfig{"assets": {Type: apiTypePublic}}
	adjustAPIConfigPaths(config, t.TempDir())
	router := newRequestRegressionRouter(t, config)

	req := httptest.NewRequest(http.MethodGet, "/assets/file.txt", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestPublicEndpointRunsParamCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	publicDir := filepath.Join(tempDir, "public")
	if err := os.Mkdir(publicDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "app.js"), []byte("console.log('private');"), 0644); err != nil {
		t.Fatal(err)
	}
	checkScript := filepath.Join(tempDir, "check.js")
	if err := os.WriteFile(checkScript, []byte(`
if (nyanAllParams.deny === "1") {
  ({ success: false, status: 401, result: { message: "denied", path: nyanAllParams.nyan_public_path } });
} else {
  ({ success: true, status: 200, result: { path: nyanAllParams.nyan_public_path } });
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	config := APIConfig{"assets": {
		Type:       apiTypePublic,
		Path:       "./public",
		ParamCheck: "./check.js",
	}}
	adjustAPIConfigPaths(config, tempDir)
	router := newRequestRegressionRouter(t, config)

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js?deny=1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	assertParamCheckResponse(t, rec.Body.Bytes(), false, http.StatusUnauthorized)

	req = httptest.NewRequest(http.MethodGet, "/assets/app.js?nyan_mode=checkOnly", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	assertParamCheckResponse(t, rec.Body.Bytes(), true, http.StatusOK)
	if rec.Body.String() == "console.log('private');" {
		t.Fatal("checkOnly returned file content")
	}
}

func TestPublicEndpointBlocksJSONStringParamCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	publicDir := filepath.Join(tempDir, "public")
	if err := os.Mkdir(publicDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "test.txt"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	checkScript := filepath.Join(tempDir, "check.js")
	if err := os.WriteFile(checkScript, []byte(`
JSON.stringify({
  success: false,
  status: 401,
  result: {}
});
`), 0644); err != nil {
		t.Fatal(err)
	}

	config := APIConfig{"public": {
		Type:       apiTypePublic,
		Path:       "./public",
		ParamCheck: "./check.js",
	}}
	adjustAPIConfigPaths(config, tempDir)
	router := newRequestRegressionRouter(t, config)

	for _, target := range []string{"/public/test.txt", "/public/test.txt?nyan_mode=checkOnly"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d; body=%q", target, rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
		assertParamCheckResponse(t, rec.Body.Bytes(), false, http.StatusUnauthorized)
		if rec.Body.String() == "test" {
			t.Fatalf("%s returned protected file content", target)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s Cache-Control = %q, want no-store", target, got)
		}
	}
}

func TestPublicEndpointRunsOutCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	publicDir := filepath.Join(tempDir, "public")
	if err := os.Mkdir(publicDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "test.txt"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	outCheckScript := filepath.Join(tempDir, "out_check.js")
	if err := os.WriteFile(outCheckScript, []byte(`
if (nyanAllParams.nyan_output_body === "test") {
  ({ success: true, status: 200, result: {} });
} else {
  ({ success: false, status: 409, result: { message: "file mismatch" } });
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	config := APIConfig{"public": {
		Type:     apiTypePublic,
		Path:     "./public",
		OutCheck: "./out_check.js",
	}}
	adjustAPIConfigPaths(config, tempDir)
	router := newRequestRegressionRouter(t, config)

	req := httptest.NewRequest(http.MethodGet, "/public/test.txt", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := rec.Body.String(), "test"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestEndpointConfigAcceptsLowercaseOutcheck(t *testing.T) {
	var config EndpointConfig
	if err := json.Unmarshal([]byte(`{
  "type": "public",
  "path": "./public",
  "paramcheck": "./javascript/check_login.js",
  "outcheck": "./javascript/outcheck.js"
}`), &config); err != nil {
		t.Fatal(err)
	}

	if got, want := config.ParamCheck, "./javascript/check_login.js"; got != want {
		t.Fatalf("ParamCheck = %q, want %q", got, want)
	}
	if got, want := config.OutCheck, "./javascript/outcheck.js"; got != want {
		t.Fatalf("OutCheck = %q, want %q", got, want)
	}
}

func TestResolveServiceFilePathsDefaultsToExecDir(t *testing.T) {
	t.Setenv("NYAN_API_PATH", "")
	t.Setenv("NYAN_CONFIG_PATH", "")
	execDir := t.TempDir()
	writeTestFile(t, filepath.Join(execDir, "api.json"), "{}")
	writeTestFile(t, filepath.Join(execDir, "config.json"), "{}")

	paths, err := resolveServiceFilePaths(execDir, nil)
	if err != nil {
		t.Fatalf("resolveServiceFilePaths() error = %v", err)
	}
	if paths.API.Path != filepath.Join(execDir, "api.json") {
		t.Fatalf("API path = %q, want %q", paths.API.Path, filepath.Join(execDir, "api.json"))
	}
	if paths.API.Source != "default" {
		t.Fatalf("API source = %q, want default", paths.API.Source)
	}
	if paths.Config.Path != filepath.Join(execDir, "config.json") {
		t.Fatalf("Config path = %q, want %q", paths.Config.Path, filepath.Join(execDir, "config.json"))
	}
	if paths.Config.Source != "default" {
		t.Fatalf("Config source = %q, want default", paths.Config.Source)
	}
}

func TestResolveServiceFilePathsCLIOverridesEnvironment(t *testing.T) {
	execDir := t.TempDir()
	envDir := t.TempDir()
	cliDir := t.TempDir()
	writeTestFile(t, filepath.Join(execDir, "api.json"), "{}")
	writeTestFile(t, filepath.Join(execDir, "config.json"), "{}")
	writeTestFile(t, filepath.Join(envDir, "api.json"), "{}")
	writeTestFile(t, filepath.Join(envDir, "config.json"), "{}")
	writeTestFile(t, filepath.Join(cliDir, "api.json"), "{}")
	writeTestFile(t, filepath.Join(cliDir, "config.json"), "{}")
	t.Setenv("NYAN_API_PATH", filepath.Join(envDir, "api.json"))
	t.Setenv("NYAN_CONFIG_PATH", filepath.Join(envDir, "config.json"))

	paths, err := resolveServiceFilePaths(execDir, []string{
		"--api", filepath.Join(cliDir, "api.json"),
		"--config", filepath.Join(cliDir, "config.json"),
	})
	if err != nil {
		t.Fatalf("resolveServiceFilePaths() error = %v", err)
	}
	if paths.API.Path != filepath.Join(cliDir, "api.json") {
		t.Fatalf("API path = %q, want CLI path", paths.API.Path)
	}
	if paths.API.Source != "--api" {
		t.Fatalf("API source = %q, want --api", paths.API.Source)
	}
	if paths.Config.Path != filepath.Join(cliDir, "config.json") {
		t.Fatalf("Config path = %q, want CLI path", paths.Config.Path)
	}
	if paths.Config.Source != "--config" {
		t.Fatalf("Config source = %q, want --config", paths.Config.Source)
	}
}

func TestResolveServiceFilePathsUsesEnvironmentBeforeDefault(t *testing.T) {
	execDir := t.TempDir()
	envDir := t.TempDir()
	writeTestFile(t, filepath.Join(execDir, "api.json"), "{}")
	writeTestFile(t, filepath.Join(execDir, "config.json"), "{}")
	writeTestFile(t, filepath.Join(envDir, "api.json"), "{}")
	writeTestFile(t, filepath.Join(envDir, "config.json"), "{}")
	t.Setenv("NYAN_API_PATH", filepath.Join(envDir, "api.json"))
	t.Setenv("NYAN_CONFIG_PATH", filepath.Join(envDir, "config.json"))

	paths, err := resolveServiceFilePaths(execDir, nil)
	if err != nil {
		t.Fatalf("resolveServiceFilePaths() error = %v", err)
	}
	if paths.API.Path != filepath.Join(envDir, "api.json") {
		t.Fatalf("API path = %q, want env path", paths.API.Path)
	}
	if paths.API.Source != "NYAN_API_PATH" {
		t.Fatalf("API source = %q, want NYAN_API_PATH", paths.API.Source)
	}
	if paths.Config.Path != filepath.Join(envDir, "config.json") {
		t.Fatalf("Config path = %q, want env path", paths.Config.Path)
	}
	if paths.Config.Source != "NYAN_CONFIG_PATH" {
		t.Fatalf("Config source = %q, want NYAN_CONFIG_PATH", paths.Config.Source)
	}
}

func TestResolveServiceFilePathsResolvesRelativeCLIPaths(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	execDir := t.TempDir()
	writeTestFile(t, filepath.Join(cwd, "api.json"), "{}")
	writeTestFile(t, filepath.Join(cwd, "config.json"), "{}")

	paths, err := resolveServiceFilePaths(execDir, []string{"--api", "./api.json", "--config", "./config.json"})
	if err != nil {
		t.Fatalf("resolveServiceFilePaths() error = %v", err)
	}
	if paths.API.Path != filepath.Join(cwd, "api.json") {
		t.Fatalf("API path = %q, want %q", paths.API.Path, filepath.Join(cwd, "api.json"))
	}
	if paths.Config.Path != filepath.Join(cwd, "config.json") {
		t.Fatalf("Config path = %q, want %q", paths.Config.Path, filepath.Join(cwd, "config.json"))
	}
}

func TestResolveServiceFilePathsReportsMissingFileWithSource(t *testing.T) {
	execDir := t.TempDir()
	configPath := filepath.Join(execDir, "config.json")
	writeTestFile(t, configPath, "{}")

	_, err := resolveServiceFilePaths(execDir, []string{"--api", "./missing-api.json", "--config", configPath})
	if err == nil {
		t.Fatal("resolveServiceFilePaths() error = nil, want missing file error")
	}
	if !strings.Contains(err.Error(), "api file not found:") || !strings.Contains(err.Error(), "(source: --api)") {
		t.Fatalf("error = %q, want missing api file with --api source", err.Error())
	}
}

func TestAdjustConfigPathsResolvesFromConfigFileDirectory(t *testing.T) {
	configBaseDir := t.TempDir()
	config := Config{
		CertFile:          "./ssl/localhost.crt",
		KeyFile:           "./ssl/localhost.key",
		JavaScriptInclude: []string{"./javascript/base.js"},
		Log: LogConfig{
			Filename: "./logs/nyanpui.log",
		},
	}

	adjustConfigPaths(configBaseDir, &config)

	if config.CertFile != filepath.Join(configBaseDir, "ssl/localhost.crt") {
		t.Fatalf("CertFile = %q, want config-relative path", config.CertFile)
	}
	if config.KeyFile != filepath.Join(configBaseDir, "ssl/localhost.key") {
		t.Fatalf("KeyFile = %q, want config-relative path", config.KeyFile)
	}
	if config.JavaScriptInclude[0] != filepath.Join(configBaseDir, "javascript/base.js") {
		t.Fatalf("JavaScriptInclude[0] = %q, want config-relative path", config.JavaScriptInclude[0])
	}
	if config.Log.Filename != filepath.Join(configBaseDir, "logs/nyanpui.log") {
		t.Fatalf("Log.Filename = %q, want config-relative path", config.Log.Filename)
	}
}

func TestLoadAPIConfigResolvesEndpointPathsFromAPIFileDirectory(t *testing.T) {
	apiBaseDir := t.TempDir()
	apiPath := filepath.Join(apiBaseDir, "api.json")
	writeTestFile(t, apiPath, `{
		"html": {
			"script": "./javascript/html.js",
			"html": "./html/index.html",
			"paramCheck": "./javascript/check.js",
			"outCheck": "./javascript/out.js"
		},
		"public": {
			"type": "public",
			"path": "./public"
		}
	}`)
	previous := currentAPISnapshot()
	setAPIConfig(nil)
	t.Cleanup(func() { publishAPISnapshot(previous) })

	if err := loadAPIConfig(apiPath, apiBaseDir); err != nil {
		t.Fatalf("loadAPIConfig() error = %v", err)
	}

	if apiConfig["html"].Script != filepath.Join(apiBaseDir, "javascript/html.js") {
		t.Fatalf("Script = %q, want api-relative path", apiConfig["html"].Script)
	}
	if apiConfig["html"].HTML != filepath.Join(apiBaseDir, "html/index.html") {
		t.Fatalf("HTML = %q, want api-relative path", apiConfig["html"].HTML)
	}
	if apiConfig["html"].ParamCheck != filepath.Join(apiBaseDir, "javascript/check.js") {
		t.Fatalf("ParamCheck = %q, want api-relative path", apiConfig["html"].ParamCheck)
	}
	if apiConfig["html"].OutCheck != filepath.Join(apiBaseDir, "javascript/out.js") {
		t.Fatalf("OutCheck = %q, want api-relative path", apiConfig["html"].OutCheck)
	}
	if apiConfig["public"].Path != filepath.Join(apiBaseDir, "public") {
		t.Fatalf("Path = %q, want api-relative path", apiConfig["public"].Path)
	}
}

func TestParseAPIHotReloadInterval(t *testing.T) {
	tests := []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{"", time.Second, false},
		{"250ms", 250 * time.Millisecond, false},
		{"1m", time.Minute, false},
		{"later", 0, true},
		{"0s", 0, true},
		{"-1s", 0, true},
	}
	for _, tt := range tests {
		got, err := parseAPIHotReloadInterval(tt.value)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseAPIHotReloadInterval(%q) error = nil", tt.value)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Fatalf("parseAPIHotReloadInterval(%q) = %s, %v; want %s", tt.value, got, err, tt.want)
		}
	}
}

func TestConfigAPIHotReloadDefaultsAndOverrides(t *testing.T) {
	tests := []struct {
		data string
		want APIHotReloadConfig
	}{
		{`{}`, APIHotReloadConfig{Enabled: true, Interval: "1s"}},
		{`{"APIHotReload":{"Enabled":false}}`, APIHotReloadConfig{Enabled: false, Interval: "1s"}},
		{`{"APIHotReload":{"Enabled":true,"Interval":"2s"}}`, APIHotReloadConfig{Enabled: true, Interval: "2s"}},
	}
	for _, tt := range tests {
		var got Config
		applyConfigDefaults(&got)
		if err := json.Unmarshal([]byte(tt.data), &got); err != nil {
			t.Fatal(err)
		}
		if got.APIHotReload != tt.want {
			t.Fatalf("APIHotReload = %#v, want %#v", got.APIHotReload, tt.want)
		}
	}
}

func TestDecodeAPIConfigResolvesAllRelativePaths(t *testing.T) {
	base := t.TempDir()
	config, err := decodeAPIConfig([]byte(`{"x":{"script":"s.js","html":"h.html","path":"pub","paramCheck":"p.js","outCheck":"o.js"}}`), base)
	if err != nil {
		t.Fatal(err)
	}
	got := config["x"]
	for label, value := range map[string]string{"script": got.Script, "html": got.HTML, "path": got.Path, "paramCheck": got.ParamCheck, "outCheck": got.OutCheck} {
		if !filepath.IsAbs(value) {
			t.Fatalf("%s path is not absolute: %q", label, value)
		}
	}
}

func TestReloadAPIConfigGraphKeepsLastGoodDefinition(t *testing.T) {
	apiDir := t.TempDir()
	apiPath := filepath.Join(apiDir, "api.json")
	writeTestFile(t, apiPath, `{"old":{"description":"active"}}`)
	initial, err := readAPIConfigGraph(apiPath, apiDir)
	if err != nil {
		t.Fatal(err)
	}
	oldSnapshot, oldManager := currentAPISnapshot(), backgroundRuntimes
	publishAPISnapshot(initial.Snapshot)
	backgroundRuntimes = nil
	t.Cleanup(func() { publishAPISnapshot(oldSnapshot); backgroundRuntimes = oldManager })

	writeTestFile(t, apiPath, `{"new":{"description":"updated"}}`)
	states, reloaded, err := reloadAPIConfigGraphIfChanged(apiPath, apiDir, initial.Snapshot.FileStates)
	if err != nil || !reloaded {
		t.Fatalf("reload=%t err=%v", reloaded, err)
	}
	if _, ok := currentAPIConfig()["old"]; ok {
		t.Fatal("old API remains")
	}
	lastGood := currentAPISnapshot()

	writeTestFile(t, apiPath, `{"broken":`)
	invalidStates, reloaded, err := reloadAPIConfigGraphIfChanged(apiPath, apiDir, states)
	if err == nil || reloaded {
		t.Fatalf("invalid reload=%t err=%v", reloaded, err)
	}
	if _, ok := currentAPIConfig()["new"]; !ok {
		t.Fatal("last-known-good config was lost")
	}
	if currentAPISnapshot() != lastGood {
		t.Fatal("invalid configuration replaced the last-known-good snapshot")
	}
	secondStates, reloaded, err := reloadAPIConfigGraphIfChanged(apiPath, apiDir, invalidStates)
	if err != nil || reloaded || !maps.Equal(secondStates, invalidStates) {
		t.Fatalf("unchanged invalid content reprocessed: reload=%t err=%v", reloaded, err)
	}

	writeTestFile(t, apiPath, `{"fixed":{"description":"ok"}}`)
	_, reloaded, err = reloadAPIConfigGraphIfChanged(apiPath, apiDir, invalidStates)
	if err != nil || !reloaded {
		t.Fatalf("fixed reload=%t err=%v", reloaded, err)
	}
	if _, ok := currentAPIConfig()["fixed"]; !ok {
		t.Fatal("fixed definition was not published")
	}
}

func TestReloadAPIConfigGraphRejectsInvalidBackgroundCandidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.json")
	writeTestFile(t, path, `{"current":{}}`)
	initial, err := readAPIConfigGraph(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	oldSnapshot, oldManager := currentAPISnapshot(), backgroundRuntimes
	publishAPISnapshot(initial.Snapshot)
	backgroundRuntimes = nil
	t.Cleanup(func() { publishAPISnapshot(oldSnapshot); backgroundRuntimes = oldManager })
	writeTestFile(t, path, `{"job":{"type":"schedule","trigger":{"type":"cron","value":"* * * * *"}}}`)
	_, reloaded, err := reloadAPIConfigGraphIfChanged(path, dir, initial.Snapshot.FileStates)
	if err == nil || reloaded {
		t.Fatalf("reload=%t err=%v", reloaded, err)
	}
	if _, ok := currentAPIConfig()["current"]; !ok {
		t.Fatal("current config changed")
	}
	if currentAPISnapshot() != initial.Snapshot {
		t.Fatal("invalid background configuration replaced the active snapshot")
	}
}

func TestDynamicDispatcherReflectsAddDeleteAndSlashName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	script := filepath.Join(dir, "hot.js")
	writeTestFile(t, script, `"hot";`)
	previous := currentAPISnapshot()
	setAPIConfig(APIConfig{"nested/hot": {Script: script}})
	t.Cleanup(func() { publishAPISnapshot(previous) })
	router := gin.New()
	router.NoRoute(func(c *gin.Context) {
		if !dispatchDynamicEndpoint(c) {
			c.Status(http.StatusNotFound)
		}
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nested/hot", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "hot" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	setAPIConfig(APIConfig{})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nested/hot", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("deleted status=%d", rec.Code)
	}
}

func TestDynamicDispatcherUsesLongestPublicPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	shortDir, longDir := filepath.Join(dir, "short"), filepath.Join(dir, "long")
	if err := os.MkdirAll(shortDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(longDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(longDir, "file.txt"), "long")
	previous := currentAPISnapshot()
	setAPIConfig(APIConfig{"assets": {Type: apiTypePublic, Path: shortDir}, "assets/deep": {Type: apiTypePublic, Path: longDir}})
	t.Cleanup(func() { publishAPISnapshot(previous) })
	router := gin.New()
	router.NoRoute(func(c *gin.Context) {
		if !dispatchDynamicEndpoint(c) {
			c.Status(http.StatusNotFound)
		}
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/deep/file.txt", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "long" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestAPIConfigConcurrentReadAndReplace(t *testing.T) {
	previous := currentAPISnapshot()
	setAPIConfig(APIConfig{"api": {Description: "initial"}})
	t.Cleanup(func() { publishAPISnapshot(previous) })
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = currentAPIConfig()["api"]
			}
		}()
	}
	for i := 0; i < 1000; i++ {
		setAPIConfig(APIConfig{"api": {Description: fmt.Sprintf("updated-%d", i)}})
	}
	wg.Wait()
}

func TestCronScheduleNext(t *testing.T) {
	for _, tc := range []struct{ name, expression, after, want string }{
		{"minute_boundary", "* * * * *", "2026-09-20T10:07:30Z", "2026-09-20T10:08:00Z"},
		{"range_and_step", "*/15 9-10 * * 1-5", "2026-09-18T10:59:00Z", "2026-09-21T09:00:00Z"},
		{"sunday_alias", "0 0 * * 7", "2026-09-19T23:59:00Z", "2026-09-20T00:00:00Z"},
		{"day_or_weekday_uses_weekday", "0 9 1 * 1", "2026-09-01T09:00:00Z", "2026-09-07T09:00:00Z"},
		{"day_or_weekday_uses_day", "0 9 1 * 1", "2026-08-31T09:00:00Z", "2026-09-01T09:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schedule, err := parseCronSchedule(tc.expression)
			if err != nil {
				t.Fatal(err)
			}
			after, err := time.Parse(time.RFC3339, tc.after)
			if err != nil {
				t.Fatal(err)
			}
			if got := schedule.next(after).Format(time.RFC3339); got != tc.want {
				t.Fatalf("next occurrence of %q after %s = %s, want %s", tc.expression, tc.after, got, tc.want)
			}
		})
	}
	for _, invalid := range []string{"* * * *", "60 * * * *", "*/0 * * * *", "0 9 * * 5-1"} {
		t.Run("invalid_"+invalid, func(t *testing.T) {
			if _, err := parseCronSchedule(invalid); err == nil {
				t.Fatalf("invalid cron expression accepted: %q", invalid)
			}
		})
	}
}

func TestBackgroundRuntimeManagerUpdatesAndStopsSchedule(t *testing.T) {
	manager := newBackgroundRuntimeManager()
	cleanupBackgroundRuntimeManager(t, manager)
	firstSchedule, err := parseCronSchedule("0 0 1 1 *")
	if err != nil {
		t.Fatal(err)
	}
	first := scheduleJobConfig{name: "job", scriptPath: "/tmp/job-v1.js", trigger: TriggerConfig{Type: "cron", Value: "0 0 1 1 *"}, schedule: firstSchedule}
	manager.reconcile(map[string]scheduleJobConfig{"job": first}, nil)
	manager.mu.Lock()
	runtime := manager.schedules["job"]
	manager.mu.Unlock()
	secondSchedule, err := parseCronSchedule("0 0 2 1 *")
	if err != nil {
		t.Fatal(err)
	}
	second := scheduleJobConfig{name: "job", scriptPath: "/tmp/job-v2.js", trigger: TriggerConfig{Type: "cron", Value: "0 0 2 1 *"}, schedule: secondSchedule}
	manager.reconcile(map[string]scheduleJobConfig{"job": second}, nil)
	manager.mu.Lock()
	updated := manager.schedules["job"]
	manager.mu.Unlock()
	if updated != runtime {
		t.Fatal("schedule update created a second runtime")
	}
	if got, active := runtime.current(false); !active || got.scriptPath != second.scriptPath {
		t.Fatalf("config=%#v active=%t", got, active)
	}
	manager.reconcile(nil, nil)
	waitForRuntimeSignal(t, runtime.done, "schedule stop")
}

func TestBackgroundRuntimeManagerReconnectsOnlyForURLChange(t *testing.T) {
	firstURL, firstConnected, firstDisconnected := newHotReloadWebSocketServer(t)
	secondURL, secondConnected, secondDisconnected := newHotReloadWebSocketServer(t)
	manager := newBackgroundRuntimeManager()
	cleanupBackgroundRuntimeManager(t, manager)
	first := wsClientConfig{name: "client", scriptPath: "/tmp/v1.js", connectURL: firstURL}
	manager.reconcile(nil, map[string]wsClientConfig{"client": first})
	waitForRuntimeSignal(t, firstConnected, "first connect")
	manager.mu.Lock()
	runtime := manager.wsClients["client"]
	manager.mu.Unlock()
	soft := first
	soft.scriptPath = "/tmp/v2.js"
	soft.description = "updated"
	manager.reconcile(nil, map[string]wsClientConfig{"client": soft})
	select {
	case <-firstDisconnected:
		t.Fatal("soft update disconnected")
	case <-time.After(100 * time.Millisecond):
	}
	changed := soft
	changed.connectURL = secondURL
	manager.reconcile(nil, map[string]wsClientConfig{"client": changed})
	waitForRuntimeSignal(t, firstDisconnected, "old disconnect")
	waitForRuntimeSignal(t, secondConnected, "second connect")
	manager.reconcile(nil, nil)
	waitForRuntimeSignal(t, secondDisconnected, "second disconnect")
	waitForRuntimeSignal(t, runtime.done, "ws client stop")
}

func TestWSClientBackoffResetsAfterSuccessfulConnection(t *testing.T) {
	type attempt struct {
		at   time.Time
		conn *websocket.Conn
	}
	attempts := make(chan attempt, 8)
	var requestMu sync.Mutex
	requestCount := 0
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("local listener unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestMu.Lock()
		requestCount++
		count := requestCount
		requestMu.Unlock()
		if count <= 2 {
			attempts <- attempt{at: started}
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		attempts <- attempt{at: started, conn: conn}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	runtime := newWSClientRuntime(wsClientConfig{name: "backoff", connectURL: "ws" + server.URL[len("http"):]})
	go runtime.run()
	t.Cleanup(func() {
		runtime.update(nil)
		waitForRuntimeSignal(t, runtime.done, "ws client stop")
	})
	waitAttempt := func(timeout time.Duration) attempt {
		t.Helper()
		select {
		case result := <-attempts:
			return result
		case <-time.After(timeout):
			t.Fatalf("no connection attempt within %s", timeout)
			return attempt{}
		}
	}
	previous := waitAttempt(3 * time.Second)
	for _, minimum := range []time.Duration{time.Second, 2 * time.Second} {
		next := waitAttempt(5 * time.Second)
		if elapsed := next.at.Sub(previous.at); elapsed < minimum-100*time.Millisecond {
			t.Fatalf("retry after %s, want at least %s", elapsed, minimum)
		}
		previous = next
	}
	// After two failed dials the old implementation waits four seconds, even
	// though the third dial succeeded. Every later successful session must
	// also reset the delay; normal disconnects must not accumulate backoff.
	for i := 0; i < 3; i++ {
		if previous.conn == nil {
			t.Fatal("expected a successful WebSocket connection")
		}
		disconnected := time.Now()
		if err := previous.conn.Close(); err != nil {
			t.Fatal(err)
		}
		previous = waitAttempt(3 * time.Second)
		if elapsed := previous.at.Sub(disconnected); elapsed < 900*time.Millisecond {
			t.Fatalf("reconnected after %s, want the one-second delay", elapsed)
		}
	}
}

func cleanupBackgroundRuntimeManager(t *testing.T, manager *backgroundRuntimeManager) {
	t.Helper()
	t.Cleanup(func() {
		manager.mu.Lock()
		done := make([]<-chan struct{}, 0, len(manager.schedules)+len(manager.wsClients))
		for _, job := range manager.schedules {
			done = append(done, job.done)
		}
		for _, client := range manager.wsClients {
			done = append(done, client.done)
		}
		manager.mu.Unlock()
		manager.reconcile(nil, nil)
		for _, stopped := range done {
			select {
			case <-stopped:
			case <-time.After(3 * time.Second):
				t.Error("background runtime did not stop during cleanup")
			}
		}
	})
}

func newHotReloadWebSocketServer(t *testing.T) (string, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	connected, disconnected := make(chan struct{}, 1), make(chan struct{}, 1)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("local listener unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		select {
		case connected <- struct{}{}:
		default:
		}
		defer func() {
			_ = conn.Close()
			select {
			case disconnected <- struct{}{}:
			default:
			}
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return "ws" + server.URL[len("http"):], connected, disconnected
}

func waitForRuntimeSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestPublicEndpointBlocksLowercaseOutcheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	publicDir := filepath.Join(tempDir, "public")
	if err := os.Mkdir(publicDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "test.txt"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	outCheckScript := filepath.Join(tempDir, "outcheck.js")
	if err := os.WriteFile(outCheckScript, []byte(`
JSON.stringify({
  success: false,
  status: 401,
  result: { outcheck: "401 check" }
});
`), 0644); err != nil {
		t.Fatal(err)
	}

	var config EndpointConfig
	if err := json.Unmarshal([]byte(`{
  "type": "public",
  "path": "./public",
  "outcheck": "./outcheck.js"
}`), &config); err != nil {
		t.Fatal(err)
	}

	apiConfig := APIConfig{"public": config}
	adjustAPIConfigPaths(apiConfig, tempDir)
	router := newRequestRegressionRouter(t, apiConfig)

	req := httptest.NewRequest(http.MethodGet, "/public/test.txt", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	assertParamCheckResponse(t, rec.Body.Bytes(), false, http.StatusUnauthorized)
	if rec.Body.String() == "test" {
		t.Fatal("outcheck failure returned public file content")
	}
}

func TestHandleAPIRequestRunsParamCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	mainScript := filepath.Join(tempDir, "main.js")
	if err := os.WriteFile(mainScript, []byte(`"main ok";`), 0644); err != nil {
		t.Fatal(err)
	}
	checkScript := filepath.Join(tempDir, "check.js")
	if err := os.WriteFile(checkScript, []byte(`
if (nyanAllParams.deny === "1") {
  ({ success: false, status: 403, result: { message: "forbidden" } });
} else {
  ({ success: true, status: 200, result: { api: nyanAllParams.api } });
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	config := EndpointConfig{
		Script:     mainScript,
		ParamCheck: checkScript,
	}
	router := newRequestRegressionRouter(t, APIConfig{"secure": config})

	req := httptest.NewRequest(http.MethodGet, "/secure?deny=1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	assertParamCheckResponse(t, rec.Body.Bytes(), false, http.StatusForbidden)

	req = httptest.NewRequest(http.MethodGet, "/secure?nyan_mode=checkOnly", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	assertParamCheckResponse(t, rec.Body.Bytes(), true, http.StatusOK)
	if rec.Body.String() == "main ok" {
		t.Fatal("checkOnly ran main script")
	}

	req = httptest.NewRequest(http.MethodGet, "/secure", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := rec.Body.String(), "main ok"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestHandleAPIRequestRunsOutCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	mainScript := filepath.Join(tempDir, "main.js")
	if err := os.WriteFile(mainScript, []byte(`"main ok";`), 0644); err != nil {
		t.Fatal(err)
	}
	outCheckScript := filepath.Join(tempDir, "out_check.js")
	if err := os.WriteFile(outCheckScript, []byte(`
if (nyanAllParams.nyan_output.body === "main ok") {
  ({ success: true, status: 200, result: {} });
} else {
  ({ success: false, status: 409, result: { message: "output mismatch", body: nyanAllParams.nyan_output.body } });
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	router := newRequestRegressionRouter(t, APIConfig{"checked": {
		Script:   mainScript,
		OutCheck: outCheckScript,
	}})

	req := httptest.NewRequest(http.MethodGet, "/checked", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := rec.Body.String(), "main ok"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestHandleAPIRequestOutCheckBlocksMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	mainScript := filepath.Join(tempDir, "main.js")
	if err := os.WriteFile(mainScript, []byte(`"unexpected";`), 0644); err != nil {
		t.Fatal(err)
	}
	outCheckScript := filepath.Join(tempDir, "out_check.js")
	if err := os.WriteFile(outCheckScript, []byte(`
if (nyanAllParams.nyan_output_body === "main ok") {
  ({ success: true, status: 200, result: {} });
} else {
  ({ success: false, status: 409, result: { message: "output mismatch", body: nyanAllParams.nyan_output_body } });
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	router := newRequestRegressionRouter(t, APIConfig{"checked": {
		Script:   mainScript,
		OutCheck: outCheckScript,
	}})

	req := httptest.NewRequest(http.MethodGet, "/checked", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusConflict, rec.Body.String())
	}
	assertParamCheckResponse(t, rec.Body.Bytes(), false, http.StatusConflict)
	if rec.Body.String() == "unexpected" {
		t.Fatal("outCheck failure returned original output")
	}
}

func TestHandleAPIRequestPushConditions(t *testing.T) {
	const allow = `({success:true,status:200,result:null});`
	const deny = `({success:false,status:403,result:"denied"});`
	const jsonResponse = `({status:200,contentType:"application/json",headers:{"X-Test":"ok"},body:{ok:true}});`
	for _, tc := range []struct {
		name        string
		script      string
		paramCheck  string
		outCheck    string
		query       string
		htmlOnly    bool
		missingHTML bool
		noPush      bool
		wantStatus  int
		wantBody    string
		wantPush    bool
	}{
		{name: "string", script: `"saved";`, wantStatus: 200, wantBody: "saved", wantPush: true},
		{name: "json_object", script: jsonResponse, wantStatus: 200, wantBody: `{"ok":true}`, wantPush: true},
		{name: "created", script: `({status:201,body:"created"});`, wantStatus: 201, wantBody: "created", wantPush: true},
		{name: "no_content", script: `({status:204});`, wantStatus: 204, wantPush: true},
		{name: "redirect", script: `({status:302,headers:{Location:"/done"}});`, wantStatus: 302, wantPush: true},
		{name: "null", script: `null;`, wantStatus: 200, wantPush: true},
		{name: "undefined", script: `undefined;`, wantStatus: 200, wantPush: true},
		{name: "html_only", htmlOnly: true, wantStatus: 200, wantBody: "<p>saved</p>", wantPush: true},
		{name: "bad_request", script: `({status:400,body:"failed"});`, wantStatus: 400},
		{name: "server_error", script: `({status:500,body:"failed"});`, wantStatus: 500},
		{name: "invalid_status", script: `({status:700,body:"failed"});`, wantStatus: 500},
		{name: "param_rejected", script: jsonResponse, paramCheck: deny, wantStatus: 403},
		{name: "out_rejected", script: jsonResponse, outCheck: deny, wantStatus: 403},
		{name: "html_out_rejected", htmlOnly: true, outCheck: deny, wantStatus: 403},
		{name: "param_non_200", script: jsonResponse, paramCheck: `({success:true,status:201,result:null});`, wantStatus: 201},
		{name: "out_false_with_200", script: jsonResponse, outCheck: `({success:false,status:200,result:null});`, wantStatus: 200},
		{name: "param_exception", script: jsonResponse, paramCheck: `throw new Error("failed");`, wantStatus: 500},
		{name: "out_exception", script: jsonResponse, outCheck: `throw new Error("failed");`, wantStatus: 500},
		{name: "check_only", script: jsonResponse, query: "&nyan_mode=checkOnly", wantStatus: 200},
		{name: "script_exception", script: `throw new Error("failed");`, wantStatus: 500},
		{name: "invalid_body", script: `({body:{encoding:"base64",data:"!"}});`, wantStatus: 500},
		{name: "missing_html", htmlOnly: true, missingHTML: true, wantStatus: 500},
		{name: "without_push", script: jsonResponse, noPush: true, wantStatus: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := EndpointConfig{Push: "updates"}
			if tc.htmlOnly {
				config.HTML = writeFixtureFile(t, "<p>saved</p>")
				if tc.missingHTML {
					config.HTML += ".missing"
				}
			} else {
				config.Script = writeFixtureFile(t, tc.script)
			}
			if !tc.missingHTML {
				paramCheck, outCheck := allow, allow
				if tc.paramCheck != "" {
					paramCheck = tc.paramCheck
				}
				if tc.outCheck != "" {
					outCheck = tc.outCheck
				}
				config.ParamCheck = writeFixtureFile(t, paramCheck)
				config.OutCheck = writeFixtureFile(t, outCheck)
			}
			if tc.noPush {
				config.Push = ""
			}
			marker := filepath.Join(t.TempDir(), "push-ran")
			push := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q, (nyanGetFile(%q) || "") + nyanAllParams.id + ":" + nyanGetCookie("session")); "pushed";`, marker, marker))
			router := newRequestRegressionRouter(t, APIConfig{"save": config, "updates": {Script: push}})
			req := httptest.NewRequest(http.MethodGet, "/save?id=item"+tc.query, nil)
			req.AddCookie(&http.Cookie{Name: "session", Value: "owner"})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantPush && rec.Body.String() != tc.wantBody {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tc.wantBody)
			}
			if tc.name == "json_object" && (rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("X-Test") != "ok") {
				t.Fatalf("response headers changed: %v", rec.Header())
			}
			if tc.name == "redirect" && rec.Header().Get("Location") != "/done" {
				t.Fatalf("redirect location = %q, want /done", rec.Header().Get("Location"))
			}
			body, err := os.ReadFile(marker)
			if tc.wantPush {
				if err != nil || string(body) != "item:owner" {
					t.Fatalf("Push must run once with request parameters and cookies: body=%q, error=%v", body, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("Push must not run: body=%q, error=%v", body, err)
			}
		})
	}
}

func TestHTTPStructuredResponseSendsWebSocketPush(t *testing.T) {
	script := writeFixtureFile(t, `({status:200,contentType:"application/json",body:{ok:true}});`)
	push := writeFixtureFile(t, `JSON.stringify({updated:nyanAllParams.id});`)
	router := newRequestRegressionRouter(t, APIConfig{
		"save": {Script: script, Push: "updates"}, "updates": {Script: push},
	})
	server := httptest.NewServer(router)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/updates", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	// An echo round trip ensures the subscriber is registered before the HTTP request.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/save?id=item", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("unexpected HTTP response: status=%d, body=%s", rec.Code, rec.Body.String())
	}
	messageType, body, err := conn.ReadMessage()
	if err != nil || messageType != websocket.TextMessage || string(body) != `{"updated":"item"}` {
		t.Fatalf("unexpected Push: type=%d, body=%q, error=%v", messageType, body, err)
	}
}

func TestHandleAPIRequestOutCheckSeesStructuredResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	mainScript := filepath.Join(tempDir, "main.js")
	if err := os.WriteFile(mainScript, []byte(`({
  status: 201,
  contentType: "application/json; charset=utf-8",
  headers: { "X-Test": "ok" },
  body: { value: "created" }
});`), 0644); err != nil {
		t.Fatal(err)
	}
	outCheckScript := filepath.Join(tempDir, "out_check.js")
	if err := os.WriteFile(outCheckScript, []byte(`
if (nyanAllParams.nyan_output.status === 201 && nyanAllParams.nyan_output.body.indexOf("created") >= 0) {
  ({ success: true, status: 200, result: {} });
} else {
  ({ success: false, status: 500, result: nyanAllParams.nyan_output });
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	router := newRequestRegressionRouter(t, APIConfig{"checked": {
		Script:   mainScript,
		OutCheck: outCheckScript,
	}})

	req := httptest.NewRequest(http.MethodGet, "/checked", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if got, want := rec.Header().Get("X-Test"), "ok"; got != want {
		t.Fatalf("X-Test = %q, want %q", got, want)
	}
	if got, want := rec.Body.String(), `{"value":"created"}`; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func assertParamCheckResponse(t *testing.T, body []byte, success bool, status int) {
	t.Helper()

	var response ParamCheckResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("failed to decode paramCheck response %q: %v", string(body), err)
	}
	if response.Success != success {
		t.Fatalf("success = %v, want %v; body=%q", response.Success, success, string(body))
	}
	if response.Status != status {
		t.Fatalf("status = %d, want %d; body=%q", response.Status, status, string(body))
	}
}

func TestReadAPIConfigGraphExpandsNestedIncludesAndResolvesPaths(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "api.json")
	child := filepath.Join(dir, "sub", "api.json")
	grandchild := filepath.Join(dir, "sub", "admin", "api.json")
	writeTestFile(t, root, `{"root":{"script":"./root.js"},"sub":{"type":"include","path":"./sub/api.json"}}`)
	writeTestFile(t, child, `{"item":{"script":"./item.js","html":"./item.html"},"admin":{"type":"include","path":"./admin/api.json"}}`)
	writeTestFile(t, grandchild, `{"user":{"script":"./user.js","paramCheck":"./check.js","outCheck":"./out.js"}}`)

	loaded, err := readAPIConfigGraph(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	config := loaded.Snapshot.Config
	if len(config) != 3 {
		t.Fatalf("api count = %d, want 3: %#v", len(config), config)
	}
	if got := config["sub/item"].Script; got != filepath.Join(dir, "sub", "item.js") {
		t.Fatalf("sub/item script = %q", got)
	}
	if got := config["sub/item"].HTML; got != filepath.Join(dir, "sub", "item.html") {
		t.Fatalf("sub/item html = %q", got)
	}
	if got := config["sub/admin/user"].ParamCheck; got != filepath.Join(dir, "sub", "admin", "check.js") {
		t.Fatalf("nested paramCheck = %q", got)
	}
	if _, exists := config["sub"]; exists {
		t.Fatal("include definition was published as an API")
	}
	if len(loaded.Snapshot.FileStates) != 3 {
		t.Fatalf("watched files = %d, want 3", len(loaded.Snapshot.FileStates))
	}
}

func TestReadAPIConfigGraphRejectsInvalidIncludes(t *testing.T) {
	t.Run("cycle", func(t *testing.T) {
		dir := t.TempDir()
		root := filepath.Join(dir, "api.json")
		child := filepath.Join(dir, "child.json")
		writeTestFile(t, root, `{"child":{"type":"include","path":"./child.json"}}`)
		writeTestFile(t, child, `{"root":{"type":"include","path":"./api.json"}}`)
		if _, err := readAPIConfigGraph(root, dir); err == nil || !strings.Contains(err.Error(), "cycle") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("duplicate key", func(t *testing.T) {
		dir := t.TempDir()
		root := filepath.Join(dir, "api.json")
		writeTestFile(t, root, `{"x":{"script":"a"},"x":{"script":"b"}}`)
		if _, err := readAPIConfigGraph(root, dir); err == nil || !strings.Contains(err.Error(), "duplicate key") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("mount conflict", func(t *testing.T) {
		dir := t.TempDir()
		root := filepath.Join(dir, "api.json")
		writeTestFile(t, root, `{"sub":{"type":"include","path":"child.json"},"sub/existing":{"script":"x.js"}}`)
		writeTestFile(t, filepath.Join(dir, "child.json"), `{}`)
		if _, err := readAPIConfigGraph(root, dir); err == nil || !strings.Contains(err.Error(), "conflicts") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestReloadAPIConfigGraphTracksGrandchildAndRecoversMissingInclude(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "api.json")
	child := filepath.Join(dir, "child.json")
	grandchild := filepath.Join(dir, "grandchild.json")
	writeTestFile(t, root, `{"sub":{"type":"include","path":"child.json"}}`)
	writeTestFile(t, child, `{"nested":{"type":"include","path":"grandchild.json"}}`)
	writeTestFile(t, grandchild, `{"one":{"script":"one.js"}}`)
	loaded, err := readAPIConfigGraph(root, dir)
	if err != nil {
		t.Fatal(err)
	}
	old := currentAPISnapshot()
	publishAPISnapshot(loaded.Snapshot)
	t.Cleanup(func() { publishAPISnapshot(old) })

	writeTestFile(t, grandchild, `{"two":{"script":"two.js"}}`)
	states, reloaded, err := reloadAPIConfigGraphIfChanged(root, dir, loaded.Snapshot.FileStates)
	if err != nil || !reloaded {
		t.Fatalf("reloaded=%v err=%v", reloaded, err)
	}
	if _, ok := currentAPIConfig()["sub/nested/two"]; !ok {
		t.Fatal("grandchild change was not published")
	}

	writeTestFile(t, child, `{"missing":{"type":"include","path":"missing.json"}}`)
	states, reloaded, err = reloadAPIConfigGraphIfChanged(root, dir, states)
	if err == nil || reloaded {
		t.Fatalf("reloaded=%v err=%v", reloaded, err)
	}
	missing := filepath.Join(dir, "missing.json")
	if _, watched := states[missing]; !watched {
		t.Fatalf("missing candidate is not watched: %#v", states)
	}
	writeTestFile(t, missing, `{"ready":{"script":"ready.js"}}`)
	_, reloaded, err = reloadAPIConfigGraphIfChanged(root, dir, states)
	if err != nil || !reloaded {
		t.Fatalf("recovery reloaded=%v err=%v", reloaded, err)
	}
	if _, ok := currentAPIConfig()["sub/missing/ready"]; !ok {
		t.Fatal("created include was not published")
	}
}

func TestHandleNyanDetailPublishesStaticSchemas(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	paramCheck := filepath.Join(dir, "param.js")
	outCheck := filepath.Join(dir, "out.js")
	writeTestFile(t, paramCheck, `const nyanInputSchema = {type:"object",properties:{id:{type:"integer"}},required:["id"]};`)
	writeTestFile(t, outCheck, `const nyanOutputSchema = {type:"object",properties:{status:{const:200}}};`)
	old := currentAPISnapshot()
	setAPIConfig(APIConfig{"sub/get": {ParamCheck: paramCheck, OutCheck: outCheck, Description: "nested"}, "assets": {Type: apiTypePublic}})
	t.Cleanup(func() { publishAPISnapshot(old) })
	router := gin.New()
	router.GET("/nyan", handleNyan)
	router.GET("/nyan/*apiName", handleNyanDetail)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/nyan/sub/get", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	sources := response["schemaSource"].(map[string]interface{})
	if sources["input"] != schemaSourceParamCheck || sources["output"] != schemaSourceOutCheck {
		t.Fatalf("sources=%#v", sources)
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/nyan/", nil))
	if strings.Contains(list.Body.String(), "assets") {
		t.Fatalf("public API leaked into list: %s", list.Body.String())
	}
}

func TestNyanCallMeKeepsCapturedSnapshotGeneration(t *testing.T) {
	dir := t.TempDir()
	caller := filepath.Join(dir, "caller.js")
	oldTarget := filepath.Join(dir, "old.js")
	newTarget := filepath.Join(dir, "new.js")
	writeTestFile(t, caller, `JSON.stringify(nyanCallMe({api:"sub/target"}));`)
	writeTestFile(t, oldTarget, `({status:200,generation:"old"});`)
	writeTestFile(t, newTarget, `({status:200,generation:"new"});`)
	captured := &APIConfigSnapshot{Config: APIConfig{"sub/caller": {Script: caller}, "sub/target": {Script: oldTarget}}}
	latest := &APIConfigSnapshot{Config: APIConfig{"sub/caller": {Script: caller}, "sub/target": {Script: newTarget}}}
	old := currentAPISnapshot()
	publishAPISnapshot(latest)
	t.Cleanup(func() { publishAPISnapshot(old) })

	result, err := runJavaScriptWithSnapshot(captured, caller, "", map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"generation":"old"`) {
		t.Fatalf("result=%q, want captured generation", result)
	}
}

func TestFileHelpersUseRootAPIPathAcrossNestedIncludes(t *testing.T) {
	rootDir := t.TempDir()
	childDir := filepath.Join(rootDir, "child")
	grandchildDir := filepath.Join(childDir, "grandchild")
	workingDir := t.TempDir()
	t.Chdir(workingDir)

	rootPath := filepath.Join(rootDir, "root-api.json")
	writeTestFile(t, rootPath, `{"files":{"script":"./scripts/files.js","html":"./page.html"},"child":{"type":"include","path":"./child/api.json"}}`)
	writeTestFile(t, filepath.Join(childDir, "api.json"), `{"files":{"script":"./scripts/files.js","html":"./page.html"},"grandchild":{"type":"include","path":"./grandchild/api.json"}}`)
	writeTestFile(t, filepath.Join(grandchildDir, "api.json"), `{"files":{"script":"./scripts/files.js","html":"./page.html"},"assets":{"type":"public","path":"./assets"}}`)
	const script = `
const before = nyanGetFile("./data/shared.txt");
const saved = nyanSaveFile("./data/shared.txt", "updated root");
const after = nyanGetFile("./data/shared.txt");
const b64 = nyanReadFileB64("./data/binary.bin");
const deleted = nyanDeleteFile("./data/delete.txt");
({status:200,contentType:"application/json",body:{
  before:before, saved:saved, after:after, b64:b64, deleted:deleted,
  missingAfterDelete:nyanGetFile("./data/delete.txt") === null, html:nyanHtmlCode
}});`
	for _, entry := range []struct{ dir, label string }{
		{rootDir, "root"}, {childDir, "child"}, {grandchildDir, "grandchild"}, {workingDir, "cwd"},
	} {
		writeTestFile(t, filepath.Join(entry.dir, "scripts", "files.js"), script)
		writeTestFile(t, filepath.Join(entry.dir, "page.html"), entry.label+" html")
		writeTestFile(t, filepath.Join(entry.dir, "data", "shared.txt"), entry.label)
		writeTestFile(t, filepath.Join(entry.dir, "data", "delete.txt"), entry.label)
		writeTestFile(t, filepath.Join(entry.dir, "data", "binary.bin"), entry.label)
	}
	writeTestFile(t, filepath.Join(rootDir, "data", "binary.bin"), "\x00\x01\xff")
	writeTestFile(t, filepath.Join(grandchildDir, "assets", "file.txt"), "grandchild public file")

	loaded, err := readAPIConfigGraph(rootPath, rootDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Snapshot.RootPath != rootPath {
		t.Fatalf("root path = %q, want %q", loaded.Snapshot.RootPath, rootPath)
	}
	router := newRequestRegressionRouter(t, loaded.Snapshot.Config)
	publishAPISnapshot(loaded.Snapshot)
	for _, tc := range []struct{ api, html string }{
		{"files", "root html"}, {"child/files", "child html"}, {"child/grandchild/files", "grandchild html"},
	} {
		t.Run(tc.api, func(t *testing.T) {
			writeTestFile(t, filepath.Join(rootDir, "data", "shared.txt"), "root")
			writeTestFile(t, filepath.Join(rootDir, "data", "delete.txt"), "root")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/"+tc.api, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var body struct {
				Before, After, B64, HTML           string
				Saved, Deleted, MissingAfterDelete bool
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Before != "root" || body.After != "updated root" || body.B64 != "AAH/" || body.HTML != tc.html || !body.Saved || !body.Deleted || !body.MissingAfterDelete {
				t.Fatalf("unexpected file operation result: %+v", body)
			}
			data, err := os.ReadFile(filepath.Join(rootDir, "data", "shared.txt"))
			if err != nil || string(data) != "updated root" {
				t.Fatalf("root file = %q, error = %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(rootDir, "data", "delete.txt")); !os.IsNotExist(err) {
				t.Fatalf("root delete file remains: %v", err)
			}
			for _, entry := range []struct{ dir, label string }{{childDir, "child"}, {grandchildDir, "grandchild"}, {workingDir, "cwd"}} {
				for _, name := range []string{"shared.txt", "delete.txt", "binary.bin"} {
					data, err := os.ReadFile(filepath.Join(entry.dir, "data", name))
					if err != nil || string(data) != entry.label {
						t.Fatalf("%s/%s changed: content = %q, error = %v", entry.label, name, data, err)
					}
				}
			}
		})
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/child/grandchild/assets/file.txt", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "grandchild public file" {
		t.Fatalf("included public path: status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
}

func TestFileHelpersPreserveAbsolutePathsAndMissingFileBehavior(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot *APIConfigSnapshot
	}{
		{"nil snapshot", nil},
		{"empty root", &APIConfigSnapshot{}},
		{"different root", &APIConfigSnapshot{RootPath: filepath.Join(t.TempDir(), "api.json")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "new", "nested", "file.txt")
			binary := filepath.Join(dir, "binary.bin")
			writeTestFile(t, binary, "\x00\x01\xff")
			vm := setupGojaRuntimeWithSnapshot(tc.snapshot)
			value, err := vm.RunString(fmt.Sprintf(`
const path = %q;
const binary = %q;
const directory = %q;
const missingBefore = nyanGetFile(path) === null;
const directoryIsNull = nyanGetFile(directory) === null;
const saved = nyanSaveFile(path, "absolute contents");
const read = nyanGetFile(path);
const b64 = nyanReadFileB64(binary);
const deleted = nyanDeleteFile(path);
const missingAfter = nyanGetFile(path) === null;
const deletedMissing = nyanDeleteFile(path);
let missingB64Throws = false;
try { nyanReadFileB64(path); } catch (error) { missingB64Throws = true; }
missingBefore && directoryIsNull && saved === true && read === "absolute contents" &&
  b64 === "AAH/" && deleted === true && missingAfter && deletedMissing === true && missingB64Throws;
`, file, binary, dir))
			if err != nil {
				t.Fatal(err)
			}
			if !value.ToBoolean() {
				t.Fatal("absolute paths or missing-file behavior changed")
			}
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatalf("absolute file was not deleted: %v", err)
			}
		})
	}
}

func TestFileHelpersRejectRelativePathsWithoutRootAPI(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeTestFile(t, filepath.Join(dir, "existing.txt"), "keep cwd file")
	for _, tc := range []struct {
		name     string
		snapshot *APIConfigSnapshot
	}{
		{"nil snapshot", nil},
		{"empty root", &APIConfigSnapshot{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, script := range []string{
				`nyanGetFile("existing.txt")`,
				`nyanSaveFile("existing.txt", "changed")`,
				`nyanDeleteFile("existing.txt")`,
				`nyanReadFileB64("existing.txt")`,
			} {
				vm := setupGojaRuntimeWithSnapshot(tc.snapshot)
				if _, err := vm.RunString(script); err == nil || !strings.Contains(err.Error(), "root API configuration path is unavailable") {
					t.Errorf("%s error = %v, want unavailable root API path", script, err)
				}
			}
			data, err := os.ReadFile(filepath.Join(dir, "existing.txt"))
			if err != nil || string(data) != "keep cwd file" {
				t.Fatalf("cwd fallback changed file: content = %q, error = %v", data, err)
			}
		})
	}
}

func TestFileHelpersKeepCapturedRootAfterSnapshotChanges(t *testing.T) {
	capturedDir, latestDir := t.TempDir(), t.TempDir()
	for _, entry := range []struct{ dir, contents string }{{capturedDir, "captured"}, {latestDir, "latest"}} {
		writeTestFile(t, filepath.Join(entry.dir, "read.txt"), entry.contents)
		writeTestFile(t, filepath.Join(entry.dir, "delete.txt"), entry.contents)
		writeTestFile(t, filepath.Join(entry.dir, "write.txt"), entry.contents)
	}
	captured := &APIConfigSnapshot{RootPath: filepath.Join(capturedDir, "api.json")}
	latest := &APIConfigSnapshot{RootPath: filepath.Join(latestDir, "api.json")}
	old := currentAPISnapshot()
	publishAPISnapshot(captured)
	t.Cleanup(func() { publishAPISnapshot(old) })
	vm := setupGojaRuntime()
	publishAPISnapshot(latest)

	value, err := vm.RunString(`
const before = nyanGetFile("read.txt");
const b64 = nyanReadFileB64("read.txt");
const saved = nyanSaveFile("write.txt", "updated captured");
const deleted = nyanDeleteFile("delete.txt");
before === "captured" && b64 === "Y2FwdHVyZWQ=" && saved === true && deleted === true;
`)
	if err != nil {
		t.Fatal(err)
	}
	if !value.ToBoolean() {
		t.Fatal("runtime did not retain its captured root API path")
	}
	data, err := os.ReadFile(filepath.Join(capturedDir, "write.txt"))
	if err != nil || string(data) != "updated captured" {
		t.Fatalf("captured write file = %q, error = %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(capturedDir, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("captured delete file remains: %v", err)
	}
	for _, name := range []string{"read.txt", "write.txt", "delete.txt"} {
		data, err := os.ReadFile(filepath.Join(latestDir, name))
		if err != nil || string(data) != "latest" {
			t.Fatalf("latest %s changed: content = %q, error = %v", name, data, err)
		}
	}
}

func TestStaticSchemaParserRejectsDynamicValues(t *testing.T) {
	if _, err := parseStaticJavaScriptValue("schema.js", `{type:createType()}`); err == nil {
		t.Fatal("dynamic function call was accepted")
	}
	if _, err := parseStaticJavaScriptValue("schema.js", `{...common}`); err == nil {
		t.Fatal("spread property was accepted")
	}
}

func TestMCPJSONSchemaValidationRejectsInvalidValuesAndExternalReferences(t *testing.T) {
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"value": map[string]interface{}{"type": "integer"}},
		"required":             []interface{}{"value"},
		"additionalProperties": false,
	}
	if err := validateMCPJSONSchemaValue(schema, map[string]interface{}{"value": float64(1)}); err != nil {
		t.Fatal(err)
	}
	if err := validateMCPJSONSchemaValue(schema, map[string]interface{}{"value": "not-an-integer"}); err == nil {
		t.Fatal("invalid input was accepted")
	}
	if _, err := compileMCPJSONSchema(map[string]interface{}{"$ref": "https://example.test/schema.json"}); err == nil {
		t.Fatal("external JSON Schema reference was accepted")
	}
}

func TestMCPRateAndConcurrencyLimits(t *testing.T) {
	now := time.Now()
	limit := &MCPRateLimit{Requests: 2, Window: "1m"}
	endpointName := t.Name()
	if allowed, _ := mcpRateLimitAllows(endpointName, limit, "192.0.2.10:1234", now); !allowed {
		t.Fatal("first request was rejected")
	}
	if allowed, _ := mcpRateLimitAllows(endpointName, limit, "192.0.2.10:5678", now); !allowed {
		t.Fatal("second request was rejected")
	}
	if allowed, retry := mcpRateLimitAllows(endpointName, limit, "192.0.2.10:9999", now); allowed || retry <= 0 {
		t.Fatalf("third request allowed=%v retry=%s", allowed, retry)
	}
	if allowed, _ := mcpRateLimitAllows(endpointName, limit, "192.0.2.10:1234", now.Add(time.Minute)); !allowed {
		t.Fatal("request after window was rejected")
	}
	release, acquired := acquireMCPExecutionSlot(endpointName, 1)
	if !acquired {
		t.Fatal("first concurrency slot was rejected")
	}
	if _, acquired := acquireMCPExecutionSlot(endpointName, 1); acquired {
		t.Fatal("concurrency limit was not enforced")
	}
	release()
	if releaseAgain, acquired := acquireMCPExecutionSlot(endpointName, 1); !acquired {
		t.Fatal("released concurrency slot was not reusable")
	} else {
		releaseAgain()
	}
}

func isolateMCPConcurrencyLimiters(t *testing.T) {
	t.Helper()
	previousSnapshot := currentAPISnapshot()
	mcpConcurrencyLimiters.Lock()
	previousLimiters, previousCurrent := mcpConcurrencyLimiters.Limiters, mcpConcurrencyLimiters.Current
	mcpConcurrencyLimiters.Limiters = make(map[string]chan struct{})
	mcpConcurrencyLimiters.Current = make(map[string]bool)
	mcpConcurrencyLimiters.Unlock()
	publishAPISnapshot(nil)
	t.Cleanup(func() {
		publishAPISnapshot(previousSnapshot)
		mcpConcurrencyLimiters.Lock()
		mcpConcurrencyLimiters.Limiters, mcpConcurrencyLimiters.Current = previousLimiters, previousCurrent
		mcpConcurrencyLimiters.Unlock()
	})
}

func acquireTestMCPExecutionSlot(t *testing.T, name string, limit int) func() {
	t.Helper()
	release, acquired := acquireMCPExecutionSlot(name, limit)
	if !acquired {
		t.Fatalf("could not acquire slot for %s with limit %d", name, limit)
	}
	var once sync.Once
	releaseOnce := func() { once.Do(release) }
	t.Cleanup(releaseOnce)
	return releaseOnce
}

func TestMCPConcurrencyLimitersFollowConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name         string
		initialLimit int
		next         APIConfig
		retained     bool
	}{
		{"unchanged", 1, APIConfig{"mcp": {Type: apiTypeMCP, Transport: "streamable_http", MaxConcurrent: 1}}, true},
		{"default matches explicit", 0, APIConfig{"mcp": {Type: apiTypeMCP, Transport: "streamable_http", MaxConcurrent: 16}}, true},
		{"limit changed", 1, APIConfig{"mcp": {Type: apiTypeMCP, Transport: "streamable_http", MaxConcurrent: 2}}, false},
		{"endpoint removed", 1, APIConfig{}, false},
		{"stdio", 1, APIConfig{"mcp": {Type: apiTypeMCP, Transport: "stdio", MaxConcurrent: 1}}, false},
		{"ordinary API", 1, APIConfig{"mcp": {Type: "api"}}, false},
		{"snapshot cleared", 1, nil, false},
	} {
		for _, busy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/busy=%t", tc.name, busy), func(t *testing.T) {
				isolateMCPConcurrencyLimiters(t)
				endpoint := EndpointConfig{Type: apiTypeMCP, Transport: "streamable_http", MaxConcurrent: tc.initialLimit}
				publishAPISnapshot(&APIConfigSnapshot{Config: APIConfig{"mcp": endpoint}})
				limit := mcpMaxConcurrent(endpoint)
				var releases []func()
				for i := 0; i < limit; i++ {
					releases = append(releases, acquireTestMCPExecutionSlot(t, "mcp", limit))
				}
				key := mcpConcurrencyKey("mcp", limit)
				mcpConcurrencyLimiters.Lock()
				original := mcpConcurrencyLimiters.Limiters[key]
				mcpConcurrencyLimiters.Unlock()
				if !busy {
					for _, release := range releases {
						release()
					}
				}
				var next *APIConfigSnapshot
				if tc.next != nil {
					next = &APIConfigSnapshot{Config: tc.next}
				}
				publishAPISnapshot(next)
				mcpConcurrencyLimiters.Lock()
				actual := mcpConcurrencyLimiters.Limiters[key]
				mcpConcurrencyLimiters.Unlock()
				if tc.retained || busy {
					if actual != original {
						t.Fatal("reload discarded the current or still-busy limiter")
					}
				} else if actual != nil {
					t.Fatal("reload retained an idle obsolete limiter")
				}
				if tc.retained && busy {
					if release, acquired := acquireMCPExecutionSlot("mcp", limit); acquired {
						release()
						t.Fatal("reload reset the in-flight request count")
					}
				}
				for _, release := range releases {
					release()
				}
				mcpConcurrencyLimiters.Lock()
				actual = mcpConcurrencyLimiters.Limiters[key]
				mcpConcurrencyLimiters.Unlock()
				if tc.retained && actual != original {
					t.Fatal("current limiter was removed after its requests finished")
				}
				if !tc.retained && actual != nil {
					t.Fatal("obsolete limiter remained after its last request finished")
				}
			})
		}
	}
}

func TestMCPConcurrencyLimiterReusesBusyPreviousLimit(t *testing.T) {
	isolateMCPConcurrencyLimiters(t)
	config := func(limit int) *APIConfigSnapshot {
		return &APIConfigSnapshot{Config: APIConfig{"mcp": {Type: apiTypeMCP, Transport: "streamable_http", MaxConcurrent: limit}}}
	}
	publishAPISnapshot(config(8))
	releaseOriginal := acquireTestMCPExecutionSlot(t, "mcp", 8)
	publishAPISnapshot(config(16))
	releaseNew := acquireTestMCPExecutionSlot(t, "mcp", 16)
	releaseNew()
	publishAPISnapshot(config(8))
	for i := 0; i < 7; i++ {
		acquireTestMCPExecutionSlot(t, "mcp", 8)
	}
	if release, acquired := acquireMCPExecutionSlot("mcp", 8); acquired {
		release()
		t.Fatal("returning to limit 8 lost the original in-flight request")
	}
	mcpConcurrencyLimiters.Lock()
	count := len(mcpConcurrencyLimiters.Limiters)
	mcpConcurrencyLimiters.Unlock()
	if count != 1 {
		t.Fatalf("retained %d limiters after returning to limit 8, want 1", count)
	}
	releaseOriginal()
	acquireTestMCPExecutionSlot(t, "mcp", 8)
}

func TestMCPConcurrencyLimiterCleansDelayedOldSnapshotRequest(t *testing.T) {
	isolateMCPConcurrencyLimiters(t)
	captured := &APIConfigSnapshot{Config: APIConfig{"mcp": {Type: apiTypeMCP, Transport: "streamable_http", MaxConcurrent: 1}}}
	publishAPISnapshot(captured)
	publishAPISnapshot(nil)
	// A request can capture the old snapshot before reload and reach acquisition later.
	release := acquireTestMCPExecutionSlot(t, "mcp", mcpMaxConcurrent(captured.Config["mcp"]))
	release()
	mcpConcurrencyLimiters.Lock()
	count := len(mcpConcurrencyLimiters.Limiters)
	mcpConcurrencyLimiters.Unlock()
	if count != 0 {
		t.Fatalf("delayed request left %d obsolete limiters behind", count)
	}
}

func TestMCPConcurrencyLimiterConcurrentReloadAndRequests(t *testing.T) {
	isolateMCPConcurrencyLimiters(t)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			<-start
			for i := 0; i < 200; i++ {
				if release, acquired := acquireMCPExecutionSlot("mcp", 1+(worker+i)%4); acquired {
					runtime.Gosched()
					release()
				}
			}
		}(worker)
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for i := 0; i < 200; i++ {
			publishAPISnapshot(&APIConfigSnapshot{Config: APIConfig{"mcp": {Type: apiTypeMCP, Transport: "streamable_http", MaxConcurrent: 1 + i%4}}})
			runtime.Gosched()
		}
	}()
	close(start)
	workers.Wait()
	publishAPISnapshot(nil)
	mcpConcurrencyLimiters.Lock()
	count := len(mcpConcurrencyLimiters.Limiters)
	mcpConcurrencyLimiters.Unlock()
	if count != 0 {
		t.Fatalf("concurrent reloads and completed requests left %d obsolete limiters", count)
	}
}

func TestMCPHTTPConcurrencyLimitSurvivesReload(t *testing.T) {
	isolateMCPConcurrencyLimiters(t)
	config := APIConfig{"mcp": {Type: apiTypeMCP, Transport: "streamable_http", MaxConcurrent: 1}}
	router := newRequestRegressionRouter(t, config)
	release := acquireTestMCPExecutionSlot(t, "mcp", 1)
	publishAPISnapshot(&APIConfigSnapshot{Config: config})
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`
	response := serveMCPRegressionRequest(router, body)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("busy request status=%d retry=%q body=%s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
	release()
	response = serveMCPRegressionRequest(router, body)
	envelope := decodeJSONRPCCheckResponse(t, response, "1")
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(envelope["result"], &result); err != nil || result.ProtocolVersion != mcpProtocol20251125 || envelope["error"] != nil {
		t.Fatalf("unexpected initialization after release: body=%s error=%v", response.Body.String(), err)
	}
}

func TestMCPDoesNotRegisterUserManagement(t *testing.T) {
	hook := writeFixtureFile(t, `({authenticated:false});`)
	tool := writeFixtureFile(t, `({ok:true});`)
	config := APIConfig{"tool": {Script: tool}, "verify": {Script: hook}, "mcp": {
		Type: apiTypeMCP, Transport: "streamable_http", ProtocolVersions: []string{mcpProtocol20251125},
		Tools: []MCPToolConfig{{Name: "tool", API: "tool"}},
	}}
	completeTestOAuthConfiguration(t, config)
	router := newRequestRegressionRouter(t, config)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, "/oauth/admin/users", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("removed management API HTTP %s status=%d body=%s", method, rec.Code, rec.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/nyan-rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"oauth/admin/users","params":{"oauth_hook":"oauthAdminUser"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	response := decodeJSONRPCCheckResponse(t, rec, "1")
	var rpcError JSONRPCError
	if err := json.Unmarshal(response["error"], &rpcError); err != nil || rpcError.Code != -32601 || response["result"] != nil {
		t.Fatalf("removed management API JSON-RPC response=%s error=%v", rec.Body.String(), err)
	}
}

func TestOAuthHooksReceiveHTTPContextAndControlResponse(t *testing.T) {
	hook := writeFixtureFile(t, `({
  status:201, headers:{"X-OAuth-Hook":nyanAllParams.oauth_hook},
  body:{hook:nyanAllParams.oauth_hook, method:nyanAllParams.method,
    issuer:nyanAllParams.issuer, resource:nyanAllParams.resource,
    authorize:nyanAllParams.authorization_endpoint,
    authorization:nyanAllParams.authorization, cookie:nyanAllParams.cookies.session,
    query:nyanAllParams.query, body:nyanAllParams.body, form:nyanAllParams.form}
});`)
	config := APIConfig{"tool": {Script: hook}, "verify": {Script: hook}, "mcp": {
		Type: apiTypeMCP, Transport: "streamable_http", ProtocolVersions: []string{mcpProtocol20251125},
		Tools: []MCPToolConfig{{Name: "tool", API: "tool"}},
	}}
	completeTestOAuthConfiguration(t, config)
	router := newRequestRegressionRouter(t, config)
	globalConfig.OAuthStateRoot = filepath.Join(t.TempDir(), "state")
	for _, tc := range []struct {
		name, method, target, contentType, body, hook, payloadField, payload string
	}{
		{name: "authorize_query", method: http.MethodGet, target: "/authorize?state=from-query", hook: "oauthAuthorize", payloadField: "query", payload: `{"state":["from-query"]}`},
		{name: "register_json", method: http.MethodPost, target: "/register", contentType: "application/json; charset=utf-8", body: `{"client_name":"fixture"}`, hook: "oauthRegister", payloadField: "body", payload: `{"client_name":"fixture"}`},
		{name: "token_form", method: http.MethodPost, target: "/token", contentType: "application/x-www-form-urlencoded", body: "grant_type=fixture", hook: "oauthToken", payloadField: "form", payload: `{"grant_type":["fixture"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "https://example.test:8443"+tc.target, strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			req.Header.Set("Authorization", "Bearer fixture")
			req.AddCookie(&http.Cookie{Name: "session", Value: "fixture-cookie"})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated || rec.Header().Get("X-OAuth-Hook") != tc.hook {
				t.Fatalf("hook response status=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]string{
				"hook": tc.hook, "method": tc.method, "issuer": "https://example.test:8443",
				"resource": "https://example.test:8443/mcp", "authorize": "https://example.test:8443/authorize",
				"authorization": "Bearer fixture", "cookie": "fixture-cookie",
			} {
				var got string
				if err := json.Unmarshal(body[key], &got); err != nil || got != want {
					t.Fatalf("hook input %s=%s, want %q; error=%v", key, body[key], want, err)
				}
			}
			if string(body[tc.payloadField]) != tc.payload {
				t.Fatalf("hook %s=%s, want %s", tc.payloadField, body[tc.payloadField], tc.payload)
			}
		})
	}
}

func TestOAuthHTTPChecksReceiveContextAndWireResponse(t *testing.T) {
	for _, tc := range []struct {
		name, api, hook, method, contentType, requestBody, script, body, outputField string
		status                                                                       int
	}{
		{api: "authorize", hook: "oauthAuthorize", method: http.MethodGet, script: `"approved";`, body: `"approved"`, status: 200},
		{api: "token", hook: "oauthToken", method: http.MethodPost, contentType: "application/x-www-form-urlencoded", requestBody: "grant_type=fixture", script: `({status:201,contentType:"text/plain",headers:{"X-OAuth-Result":"accepted"},body:"issued"});`, body: "issued", status: 201},
		{api: "register", hook: "oauthRegister", method: http.MethodPost, contentType: "application/json", requestBody: `{"client_name":"fixture"}`, script: `({client_id:"fixture"});`, body: `{"client_id":"fixture"}`, status: 200},
		{api: "metadata", hook: "authorizationServerMetadata", method: http.MethodGet, outputField: "issuer", status: 200},
		{api: "resource_metadata", hook: "protectedResourceMetadata", method: http.MethodGet, outputField: "resource", status: 200},
		{name: "authorize_null", api: "authorize", hook: "oauthAuthorize", method: http.MethodGet, script: `null;`, body: `{"error":"OAuth hook denied the request"}`, status: 401},
		{name: "authorize_undefined", api: "authorize", hook: "oauthAuthorize", method: http.MethodGet, script: `undefined;`, body: `{"error":"OAuth hook denied the request"}`, status: 401},
	} {
		name := tc.name
		if name == "" {
			name = tc.api
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			paramMarker, outMarker := filepath.Join(dir, "param"), filepath.Join(dir, "out")
			hook := writeFixtureFile(t, tc.script)
			cfg := APIConfig{"tool": {Script: hook}, "verify": {Script: hook}, "mcp": {Type: apiTypeMCP, Transport: "streamable_http", Tools: []MCPToolConfig{{Name: "tool", API: "tool"}}}}
			completeTestOAuthConfiguration(t, cfg)
			contextCheck := fmt.Sprintf(`
if (nyanAllParams.oauth_api !== %q || nyanAllParams.oauth_hook !== %q ||
    nyanAllParams.method !== %q || nyanAllParams.path !== %q ||
    nyanAllParams.request_path !== %q || nyanAllParams.endpoint !== "mcp" ||
    nyanAllParams.issuer !== "https://example.test" || nyanAllParams.resource !== "https://example.test/mcp" ||
    nyanAllParams.authorization !== "Bearer fixture" || nyanAllParams.cookies.session !== "fixture-cookie" ||
    nyanAllParams.query.state[0] !== "fixture-state" || nyanGetRequestHeaders()["X-Check"] !== "fixture-header") {
  throw new Error("OAuth context did not reach check");
}
if (nyanAllParams.oauth_api === "token" && nyanAllParams.form.grant_type[0] !== "fixture") throw new Error("missing form");
if (nyanAllParams.oauth_api === "register" && nyanAllParams.body.client_name !== "fixture") throw new Error("missing JSON body");
`, tc.api, tc.hook, tc.method, "/"+tc.api, "/"+tc.api)
			outputCheck := fmt.Sprintf(`
if (nyanAllParams.nyan_output.status !== %d) throw new Error("wrong output status");
if (%q !== "") {
  const body = JSON.parse(nyanAllParams.nyan_output.body);
  if (!body[%q] || body.scopes_supported[0] !== "read") throw new Error("missing metadata");
} else if (nyanAllParams.nyan_output.body !== %q) throw new Error("wrong wire body");
if (nyanAllParams.oauth_api === "token") {
  if (nyanAllParams.nyan_output.contentType !== "text/plain" || nyanAllParams.nyan_output.headers["X-OAuth-Result"] !== "accepted") throw new Error("missing response headers");
} else if (nyanAllParams.nyan_output.contentType.indexOf("application/json") !== 0) throw new Error("wrong content type");
`, tc.status, tc.outputField, tc.outputField, tc.body)
			endpoint := cfg[tc.api]
			endpoint.ParamCheck = writeFixtureFile(t, contextCheck+fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({success:true,status:200,result:null});`, paramMarker))
			endpoint.OutCheck = writeFixtureFile(t, contextCheck+outputCheck+fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({success:true,status:200,result:null});`, outMarker))
			cfg[tc.api] = endpoint
			router := newRequestRegressionRouter(t, cfg)
			req := httptest.NewRequest(tc.method, "https://example.test/"+tc.api+"?state=fixture-state", strings.NewReader(tc.requestBody))
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Authorization", "Bearer fixture")
			req.Header.Set("X-Check", "fixture-header")
			req.AddCookie(&http.Cookie{Name: "session", Value: "fixture-cookie"})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status || tc.outputField == "" && rec.Body.String() != tc.body {
				t.Fatalf("OAuth response status=%d body=%s", rec.Code, rec.Body.String())
			}
			if tc.api == "token" && rec.Header().Get("X-OAuth-Result") != "accepted" {
				t.Fatalf("hook response header missing: %v", rec.Header())
			}
			assertJSONRPCExecutionMarker(t, paramMarker, true)
			assertJSONRPCExecutionMarker(t, outMarker, true)
		})
	}
}

func TestOAuthHTTPCheckRejections(t *testing.T) {
	for _, api := range []string{"authorize", "token", "register", "metadata", "resource_metadata"} {
		for _, stage := range []string{"paramCheck", "outCheck"} {
			for _, tc := range []struct {
				name, script string
				success      bool
				status       int
			}{
				{name: "denied", script: `({success:false,status:403,result:"check rejected"});`, status: 403},
				{name: "false_with_200", script: `({success:false,status:200,result:"check rejected"});`, status: 200},
				{name: "true_with_non_200", script: `({success:true,status:201,result:"check rejected"});`, success: true, status: 201},
				{name: "json_string", script: `JSON.stringify({success:false,status:401,result:"check rejected"});`, status: 401},
				{name: "exception", script: `throw new Error("check failed");`, status: 500},
				{name: "malformed", script: `({status:403,result:"check rejected"});`, status: 500},
			} {
				t.Run(api+"/"+stage+"/"+tc.name, func(t *testing.T) {
					dir := t.TempDir()
					hookMarker, outMarker := filepath.Join(dir, "hook"), filepath.Join(dir, "out")
					hook := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({status:201,headers:{"X-OAuth-Result":"private"},body:"PRIVATE_RESULT"});`, hookMarker))
					cfg := APIConfig{"tool": {Script: hook}, "verify": {Script: hook}, "mcp": {Type: apiTypeMCP, Transport: "streamable_http", Tools: []MCPToolConfig{{Name: "tool", API: "tool"}}}}
					completeTestOAuthConfiguration(t, cfg)
					param, out := `({success:true,status:200,result:null});`, `({success:true,status:200,result:null});`
					if stage == "paramCheck" {
						param = tc.script
					} else {
						out = tc.script
					}
					endpoint := cfg[api]
					endpoint.ParamCheck = writeFixtureFile(t, param)
					endpoint.OutCheck = writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); `, outMarker)+out)
					cfg[api] = endpoint
					router := newRequestRegressionRouter(t, cfg)
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://example.test/"+api, nil))
					if rec.Code != tc.status || strings.Contains(rec.Body.String(), "PRIVATE_RESULT") || rec.Header().Get("X-OAuth-Result") != "" {
						t.Fatalf("rejected OAuth response status=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
					}
					assertParamCheckResponse(t, rec.Body.Bytes(), tc.success, tc.status)
					if tc.status != 500 && !strings.Contains(rec.Body.String(), `"result":"check rejected"`) {
						t.Fatalf("check result was replaced: %s", rec.Body.String())
					}
					assertJSONRPCExecutionMarker(t, hookMarker, stage == "outCheck" && api != "metadata" && api != "resource_metadata")
					assertJSONRPCExecutionMarker(t, outMarker, stage == "outCheck")
				})
			}
		}
	}
}

func TestOAuthMetadataOptionsSkipsChecks(t *testing.T) {
	for _, api := range []string{"metadata", "resource_metadata"} {
		t.Run(api, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "check")
			hook := writeFixtureFile(t, `({authenticated:true});`)
			cfg := APIConfig{"tool": {Script: hook}, "verify": {Script: hook}, "mcp": {Type: apiTypeMCP, Transport: "streamable_http", Tools: []MCPToolConfig{{Name: "tool", API: "tool"}}}}
			completeTestOAuthConfiguration(t, cfg)
			check := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({success:false,status:403,result:null});`, marker))
			cfg[api] = EndpointConfig{ParamCheck: check, OutCheck: check}
			router := newRequestRegressionRouter(t, cfg)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "https://example.test/"+api, nil))
			if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
				t.Fatalf("metadata preflight status=%d body=%s", rec.Code, rec.Body.String())
			}
			assertJSONRPCExecutionMarker(t, marker, false)
		})
	}
}

func TestOAuthVerifierChecksProtectToolExecution(t *testing.T) {
	for _, stage := range []string{"paramCheck", "outCheck"} {
		for _, tc := range []struct {
			name, script, decision string
			allowed                bool
		}{
			{name: "authenticated", script: `({success:true,status:200,result:null});`, decision: "authenticated", allowed: true},
			{name: "allowed", script: `({success:true,status:200,result:null});`, decision: "allowed", allowed: true},
			{name: "denied", script: `({success:false,status:403,result:"private rejection"});`},
			{name: "false_with_200", script: `({success:false,status:200,result:null});`},
			{name: "true_with_non_200", script: `({success:true,status:201,result:null});`},
			{name: "exception", script: `throw new Error("private check failure");`},
			{name: "malformed", script: `({status:200});`},
		} {
			t.Run(stage+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				paramMarker, hookMarker, outMarker, toolMarker := filepath.Join(dir, "param"), filepath.Join(dir, "hook"), filepath.Join(dir, "out"), filepath.Join(dir, "tool")
				decision := tc.decision
				if decision == "" {
					decision = "authenticated"
				}
				hook := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({%s:true,principal:{id:"checked-user"}});`, hookMarker, decision))
				tool := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({user:nyanAllParams.mcp_principal.id});`, toolMarker))
				cfg := APIConfig{"tool": {Script: tool}, "verify": {Script: hook}, "mcp": {Type: apiTypeMCP, Transport: "streamable_http", ProtocolVersions: []string{mcpProtocol20251125}, Tools: []MCPToolConfig{{Name: "tool", API: "tool", InputSchema: map[string]interface{}{"type": "object"}}}}}
				completeTestOAuthConfiguration(t, cfg)
				contextCheck := `if (nyanAllParams.oauth_hook !== "oauthValidateAccessToken" || nyanAllParams.oauth_api !== "verify" || nyanAllParams.request_path !== "/mcp" || nyanAllParams.path !== "/verify" || nyanAllParams.tool !== "tool" || nyanAllParams.required_scopes[0] !== "read" || nyanAllParams.body.method !== "tools/call") throw new Error("missing verification context"); `
				param, out := `({success:true,status:200,result:null});`, `({success:true,status:200,result:null});`
				if stage == "paramCheck" {
					param = tc.script
				} else {
					out = tc.script
				}
				endpoint := cfg["verify"]
				endpoint.ParamCheck = writeFixtureFile(t, contextCheck+fmt.Sprintf(`nyanSaveFile(%q,"ran"); `, paramMarker)+param)
				endpoint.OutCheck = writeFixtureFile(t, contextCheck+fmt.Sprintf(`const result=JSON.parse(nyanAllParams.nyan_output.body); if (nyanAllParams.nyan_output.status !== 200 || result.%s !== true || result.principal.id !== "checked-user") throw new Error("verification result was replaced"); nyanSaveFile(%q,"ran"); `, decision, outMarker)+out)
				cfg["verify"] = endpoint
				router := newRequestRegressionRouter(t, cfg)
				rec := serveMCPRegressionRequest(router, `{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"tool","arguments":{}}}`)
				if tc.allowed {
					if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"structuredContent":{"user":"checked-user"}`) {
						t.Fatalf("verified principal did not reach tool: status=%d body=%s", rec.Code, rec.Body.String())
					}
				} else if rec.Code != http.StatusUnauthorized || rec.Header().Get("WWW-Authenticate") == "" || strings.Contains(rec.Body.String(), "private") {
					t.Fatalf("verifier check did not fail closed: status=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
				}
				var envelope map[string]json.RawMessage
				if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || string(envelope["jsonrpc"]) != `"2.0"` || string(envelope["id"]) != "17" {
					t.Fatalf("MCP response envelope changed: body=%s error=%v", rec.Body.String(), err)
				}
				assertJSONRPCExecutionMarker(t, paramMarker, true)
				assertJSONRPCExecutionMarker(t, hookMarker, tc.allowed || stage == "outCheck")
				assertJSONRPCExecutionMarker(t, outMarker, tc.allowed || stage == "outCheck")
				assertJSONRPCExecutionMarker(t, toolMarker, tc.allowed)
			})
		}
	}
}

func TestOAuthCheckOnlySkipsExecution(t *testing.T) {
	for _, tc := range []struct {
		name, api, method, query, contentType, body string
		noCheck, reject                             bool
	}{
		{name: "authorize_query", api: "authorize", method: http.MethodGet, query: "?nyan_mode=checkOnly"},
		{name: "token_form", api: "token", method: http.MethodPost, contentType: "application/x-www-form-urlencoded", body: "nyan_mode=checkOnly"},
		{name: "register_json", api: "register", method: http.MethodPost, contentType: "application/json", body: `{"nyan_mode":"checkOnly"}`},
		{name: "metadata", api: "metadata", method: http.MethodGet, query: "?nyan_mode=checkOnly"},
		{name: "resource_metadata", api: "resource_metadata", method: http.MethodGet, query: "?nyan_mode=checkOnly"},
		{name: "no_param_check", api: "authorize", method: http.MethodGet, query: "?nyan_mode=checkOnly", noCheck: true},
		{name: "check_rejected", api: "authorize", method: http.MethodGet, query: "?nyan_mode=checkOnly", reject: true},
		{name: "query_overrides_json", api: "register", method: http.MethodPost, query: "?nyan_mode=checkOnly", contentType: "application/json", body: `{"nyan_mode":"run"}`},
		{name: "verifier", api: "verify", method: http.MethodPost, query: "?nyan_mode=checkOnly"},
		{name: "verifier_no_param_check", api: "verify", method: http.MethodPost, query: "?nyan_mode=checkOnly", noCheck: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			paramMarker, hookMarker, outMarker, toolMarker := filepath.Join(dir, "param"), filepath.Join(dir, "hook"), filepath.Join(dir, "out"), filepath.Join(dir, "tool")
			hook := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({authenticated:true,principal:{id:"user"}});`, hookMarker))
			tool := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({ok:true});`, toolMarker))
			cfg := APIConfig{"tool": {Script: tool}, "verify": {Script: hook}, "mcp": {Type: apiTypeMCP, Transport: "streamable_http", ProtocolVersions: []string{mcpProtocol20251125}, Tools: []MCPToolConfig{{Name: "tool", API: "tool"}}}}
			completeTestOAuthConfiguration(t, cfg)
			endpoint := cfg[tc.api]
			status := http.StatusOK
			if tc.reject {
				status = http.StatusForbidden
			}
			if !tc.noCheck {
				endpoint.ParamCheck = writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({success:%t,status:%d,result:"checked"});`, paramMarker, !tc.reject, status))
			}
			endpoint.OutCheck = writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({success:true,status:200,result:null});`, outMarker))
			cfg[tc.api] = endpoint
			router := newRequestRegressionRouter(t, cfg)
			target, body, contentType := tc.api, tc.body, tc.contentType
			if tc.api == "verify" {
				target, contentType = "mcp", "application/json"
				body = `{"jsonrpc":"2.0","id":18,"method":"tools/call","params":{"name":"tool","arguments":{}}}`
			}
			req := httptest.NewRequest(tc.method, "https://example.test/"+target+tc.query, strings.NewReader(body))
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("MCP-Protocol-Version", mcpProtocol20251125)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if tc.api == "verify" {
				if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"isError":true`) {
					t.Fatalf("checkOnly must not authenticate: status=%d body=%s", rec.Code, rec.Body.String())
				}
			} else {
				if rec.Code != status {
					t.Fatalf("checkOnly status=%d body=%s", rec.Code, rec.Body.String())
				}
				assertParamCheckResponse(t, rec.Body.Bytes(), !tc.reject, status)
			}
			assertJSONRPCExecutionMarker(t, paramMarker, !tc.noCheck)
			for _, marker := range []string{hookMarker, outMarker, toolMarker} {
				assertJSONRPCExecutionMarker(t, marker, false)
			}
		})
	}
}

func TestOAuthVerifierPassingChecksCannotOverrideDenial(t *testing.T) {
	for _, decision := range []string{`({authenticated:false});`, `({allowed:false});`, `false;`, `null;`, `undefined;`, `({status:401,body:{error:"invalid_token"}});`} {
		t.Run(decision, func(t *testing.T) {
			dir := t.TempDir()
			outMarker, toolMarker := filepath.Join(dir, "out"), filepath.Join(dir, "tool")
			hook := writeFixtureFile(t, decision)
			tool := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({ok:true});`, toolMarker))
			cfg := APIConfig{"tool": {Script: tool}, "verify": {Script: hook}, "mcp": {Type: apiTypeMCP, Transport: "streamable_http", ProtocolVersions: []string{mcpProtocol20251125}, Tools: []MCPToolConfig{{Name: "tool", API: "tool"}}}}
			completeTestOAuthConfiguration(t, cfg)
			endpoint := cfg["verify"]
			endpoint.ParamCheck = writeFixtureFile(t, `({success:true,status:200,result:null});`)
			endpoint.OutCheck = writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({success:true,status:200,result:null});`, outMarker))
			cfg["verify"] = endpoint
			router := newRequestRegressionRouter(t, cfg)
			rec := serveMCPRegressionRequest(router, `{"jsonrpc":"2.0","id":19,"method":"tools/call","params":{"name":"tool","arguments":{}}}`)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("passing checks must not override verifier: status=%d body=%s", rec.Code, rec.Body.String())
			}
			assertJSONRPCExecutionMarker(t, outMarker, true)
			assertJSONRPCExecutionMarker(t, toolMarker, false)
		})
	}
}

func TestArgon2idHashAndVerify(t *testing.T) {
	const password = "fixture-password"
	encoded, err := argon2idHash(password)
	if err != nil || !argon2idVerify(password, encoded) {
		t.Fatalf("hash round trip failed: %v", err)
	}
	parts := strings.Split(encoded, "$")
	profile := argon2idProfiles[currentArgon2idProfile]
	if len(parts) != 6 || parts[3] != profile.parameters() {
		t.Fatalf("hash does not use the current generation profile: %q", encoded)
	}
	for _, field := range []struct {
		name    string
		encoded string
		length  uint32
	}{
		{"salt", parts[4], profile.saltLength},
		{"digest", parts[5], profile.keyLength},
	} {
		decoded, err := base64.RawStdEncoding.DecodeString(field.encoded)
		if err != nil || len(decoded) != int(field.length) {
			t.Fatalf("generated %s length=%d, want %d; error=%v", field.name, len(decoded), field.length, err)
		}
	}
	changed := func(index int, value string) string {
		copyOfParts := append([]string(nil), parts...)
		copyOfParts[index] = value
		return strings.Join(copyOfParts, "$")
	}
	for _, tc := range []struct{ name, password, encoded string }{
		{"wrong_password", "wrong-password", encoded},
		{"empty_hash", password, ""},
		{"unexpected_prefix", password, changed(0, "junk")},
		{"extra_field", password, encoded + "$extra"},
		{"wrong_algorithm", password, changed(1, "argon2i")},
		{"wrong_version", password, changed(2, "v=16")},
		{"zero_work", password, changed(3, "m=0,t=0,p=0")},
		{"excessive_work", password, changed(3, "m=4294967295,t=4294967295,p=255")},
		{"parameter_suffix", password, changed(3, parts[3]+",extra=1")},
		{"parameter_whitespace", password, changed(3, parts[3]+" ")},
		{"invalid_salt", password, changed(4, "!")},
		{"invalid_salt_encoding", password, changed(4, strings.Repeat("!", len(parts[4])))},
		{"short_salt", password, changed(4, "AA")},
		{"long_salt", password, changed(4, parts[4]+"A")},
		{"salt_newline", password, changed(4, parts[4][:len(parts[4])-1]+"\n")},
		{"invalid_digest_encoding", password, changed(5, strings.Repeat("!", len(parts[5])))},
		{"short_digest", password, changed(5, "AA")},
		{"long_digest", password, changed(5, parts[5]+"A")},
		{"digest_newline", password, changed(5, parts[5][:len(parts[5])-1]+"\n")},
		{"oversize_password", strings.Repeat("x", 4097), encoded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if argon2idVerify(tc.password, tc.encoded) {
				t.Fatal("invalid password or hash was accepted")
			}
		})
	}
	for _, invalid := range []string{"", strings.Repeat("x", 4097)} {
		if _, err := argon2idHash(invalid); err == nil {
			t.Fatal("invalid password was hashed")
		}
	}
}

func TestArgon2idStoredHashCompatibility(t *testing.T) {
	// Keep this saved hash independent of the current generation profile so a
	// future default change cannot silently invalidate existing passwords.
	// Password: fixture-password; salt: 0123456789abcdef; digest length: 32 bytes.
	const savedHash = "$argon2id$v=19$m=65536,t=3,p=2$MDEyMzQ1Njc4OWFiY2RlZg$8AcZ9tO47h2U7BO3dpzQuEogqm6bqJy8+taXF1/F90g"
	t.Run("Go", func(t *testing.T) {
		if !argon2idVerify("fixture-password", savedHash) {
			t.Fatal("previously saved hash was rejected")
		}
		if argon2idVerify("wrong-password", savedHash) {
			t.Fatal("saved hash accepted the wrong password")
		}
	})
	t.Run("JavaScript", func(t *testing.T) {
		script := writeFixtureFile(t, fmt.Sprintf(`
nyanArgon2idVerify("fixture-password", %q) === true &&
nyanArgon2idVerify("wrong-password", %q) === false;
`, savedHash, savedHash))
		value, err := runJavaScriptValueWithSnapshot(&APIConfigSnapshot{}, script, "", map[string]interface{}{"state_directory": t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if value.Export() != true {
			t.Fatalf("JavaScript saved hash verification failed: %v", value)
		}
	})
}

func TestJavaScriptOAuthStateLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	script := writeFixtureFile(t, `
nyanOAuthWrite("tokens/one.json", '{"value":"one"}');
const read = nyanOAuthRead("tokens/one.json");
const consumed = nyanOAuthConsume("tokens/one.json");
const missing = nyanOAuthRead("tokens/one.json");
const replay = nyanOAuthConsume("tokens/one.json");
nyanOAuthWrite("tokens/remove.json", '{"value":"remove"}');
const deleted = nyanOAuthDelete("tokens/remove.json");
const deletedAgain = nyanOAuthDelete("tokens/remove.json");
JSON.stringify({read:read,consumed:consumed,missing:missing,replay:replay,deleted:deleted,deletedAgain:deletedAgain});
`)
	value, err := runJavaScriptValueWithSnapshot(&APIConfigSnapshot{}, script, "", map[string]interface{}{"state_directory": root})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Read, Consumed, Missing, Replay string
		Deleted, DeletedAgain           bool
	}
	if err := json.Unmarshal([]byte(value.String()), &result); err != nil {
		t.Fatal(err)
	}
	if result.Read != `{"value":"one"}` || result.Consumed != result.Read || result.Missing != "" || result.Replay != "" || !result.Deleted || !result.DeletedAgain {
		t.Fatalf("unexpected state lifecycle: %+v", result)
	}
	if keys, err := oauthListState(root, "tokens"); err != nil || len(keys) != 0 {
		t.Fatalf("consumed/deleted state remains: keys=%v error=%v", keys, err)
	}
}

func TestMCPHTTPRejectsInvalidRequestsBeforeToolExecution(t *testing.T) {
	const call = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"tool","arguments":{}}}`
	for _, tc := range []struct {
		name, method, contentType, accept, version, body string
		status                                           int
		rpcError, runsTool                               bool
	}{
		{name: "valid_charset", contentType: "application/json; charset=utf-8", accept: "application/json, text/event-stream", version: mcpProtocol20251125, body: call, status: 200, runsTool: true},
		{name: "wrong_method", method: http.MethodGet, status: 405},
		{name: "wrong_content_type", contentType: "text/plain", accept: "application/json, text/event-stream", body: call, status: 415},
		{name: "missing_event_stream_accept", contentType: "application/json", accept: "application/json", body: call, status: 406},
		{name: "malformed_json", contentType: "application/json", accept: "application/json, text/event-stream", body: "{", status: 200, rpcError: true},
		{name: "batch", contentType: "application/json", accept: "application/json, text/event-stream", body: "[" + call + "]", status: 200, rpcError: true},
		{name: "duplicate_id", contentType: "application/json", accept: "application/json, text/event-stream", body: `{"jsonrpc":"2.0","id":1,"id":2,"method":"ping"}`, status: 200, rpcError: true},
		{name: "missing_protocol", contentType: "application/json", accept: "application/json, text/event-stream", body: call, status: 400},
		{name: "oversized_body", contentType: "application/json", accept: "application/json, text/event-stream", body: strings.Repeat(" ", (1<<20)+1), status: 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "tool-ran")
			script := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({status:200,body:{ok:true}});`, marker))
			config := APIConfig{"tool": {Script: script}, "mcp": {
				Type: apiTypeMCP, Transport: "streamable_http", ProtocolVersions: []string{mcpProtocol20251125},
				AllowedOrigins: []string{"https://client.example"}, Tools: []MCPToolConfig{{Name: "tool", API: "tool"}},
			}}
			if err := validateMCPConfiguration(config); err != nil {
				t.Fatal(err)
			}
			router := newRequestRegressionRouter(t, config)
			method := tc.method
			if method == "" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, "https://example.test/mcp", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Accept", tc.accept)
			req.Header.Set("MCP-Protocol-Version", tc.version)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d, want %d; body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.rpcError {
				response := decodeJSONRPCCheckResponse(t, rec, "null")
				var rpcError JSONRPCError
				if err := json.Unmarshal(response["error"], &rpcError); err != nil || rpcError.Code == 0 || response["result"] != nil {
					t.Fatalf("invalid request did not return a JSON-RPC error: body=%s error=%v", rec.Body.String(), err)
				}
			}
			if tc.runsTool {
				response := decodeJSONRPCCheckResponse(t, rec, "7")
				var result struct {
					IsError           bool `json:"isError"`
					StructuredContent struct {
						OK bool `json:"ok"`
					} `json:"structuredContent"`
				}
				if err := json.Unmarshal(response["result"], &result); err != nil || result.IsError || !result.StructuredContent.OK || response["error"] != nil {
					t.Fatalf("valid request did not return tool output: body=%s error=%v", rec.Body.String(), err)
				}
			}
			assertJSONRPCExecutionMarker(t, marker, tc.runsTool)
		})
	}
}

func TestMCPAndOAuthDelegateDecisionsToJavaScriptHooks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	hook := filepath.Join(dir, "oauth_validate.js")
	toolScript := filepath.Join(dir, "tool.js")
	writeTestFile(t, hook, `
for (const key of ["operator_username", "operator_password", "admin_user_endpoint"]) {
  if (Object.prototype.hasOwnProperty.call(nyanAllParams, key)) {
    throw new Error("removed operator parameter: " + key);
  }
}
if (typeof nyanOAuthAdminAuthorized !== "undefined") {
  throw new Error("removed operator authentication helper is still available");
}
({authenticated: nyanAllParams.authorization === "Bearer test-token", principal:{id:"user-1"}});
`)
	writeTestFile(t, toolScript, `({status:200,contentType:"application/json",body:{ok:true,items:[1,2,3]}});`)
	snapshot := &APIConfigSnapshot{Config: APIConfig{
		"server_mcp":                             {Type: apiTypeMCP, Transport: "streamable_http", ProtocolVersions: []string{"2025-11-25"}, AllowedOrigins: []string{"https://chatgpt.com"}, RedirectURIAllowedPrefixes: []string{"https://chatgpt.com/connector/oauth/"}, OAuth: MCPOAuthHooks{AuthorizationServerMetadata: ".well-known/oauth-authorization-server", ProtectedResourceMetadataAPI: ".well-known/oauth-protected-resource/server_mcp", Authorize: "oauth/authorize", Token: "oauth/token", Register: "oauth/register", VerifyAccess: "oauth/verify_access"}, Tools: []MCPToolConfig{{Name: "sample", API: "sample"}}, Instructions: "test"},
		"sample":                                 {Type: "api", Script: toolScript, SecuritySchemes: []map[string]interface{}{{"type": "oauth2", "scopes": []string{"nyanpui:read"}}}},
		".well-known/oauth-authorization-server": {Type: "api"}, ".well-known/oauth-protected-resource/server_mcp": {Type: "api"},
		"oauth/authorize": {Type: "api", Script: hook}, "oauth/token": {Type: "api", Script: hook}, "oauth/register": {Type: "api", Script: hook}, "oauth/verify_access": {Type: "api", Script: hook, Scopes: []string{"nyanpui:read"}},
	}}
	if err := validateMCPConfiguration(snapshot.Config); err != nil {
		t.Fatal(err)
	}
	oldSnapshot := currentAPISnapshot()
	oldConfig := globalConfig
	publishAPISnapshot(snapshot)
	globalConfig = Config{Name: "NyanPUI", Version: "test"}
	t.Cleanup(func() { publishAPISnapshot(oldSnapshot); globalConfig = oldConfig })

	router := gin.New()
	router.Use(CORSMiddleware())
	router.NoRoute(func(c *gin.Context) {
		if !dispatchMCPOrOAuth(c) {
			c.Status(http.StatusNotFound)
		}
	})
	preflight := httptest.NewRequest(http.MethodOptions, "/server_mcp", nil)
	preflight.Header.Set("Origin", "https://chatgpt.com")
	preflightResponse := httptest.NewRecorder()
	router.ServeHTTP(preflightResponse, preflight)
	if preflightResponse.Code != http.StatusNoContent || preflightResponse.Header().Get("Access-Control-Allow-Origin") != "https://chatgpt.com" {
		t.Fatalf("preflight status=%d headers=%v", preflightResponse.Code, preflightResponse.Header())
	}
	forbiddenPreflight := httptest.NewRequest(http.MethodOptions, "/server_mcp", nil)
	forbiddenPreflight.Header.Set("Origin", "https://attacker.test")
	forbiddenPreflightResponse := httptest.NewRecorder()
	router.ServeHTTP(forbiddenPreflightResponse, forbiddenPreflight)
	if forbiddenPreflightResponse.Code != http.StatusForbidden || forbiddenPreflightResponse.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("forbidden preflight status=%d headers=%v", forbiddenPreflightResponse.Code, forbiddenPreflightResponse.Header())
	}
	notification := httptest.NewRequest(http.MethodPost, "/server_mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	notification.Header.Set("Content-Type", "application/json")
	notification.Header.Set("Accept", "application/json, text/event-stream")
	notification.Header.Set("MCP-Protocol-Version", "2025-11-25")
	notificationResponse := httptest.NewRecorder()
	router.ServeHTTP(notificationResponse, notification)
	if notificationResponse.Code != http.StatusAccepted || notificationResponse.Body.Len() != 0 {
		t.Fatalf("notification status=%d body=%s", notificationResponse.Code, notificationResponse.Body.String())
	}
	body, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]interface{}{"name": "sample", "arguments": map[string]interface{}{}}})
	req := httptest.NewRequest(http.MethodPost, "/server_mcp", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"structuredContent"`) || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), hook) {
		t.Fatal("hook filesystem path leaked in MCP response")
	}
}

func TestOAuthListStatePreservesInterruptedWrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "oauth-state")
	for _, key := range []string{"tokens/z.json", "tokens/a.json"} {
		if err := oauthWriteState(root, key, `{"valid":true}`); err != nil {
			t.Fatal(err)
		}
	}
	temp, err := os.CreateTemp(filepath.Join(root, "tokens"), ".nyanpui-oauth-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	const incompleteJSON = `{"unfinished":`
	if _, err := temp.WriteString(incompleteJSON); err != nil {
		temp.Close()
		t.Fatal(err)
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}

	keys, err := oauthListState(root, "tokens")
	if err != nil || strings.Join(keys, ",") != "tokens/a.json,tokens/z.json" {
		t.Fatalf("interrupted write broke listing: keys=%v error=%v", keys, err)
	}
	script := writeFixtureFile(t, `JSON.stringify(nyanOAuthList("tokens"));`)
	value, err := runJavaScriptValueWithSnapshot(&APIConfigSnapshot{}, script, "", map[string]interface{}{"state_directory": root})
	if err != nil {
		t.Fatalf("interrupted write broke JavaScript listing: %v", err)
	}
	if got := value.String(); got != `["tokens/a.json","tokens/z.json"]` {
		t.Fatalf("JavaScript listing = %s", got)
	}
	content, err := os.ReadFile(temp.Name())
	if err != nil || string(content) != incompleteJSON {
		t.Fatalf("listing changed the temporary file: content=%q error=%v", content, err)
	}
}

func TestOAuthListStateRejectsUnsafeEntries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		filename string
		kind     string
	}{
		{name: "unknown_tmp", filename: "other.tmp"},
		{name: "wrong_prefix", filename: "nyanpui-oauth-123.tmp"},
		{name: "wrong_suffix", filename: ".nyanpui-oauth-123.tmp.bak"},
		{name: "temporary_directory", filename: ".nyanpui-oauth-123.tmp", kind: "directory"},
		{name: "temporary_symlink", filename: ".nyanpui-oauth-123.tmp", kind: "symlink"},
		{name: "temporary_permissions", filename: ".nyanpui-oauth-123.tmp", kind: "permissions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.kind == "permissions" && runtime.GOOS == "windows" {
				t.Skip("OAuth state does not enforce POSIX permissions on Windows")
			}
			root := filepath.Join(t.TempDir(), "oauth-state")
			if err := oauthWriteState(root, "tokens/valid.json", `{"valid":true}`); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "tokens", tc.filename)
			switch tc.kind {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(root, "tokens", "valid.json"), path); err != nil {
					t.Skipf("cannot create a symbolic link: %v", err)
				}
			default:
				if err := os.WriteFile(path, []byte(`{"unfinished":`), 0600); err != nil {
					t.Fatal(err)
				}
				if tc.kind == "permissions" {
					if err := os.Chmod(path, 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			if keys, err := oauthListState(root, "tokens"); err == nil || len(keys) != 0 {
				t.Fatalf("unsafe entry was accepted: keys=%v error=%v", keys, err)
			}
			script := writeFixtureFile(t, `nyanOAuthList("tokens");`)
			if _, err := runJavaScriptValueWithSnapshot(&APIConfigSnapshot{}, script, "", map[string]interface{}{"state_directory": root}); err == nil {
				t.Fatal("JavaScript listing accepted an unsafe state entry")
			}
		})
	}
}

func TestCurrentMCPConfigSupportsMultipleTransportsAndSharedTool(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tool.js")
	writeTestFile(t, script, `({ok:true,transport:nyanAllParams.mcp_principal.transport});`)
	data := []byte(fmt.Sprintf(`{"shared":{"type":"api","script":%q,"title":"Shared","description":"shared tool"},"http_mcp":{"type":"mcp","transport":"streamable_http","allowedOrigins":["https://chatgpt.com"],"tools":["shared"]},"local_mcp":{"type":"mcp","transport":"stdio","tools":["shared"]}}`, script))
	config, err := decodeAPIConfig(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(config["http_mcp"].Tools) != 1 || config["http_mcp"].Tools[0].Title != "Shared" {
		t.Fatalf("HTTP tools=%#v", config["http_mcp"].Tools)
	}
	if len(config["local_mcp"].Tools) != 1 || config["local_mcp"].Tools[0].API != "shared" {
		t.Fatalf("stdio tools=%#v", config["local_mcp"].Tools)
	}
}

func TestCurrentMCPConfigRejectsRemovedAndUnknownFields(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tool.js")
	writeTestFile(t, script, `({ok:true});`)
	for _, body := range []string{fmt.Sprintf(`{"tool":{"type":"api","script":%q},"m":{"type":"mcp","transports":["stdio"],"tools":["tool"]}}`, script), fmt.Sprintf(`{"tool":{"type":"api","script":%q},"m":{"type":"mcp","transport":"stdio","tools":["tool"],"resource":"https://example.test/m"}}`, script), fmt.Sprintf(`{"tool":{"type":"api","script":%q},"m":{"type":"mcp","transport":"stdio","tools":["tool"],"surprise":true}}`, script)} {
		if _, err := decodeAPIConfig([]byte(body), dir); err == nil {
			t.Fatalf("invalid MCP config accepted: %s", body)
		}
	}
}

func TestMCPConfigRejectsRemovedAdminUser(t *testing.T) {
	data := []byte(`{"server":{"type":"mcp","transport":"streamable_http","oauth":{"adminUser":"oauth/admin/users"}}}`)
	if _, err := decodeAPIConfig(data, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unknown OAuth field adminUser") {
		t.Fatalf("removed oauth.adminUser must be rejected explicitly: %v", err)
	}
}

func TestMCPStdioLifecycleAndPrincipal(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tool.js")
	writeTestFile(t, script, `({transport:nyanAllParams.mcp_principal.transport,user:nyanAllParams.mcp_principal.user_id});`)
	config := APIConfig{"tool": {Type: "api", Script: script}, "local": {Type: apiTypeMCP, Transport: "stdio", Tools: []MCPToolConfig{{Name: "tool", API: "tool"}}}}
	if err := validateMCPConfiguration(config); err != nil {
		t.Fatal(err)
	}
	snapshot := &APIConfigSnapshot{Config: config}
	input := strings.Join([]string{`{"jsonrpc":"2.0","id":1,"method":"ping"}`, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"tool","arguments":{}}}`}, "\n") + "\n"
	var output bytes.Buffer
	if err := serveMCPStdio(strings.NewReader(input), &output, snapshot, config["local"]); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("responses=%q", output.String())
	}
	if !strings.Contains(lines[0], `"code":-32002`) {
		t.Fatalf("pre-init response=%s", lines[0])
	}
	if !strings.Contains(lines[2], `\"transport\":\"stdio\"`) || !strings.Contains(lines[2], `\"user\":\"local-process\"`) {
		t.Fatalf("tool response=%s", lines[2])
	}
}

func TestMCPHTTPCanonicalAndQueryEndpoints(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tool.js")
	writeTestFile(t, script, `({ok:true});`)
	config := APIConfig{"tool": {Type: "api", Script: script}, "server": {Type: apiTypeMCP, Transport: "streamable_http", AllowedOrigins: []string{"https://chatgpt.com"}, Tools: []MCPToolConfig{{Name: "tool", API: "tool"}}}, "local": {Type: apiTypeMCP, Transport: "stdio", Tools: []MCPToolConfig{{Name: "tool", API: "tool"}}}}
	if err := validateMCPConfiguration(config); err != nil {
		t.Fatal(err)
	}
	old := currentAPISnapshot()
	publishAPISnapshot(&APIConfigSnapshot{Config: config})
	t.Cleanup(func() { publishAPISnapshot(old) })
	router := gin.New()
	router.Any("/", func(c *gin.Context) {
		if !dispatchMCPOrOAuth(c) {
			c.Status(http.StatusNotFound)
		}
	})
	router.NoRoute(func(c *gin.Context) {
		if !dispatchMCPOrOAuth(c) {
			c.Status(http.StatusNotFound)
		}
	})
	for _, target := range []string{"/server", "/?api=server"} {
		request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/local", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("stdio HTTP status=%d", response.Code)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// Regression coverage for request isolation and transport consistency.
func writeFixtureFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fixture")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func newRequestRegressionRouter(t *testing.T, config APIConfig) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	old, oldGlobal := currentAPISnapshot(), globalConfig
	publishAPISnapshot(&APIConfigSnapshot{Config: config})
	globalConfig = Config{}
	t.Cleanup(func() { publishAPISnapshot(old); globalConfig = oldGlobal })
	r := gin.New()
	r.POST("/nyan-rpc", handleJSONRPC)
	r.NoRoute(func(c *gin.Context) {
		if !dispatchMCPOrOAuth(c) && !dispatchDynamicEndpoint(c) {
			c.Status(404)
		}
	})
	return r
}

func serveMCPRegressionRequest(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "https://example.test/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", mcpProtocol20251125)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestMCPStdioAcceptsStandardInitialize(t *testing.T) {
	state := mcpStdioCreated
	mcp := EndpointConfig{Transport: "stdio", ProtocolVersions: []string{mcpProtocol20251125}}
	response := handleMCPStdioMessage(&APIConfigSnapshot{}, mcp, &state, []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`))
	encoded, _ := json.Marshal(response.Payload)
	if state != mcpStdioWaitingForInitialized {
		t.Fatalf("standard initialize rejected: %s", encoded)
	}
}

func completeTestOAuthConfiguration(t *testing.T, cfg APIConfig) {
	t.Helper()
	mcp := cfg["mcp"]
	mcp.AllowedOrigins = []string{"https://client.example"}
	mcp.RedirectURIAllowedPrefixes = []string{"https://client.example/callback/"}
	mcp.OAuth = MCPOAuthHooks{AuthorizationServerMetadata: "metadata", ProtectedResourceMetadataAPI: "resource_metadata", Authorize: "authorize", Token: "token", Register: "register", VerifyAccess: "verify"}
	cfg["metadata"], cfg["resource_metadata"] = EndpointConfig{}, EndpointConfig{}
	hook := cfg["verify"]
	hook.Scopes = []string{"read"}
	cfg["verify"] = hook
	for _, name := range []string{"authorize", "token", "register"} {
		cfg[name] = EndpointConfig{Script: hook.Script}
	}
	backing := cfg["tool"]
	backing.SecuritySchemes = []map[string]interface{}{{"type": "oauth2", "scopes": []string{"read"}}}
	cfg["tool"], cfg["mcp"] = backing, mcp
	if err := validateMCPConfiguration(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestNyanGetRequestHeadersReturnsIndependentCopies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		context    bool
		request    bool
		withHeader bool
	}{
		{name: "without_context"},
		{name: "without_request", context: true},
		{name: "empty_headers", context: true, request: true},
		{name: "multiple_header_values", context: true, request: true, withHeader: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requestContext *gin.Context
			if tc.context {
				requestContext, _ = gin.CreateTestContext(httptest.NewRecorder())
				if tc.request {
					requestContext.Request = httptest.NewRequest(http.MethodGet, "/", nil)
					if tc.withHeader {
						requestContext.Request.Header.Set("Origin", "https://allowed.example")
						requestContext.Request.Header.Add("X-Values", "first")
						requestContext.Request.Header.Add("X-Values", "second")
					}
				}
			}
			vm := setupGojaRuntimeWithContext(&APIConfigSnapshot{}, requestContext)
			value, err := vm.RunString(`
const first = nyanGetRequestHeaders();
first.Origin = "mutated";
delete first["X-Values"];
first["X-Injected"] = "injected";
JSON.stringify(nyanGetRequestHeaders());
`)
			if err != nil {
				t.Fatal(err)
			}
			var headers map[string]string
			if err := json.Unmarshal([]byte(value.String()), &headers); err != nil || headers == nil {
				t.Fatalf("headers must be an object of strings: value=%s error=%v", value.String(), err)
			}
			if !tc.withHeader {
				if len(headers) != 0 {
					t.Fatalf("request without headers returned %v", headers)
				}
				return
			}
			if len(headers) != 2 || headers["Origin"] != "https://allowed.example" || headers["X-Values"] != "first,second" {
				t.Fatalf("request header copy changed: %v", headers)
			}
			original := requestContext.Request.Header
			values := original.Values("X-Values")
			if len(original) != 2 || original.Get("Origin") != "https://allowed.example" || len(values) != 2 || values[0] != "first" || values[1] != "second" {
				t.Fatalf("JavaScript mutated the original HTTP headers: %v", original)
			}
		})
	}
}

func TestCookieRequestIsolation(t *testing.T) {
	enteredA, enteredB := make(chan struct{}), make(chan struct{})
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	abort := make(chan struct{})
	gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered, release := enteredA, releaseA
		if r.URL.Path == "/b" {
			entered, release = enteredB, releaseB
		}
		close(entered)
		select {
		case <-release:
		case <-abort:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(gate.Close)
	script := writeFixtureFile(t, `nyanGetAPI(nyanAllParams.gate,"",""); nyanGetCookie("session") + ":" + nyanGetRequestHeaders()["X-Request-Owner"];`)
	r := newRequestRegressionRouter(t, APIConfig{"who": {Script: script}})
	type result struct {
		status int
		body   string
	}
	var requests sync.WaitGroup
	// Registered after the router cleanup, so outstanding requests finish before
	// its snapshot and global configuration are restored, including on Fatal.
	t.Cleanup(func() {
		close(abort)
		done := make(chan struct{})
		go func() { requests.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("cookie isolation requests did not stop during cleanup")
		}
	})
	run := func(id string, done chan<- result) {
		defer requests.Done()
		req := httptest.NewRequest("POST", "/who", strings.NewReader(`{"gate":"`+gate.URL+`/`+id+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-Owner", id)
		req.AddCookie(&http.Cookie{Name: "session", Value: id})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		done <- result{status: rec.Code, body: rec.Body.String()}
	}
	waitResult := func(done <-chan result, label string) result {
		t.Helper()
		select {
		case got := <-done:
			return got
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for cookie request %s", label)
			return result{}
		}
	}
	doneA, doneB := make(chan result, 1), make(chan result, 1)
	requests.Add(1)
	go run("a", doneA)
	waitForRuntimeSignal(t, enteredA, "cookie request A entered gate")
	requests.Add(1)
	go run("b", doneB)
	waitForRuntimeSignal(t, enteredB, "cookie request B entered gate")
	close(releaseA)
	resultA := waitResult(doneA, "A")
	close(releaseB)
	resultB := waitResult(doneB, "B")
	if resultA.status != http.StatusOK || resultA.body != "a:a" || resultB.status != http.StatusOK || resultB.body != "b:b" {
		t.Fatalf("request A got %+v; request B got %+v", resultA, resultB)
	}
}

func TestWebSocketParamCheckUsesRequestOriginHeader(t *testing.T) {
	check := writeFixtureFile(t, `
const allowed = nyanGetRequestHeaders().Origin === "https://allowed.example";
({success:allowed,status:allowed ? 200 : 403,result:allowed ? null : "origin denied"});
`)
	html := writeFixtureFile(t, "PRIVATE_HTML")
	router := newRequestRegressionRouter(t, APIConfig{"private": {HTML: html, ParamCheck: check}})
	server := httptest.NewServer(router)
	defer server.Close()
	forgedQuery := url.Values{
		"Origin": {"https://allowed.example"}, "origin": {"https://allowed.example"},
		"headers":  {`{"Origin":"https://allowed.example"}`},
		"_headers": {`{"Origin":"https://allowed.example"}`}, "_headers_raw": {`{"Origin":["https://allowed.example"]}`},
	}.Encode()
	for _, tc := range []struct {
		name   string
		origin string
		query  string
		allow  bool
	}{
		{name: "allowed", origin: "https://allowed.example", allow: true},
		{name: "denied", origin: "https://untrusted.example"},
		{name: "missing"},
		{name: "forged_query_with_denied_header", origin: "https://untrusted.example", query: forgedQuery},
		{name: "forged_query_without_header", query: forgedQuery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{}
			if tc.origin != "" {
				headers.Set("Origin", tc.origin)
			}
			target := "ws" + strings.TrimPrefix(server.URL, "http") + "/private"
			if tc.query != "" {
				target += "?" + tc.query
			}
			conn, response, err := websocket.DefaultDialer.Dial(target, headers)
			if conn != nil {
				defer conn.Close()
			}
			if !tc.allow {
				if err == nil || conn != nil || response == nil || response.StatusCode != http.StatusForbidden {
					t.Fatalf("Origin rejection must precede upgrade: error=%v response=%v", err, response)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				assertParamCheckResponse(t, body, false, http.StatusForbidden)
				return
			}
			if err != nil || conn == nil {
				t.Fatalf("allowed Origin did not upgrade: error=%v response=%v", err, response)
			}
			conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"echo":"allowed"}`)); err != nil {
				t.Fatal(err)
			}
			if _, body, err := conn.ReadMessage(); err != nil || string(body) != `{"echo":"allowed"}` {
				t.Fatalf("allowed WebSocket did not echo: body=%s error=%v", body, err)
			}
		})
	}
}

func TestWebSocketChecksAuthorization(t *testing.T) {
	deny := writeFixtureFile(t, `({success:false,status:401,result:"denied"});`)
	secret := writeFixtureFile(t, "PRIVATE_HTML")
	r := newRequestRegressionRouter(t, APIConfig{"private": {HTML: secret, ParamCheck: deny}})
	normal := httptest.NewRecorder()
	r.ServeHTTP(normal, httptest.NewRequest("GET", "/private", nil))
	if normal.Code != http.StatusUnauthorized {
		t.Fatalf("HTTP baseline=%d", normal.Code)
	}
	server := httptest.NewServer(r)
	defer server.Close()
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/private", http.Header{"Origin": []string{"https://untrusted.example"}})
	if conn != nil {
		defer conn.Close()
	}
	if response != nil {
		defer response.Body.Close()
	}
	if err == nil || conn != nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("authorization must reject the upgrade: error=%v response=%v", err, response)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	assertParamCheckResponse(t, body, false, http.StatusUnauthorized)
}

func TestJSONRPCRejectsNonPublicAPIs(t *testing.T) {
	const target = "group/private"
	cases := []struct {
		name    string
		apiType string
		oauth   MCPOAuthHooks
		missing bool
	}{
		{name: "ws_client", apiType: apiTypeWSClient},
		{name: "public", apiType: apiTypePublic},
		{name: "schedule", apiType: apiTypeSchedule},
		{name: "mcp", apiType: apiTypeMCP},
		{name: "include", apiType: "include"},
		{name: "unknown_type", apiType: "unknown"},
		{name: "missing", missing: true},
		{name: "authorizationServerMetadata", oauth: MCPOAuthHooks{AuthorizationServerMetadata: target}},
		{name: "protectedResourceMetadata", oauth: MCPOAuthHooks{ProtectedResourceMetadataAPI: target}},
		{name: "authorize", oauth: MCPOAuthHooks{Authorize: target}},
		{name: "token", oauth: MCPOAuthHooks{Token: target}},
		{name: "register", oauth: MCPOAuthHooks{Register: target}},
		{name: "verifyAccess", oauth: MCPOAuthHooks{VerifyAccess: target}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "script-ran")
			check := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"check ran"); ({success:true,status:200,result:null});`, marker))
			script := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"script ran"); "PRIVATE_RESULT";`, marker))
			config := APIConfig{
				target:    {Type: tc.apiType, Script: script, ParamCheck: check, OutCheck: check, Push: "updates"},
				"updates": {Script: script},
				// An unrelated MCP definition must not hide roles in another definition.
				"mcp/first":  {Type: apiTypeMCP, Transport: "stdio"},
				"mcp/second": {Type: apiTypeMCP, Transport: "streamable_http", OAuth: tc.oauth},
			}
			if tc.missing {
				delete(config, target)
			}
			router := newRequestRegressionRouter(t, config)
			for _, mode := range []string{"", "checkOnly"} {
				body, err := json.Marshal(map[string]interface{}{
					"jsonrpc": "2.0", "id": "private-request", "method": target,
					"params": map[string]interface{}{
						"nyan_mode": mode, "api": "updates", "state_directory": dir,
						"oauth_hook": "caller-selected", "resource": "https://caller.example/mcp", "required_scopes": []string{},
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, "/nyan-rpc?api=updates", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				response := decodeJSONRPCCheckResponse(t, rec, `"private-request"`)
				var rpcError JSONRPCError
				if err := json.Unmarshal(response["error"], &rpcError); err != nil || rpcError.Code != -32601 || rpcError.Message != "Method not found" {
					t.Fatalf("private API was not rejected: body=%s error=%v", rec.Body.String(), err)
				}
				if _, exists := response["result"]; exists {
					t.Fatalf("private API returned a result: %s", rec.Body.String())
				}
				assertJSONRPCExecutionMarker(t, marker, false)
			}
		})
	}
}

func TestJSONRPCExposureUsesCurrentSnapshot(t *testing.T) {
	script := writeFixtureFile(t, `"ordinary API";`)
	router := newRequestRegressionRouter(t, APIConfig{})
	for _, tc := range []struct {
		name    string
		apiType string
		oauth   MCPOAuthHooks
		allowed bool
	}{
		{name: "implicit_api", allowed: true},
		{name: "explicit_api", apiType: "api", allowed: true},
		{name: "becomes_oauth_hook", apiType: "api", oauth: MCPOAuthHooks{VerifyAccess: "private"}},
		{name: "oauth_role_removed", apiType: "api", allowed: true},
		{name: "becomes_background_job", apiType: apiTypeWSClient},
		{name: "ordinary_api_restored", allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Hot reload publishes a new snapshot; reuse the same HTTP router.
			publishAPISnapshot(&APIConfigSnapshot{Config: APIConfig{
				"private": {Type: tc.apiType, Script: script, SecuritySchemes: []map[string]interface{}{{"type": "oauth2", "scopes": []string{"read"}}}},
				"mcp":     {Type: apiTypeMCP, OAuth: tc.oauth, Tools: []MCPToolConfig{{Name: "private", API: "private"}}},
			}})
			rec := serveJSONRPCCheckRequest(router, "42", `{}`)
			response := decodeJSONRPCCheckResponse(t, rec, "42")
			if tc.allowed {
				if string(response["result"]) != `"ordinary API"` || response["error"] != nil {
					t.Fatalf("ordinary API was blocked: %s", rec.Body.String())
				}
			} else {
				var rpcError JSONRPCError
				if err := json.Unmarshal(response["error"], &rpcError); err != nil || rpcError.Code != -32601 {
					t.Fatalf("stale exposure allowed a private API: %s", rec.Body.String())
				}
			}
		})
	}
}

func TestJSONRPCRequestAndScriptErrors(t *testing.T) {
	const request = `{"jsonrpc":"2.0","id":"request-42","method":"private","params":{}}`
	for _, tc := range []struct {
		name          string
		body          string
		query         string
		script        string
		withoutScript bool
		wantID        string
		wantCode      int
		wantMessage   string
		wantDetail    string
		wantMain      bool
	}{
		{name: "invalid_json", body: `{"jsonrpc":`, wantID: "null", wantCode: -32700, wantMessage: "Parse error"},
		{name: "invalid_version", body: `{"jsonrpc":"1.0","id":"request-42","method":"private","params":{}}`, wantCode: -32600, wantMessage: "Invalid Request: 'jsonrpc' must be '2.0'"},
		{name: "missing_script", withoutScript: true, wantCode: -32603, wantMessage: "No script defined for JSON-RPC API"},
		{name: "reserved_principal", body: `{"jsonrpc":"2.0","id":"request-42","method":"private","params":{"mcp_principal":null}}`, wantCode: -32602, wantMessage: "Invalid params", wantDetail: "reserved parameter mcp_principal"},
		{name: "reserved_tool_query", query: "?mcp_tool=forged", wantCode: -32602, wantMessage: "Invalid params", wantDetail: "reserved parameter mcp_tool"},
		{name: "script_exception", script: `throw new Error("main failed");`, wantCode: -32603, wantMessage: "Script execution error", wantDetail: "main failed", wantMain: true},
		{name: "invalid_base64", script: `({body:{encoding:"base64",data:"!"}});`, wantCode: -32603, wantMessage: "Invalid script response", wantDetail: "invalid base64 body", wantMain: true},
		{name: "cyclic_result", script: `const result = {}; result.self = result; result;`, wantCode: -32603, wantMessage: "Invalid script response", wantMain: true},
		{name: "nan_result", script: `NaN;`, wantCode: -32603, wantMessage: "Invalid script response", wantMain: true},
		{name: "infinite_result", script: `({value: Infinity});`, wantCode: -32603, wantMessage: "Invalid script response", wantMain: true},
		{name: "function_result", script: `(function () { return "not JSON"; });`, wantCode: -32603, wantMessage: "Invalid script response", wantMain: true},
		{name: "throwing_getter", script: `({get body() { throw new Error("getter failed"); }});`, wantCode: -32603, wantMessage: "Invalid script response", wantDetail: "getter failed", wantMain: true},
		{name: "throwing_array_conversion", script: `Array.prototype.toString = function () { throw new Error("conversion failed"); }; [1,2];`, wantCode: -32603, wantMessage: "Invalid script response", wantDetail: "conversion failed", wantMain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mainMarker, outMarker, pushMarker := filepath.Join(dir, "main-ran"), filepath.Join(dir, "out-ran"), filepath.Join(dir, "push-ran")
			script := tc.script
			if script == "" {
				script = `"PRIVATE_RESULT";`
			}
			config := EndpointConfig{
				OutCheck: writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({success:true,status:200,result:null});`, outMarker)),
				Push:     "updates",
			}
			if !tc.withoutScript {
				config.Script = writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); `, mainMarker)+script)
			}
			push := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); "pushed";`, pushMarker))
			router := newRequestRegressionRouter(t, APIConfig{"private": config, "updates": {Script: push}})
			body := tc.body
			if body == "" {
				body = request
			}
			req := httptest.NewRequest(http.MethodPost, "/nyan-rpc"+tc.query, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			wantID := tc.wantID
			if wantID == "" {
				wantID = `"request-42"`
			}
			response := decodeJSONRPCCheckResponse(t, rec, wantID)
			if _, exists := response["result"]; exists {
				t.Fatalf("failed request returned result: %s", rec.Body.String())
			}
			var rpcError JSONRPCError
			if err := json.Unmarshal(response["error"], &rpcError); err != nil {
				t.Fatalf("invalid error response: %s; error=%v", rec.Body.String(), err)
			}
			if rpcError.Code != tc.wantCode || rpcError.Message != tc.wantMessage {
				t.Fatalf("error = %#v, want code=%d message=%q", rpcError, tc.wantCode, tc.wantMessage)
			}
			if tc.wantDetail != "" && !strings.Contains(fmt.Sprint(rpcError.Data), tc.wantDetail) {
				t.Fatalf("error.data = %#v, want %q", rpcError.Data, tc.wantDetail)
			}
			assertJSONRPCExecutionMarker(t, mainMarker, tc.wantMain)
			assertJSONRPCExecutionMarker(t, outMarker, false)
			assertJSONRPCExecutionMarker(t, pushMarker, false)
		})
	}
}

func TestJSONRPCPreservesResultTypes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		want   string
	}{
		{name: "object", script: `({ok:true,count:3,nested:{items:[false,null,"value"]}});`, want: `{"ok":true,"count":3,"nested":{"items":[false,null,"value"]}}`},
		{name: "empty_object", script: `({});`, want: `{}`},
		{name: "array", script: `[1,"two",false,null,{ok:true}];`, want: `[1,"two",false,null,{"ok":true}]`},
		{name: "empty_array", script: `[];`, want: `[]`},
		{name: "zero", script: `0;`, want: `0`},
		{name: "integer", script: `123;`, want: `123`},
		{name: "fraction", script: `-1.25;`, want: `-1.25`},
		{name: "false", script: `false;`, want: `false`},
		{name: "true", script: `true;`, want: `true`},
		{name: "null", script: `null;`, want: `null`},
		{name: "undefined", script: `undefined;`, want: `null`},
		{name: "string", script: `"hello";`, want: `"hello"`},
		{name: "empty_string", script: `"";`, want: `""`},
		{name: "json_string", script: `JSON.stringify({ok:true});`, want: `"{\"ok\":true}"`},
		{name: "numeric_string", script: `"123";`, want: `"123"`},
		{name: "null_string", script: `"null";`, want: `"null"`},
		{name: "http_response_object", script: `({status:201,contentType:"application/json",headers:{"X-Test":"ok"},body:{ok:true}});`, want: `{"status":201,"contentType":"application/json","headers":{"X-Test":"ok"},"body":{"ok":true}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "push-ran")
			push := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); "pushed";`, marker))
			router := newRequestRegressionRouter(t, APIConfig{
				"private": {Script: writeFixtureFile(t, tc.script), Push: "updates"},
				"updates": {Script: push},
			})
			rec := serveJSONRPCCheckRequest(router, `"typed-result"`, `{}`)
			response := decodeJSONRPCCheckResponse(t, rec, `"typed-result"`)
			if _, exists := response["error"]; exists {
				t.Fatalf("successful script returned error: %s", rec.Body.String())
			}
			result, exists := response["result"]
			if !exists {
				t.Fatalf("success omitted result: %s", rec.Body.String())
			}
			var got, want interface{}
			if err := json.Unmarshal(result, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("result = %s, want %s", result, tc.want)
			}
			assertJSONRPCExecutionMarker(t, marker, true)
		})
	}
}

func TestJSONRPCStructuredResultOutCheckUsesSingleExport(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(fmt.Sprintf("allowed_%t", allowed), func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "push-ran")
			outCheck := writeFixtureFile(t, fmt.Sprintf(`
if (nyanAllParams.nyan_output_status !== 201 ||
    nyanAllParams.nyan_output_content_type !== "application/json" ||
    nyanAllParams.nyan_output.headers["X-Test"] !== "ok" ||
    JSON.parse(nyanAllParams.nyan_output_body).count !== 1) {
  throw new Error("outCheck input changed");
}
({success:%t,status:%d,result:"checked"});`, allowed, map[bool]int{true: 200, false: 403}[allowed]))
			router := newRequestRegressionRouter(t, APIConfig{
				"private": {
					// The getter must be evaluated once so the checked body and RPC result agree.
					Script:   writeFixtureFile(t, `let count = 0; ({status:201,contentType:"application/json",headers:{"X-Test":"ok"},get body() { return {count: ++count}; }});`),
					OutCheck: outCheck,
					Push:     "updates",
				},
				"updates": {Script: writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); "pushed";`, marker))},
			})
			rec := serveJSONRPCCheckRequest(router, "42", `{}`)
			if allowed {
				response := decodeJSONRPCCheckResponse(t, rec, "42")
				var result struct {
					Status int `json:"status"`
					Body   struct {
						Count int `json:"count"`
					} `json:"body"`
				}
				if err := json.Unmarshal(response["result"], &result); err != nil || result.Status != 201 || result.Body.Count != 1 || response["error"] != nil {
					t.Fatalf("outCheck altered structured result: %s; error=%v", rec.Body.String(), err)
				}
			} else {
				assertJSONRPCCheckError(t, rec, "42", "outCheck", false, http.StatusForbidden)
			}
			assertJSONRPCExecutionMarker(t, marker, allowed)
		})
	}
}

func TestJSONRPCOutCheck(t *testing.T) {
	script := writeFixtureFile(t, `"PRIVATE_RESULT";`)
	deny := writeFixtureFile(t, `({success:false,status:403,result:"denied"});`)
	r := newRequestRegressionRouter(t, APIConfig{"private": {Script: script, OutCheck: deny}})
	normal := httptest.NewRecorder()
	r.ServeHTTP(normal, httptest.NewRequest("GET", "/private", nil))
	if normal.Code != 403 {
		t.Fatalf("HTTP baseline=%d", normal.Code)
	}
	req := httptest.NewRequest("POST", "/nyan-rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"private","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	check := assertJSONRPCCheckError(t, rec, "1", "outCheck", false, http.StatusForbidden)
	if check.Result != "denied" {
		t.Fatalf("error.data.result = %#v, want denied", check.Result)
	}
	if strings.Contains(rec.Body.String(), "PRIVATE_RESULT") {
		t.Fatalf("outCheck bypassed: %s", rec.Body.String())
	}
}

func TestJSONRPCCheckRejections(t *testing.T) {
	cases := []struct {
		name        string
		script      string
		id          string
		wantSuccess bool
		wantStatus  int
		wantResult  string
		wantMessage string
		checkOnly   bool
	}{
		{name: "denied", script: `({success:false,status:403,result:{reason:"denied"}});`, id: "9007199254740993", wantStatus: 403, wantResult: `{"reason":"denied"}`},
		{name: "false_with_200", script: `({success:false,status:200,result:"denied"});`, id: `"request-42"`, wantStatus: 200, wantResult: `"denied"`},
		{name: "true_with_non_200", script: `({success:true,status:201,result:["denied",3]});`, id: "null", wantSuccess: true, wantStatus: 201, wantResult: `["denied",3]`},
		{name: "json_string", script: `JSON.stringify({success:false,status:401,result:null});`, id: "0", wantStatus: 401, wantResult: "null"},
		{name: "exception", script: `throw new Error("check failed");`, id: "42", wantStatus: 500, wantMessage: "check failed"},
		{name: "malformed_object", script: `({status:403,result:"denied"});`, id: `"request-42"`, wantStatus: 500, wantMessage: "response success must be boolean"},
		{name: "invalid_status", script: `({success:false,status:700,result:"denied"});`, id: "null", wantStatus: 500, wantMessage: "response status is out of range"},
		{name: "undefined", script: `undefined;`, id: "42", wantStatus: 500, wantMessage: "must return an object"},
		{name: "check_only_denied", script: `({success:false,status:403,result:"denied"});`, id: `"check-only"`, wantStatus: 403, wantResult: `"denied"`, checkOnly: true},
	}
	for _, checkName := range []string{"paramCheck", "outCheck"} {
		t.Run(checkName, func(t *testing.T) {
			for _, tc := range cases {
				if checkName == "outCheck" && tc.checkOnly {
					continue
				}
				t.Run(tc.name, func(t *testing.T) {
					dir := t.TempDir()
					mainMarker := filepath.Join(dir, "main-ran")
					outMarker := filepath.Join(dir, "out-ran")
					pushMarker := filepath.Join(dir, "push-ran")
					script := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); "PRIVATE_RESULT";`, mainMarker))
					push := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); "pushed";`, pushMarker))
					paramScript, outScript := `({success:true,status:200,result:null});`, `({success:true,status:200,result:null});`
					if checkName == "paramCheck" {
						paramScript = tc.script
					} else {
						outScript = tc.script
					}
					config := EndpointConfig{
						Script:     script,
						ParamCheck: writeFixtureFile(t, paramScript),
						OutCheck:   writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); `, outMarker)+outScript),
						Push:       "updates",
					}
					router := newRequestRegressionRouter(t, APIConfig{"private": config, "updates": {Script: push}})
					params := `{}`
					if tc.checkOnly {
						params = `{"nyan_mode":"checkOnly"}`
					}
					rec := serveJSONRPCCheckRequest(router, tc.id, params)
					check := assertJSONRPCCheckError(t, rec, tc.id, checkName, tc.wantSuccess, tc.wantStatus)
					if tc.wantMessage != "" {
						result, ok := check.Result.(map[string]interface{})
						if !ok || !strings.Contains(fmt.Sprint(result["message"]), tc.wantMessage) {
							t.Fatalf("error.data.result = %#v, want message containing %q", check.Result, tc.wantMessage)
						}
					} else if result, err := json.Marshal(check.Result); err != nil || string(result) != tc.wantResult {
						t.Fatalf("error.data.result = %s, want %s; error=%v", result, tc.wantResult, err)
					}
					assertJSONRPCExecutionMarker(t, mainMarker, checkName == "outCheck")
					assertJSONRPCExecutionMarker(t, outMarker, checkName == "outCheck")
					assertJSONRPCExecutionMarker(t, pushMarker, false)
					if strings.Contains(rec.Body.String(), "PRIVATE_RESULT") {
						t.Fatalf("rejected response leaked main script output: %s", rec.Body.String())
					}
				})
			}
		})
	}
}

func TestJSONRPCCheckSuccess(t *testing.T) {
	for _, tc := range []struct {
		name           string
		id             string
		checkOnly      bool
		withParamCheck bool
	}{
		{name: "full_request", id: "9007199254740993", withParamCheck: true},
		{name: "check_only_with_check", id: `"check-only"`, checkOnly: true, withParamCheck: true},
		{name: "check_only_without_check", id: "null", checkOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mainMarker := filepath.Join(dir, "main-ran")
			paramMarker := filepath.Join(dir, "param-ran")
			outMarker := filepath.Join(dir, "out-ran")
			pushMarker := filepath.Join(dir, "push-ran")
			config := EndpointConfig{
				Script: writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); "PRIVATE_RESULT";`, mainMarker)),
				OutCheck: writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran");
if (nyanAllParams.nyan_output_body !== "PRIVATE_RESULT") { throw new Error("unexpected output"); }
({success:true,status:200,result:null});`, outMarker)),
				Push: "updates",
			}
			if tc.withParamCheck {
				config.ParamCheck = writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); ({success:true,status:200,result:{checked:"yes"}});`, paramMarker))
			}
			push := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); "pushed";`, pushMarker))
			router := newRequestRegressionRouter(t, APIConfig{"private": config, "updates": {Script: push}})
			params := `{}`
			if tc.checkOnly {
				params = `{"nyan_mode":"checkOnly"}`
			}
			rec := serveJSONRPCCheckRequest(router, tc.id, params)
			response := decodeJSONRPCCheckResponse(t, rec, tc.id)
			if _, exists := response["error"]; exists {
				t.Fatalf("success has error: %s", rec.Body.String())
			}
			if tc.checkOnly {
				var check ParamCheckResponse
				if err := json.Unmarshal(response["result"], &check); err != nil {
					t.Fatalf("invalid checkOnly result: %s; error=%v", rec.Body.String(), err)
				}
				if !check.Success || check.Status != http.StatusOK {
					t.Fatalf("checkOnly result = %#v", check)
				}
				wantResult := "null"
				if tc.withParamCheck {
					wantResult = `{"checked":"yes"}`
				}
				if result, err := json.Marshal(check.Result); err != nil || string(result) != wantResult {
					t.Fatalf("checkOnly result.result = %s, want %s; error=%v", result, wantResult, err)
				}
			} else if string(response["result"]) != `"PRIVATE_RESULT"` {
				t.Fatalf("result = %s, want PRIVATE_RESULT", response["result"])
			}
			assertJSONRPCExecutionMarker(t, paramMarker, tc.withParamCheck)
			assertJSONRPCExecutionMarker(t, mainMarker, !tc.checkOnly)
			assertJSONRPCExecutionMarker(t, outMarker, !tc.checkOnly)
			assertJSONRPCExecutionMarker(t, pushMarker, !tc.checkOnly)
		})
	}
}

func serveJSONRPCCheckRequest(router *gin.Engine, id, params string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/nyan-rpc", strings.NewReader(`{"jsonrpc":"2.0","id":`+id+`,"method":"private","params":`+params+`}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeJSONRPCCheckResponse(t *testing.T, rec *httptest.ResponseRecorder, id string) map[string]json.RawMessage {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON-RPC response: %s; error=%v", rec.Body.String(), err)
	}
	if string(response["jsonrpc"]) != `"2.0"` || string(response["id"]) != id {
		t.Fatalf("JSON-RPC version or id changed: %s, want id=%s", rec.Body.String(), id)
	}
	return response
}

func assertJSONRPCCheckError(t *testing.T, rec *httptest.ResponseRecorder, id, checkName string, success bool, status int) ParamCheckResponse {
	t.Helper()
	response := decodeJSONRPCCheckResponse(t, rec, id)
	if _, exists := response["result"]; exists {
		t.Fatalf("error response has top-level result: %s", rec.Body.String())
	}
	var rpcError struct {
		Code    int                `json:"code"`
		Message string             `json:"message"`
		Data    ParamCheckResponse `json:"data"`
	}
	if err := json.Unmarshal(response["error"], &rpcError); err != nil {
		t.Fatalf("invalid JSON-RPC error: %s; error=%v", rec.Body.String(), err)
	}
	if rpcError.Code != -32000 || rpcError.Message != checkName+" rejected" {
		t.Fatalf("JSON-RPC error = %#v", rpcError)
	}
	if rpcError.Data.Success != success || rpcError.Data.Status != status {
		t.Fatalf("error.data = %#v, want success=%v status=%d", rpcError.Data, success, status)
	}
	return rpcError.Data
}

func assertJSONRPCExecutionMarker(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if got := err == nil; got != want {
		t.Fatalf("script execution marker %s exists=%v, want %v", filepath.Base(path), got, want)
	}
}

func TestOAuthVerifierFailsClosed(t *testing.T) {
	for _, body := range []string{`false;`, `({status:401,body:{error:"invalid_token"}});`} {
		t.Run(body, func(t *testing.T) {
			hook := writeFixtureFile(t, body)
			tool := writeFixtureFile(t, `({ok:true});`)
			cfg := APIConfig{"tool": {Script: tool}, "verify": {Script: hook}, "mcp": {Type: "mcp", Transport: "streamable_http", ProtocolVersions: []string{mcpProtocol20251125}, OAuth: MCPOAuthHooks{VerifyAccess: "verify"}, Tools: []MCPToolConfig{{Name: "tool", API: "tool", InputSchema: map[string]interface{}{"type": "object"}}}}}
			completeTestOAuthConfiguration(t, cfg)
			r := newRequestRegressionRouter(t, cfg)
			rec := serveMCPRegressionRequest(r, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"tool","arguments":{}}}`)
			if rec.Code != 401 {
				t.Fatalf("verifier denial did not block tool: status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestRawAPICannotForgeMCPPrincipal(t *testing.T) {
	hook := writeFixtureFile(t, `({authenticated:false});`)
	marker := filepath.Join(t.TempDir(), "tool-ran")
	tool := writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q, "ran"); ({status:200,contentType:"application/json",body:{user:nyanAllParams.mcp_principal.user_id}});`, marker))
	cfg := APIConfig{"tool": {Script: tool}, "verify": {Script: hook}, "mcp": {Type: "mcp", Transport: "streamable_http", ProtocolVersions: []string{mcpProtocol20251125}, OAuth: MCPOAuthHooks{VerifyAccess: "verify"}, Tools: []MCPToolConfig{{Name: "tool", API: "tool", InputSchema: map[string]interface{}{"type": "object"}}}}}
	completeTestOAuthConfiguration(t, cfg)
	r := newRequestRegressionRouter(t, cfg)
	mcp := serveMCPRegressionRequest(r, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"tool","arguments":{}}}`)
	if mcp.Code != 401 {
		t.Fatalf("MCP baseline=%d", mcp.Code)
	}
	req := httptest.NewRequest("POST", "/tool", strings.NewReader(`{"mcp_principal":{"user_id":"forged-admin"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "forged-admin") {
		t.Fatalf("raw API accepted client-supplied principal: %d %s", rec.Code, rec.Body.String())
	}
	assertJSONRPCExecutionMarker(t, marker, false)
}

func isolateTestStorage(t *testing.T) {
	t.Helper()
	storageMu.Lock()
	oldStorage := storage
	storage = make(map[string]string)
	storageMu.Unlock()
	t.Cleanup(func() {
		storageMu.Lock()
		storage = oldStorage
		storageMu.Unlock()
	})
}

func TestRemoveItemSharesStorageAcrossRuntimes(t *testing.T) {
	isolateTestStorage(t)
	vmA, vmB := setupGojaRuntime(), setupGojaRuntime()
	if _, err := vmA.RunString(`
		nyanSetItem("target", "remove");
		nyanSetItem("keep", "preserve");
		nyanSetItem("undefined", "preserve undefined");
		nyanSetItem(7, "numeric");
		nyanSetItem("", "empty");
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := vmB.RunString(`
		if (nyanGetItem("target") !== "remove" || nyanGetItem("7") !== "numeric" || nyanGetItem("") !== "empty") {
			throw new Error("values are not shared between runtimes");
		}
		for (const key of ["target", "target", "missing", 7, ""]) {
			if (nyanRemoveItem(key) !== null || nyanGetItem(key) !== null) {
				throw new Error("removal did not return null and remove key: " + key);
			}
		}
		if (nyanRemoveItem() !== null) {
			throw new Error("removal without a key did not return null");
		}
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := vmA.RunString(`
		for (const key of ["target", "7", ""]) {
			if (nyanGetItem(key) !== null) {
				throw new Error("removal is not visible in the original runtime: " + key);
			}
		}
		if (nyanGetItem("keep") !== "preserve" || nyanGetItem("undefined") !== "preserve undefined") {
			throw new Error("removal changed an unrelated key");
		}
	`); err != nil {
		t.Fatal(err)
	}
}

func TestStorageConcurrency(t *testing.T) {
	isolateTestStorage(t)
	vmA, vmB, vmC := setupGojaRuntime(), setupGojaRuntime(), setupGojaRuntime()
	start := make(chan struct{})
	results := make(chan error, 3)
	go func() {
		<-start
		_, err := vmA.RunString(`for (let i = 0; i < 100; i++) nyanSetItem("key", "value-" + i);`)
		results <- err
	}()
	go func() {
		<-start
		_, err := vmB.RunString(`for (let i = 0; i < 100; i++) nyanGetItem("key");`)
		results <- err
	}()
	go func() {
		<-start
		_, err := vmC.RunString(`for (let i = 0; i < 100; i++) nyanRemoveItem("key");`)
		results <- err
	}()
	close(start)
	for range 3 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if _, err := vmA.RunString(`nyanSetItem("key", "final");`); err != nil {
		t.Fatal(err)
	}
	value, err := vmB.RunString(`nyanGetItem("key");`)
	if err != nil || value.String() != "final" {
		t.Fatalf("stored value = %v, error = %v", value, err)
	}
	if _, err := vmC.RunString(`nyanRemoveItem("key");`); err != nil {
		t.Fatal(err)
	}
	value, err = vmA.RunString(`nyanGetItem("key") === null;`)
	if err != nil || !value.ToBoolean() {
		t.Fatalf("removed key was retained: result = %v, error = %v", value, err)
	}
}

func TestWebSocketChecksTargetAndPreservesPublicMessages(t *testing.T) {
	publicHTML := writeFixtureFile(t, "PUBLIC_HTML")
	privateHTML := writeFixtureFile(t, "PRIVATE_HTML")
	denyInput := writeFixtureFile(t, `({success:false,status:401,result:"denied"});`)
	denyOutput := writeFixtureFile(t, `({success:false,status:403,result:"denied"});`)
	router := newRequestRegressionRouter(t, APIConfig{
		"public":   {HTML: publicHTML},
		"private":  {HTML: privateHTML, ParamCheck: denyInput},
		"filtered": {HTML: privateHTML, OutCheck: denyOutput},
	})
	server := httptest.NewServer(router)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/public", http.Header{"Origin": []string{"https://existing-client.example"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for _, test := range []struct{ message, want string }{
		{`{"echo":"hello"}`, `{"echo":"hello"}`},
		{`{"api":"public"}`, "PUBLIC_HTML"},
		{`{"api":"private"}`, `"status":401`},
		{`{"api":"filtered"}`, `"status":403`},
	} {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(test.message)); err != nil {
			t.Fatal(err)
		}
		_, body, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), test.want) || strings.Contains(string(body), "PRIVATE_HTML") {
			t.Fatalf("message %s returned %s, expected %s", test.message, body, test.want)
		}
	}
}

func TestWebSocketConcurrentPushAndReplies(t *testing.T) {
	html := writeFixtureFile(t, "PUSH")
	router := newRequestRegressionRouter(t, APIConfig{"updates": {HTML: html}})
	server := httptest.NewServer(router)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/updates", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	// An echo round trip ensures the connection is registered before pushing.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	const count = 32
	snapshot := currentAPISnapshot()
	var writers sync.WaitGroup
	writers.Add(3)
	for i := 0; i < 2; i++ {
		go func() {
			defer writers.Done()
			for j := 0; j < count; j++ {
				performPushWithSnapshot(snapshot, EndpointConfig{Push: "updates"}, nil)
			}
		}()
	}
	writeErrors := make(chan error, 1)
	go func() {
		defer writers.Done()
		for j := 0; j < count; j++ {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"echo":"true"}`)); err != nil {
				writeErrors <- err
				return
			}
		}
		writeErrors <- nil
	}()
	writersDone := make(chan struct{})
	go func() { writers.Wait(); close(writersDone) }()
	t.Cleanup(func() {
		// A read/assertion failure must not leave push workers using the global
		// snapshot after the router cleanup restores it.
		_ = conn.Close()
		select {
		case <-writersDone:
		case <-time.After(3 * time.Second):
			t.Error("WebSocket writers did not stop during cleanup")
		}
	})
	received := map[string]int{}
	for i := 0; i < count*3; i++ {
		_, body, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		received[string(body)]++
	}
	waitForRuntimeSignal(t, writersDone, "WebSocket writers")
	if err := <-writeErrors; err != nil {
		t.Fatal(err)
	}
	if received["PUSH"] != count*2 || received[`{"echo":"true"}`] != count {
		t.Fatalf("lost or corrupted WebSocket messages: %v", received)
	}
}

func TestNestedJavaScriptPreservesRequestCookies(t *testing.T) {
	target := writeFixtureFile(t, `nyanSetCookie("result",nyanGetCookie("session")); nyanGetCookie("session") + ":" + nyanGetRequestHeaders()["X-Request-Owner"];`)
	caller := writeFixtureFile(t, `nyanCallMe({api:"target"});`)
	router := newRequestRegressionRouter(t, APIConfig{"caller": {Script: caller}, "target": {Script: target}})
	req := httptest.NewRequest("GET", "https://example.test/caller", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "request-owner"})
	req.Header.Set("X-Request-Owner", "header-owner")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if rec.Body.String() != "request-owner:header-owner" || len(cookies) != 1 || cookies[0].Value != "request-owner" || !cookies[0].Secure {
		t.Fatalf("nested request lost its cookie or header context: body=%s cookies=%v", rec.Body.String(), cookies)
	}
	value, err := setupGojaRuntime().RunString(`nyanGetCookie("session");`)
	if err != nil || value.String() != "" {
		t.Fatalf("background VM inherited HTTP cookies: value=%v err=%v", value, err)
	}
}

func TestMCPStdioToolReportsErrorsAndHTTPIncludesAPIName(t *testing.T) {
	script := writeFixtureFile(t, `({status:403,contentType:"application/json",body:{api:nyanAllParams.api,error:"denied"}});`)
	config := APIConfig{"tool": {Script: script}, "mcp": {Type: apiTypeMCP, Transport: "streamable_http", AllowedOrigins: []string{"https://client.example"}, Tools: []MCPToolConfig{{Name: "tool", API: "tool"}}}}
	if err := validateMCPConfiguration(config); err != nil {
		t.Fatal(err)
	}
	router := newRequestRegressionRouter(t, config)
	rec := serveMCPRegressionRequest(router, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"tool","arguments":{}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("MCP HTTP status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var httpResult struct {
		JSONRPC string                 `json:"jsonrpc"`
		ID      int                    `json:"id"`
		Error   json.RawMessage        `json:"error"`
		Result  map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &httpResult); err != nil {
		t.Fatal(err)
	}
	if httpResult.JSONRPC != "2.0" || httpResult.ID != 1 || len(httpResult.Error) != 0 {
		t.Fatalf("unexpected MCP envelope: %s", rec.Body.String())
	}
	payload, errText := executeMCPTool(currentAPISnapshot(), config["mcp"].Tools[0], nil, map[string]interface{}{"transport": "stdio"})
	if errText != "" || payload["isError"] != true || httpResult.Result["isError"] != true {
		t.Fatalf("error conversion: HTTP=%s stdio=%v err=%s", rec.Body.String(), payload, errText)
	}
	for _, result := range []map[string]interface{}{payload, httpResult.Result} {
		structured, ok := result["structuredContent"].(map[string]interface{})
		if !ok || structured["api"] != "tool" {
			t.Fatalf("Tool API identity missing: %v", result)
		}
	}
}

func decodeLogObject(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var record map[string]interface{}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("invalid JSON log %q: %v", data, err)
	}
	return record
}
func writeLoggingFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func captureServiceLogs(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	writer, flags, prefix, previousLevel := log.Writer(), log.Flags(), log.Prefix(), serviceLogLevel.Level()
	var output bytes.Buffer
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")
	serviceLogLevel.Set(level)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
		serviceLogLevel.Set(previousLevel)
	})
	return &output
}

func TestServiceLoggingLevelsAndErrorPrivacy(t *testing.T) {
	output := captureServiceLogs(t, slog.LevelInfo)
	secretError := errors.New("password=private-error\nforged log line")
	serviceLog(slog.LevelDebug, "hidden_debug_event")
	logServiceError(slog.LevelError, "execution_failed", secretError, "api", "example\napi")
	if strings.Contains(output.String(), "private-error") || strings.Contains(output.String(), "hidden_debug_event") {
		t.Fatalf("info log contains debug data: %s", output.String())
	}
	if bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("log injection produced extra lines: %s", output.String())
	}
	record := decodeLogObject(t, output.Bytes())
	if record["msg"] != "execution_failed" || record["level"] != "ERROR" || record["error_type"] == nil || record["api"] != "example\napi" {
		t.Fatalf("missing diagnostic metadata: %#v", record)
	}
	output.Reset()
	serviceLogLevel.Set(slog.LevelDebug)
	logServiceError(slog.LevelError, "execution_failed", secretError)
	record = decodeLogObject(t, output.Bytes())
	if record["error_detail"] != secretError.Error() {
		t.Fatalf("debug log missing error detail: %#v", record)
	}
	output.Reset()
	serviceLogLevel.Set(slog.LevelError)
	serviceLog(slog.LevelInfo, "hidden_info")
	serviceLog(slog.LevelWarn, "hidden_warning")
	if output.Len() != 0 {
		t.Fatalf("error level emitted lower levels: %s", output.String())
	}
}

func TestSetupLoggerRejectsInvalidLevelWithoutChangingOutput(t *testing.T) {
	output := captureServiceLogs(t, slog.LevelInfo)
	previous := globalConfig.Log
	t.Cleanup(func() { globalConfig.Log = previous })
	globalConfig.Log = LogConfig{Level: "verbose"}
	if err := setupLogger(t.TempDir()); err == nil {
		t.Fatal("invalid log.Level was accepted")
	}
	if log.Writer() != output || serviceLogLevel.Level() != slog.LevelInfo {
		t.Fatal("invalid configuration partially changed the logger")
	}
}

func TestWebSocketLoggingPrivacyAndNormalClose(t *testing.T) {
	output := captureServiceLogs(t, slog.LevelInfo)
	endpoint := "wss://user:private-password@example.com/private-path?token=private-token#private-fragment"
	serviceLog(slog.LevelInfo, "ws_client_starting", "origin", logURLOrigin(endpoint))
	record := decodeLogObject(t, output.Bytes())
	if record["origin"] != "wss://example.com" {
		t.Fatalf("URL was not sanitized: %#v", record)
	}
	output.Reset()
	logWebSocketDisconnect("ws_client_disconnected", "example", fmt.Errorf("read: %w", &websocket.CloseError{Code: 1000, Text: "private-close-text"}))
	if output.Len() != 0 {
		t.Fatalf("normal close logged as a warning: %s", output.String())
	}
	logWebSocketDisconnect("ws_client_disconnected", "example", &websocket.CloseError{Code: 1006, Text: "private-close-text"})
	record = decodeLogObject(t, output.Bytes())
	if record["level"] != "WARN" || record["close_code"] != float64(1006) || strings.Contains(output.String(), "private-close-text") {
		t.Fatalf("unexpected disconnect log: %#v", record)
	}
}

func TestMCPStdioProcessLogging(t *testing.T) {
	dir := t.TempDir()
	writeLoggingFile(t, filepath.Join(dir, "tool.js"), `console.log("stdio-console-marker"); ({ok:true})`)
	writeLoggingFile(t, filepath.Join(dir, "api.json"), `{"tool":{"script":"tool.js"},"mcp":{"type":"mcp","transport":"stdio","tools":["tool"]}}`)
	input := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-11-25\"}}\n" +
		"{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n" +
		"{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"tool\",\"arguments\":{}}}\n"
	for _, tc := range []struct {
		name, level string
		file        bool
	}{
		{name: "default_stderr"},
		{name: "debug_stderr", level: "debug"},
		{name: "rotating_file", file: true},
		{name: "debug_rotating_file", level: "debug", file: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logFile := filepath.Join(dir, tc.name+".log")
			cfg, err := json.Marshal(Config{Log: LogConfig{EnableLogging: tc.file, Filename: logFile, Level: tc.level}})
			if err != nil {
				t.Fatal(err)
			}
			configFile := filepath.Join(dir, tc.name+".json")
			writeLoggingFile(t, configFile, string(cfg))
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPStdioLoggingHelper$", "--", "--config", configFile, "--api", filepath.Join(dir, "api.json"), "--mcp-server", "mcp")
			cmd.Env = append(os.Environ(), "NYANPUI_TEST_STDIO_LOGGING_CHILD=1")
			cmd.Stdin = strings.NewReader(input)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("stdio process failed: %v; stderr=%s", err, stderr.String())
			}
			lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
			if len(lines) != 2 {
				t.Fatalf("stdout contains non-protocol data: %s", stdout.String())
			}
			for _, line := range lines {
				record := decodeLogObject(t, []byte(line))
				if record["jsonrpc"] != "2.0" || record["error"] != nil {
					t.Fatalf("invalid protocol response: %s", line)
				}
			}
			logs := stderr.Bytes()
			if tc.file {
				if len(logs) != 0 {
					t.Fatalf("file logging also wrote stderr: %s", logs)
				}
				logs, err = os.ReadFile(logFile)
				if err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Contains(logs, []byte("mcp_stdio_starting")) {
				t.Fatalf("missing startup diagnostics: %s", logs)
			}
			for _, line := range bytes.Split(bytes.TrimSpace(logs), []byte("\n")) {
				record := decodeLogObject(t, line)
				if record["msg"] == "mcp_stdio_starting" || record["msg"] == "mcp_stdio_stopped" {
					if record["api"] != "mcp" || record["server"] != nil {
						t.Fatalf("unexpected MCP log fields: %#v", record)
					}
				}
			}
			if bytes.Contains(logs, []byte("stdio-console-marker")) != (tc.level == "debug") {
				t.Fatalf("unexpected console logging: %s", logs)
			}
		})
	}
}

func TestMCPStdioLoggingHelper(t *testing.T) {
	if os.Getenv("NYANPUI_TEST_STDIO_LOGGING_CHILD") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
			main()
			os.Exit(0) // Do not let the test runner write PASS to protocol stdout.
		}
	}
	os.Exit(2)
}

func TestSetupLoggerLevelsAndStderr(t *testing.T) {
	captureServiceLogs(t, slog.LevelInfo)
	previous := globalConfig.Log
	t.Cleanup(func() { globalConfig.Log = previous })
	for _, tc := range []struct {
		input string
		want  slog.Level
	}{
		{"", slog.LevelInfo}, {"info", slog.LevelInfo}, {" DEBUG ", slog.LevelDebug}, {"warn", slog.LevelWarn}, {"error", slog.LevelError},
	} {
		globalConfig.Log = LogConfig{Level: tc.input}
		if err := setupLogger(t.TempDir()); err != nil {
			t.Fatal(err)
		}
		if log.Writer() != os.Stderr || serviceLogLevel.Level() != tc.want || log.Flags() != 0 || log.Prefix() != "" {
			t.Fatalf("unexpected logger configuration for %q", tc.input)
		}
	}
}

func TestScriptConsoleAndErrorLogging(t *testing.T) {
	output := captureServiceLogs(t, slog.LevelInfo)
	previous := globalConfig
	globalConfig = Config{}
	t.Cleanup(func() { globalConfig = previous })
	script := writeFixtureFile(t, `console.log("private-console\nsecond line", {value: 1}); "private-result"`)
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelDebug} {
		output.Reset()
		serviceLogLevel.Set(level)
		result, err := runJavaScript(script, "", nil)
		if err != nil || result != "private-result" {
			t.Fatalf("script result changed: %q, %v", result, err)
		}
		if level == slog.LevelInfo {
			if output.Len() != 0 {
				t.Fatalf("info exposed console: %s", output.String())
			}
		} else {
			record := decodeLogObject(t, output.Bytes())
			if record["message"] != "private-console\nsecond line {\"value\":1}" || bytes.Count(output.Bytes(), []byte("\n")) != 1 {
				t.Fatalf("unexpected console record: %s", output.String())
			}
		}
	}
	output.Reset()
	vm := setupGojaRuntimeWithSnapshot(nil)
	if _, err := vm.RunString(`console.log("x".repeat(10000))`); err != nil {
		t.Fatal(err)
	}
	message := decodeLogObject(t, output.Bytes())["message"].(string)
	if message != strings.Repeat("x", 4096)+"...[truncated]" {
		t.Fatal("console text was not bounded")
	}
	output.Reset()
	logServiceError(slog.LevelError, "bounded_error", errors.New(strings.Repeat("x", 10000)))
	if decodeLogObject(t, output.Bytes())["error_detail"] != message {
		t.Fatal("error detail was not bounded")
	}
	output.Reset()
	serviceLogLevel.Set(slog.LevelInfo)
	// Disabled console must not serialize objects (which could run a user toJSON hook).
	if _, err := vm.RunString(`console.log({toJSON() { throw new Error("must not run") }});`); err != nil {
		t.Fatal(err)
	}
	script = writeFixtureFile(t, `throw new Error("private-script-error")`)
	if _, err := runJavaScript(script, "", nil); err == nil {
		t.Fatal("script failure was swallowed")
	}
	record := decodeLogObject(t, output.Bytes())
	if record["msg"] != "script_failed" || strings.Contains(output.String(), "private-script-error") {
		t.Fatalf("unsafe script error log: %s", output.String())
	}
}

func TestHTTPLoggingPrivacyAndRecovery(t *testing.T) {
	output := captureServiceLogs(t, slog.LevelInfo)
	router := gin.New()
	router.Use(serviceRecovery())
	router.GET("/items/:id", func(c *gin.Context) { c.String(200, "private-response") })
	router.GET("/panic", func(c *gin.Context) { panic("private-panic") })
	router.GET("/broken", func(c *gin.Context) {
		panic(&net.OpError{Op: "write", Err: &os.SyscallError{Syscall: "write", Err: syscall.EPIPE}})
	})
	router.GET("/failure", func(c *gin.Context) { _ = c.Error(errors.New("private-error")); c.Status(400) })
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/items/private-id?token=private-query", 200},
		{"/private-unknown?token=private-query", 404},
		{"/panic", 500},
		{"/broken", 200}, // Gin's existing broken-pipe behavior.
		{"/failure", 400},
	} {
		output.Reset()
		req := httptest.NewRequest("GET", tc.path, strings.NewReader("private-body"))
		req.Header.Set("Authorization", "Bearer private-auth")
		req.Header.Set("Cookie", "session=private-cookie")
		req.Header.Set("X-API-Key", "private-key")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("response changed for %s: %d", tc.path, rec.Code)
		}
		if strings.Contains(output.String(), "private-") {
			t.Fatalf("sensitive data in log: %s", output.String())
		}
		if tc.path == "/panic" {
			record := decodeLogObject(t, output.Bytes())
			if record["msg"] != "http_panic" || record["route"] != "/panic" || record["level"] != "ERROR" {
				t.Fatalf("unexpected panic metadata: %#v", record)
			}
		} else if output.Len() != 0 {
			t.Fatalf("unexpected access log: %s", output.String())
		}
	}
}

func TestFileLoggingAppendsAndRotatesHTTPAndServiceEvents(t *testing.T) {
	captureServiceLogs(t, slog.LevelInfo)
	previous := globalConfig.Log
	t.Cleanup(func() { globalConfig.Log = previous })
	dir := t.TempDir()
	filename := filepath.Join(dir, "service.log")
	writeLoggingFile(t, filename, "{\"msg\":\"existing_log\"}\n")
	globalConfig.Log = LogConfig{EnableLogging: true, Filename: "service.log", MaxSize: 1, MaxBackups: 3}
	if err := setupLogger(dir); err != nil {
		t.Fatal(err)
	}
	rotating, ok := log.Writer().(*lumberjack.Logger)
	if !ok {
		t.Fatal("file logger is not rotating")
	}
	t.Cleanup(func() { rotating.Close() })
	router := gin.New()
	router.Use(serviceRecovery())
	router.GET("/before", func(c *gin.Context) { panic("private-before") })
	router.GET("/after", func(c *gin.Context) { panic("private-after") })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/before", nil))
	for range 150 {
		serviceLog(slog.LevelInfo, "rotation_padding", "padding", strings.Repeat("x", 8000))
	}
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/after", nil))
	httpServerErrorLogger().Print("private-server-diagnostic")
	if err := rotating.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil || len(files) != 2 {
		t.Fatalf("rotation failed: %v, %v", files, err)
	}
	var all bytes.Buffer
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			decodeLogObject(t, line)
		}
		all.Write(data)
	}
	for _, marker := range []string{"existing_log", `"route":"/before"`, `"route":"/after"`, `"msg":"http_server_error"`} {
		if !strings.Contains(all.String(), marker) {
			t.Fatalf("lost log %s during append/rotation", marker)
		}
	}
	active, err := os.ReadFile(filename)
	if err != nil || !bytes.Contains(active, []byte(`"route":"/after"`)) {
		t.Fatal("HTTP logs did not follow rotation")
	}
	if !bytes.Contains(active, []byte(`"msg":"http_server_error"`)) || bytes.Contains(all.Bytes(), []byte("private-")) {
		t.Fatal("server diagnostics did not follow rotation or leaked details")
	}
}

// Exercise main's actual HTTP/TLS wiring, including net/http's own diagnostics.
func TestHTTPProcessLogging(t *testing.T) {
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := fixture.TLS.Certificates[0]
	client := fixture.Client()
	client.Timeout = time.Second
	fixture.Close()
	defer client.CloseIdleConnections()
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, transport := range []string{"http", "https"} {
		for _, level := range []string{"info", "debug"} {
			t.Run(transport+"_"+level, func(t *testing.T) {
				listener, err := net.Listen("tcp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				port := listener.Addr().(*net.TCPAddr).Port
				listener.Close()
				dir := t.TempDir()
				configPath, apiPath := filepath.Join(dir, "config.json"), filepath.Join(dir, "api.json")
				logPath := filepath.Join(dir, "service.log")
				cfg := Config{Port: port, Log: LogConfig{Level: level, EnableLogging: level == "debug", Filename: logPath}}
				if transport == "https" {
					cfg.CertFile, cfg.KeyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
					writeLoggingFile(t, cfg.CertFile, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})))
					writeLoggingFile(t, cfg.KeyFile, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})))
				}
				encoded, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				writeLoggingFile(t, configPath, string(encoded))
				writeLoggingFile(t, apiPath, `{}`)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPStdioLoggingHelper$", "--", "--config", configPath, "--api", apiPath)
				cmd.Env = append(os.Environ(), "NYANPUI_TEST_STDIO_LOGGING_CHILD=1")
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if cmd.ProcessState == nil {
						cmd.Process.Kill()
						cmd.Wait()
					}
				}()
				address := fmt.Sprintf("127.0.0.1:%d", port)
				deadline := time.Now().Add(5 * time.Second)
				for {
					response, err := client.Get(transport + "://" + address + "/private-path?token=private-query")
					if err == nil {
						io.Copy(io.Discard, response.Body)
						response.Body.Close()
						if response.StatusCode != http.StatusNotFound {
							t.Fatalf("unexpected HTTP status: %d", response.StatusCode)
						}
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("server did not start: %v", err)
					}
					time.Sleep(10 * time.Millisecond)
				}
				if transport == "https" {
					conn, err := net.DialTimeout("tcp", address, time.Second)
					if err != nil {
						t.Fatal(err)
					}
					defer conn.Close()
					conn.SetDeadline(time.Now().Add(time.Second))
					if _, err := io.WriteString(conn, "invalid TLS handshake\n"); err != nil {
						t.Fatal(err)
					}
					// net/http logs the handshake error before closing the connection.
					io.Copy(io.Discard, conn)
					conn.Close()
				}
				cmd.Process.Kill()
				cmd.Wait()
				if stdout.Len() != 0 {
					t.Fatalf("HTTP service wrote stdout: %s", stdout.String())
				}
				logs := stderr.Bytes()
				if cfg.Log.EnableLogging {
					if len(logs) != 0 {
						t.Fatalf("file logging also wrote stderr: %s", logs)
					}
					logs, err = os.ReadFile(logPath)
					if err != nil {
						t.Fatal(err)
					}
				}
				var diagnostics int
				for _, line := range bytes.Split(bytes.TrimSpace(logs), []byte("\n")) {
					record := decodeLogObject(t, line)
					switch record["msg"] {
					case "starting", "config_loaded", "api_config_loaded", "api_hot_reload_disabled", "http_server_starting":
					case "http_server_error":
						diagnostics++
						if record["level"] != "ERROR" || record["error_type"] == nil {
							t.Fatalf("invalid server diagnostic: %#v", record)
						}
						if level == "debug" {
							detail, _ := record["error_detail"].(string)
							if !strings.Contains(detail, "TLS handshake error") {
								t.Fatalf("debug detail missing: %#v", record)
							}
						} else if record["error_detail"] != nil || bytes.Contains(line, []byte("127.0.0.1")) {
							t.Fatalf("info exposed server diagnostic: %#v", record)
						}
					default:
						t.Fatalf("unexpected request/access log: %#v", record)
					}
				}
				if (transport == "https" && diagnostics != 1) || (transport == "http" && diagnostics != 0) {
					t.Fatalf("unexpected server diagnostic count: %d", diagnostics)
				}
				if bytes.Contains(logs, []byte("private-")) {
					t.Fatalf("request values exposed: %s", logs)
				}
			})
		}
	}
}

func TestStartupLoggingFailuresStayOffStdout(t *testing.T) {
	for _, tc := range []struct {
		name, config, event string
	}{
		{"missing_config", "", "startup_options_failed"},
		{"invalid_json", `{"log":`, "config_load_failed"},
		{"invalid_level", `{"log":{"Level":"verbose"}}`, "log_level_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath, apiPath := filepath.Join(dir, "config.json"), filepath.Join(dir, "api.json")
			writeLoggingFile(t, apiPath, `{}`)
			if tc.config != "" {
				writeLoggingFile(t, configPath, tc.config)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPStdioLoggingHelper$", "--", "--config", configPath, "--api", apiPath)
			cmd.Env = append(os.Environ(), "NYANPUI_TEST_STDIO_LOGGING_CHILD=1")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
				t.Fatalf("startup exit = %v, want exit code 1; stderr=%s", err, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("startup failure polluted stdout: %s", stdout.String())
			}
			record := decodeLogObject(t, bytes.TrimSpace(stderr.Bytes()))
			if record["level"] != "ERROR" || record["msg"] != tc.event {
				t.Fatalf("unexpected startup error: %#v; want event %q", record, tc.event)
			}
		})
	}
}

func TestMCPToolRunsChecksOnBothTransports(t *testing.T) {
	const allow = `({success:true,status:200,result:{checked:true}});`
	for _, transport := range []string{"streamable_http", "stdio"} {
		for _, tc := range []struct {
			name, paramResult, outResult, script            string
			checkOnly, noParam, invalidInput, invalidOutput bool
			wantError, wantMain, wantOut                    bool
		}{
			{name: "plain_object", wantMain: true, wantOut: true},
			{name: "structured_response", script: `({status:201,contentType:"application/json",headers:{"X-Tool":"accepted"},body:{value:"private-output"}});`, wantMain: true, wantOut: true},
			{name: "param_false_200", paramResult: `({success:false,status:200,result:"denied"});`, wantError: true},
			{name: "param_non_200", paramResult: `({success:true,status:201,result:"denied"});`, wantError: true},
			{name: "param_exception", paramResult: `throw new Error("check failed");`, wantError: true},
			{name: "param_invalid", paramResult: `({success:true});`, wantError: true},
			{name: "out_false_200", outResult: `({success:false,status:200,result:"denied"});`, wantError: true, wantMain: true, wantOut: true},
			{name: "out_non_200", outResult: `({success:true,status:201,result:"denied"});`, wantError: true, wantMain: true, wantOut: true},
			{name: "out_exception", outResult: `throw new Error("check failed");`, wantError: true, wantMain: true, wantOut: true},
			{name: "out_invalid", outResult: `({status:200});`, wantError: true, wantMain: true, wantOut: true},
			{name: "check_only", checkOnly: true},
			{name: "check_only_without_param", checkOnly: true, noParam: true},
			{name: "check_only_rejected", checkOnly: true, paramResult: `({success:false,status:403,result:"denied"});`, wantError: true},
			{name: "input_schema", invalidInput: true, wantError: true},
			{name: "output_schema", invalidOutput: true, wantError: true, wantMain: true, wantOut: true},
		} {
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				paramMarker, mainMarker, outMarker := filepath.Join(dir, "param"), filepath.Join(dir, "main"), filepath.Join(dir, "out")
				paramResult, outResult, script := tc.paramResult, tc.outResult, tc.script
				if paramResult == "" {
					paramResult = allow
				}
				if outResult == "" {
					outResult = allow
				}
				if script == "" {
					script = `({value:"private-output"});`
				}
				contextCheck := fmt.Sprintf(`
if (nyanAllParams.api !== "tool" || nyanAllParams.mcp_tool !== "tool" || nyanAllParams.id !== 7 || nyanAllParams.mcp_principal.transport !== %q) throw new Error("wrong tool context");
if (%q === "streamable_http" && nyanGetRequestHeaders()["X-Check"] !== "fixture") throw new Error("missing HTTP context");
`, transport, transport)
				param := writeFixtureFile(t, `const nyanInputSchema = {type:"object",properties:{id:{type:"integer"},nyan_mode:{type:"string"}},required:["id"],additionalProperties:false};`+contextCheck+fmt.Sprintf(`nyanSaveFile(%q,"ran");`, paramMarker)+paramResult)
				outSchema := `const nyanOutputSchema = {type:"object",properties:{value:{const:"private-output"}},required:["value"]};`
				if tc.invalidOutput {
					outSchema = `const nyanOutputSchema = {type:"object",required:["missing"]};`
				}
				outputCheck := `if (JSON.parse(nyanAllParams.nyan_output.body).value !== "private-output") throw new Error("wrong output body");`
				if tc.name == "structured_response" {
					outputCheck += `if (nyanAllParams.nyan_output.status !== 201 || nyanAllParams.nyan_output.contentType !== "application/json" || nyanAllParams.nyan_output.headers["X-Tool"] !== "accepted") throw new Error("wrong output metadata");`
				}
				out := writeFixtureFile(t, outSchema+contextCheck+outputCheck+fmt.Sprintf(`nyanSaveFile(%q,"ran");`, outMarker)+outResult)
				if tc.noParam {
					param = ""
				}
				cfg := APIConfig{
					"tool": {Script: writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran");`, mainMarker)+script), ParamCheck: param, OutCheck: out},
					"mcp":  {Type: apiTypeMCP, Transport: transport, AllowedOrigins: []string{"https://client.example"}, Tools: []MCPToolConfig{{Name: "tool", API: "tool"}}},
				}
				if err := validateMCPConfiguration(cfg); err != nil {
					t.Fatal(err)
				}
				args := map[string]interface{}{"id": 7}
				if tc.checkOnly {
					args["nyan_mode"] = "checkOnly"
				}
				if tc.invalidInput {
					args["id"] = "invalid"
				}
				request, err := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]interface{}{"name": "tool", "arguments": args}})
				if err != nil {
					t.Fatal(err)
				}
				var response []byte
				if transport == "streamable_http" {
					router := newRequestRegressionRouter(t, cfg)
					req := httptest.NewRequest(http.MethodPost, "https://example.test/mcp", bytes.NewReader(request))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Accept", "application/json, text/event-stream")
					req.Header.Set("MCP-Protocol-Version", mcpProtocol20251125)
					req.Header.Set("X-Check", "fixture")
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, req)
					if rec.Code != http.StatusOK {
						t.Fatalf("HTTP status=%d body=%s", rec.Code, rec.Body.String())
					}
					response = rec.Body.Bytes()
				} else {
					input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}` + "\n" + `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" + string(request) + "\n"
					var output bytes.Buffer
					if err := serveMCPStdio(strings.NewReader(input), &output, &APIConfigSnapshot{Config: cfg}, cfg["mcp"]); err != nil {
						t.Fatal(err)
					}
					lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
					if len(lines) != 2 {
						t.Fatalf("stdio responses=%s", output.String())
					}
					response = lines[1]
				}
				var envelope struct {
					Result map[string]interface{} `json:"result"`
					Error  json.RawMessage        `json:"error"`
				}
				if err := json.Unmarshal(response, &envelope); err != nil {
					t.Fatal(err)
				}
				if tc.invalidInput && transport == "streamable_http" {
					if len(envelope.Error) == 0 {
						t.Fatalf("input schema accepted: %s", response)
					}
				} else if len(envelope.Error) != 0 || envelope.Result["isError"] != tc.wantError {
					t.Fatalf("unexpected MCP result: %s", response)
				}
				if tc.wantError && bytes.Contains(response, []byte("private-output")) {
					t.Fatalf("rejected output leaked: %s", response)
				}
				if tc.checkOnly && !tc.wantError {
					check, _ := json.Marshal(envelope.Result["structuredContent"])
					assertParamCheckResponse(t, check, true, http.StatusOK)
				} else if !tc.wantError {
					structured, ok := envelope.Result["structuredContent"].(map[string]interface{})
					if !ok || structured["value"] != "private-output" {
						t.Fatalf("success result changed: %s", response)
					}
				}
				assertJSONRPCExecutionMarker(t, paramMarker, !tc.invalidInput && !tc.noParam)
				assertJSONRPCExecutionMarker(t, mainMarker, tc.wantMain)
				assertJSONRPCExecutionMarker(t, outMarker, tc.wantOut)
			})
		}
	}
}

func TestNyanCallMeRunsTargetChecks(t *testing.T) {
	const allow = `({success:true,status:200,result:{checked:true}});`
	for _, withContext := range []bool{false, true} {
		for _, tc := range []struct {
			name, script, paramResult, outResult, wantValue, wantOutput string
			checkOnly, noParam, wantError, wantMain, wantOut            bool
		}{
			{name: "json_string", script: `JSON.stringify({value:"private-output",count:7});`, wantMain: true, wantOut: true},
			{name: "plain_object", script: `({value:"private-output",count:7});`, wantMain: true, wantOut: true},
			{name: "array", script: `[7,"private-output",false];`, wantValue: `[7,"private-output",false]`, wantMain: true, wantOut: true},
			{name: "number", script: `7;`, wantValue: `7`, wantMain: true, wantOut: true},
			{name: "null", script: `null;`, wantValue: `null`, wantMain: true, wantOut: true},
			{name: "undefined", script: `undefined;`, wantValue: `null`, wantMain: true, wantOut: true},
			{name: "structured_response", script: `({status:201,contentType:"application/json",headers:{"X-Target":"accepted"},body:{value:"private-output",count:7}});`, wantValue: `{"status":201,"contentType":"application/json","headers":{"X-Target":"accepted"},"body":{"value":"private-output","count":7}}`, wantOutput: `{"value":"private-output","count":7}`, wantMain: true, wantOut: true},
			{name: "param_false", paramResult: `({success:false,status:200,result:"denied"});`, wantError: true},
			{name: "param_non_200", paramResult: `({success:true,status:201,result:"denied"});`, wantError: true},
			{name: "param_exception", paramResult: `throw new Error("check failed");`, wantError: true},
			{name: "out_false", outResult: `({success:false,status:200,result:"denied"});`, wantError: true, wantMain: true, wantOut: true},
			{name: "out_non_200", outResult: `({success:true,status:201,result:"denied"});`, wantError: true, wantMain: true, wantOut: true},
			{name: "out_exception", outResult: `throw new Error("check failed");`, wantError: true, wantMain: true, wantOut: true},
			{name: "check_only", checkOnly: true},
			{name: "check_only_without_param", checkOnly: true, noParam: true},
			{name: "check_only_rejected", checkOnly: true, paramResult: `({success:false,status:403,result:"denied"});`, wantError: true},
		} {
			t.Run(fmt.Sprintf("context_%t/%s", withContext, tc.name), func(t *testing.T) {
				dir := t.TempDir()
				paramMarker, mainMarker, outMarker := filepath.Join(dir, "param"), filepath.Join(dir, "main"), filepath.Join(dir, "out")
				paramResult, outResult, script := tc.paramResult, tc.outResult, tc.script
				if paramResult == "" {
					paramResult = allow
				}
				if outResult == "" {
					outResult = allow
				}
				if script == "" {
					script = `JSON.stringify({value:"private-output",count:7});`
				}
				contextCheck := `if (nyanAllParams.api !== "target" || nyanAllParams.id !== 7) throw new Error("wrong API parameters");`
				if withContext {
					contextCheck += `if (nyanGetCookie("session") !== "owner" || nyanGetRequestHeaders()["X-Check"] !== "fixture") throw new Error("missing request context");`
				}
				param := writeFixtureFile(t, contextCheck+fmt.Sprintf(`nyanSaveFile(%q,"ran");`, paramMarker)+paramResult)
				if tc.noParam {
					param = ""
				}
				out := writeFixtureFile(t, contextCheck+fmt.Sprintf(`nyanSaveFile(%q,JSON.stringify(nyanAllParams.nyan_output));`, outMarker)+outResult)
				snapshot := &APIConfigSnapshot{Config: APIConfig{"target": {Script: writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran");`, mainMarker)+script), ParamCheck: param, OutCheck: out}}}
				var requestContext *gin.Context
				if withContext {
					requestContext, _ = gin.CreateTestContext(httptest.NewRecorder())
					requestContext.Request = httptest.NewRequest(http.MethodGet, "https://example.test/caller", nil)
					requestContext.Request.Header.Set("X-Check", "fixture")
					requestContext.Request.AddCookie(&http.Cookie{Name: "session", Value: "owner"})
				}
				mode := ""
				if tc.checkOnly {
					mode = `,nyan_mode:"checkOnly"`
				}
				caller := writeFixtureFile(t, `try { JSON.stringify({caught:false,value:nyanCallMe({api:"target",id:7`+mode+`})}); } catch (error) { JSON.stringify({caught:true,message:String(error)}); }`)
				value, err := runJavaScriptValueWithContext(snapshot, requestContext, caller, "", nil)
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Caught  bool            `json:"caught"`
					Value   json.RawMessage `json:"value"`
					Message string          `json:"message"`
				}
				if err := json.Unmarshal([]byte(value.String()), &result); err != nil {
					t.Fatal(err)
				}
				if result.Caught != tc.wantError {
					t.Fatalf("catch result=%s", value.String())
				}
				if tc.wantError {
					if strings.Contains(value.String(), "private-output") {
						t.Fatalf("denied result leaked: %s", value.String())
					}
					if !strings.Contains(result.Message, "paramCheck") && !strings.Contains(result.Message, "outCheck") {
						t.Fatalf("check failure lacks stage: %s", value.String())
					}
				} else if tc.checkOnly {
					assertParamCheckResponse(t, result.Value, true, http.StatusOK)
				} else {
					wantValue := tc.wantValue
					if wantValue == "" {
						wantValue = `{"value":"private-output","count":7}`
					}
					var got, want interface{}
					if err := json.Unmarshal(result.Value, &got); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(wantValue), &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("JSON parsing or result types changed: got=%s want=%s", result.Value, wantValue)
					}
				}
				assertJSONRPCExecutionMarker(t, paramMarker, !tc.noParam)
				assertJSONRPCExecutionMarker(t, mainMarker, tc.wantMain)
				assertJSONRPCExecutionMarker(t, outMarker, tc.wantOut)
				if tc.wantOut {
					data, err := os.ReadFile(outMarker)
					if err != nil {
						t.Fatal(err)
					}
					var output struct {
						Body        string            `json:"body"`
						Status      int               `json:"status"`
						ContentType string            `json:"contentType"`
						Headers     map[string]string `json:"headers"`
					}
					if err := json.Unmarshal(data, &output); err != nil {
						t.Fatal(err)
					}
					wantOutput := tc.wantOutput
					if wantOutput == "" {
						wantOutput = tc.wantValue
					}
					if wantOutput == "" {
						wantOutput = `{"value":"private-output","count":7}`
					}
					var got, want interface{}
					if err := json.Unmarshal([]byte(output.Body), &got); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(wantOutput), &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("outCheck body=%s want=%s", output.Body, wantOutput)
					}
					if tc.name == "structured_response" && (output.Status != 201 || output.ContentType != "application/json" || output.Headers["X-Target"] != "accepted") {
						t.Fatalf("outCheck metadata=%s", data)
					}
				}
			})
		}
	}
}

func TestPushRunsTargetChecksBeforeBroadcast(t *testing.T) {
	const allow = `({success:true,status:200,result:null});`
	for _, tc := range []struct {
		name, paramResult, outResult              string
		htmlOnly, withContext, checkOnly, noParam bool
		wantMain, wantOut, wantPush               bool
	}{
		{name: "script", wantMain: true, wantOut: true, wantPush: true},
		{name: "request_context", withContext: true, wantMain: true, wantOut: true, wantPush: true},
		{name: "html", htmlOnly: true, wantOut: true, wantPush: true},
		{name: "param_false", paramResult: `({success:false,status:200,result:"denied"});`},
		{name: "param_non_200", paramResult: `({success:true,status:201,result:"denied"});`},
		{name: "param_exception", paramResult: `throw new Error("check failed");`},
		{name: "out_false", outResult: `({success:false,status:200,result:"denied"});`, wantMain: true, wantOut: true},
		{name: "out_non_200", outResult: `({success:true,status:201,result:"denied"});`, wantMain: true, wantOut: true},
		{name: "out_exception", outResult: `throw new Error("check failed");`, wantMain: true, wantOut: true},
		{name: "html_out_rejected", htmlOnly: true, outResult: `({success:false,status:403,result:"denied"});`, wantOut: true},
		{name: "check_only", checkOnly: true},
		{name: "check_only_without_param", checkOnly: true, noParam: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			paramMarker, mainMarker, outMarker := filepath.Join(dir, "param"), filepath.Join(dir, "main"), filepath.Join(dir, "out")
			paramResult, outResult := tc.paramResult, tc.outResult
			if paramResult == "" {
				paramResult = allow
			}
			if outResult == "" {
				outResult = allow
			}
			contextCheck := `if (nyanAllParams.api !== "caller" || nyanAllParams.id !== "item") throw new Error("Push parameters changed");`
			if tc.withContext {
				contextCheck += `if (nyanGetCookie("session") !== "owner" || nyanGetRequestHeaders()["X-Check"] !== "fixture") throw new Error("missing request context");`
			}
			push := EndpointConfig{
				Script:     writeFixtureFile(t, fmt.Sprintf(`nyanSaveFile(%q,"ran"); "PRIVATE_PUSH";`, mainMarker)),
				ParamCheck: writeFixtureFile(t, contextCheck+fmt.Sprintf(`nyanSaveFile(%q,"ran");`, paramMarker)+paramResult),
				OutCheck:   writeFixtureFile(t, contextCheck+`if (nyanAllParams.nyan_output.body !== "PRIVATE_PUSH" || nyanAllParams.nyan_output.status !== 200 || nyanAllParams.nyan_output.contentType !== "text/html; charset=utf-8") throw new Error("wrong Push output");`+fmt.Sprintf(`nyanSaveFile(%q,"ran");`, outMarker)+outResult),
			}
			if tc.htmlOnly {
				push.Script, push.HTML = "", writeFixtureFile(t, "PRIVATE_PUSH")
			}
			if tc.noParam {
				push.ParamCheck = ""
			}
			// The subscriber's handshake has its own checks; isolate the later Push target checks.
			router := newRequestRegressionRouter(t, APIConfig{"updates": {HTML: writeFixtureFile(t, "subscriber")}})
			server := httptest.NewServer(router)
			defer server.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/updates", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(`{}`)); err != nil {
				t.Fatal(err)
			}
			if _, _, err := conn.ReadMessage(); err != nil {
				t.Fatal(err)
			}
			var requestContext *gin.Context
			if tc.withContext {
				requestContext, _ = gin.CreateTestContext(httptest.NewRecorder())
				requestContext.Request = httptest.NewRequest(http.MethodGet, "https://example.test/caller", nil)
				requestContext.Request.Header.Set("X-Check", "fixture")
				requestContext.Request.AddCookie(&http.Cookie{Name: "session", Value: "owner"})
			}
			params := map[string]interface{}{"api": "caller", "id": "item"}
			if tc.checkOnly {
				params["nyan_mode"] = "checkOnly"
			}
			performPushWithContext(&APIConfigSnapshot{Config: APIConfig{"updates": push}}, requestContext, EndpointConfig{Push: "updates"}, params)
			// Push is synchronous: the following echo must come after any broadcast.
			const echo = `{"echo":"after-push"}`
			if err := conn.WriteMessage(websocket.TextMessage, []byte(echo)); err != nil {
				t.Fatal(err)
			}
			_, body, err := conn.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantPush {
				if string(body) != "PRIVATE_PUSH" {
					t.Fatalf("Push payload=%q", body)
				}
				_, body, err = conn.ReadMessage()
				if err != nil {
					t.Fatal(err)
				}
			}
			if string(body) != echo {
				t.Fatalf("unexpected broadcast after rejected/checkOnly Push: %q", body)
			}
			assertJSONRPCExecutionMarker(t, paramMarker, !tc.noParam)
			assertJSONRPCExecutionMarker(t, mainMarker, tc.wantMain)
			assertJSONRPCExecutionMarker(t, outMarker, tc.wantOut)
		})
	}
}
