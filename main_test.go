package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
	apiConfig = nil
	t.Cleanup(func() { apiConfig = nil })

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

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
