package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func TestRegisterPublicEndpointServesFiles(t *testing.T) {
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

	router := gin.New()
	registerPublicEndpoint(router, "assets", EndpointConfig{
		Type: apiTypePublic,
		Path: "./public",
	}, tempDir)

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

func TestRegisterPublicEndpointRequiresPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	registerPublicEndpoint(router, "assets", EndpointConfig{Type: apiTypePublic}, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/assets/file.txt", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%q", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestRegisterPublicEndpointRunsParamCheck(t *testing.T) {
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

	router := gin.New()
	registerPublicEndpoint(router, "assets", EndpointConfig{
		Type:       apiTypePublic,
		Path:       "./public",
		ParamCheck: "./check.js",
	}, tempDir)

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

func TestRegisterPublicEndpointBlocksJSONStringParamCheck(t *testing.T) {
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

	router := gin.New()
	registerPublicEndpoint(router, "public", EndpointConfig{
		Type:       apiTypePublic,
		Path:       "./public",
		ParamCheck: "./check.js",
	}, tempDir)

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

func TestRegisterPublicEndpointRunsOutCheck(t *testing.T) {
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

	router := gin.New()
	registerPublicEndpoint(router, "public", EndpointConfig{
		Type:     apiTypePublic,
		Path:     "./public",
		OutCheck: "./out_check.js",
	}, tempDir)

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
	setAPIConfig(nil)
	t.Cleanup(func() { setAPIConfig(nil) })

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

func TestReloadAPIConfigKeepsLastGoodDefinition(t *testing.T) {
	apiDir := t.TempDir()
	apiPath := filepath.Join(apiDir, "api.json")
	writeTestFile(t, apiPath, `{"old":{"description":"active"}}`)
	initial, initialHash, err := readAPIConfigFile(apiPath, apiDir)
	if err != nil {
		t.Fatal(err)
	}
	setAPIConfig(initial)
	oldManager := backgroundRuntimes
	backgroundRuntimes = nil
	t.Cleanup(func() { setAPIConfig(nil); backgroundRuntimes = oldManager })

	writeTestFile(t, apiPath, `{"new":{"description":"updated"}}`)
	hash, reloaded, err := reloadAPIConfigIfChanged(apiPath, apiDir, initialHash)
	if err != nil || !reloaded {
		t.Fatalf("reload=%t err=%v", reloaded, err)
	}
	if _, ok := currentAPIConfig()["old"]; ok {
		t.Fatal("old API remains")
	}

	writeTestFile(t, apiPath, `{"broken":`)
	invalidHash, reloaded, err := reloadAPIConfigIfChanged(apiPath, apiDir, hash)
	if err == nil || reloaded {
		t.Fatalf("invalid reload=%t err=%v", reloaded, err)
	}
	if _, ok := currentAPIConfig()["new"]; !ok {
		t.Fatal("last-known-good config was lost")
	}
	secondHash, reloaded, err := reloadAPIConfigIfChanged(apiPath, apiDir, invalidHash)
	if err != nil || reloaded || secondHash != invalidHash {
		t.Fatalf("unchanged invalid content reprocessed: reload=%t err=%v", reloaded, err)
	}

	writeTestFile(t, apiPath, `{"fixed":{"description":"ok"}}`)
	_, reloaded, err = reloadAPIConfigIfChanged(apiPath, apiDir, invalidHash)
	if err != nil || !reloaded {
		t.Fatalf("fixed reload=%t err=%v", reloaded, err)
	}
}

func TestReloadAPIConfigRejectsInvalidBackgroundCandidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.json")
	writeTestFile(t, path, `{"current":{}}`)
	initial, hash, err := readAPIConfigFile(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	setAPIConfig(initial)
	t.Cleanup(func() { setAPIConfig(nil) })
	writeTestFile(t, path, `{"job":{"type":"schedule","trigger":{"type":"cron","value":"* * * * *"}}}`)
	_, reloaded, err := reloadAPIConfigIfChanged(path, dir, hash)
	if err == nil || reloaded {
		t.Fatalf("reload=%t err=%v", reloaded, err)
	}
	if _, ok := currentAPIConfig()["current"]; !ok {
		t.Fatal("current config changed")
	}
}

func TestDynamicDispatcherReflectsAddDeleteAndSlashName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	script := filepath.Join(dir, "hot.js")
	writeTestFile(t, script, `"hot";`)
	setAPIConfig(APIConfig{"nested/hot": {Script: script}})
	t.Cleanup(func() { setAPIConfig(nil) })
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
	setAPIConfig(APIConfig{"assets": {Type: apiTypePublic, Path: shortDir}, "assets/deep": {Type: apiTypePublic, Path: longDir}})
	t.Cleanup(func() { setAPIConfig(nil) })
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
	setAPIConfig(APIConfig{"api": {Description: "initial"}})
	t.Cleanup(func() { setAPIConfig(nil) })
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

func TestBackgroundRuntimeManagerUpdatesAndStopsSchedule(t *testing.T) {
	manager := newBackgroundRuntimeManager()
	firstSchedule, _ := parseCronSchedule("0 0 1 1 *")
	first := scheduleJobConfig{name: "job", scriptPath: "/tmp/job-v1.js", trigger: TriggerConfig{Type: "cron", Value: "0 0 1 1 *"}, schedule: firstSchedule}
	manager.reconcile(map[string]scheduleJobConfig{"job": first}, nil)
	manager.mu.Lock()
	runtime := manager.schedules["job"]
	manager.mu.Unlock()
	secondSchedule, _ := parseCronSchedule("0 0 2 1 *")
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

func newHotReloadWebSocketServer(t *testing.T) (string, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	connected, disconnected := make(chan struct{}, 1), make(chan struct{}, 1)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listener unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		connected <- struct{}{}
		defer func() { disconnected <- struct{}{}; _ = conn.Close() }()
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

func TestRegisterPublicEndpointBlocksLowercaseOutcheck(t *testing.T) {
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

	router := gin.New()
	registerPublicEndpoint(router, "public", config, tempDir)

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

	router := gin.New()
	config := EndpointConfig{
		Script:     mainScript,
		ParamCheck: checkScript,
	}
	router.Any("/secure", func(c *gin.Context) {
		handleAPIRequest(c, config)
	})

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

	router := gin.New()
	router.Any("/checked", func(c *gin.Context) {
		handleAPIRequest(c, EndpointConfig{
			Script:   mainScript,
			OutCheck: outCheckScript,
		})
	})

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

	router := gin.New()
	router.Any("/checked", func(c *gin.Context) {
		handleAPIRequest(c, EndpointConfig{
			Script:   mainScript,
			OutCheck: outCheckScript,
		})
	})

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

	router := gin.New()
	router.Any("/checked", func(c *gin.Context) {
		handleAPIRequest(c, EndpointConfig{
			Script:   mainScript,
			OutCheck: outCheckScript,
		})
	})

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

func TestProductionMCPConfiguration(t *testing.T) {
	path, err := filepath.Abs("api.vps.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("local production API configuration is not present")
	} else if err != nil {
		t.Fatal(err)
	}
	loaded, err := readAPIConfigGraph(path, filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	mcp := loaded.Snapshot.Config["server_mcp_http"]
	if mcp.Transport != "streamable_http" || len(mcp.Tools) != 1 || mcp.Tools[0].Name != "sample/json" {
		t.Fatalf("production MCP=%#v", mcp)
	}
}

func TestMCPAndOAuthDelegateDecisionsToJavaScriptHooks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	hook := filepath.Join(dir, "oauth_validate.js")
	toolScript := filepath.Join(dir, "tool.js")
	writeTestFile(t, hook, `({authenticated: nyanAllParams.authorization === "Bearer test-token", principal:{id:"user-1"}});`)
	writeTestFile(t, toolScript, `({status:200,contentType:"application/json",body:{ok:true,items:[1,2,3]}});`)
	snapshot := &APIConfigSnapshot{Config: APIConfig{
		"server_mcp":                             {Type: apiTypeMCP, Transport: "streamable_http", ProtocolVersions: []string{"2025-11-25"}, AllowedOrigins: []string{"https://chatgpt.com"}, RedirectURIAllowedPrefixes: []string{"https://chatgpt.com/connector/oauth/"}, OAuth: MCPOAuthHooks{AuthorizationServerMetadata: ".well-known/oauth-authorization-server", ProtectedResourceMetadataAPI: ".well-known/oauth-protected-resource/server_mcp", Authorize: "oauth/authorize", Token: "oauth/token", Register: "oauth/register", AdminUser: "oauth/admin/users", VerifyAccess: "oauth/verify_access"}, Tools: []MCPToolConfig{{Name: "sample", API: "sample"}}, Instructions: "test"},
		"sample":                                 {Type: "api", Script: toolScript, SecuritySchemes: []map[string]interface{}{{"type": "oauth2", "scopes": []string{"nyanpui:read"}}}},
		".well-known/oauth-authorization-server": {Type: "api"}, ".well-known/oauth-protected-resource/server_mcp": {Type: "api"},
		"oauth/authorize": {Type: "api", Script: hook}, "oauth/token": {Type: "api", Script: hook}, "oauth/register": {Type: "api", Script: hook}, "oauth/admin/users": {Type: "api", Script: hook}, "oauth/verify_access": {Type: "api", Script: hook, Scopes: []string{"nyanpui:read"}},
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

func TestOAuthHookJavaScriptLoadsWithoutGoState(t *testing.T) {
	path, err := filepath.Abs("javascript/oauth_hooks.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("local OAuth hook JavaScript is not present")
	} else if err != nil {
		t.Fatal(err)
	}
	oldSnapshot := currentAPISnapshot()
	oldConfig := globalConfig
	publishAPISnapshot(&APIConfigSnapshot{Config: APIConfig{}})
	globalConfig = Config{}
	t.Cleanup(func() { publishAPISnapshot(oldSnapshot); globalConfig = oldConfig })
	value, err := runJavaScriptValueWithSnapshot(currentAPISnapshot(), path, "", map[string]interface{}{"oauth_hook": "oauthValidateAccessToken", "headers": map[string]interface{}{}, "resource": "https://example.test/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := value.Export().(map[string]interface{})
	if !ok || result["authenticated"] != false {
		t.Fatalf("hook result=%#v", value.Export())
	}
}

func TestJavaScriptOAuthAuthorizationCodePKCEFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hook, err := filepath.Abs("javascript/oauth_hooks.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hook); os.IsNotExist(err) {
		t.Skip("local OAuth hook JavaScript is not present")
	} else if err != nil {
		t.Fatal(err)
	}
	toolScript := filepath.Join(t.TempDir(), "tool.js")
	writeTestFile(t, toolScript, `({status:200,contentType:"application/json",body:{ok:true,user:nyanAllParams.mcp_principal.user_id}});`)
	stateDirectory := t.TempDir()
	resource := "https://example.test:8443/server_mcp"
	snapshot := &APIConfigSnapshot{Config: APIConfig{
		"server_mcp":                             {Type: apiTypeMCP, Transport: "streamable_http", ProtocolVersions: []string{"2025-11-25"}, AllowedOrigins: []string{"https://chatgpt.com"}, RedirectURIAllowedPrefixes: []string{"https://chatgpt.com/connector/oauth/"}, OAuth: MCPOAuthHooks{AuthorizationServerMetadata: ".well-known/oauth-authorization-server", ProtectedResourceMetadataAPI: ".well-known/oauth-protected-resource/server_mcp", Authorize: "oauth/authorize", Token: "oauth/token", Register: "oauth/register", AdminUser: "oauth/admin/users", VerifyAccess: "oauth/verify_access"}, Tools: []MCPToolConfig{{Name: "sample", API: "sample"}}},
		"sample":                                 {Type: "api", Script: toolScript, SecuritySchemes: []map[string]interface{}{{"type": "oauth2", "scopes": []string{"nyanpui:read"}}}},
		".well-known/oauth-authorization-server": {Type: "api"}, ".well-known/oauth-protected-resource/server_mcp": {Type: "api"},
		"oauth/authorize": {Type: "api", Script: hook}, "oauth/token": {Type: "api", Script: hook}, "oauth/register": {Type: "api", Script: hook}, "oauth/admin/users": {Type: "api", Script: hook}, "oauth/verify_access": {Type: "api", Script: hook, Scopes: []string{"nyanpui:read"}},
	}}
	if err := validateMCPConfiguration(snapshot.Config); err != nil {
		t.Fatal(err)
	}
	oldSnapshot := currentAPISnapshot()
	oldConfig := globalConfig
	publishAPISnapshot(snapshot)
	globalConfig = Config{Name: "NyanPUI", Version: "test", BasicAuth: BasicAuthConfig{Username: "operator", Password: "operator-password"}, OAuthStateRoot: stateDirectory}
	t.Cleanup(func() { publishAPISnapshot(oldSnapshot); globalConfig = oldConfig })
	router := gin.New()
	router.NoRoute(func(c *gin.Context) {
		c.Request.Host = "example.test:8443"
		c.Request.URL.Scheme = "https"
		if !dispatchMCPOrOAuth(c) {
			c.Status(http.StatusNotFound)
		}
	})

	adminBody := strings.NewReader(`{"username":"neko","password":"oauth-password"}`)
	adminRequest := httptest.NewRequest(http.MethodPost, "/oauth/admin/users", adminBody)
	adminRequest.Header.Set("Content-Type", "application/json")
	adminRequest.SetBasicAuth("operator", "operator-password")
	admin := httptest.NewRecorder()
	router.ServeHTTP(admin, adminRequest)
	if admin.Code != http.StatusCreated {
		t.Fatalf("admin status=%d body=%s", admin.Code, admin.Body.String())
	}
	badRegisterRequest := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{"redirect_uris":["https://attacker.test/callback"],"scope":"nyanpui:read"}`))
	badRegisterRequest.Header.Set("Content-Type", "application/json")
	badRegistration := httptest.NewRecorder()
	router.ServeHTTP(badRegistration, badRegisterRequest)
	if badRegistration.Code != http.StatusBadRequest || !strings.Contains(badRegistration.Body.String(), "invalid_redirect_uri") {
		t.Fatalf("bad registration status=%d body=%s", badRegistration.Code, badRegistration.Body.String())
	}

	registerRequest := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{"client_name":"test","redirect_uris":["https://chatgpt.com/connector/oauth/test-client"],"scope":"nyanpui:read","grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`))
	registerRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	registration := httptest.NewRecorder()
	router.ServeHTTP(registration, registerRequest)
	if registration.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", registration.Code, registration.Body.String())
	}
	var client map[string]interface{}
	if err := json.Unmarshal(registration.Body.Bytes(), &client); err != nil {
		t.Fatal(err)
	}
	clientID, _ := client["client_id"].(string)
	if clientID == "" {
		t.Fatalf("registration=%#v", client)
	}

	verifier := strings.Repeat("v", 48)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	authorizeQuery := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"https://chatgpt.com/connector/oauth/test-client"}, "scope": {"nyanpui:read"}, "state": {"test-state"}, "resource": {resource}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	authorizeRequest := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+authorizeQuery.Encode(), nil)
	authorize := httptest.NewRecorder()
	router.ServeHTTP(authorize, authorizeRequest)
	if authorize.Code != http.StatusOK || len(authorize.Result().Cookies()) != 1 {
		t.Fatalf("authorize status=%d body=%s cookies=%v", authorize.Code, authorize.Body.String(), authorize.Result().Cookies())
	}
	body := authorize.Body.String()
	if !strings.Contains(body, `action="https://example.test:8443/oauth/authorize"`) {
		t.Fatalf("authorize form action is not absolute: %s", body)
	}
	if csp := authorize.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self' https://chatgpt.com") {
		t.Fatalf("authorize CSP does not allow form submission from a sandboxed OAuth modal: %q", csp)
	}
	requestID := htmlInputValue(body, "request_id")
	csrf := htmlInputValue(body, "csrf")
	if requestID == "" || csrf == "" {
		t.Fatalf("authorize form is incomplete: %s", body)
	}
	consentForm := url.Values{"request_id": {requestID}, "csrf": {csrf}, "username": {"neko"}, "password": {"oauth-password"}, "decision": {"allow"}}
	consentRequest := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(consentForm.Encode()))
	consentRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	consentRequest.AddCookie(authorize.Result().Cookies()[0])
	consent := httptest.NewRecorder()
	router.ServeHTTP(consent, consentRequest)
	if consent.Code != http.StatusSeeOther {
		t.Fatalf("consent status=%d body=%s", consent.Code, consent.Body.String())
	}
	redirect, err := url.Parse(consent.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := redirect.Query().Get("code")
	if code == "" || redirect.Query().Get("state") != "test-state" {
		t.Fatalf("redirect=%s", redirect.String())
	}

	tokenForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {"https://chatgpt.com/connector/oauth/test-client"}, "resource": {resource}, "code_verifier": {verifier}}
	tokenRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResponse := httptest.NewRecorder()
	router.ServeHTTP(tokenResponse, tokenRequest)
	if tokenResponse.Code != http.StatusOK {
		t.Fatalf("token status=%d body=%s", tokenResponse.Code, tokenResponse.Body.String())
	}
	var token map[string]interface{}
	if err := json.Unmarshal(tokenResponse.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	accessToken, _ := token["access_token"].(string)
	if accessToken == "" {
		t.Fatalf("token=%#v", token)
	}

	callBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sample","arguments":{}}}`
	callRequest := httptest.NewRequest(http.MethodPost, "/server_mcp", strings.NewReader(callBody))
	callRequest.Header.Set("Content-Type", "application/json")
	callRequest.Header.Set("Accept", "application/json, text/event-stream")
	callRequest.Header.Set("Authorization", "Bearer "+accessToken)
	callRequest.Header.Set("MCP-Protocol-Version", "2025-11-25")
	callResponse := httptest.NewRecorder()
	router.ServeHTTP(callResponse, callRequest)
	if callResponse.Code != http.StatusOK || !strings.Contains(callResponse.Body.String(), `"user":"neko"`) {
		t.Fatalf("tool status=%d body=%s", callResponse.Code, callResponse.Body.String())
	}

	reuseRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenForm.Encode()))
	reuseRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reuseResponse := httptest.NewRecorder()
	router.ServeHTTP(reuseResponse, reuseRequest)
	if reuseResponse.Code != http.StatusBadRequest || !strings.Contains(reuseResponse.Body.String(), "invalid_grant") {
		t.Fatalf("reused code status=%d body=%s", reuseResponse.Code, reuseResponse.Body.String())
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

func htmlInputValue(body, name string) string {
	marker := `name="` + name + `" value="`
	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		return ""
	}
	return body[start : start+end]
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
