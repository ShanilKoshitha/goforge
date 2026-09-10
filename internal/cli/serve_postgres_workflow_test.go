package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

var developmentLiveReloadClientPattern = regexp.MustCompile(`<script src="(/\.goforge/livereload/[A-Za-z0-9_-]{43}/client\.js)" data-goforge-generation="([0-9]+)" defer></script>`)

func TestGeneratedDevelopmentServerWorkflow(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}
	root := projectRoot(t)
	scratch := t.TempDir()
	forgeBinary := filepath.Join(scratch, "forge")
	if runtime.GOOS == "windows" {
		forgeBinary += ".exe"
	}
	baseEnvironment := append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := generatedCommand(root, baseEnvironment, "go", "build", "-o", forgeBinary, "./cmd/forge"); err != nil {
		t.Fatalf("build forge CLI: %v\n%s", err, output)
	}

	directory := filepath.Join(scratch, "development-app")
	if output, err := generatedCommand(scratch, baseEnvironment, forgeBinary, "new", "development-app", "--module", "example.com/development", "--replace", root); err != nil {
		t.Fatalf("forge new: %v\n%s", err, output)
	}
	adminPath := filepath.Join(directory, ".forge", "acceptance_db.go")
	adminProgram := strings.ReplaceAll(postgresAdminProgram, "example.com/issueboard", "example.com/development")
	if err := os.WriteFile(adminPath, []byte(adminProgram), 0o644); err != nil {
		t.Fatalf("write PostgreSQL acceptance helper: %v", err)
	}
	adminEnvironment := append(append([]string(nil), baseEnvironment...), "DATABASE_URL="+databaseURL)
	schema := fmt.Sprintf("goforge_development_%d", time.Now().UnixNano())
	if output, err := generatedCommand(directory, adminEnvironment, "go", "run", "./.forge/acceptance_db.go", "create", schema); err != nil {
		t.Fatalf("create isolated PostgreSQL schema: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		if output, err := generatedCommand(directory, adminEnvironment, "go", "run", "./.forge/acceptance_db.go", "drop", schema); err != nil {
			t.Errorf("drop isolated PostgreSQL schema: %v\n%s", err, output)
		}
	})
	isolatedURL, err := postgresSchemaURL(databaseURL, schema)
	if err != nil {
		t.Fatal(err)
	}
	address := freeAddress(t)
	environment := append(append([]string(nil), baseEnvironment...),
		"DATABASE_URL="+isolatedURL,
		"APP_ENV=local",
		"APP_ADDRESS="+address,
	)
	if output, err := generatedCommand(directory, environment, forgeBinary, "migrate"); err != nil {
		t.Fatalf("forge migrate: %v\n%s", err, output)
	}

	command := exec.Command(forgeBinary, "serve")
	command.Dir = directory
	command.Env = environment
	configureCommandProcess(command)
	var serveOutput synchronizedBuffer
	command.Stdout, command.Stderr = &serveOutput, &serveOutput
	if err := command.Start(); err != nil {
		t.Fatalf("start forge serve: %v", err)
	}
	serveRunning := true
	t.Cleanup(func() {
		if serveRunning {
			stopCommandProcess(t, command, false)
		}
	})
	baseURL := "http://" + address
	waitForHealth(t, baseURL, &serveOutput)
	initialBody := developmentResponse(t, baseURL)
	if !strings.Contains(initialBody, "Your routes, controllers, templates, and configuration are ordinary Go.") {
		t.Fatalf("initial welcome response is missing generated content: %s", initialBody)
	}
	liveReloadScript, liveReloadEvents, initialGeneration := developmentLiveReloadPage(t, initialBody)
	assertDevelopmentLiveReloadScript(t, baseURL, liveReloadScript, liveReloadEvents)
	initialReload := openDevelopmentLiveReload(t, baseURL, liveReloadEvents, initialGeneration)
	generatedPath := filepath.Join(directory, filepath.FromSlash(generatedViewsPath))
	initialArtifact, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatalf("read initial compiled views: %v", err)
	}
	cliProcessID := command.Process.Pid

	partialPath := filepath.Join(directory, "resources", "views", "partials", "tagline.forge.html")
	originalPartial, err := os.ReadFile(partialPath)
	if err != nil {
		t.Fatalf("read welcome partial: %v", err)
	}
	viewV2 := append(append([]byte(nil), originalPartial...), []byte("\n<p data-development-probe=\"view-v2\">view-v2</p>\n")...)
	if err := os.WriteFile(partialPath, viewV2, 0o644); err != nil {
		t.Fatalf("write valid Forge view: %v", err)
	}
	viewV2Body := waitForDevelopmentResponse(t, baseURL, "view-v2", &serveOutput)
	viewV2Generation := nextDevelopmentLiveReloadGeneration(t, initialGeneration)
	waitForDevelopmentLiveReload(t, initialReload, viewV2Generation)
	viewV2Body = developmentResponse(t, baseURL)
	if !strings.Contains(viewV2Body, "view-v2") {
		t.Fatalf("LiveReload generation %s did not expose its accepted view: %s", viewV2Generation, viewV2Body)
	}
	assertDevelopmentLiveReloadPage(t, viewV2Body, liveReloadScript, viewV2Generation)
	if command.Process.Pid != cliProcessID || command.ProcessState != nil {
		t.Fatal("valid view edit restarted or stopped the forge CLI supervisor")
	}
	viewV2Artifact, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatalf("read recompiled views: %v", err)
	}
	if bytes.Equal(initialArtifact, viewV2Artifact) {
		t.Fatal("valid view edit did not refresh the inspectable compiled artifact")
	}
	viewV2Reload := openDevelopmentLiveReload(t, baseURL, liveReloadEvents, viewV2Generation)

	outputOffset := len(serveOutput.String())
	if err := os.WriteFile(partialPath, []byte("@if(.Ready)\n@endfor\n"), 0o644); err != nil {
		t.Fatalf("write invalid Forge view: %v", err)
	}
	waitForDevelopmentOutput(t, &serveOutput, outputOffset, "partials/tagline.forge.html:2:1")
	assertNoDevelopmentLiveReload(t, viewV2Reload, "invalid Forge edit")
	if body := developmentResponse(t, baseURL); body != viewV2Body {
		t.Fatalf("invalid Forge edit replaced the last-good response:\n%s", body)
	}
	assertDevelopmentArtifact(t, generatedPath, viewV2Artifact, "invalid Forge edit")

	viewV3 := append(append([]byte(nil), originalPartial...), []byte("\n<p data-development-probe=\"view-v3\">view-v3</p>\n")...)
	if err := os.WriteFile(partialPath, viewV3, 0o644); err != nil {
		t.Fatalf("repair Forge view: %v", err)
	}
	viewV3Body := waitForDevelopmentResponse(t, baseURL, "view-v3", &serveOutput)
	viewV3Generation := nextDevelopmentLiveReloadGeneration(t, viewV2Generation)
	waitForDevelopmentLiveReload(t, viewV2Reload, viewV3Generation)
	viewV3Body = developmentResponse(t, baseURL)
	if !strings.Contains(viewV3Body, "view-v3") {
		t.Fatalf("LiveReload generation %s did not expose its repaired view: %s", viewV3Generation, viewV3Body)
	}
	assertDevelopmentLiveReloadPage(t, viewV3Body, liveReloadScript, viewV3Generation)
	if strings.Contains(viewV3Body, "view-v2") {
		t.Fatalf("repaired view retained stale content: %s", viewV3Body)
	}
	viewV3Artifact, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatalf("read repaired compiled views: %v", err)
	}
	nestedDirectory := filepath.Join(directory, "resources", "views", "partials", "development")
	if err := os.MkdirAll(nestedDirectory, 0o755); err != nil {
		t.Fatalf("create nested view directory: %v", err)
	}
	nestedTemporary := filepath.Join(nestedDirectory, ".probe.forge.html.atomic")
	nestedPath := filepath.Join(nestedDirectory, "probe.forge.html")
	if err := os.WriteFile(nestedTemporary, []byte(`<p data-development-probe="nested-view-v4">nested-view-v4</p>`), 0o644); err != nil {
		t.Fatalf("write nested atomic-save view: %v", err)
	}
	if err := os.Rename(nestedTemporary, nestedPath); err != nil {
		t.Fatalf("publish nested atomic-save view: %v", err)
	}
	viewV4 := append(append([]byte(nil), viewV3...), []byte("\n@include(\"partials/development/probe\")\n")...)
	if err := os.WriteFile(partialPath, viewV4, 0o644); err != nil {
		t.Fatalf("include nested view: %v", err)
	}
	viewV4Body := waitForDevelopmentResponse(t, baseURL, "nested-view-v4", &serveOutput)
	viewV4Artifact, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatalf("read nested compiled views: %v", err)
	}
	if bytes.Equal(viewV3Artifact, viewV4Artifact) {
		t.Fatal("new nested view directory did not refresh the compiled artifact")
	}

	controllerPath := filepath.Join(directory, "internal", "http", "controllers", "welcome_controller.go")
	originalController, err := os.ReadFile(controllerPath)
	if err != nil {
		t.Fatalf("read welcome controller: %v", err)
	}
	brokenGoPath := filepath.Join(directory, "internal", "http", "controllers", "development_failure_probe.go")
	outputOffset = len(serveOutput.String())
	if err := os.WriteFile(brokenGoPath, []byte("package controllers\n\nfunc developmentFailureProbe(\n"), 0o644); err != nil {
		t.Fatalf("write invalid Go source: %v", err)
	}
	viewV5 := append(append([]byte(nil), viewV4...), []byte("\n<p data-development-probe=\"view-with-broken-go\">view-with-broken-go</p>\n")...)
	if err := os.WriteFile(partialPath, viewV5, 0o644); err != nil {
		t.Fatalf("write valid view beside invalid Go source: %v", err)
	}
	waitForDevelopmentOutput(t, &serveOutput, outputOffset, "development_failure_probe.go")
	if body := developmentResponse(t, baseURL); body != viewV4Body {
		t.Fatalf("valid view beside invalid Go replaced the last-good response:\n%s", body)
	}
	assertDevelopmentArtifact(t, generatedPath, viewV4Artifact, "valid view beside invalid Go")
	if err := os.Remove(brokenGoPath); err != nil {
		t.Fatalf("remove invalid Go source: %v", err)
	}
	controllerV4 := bytes.Replace(originalController,
		[]byte("Your routes, controllers, templates, and configuration are ordinary Go."),
		[]byte("ordinary-go-v4"), 1)
	if bytes.Equal(controllerV4, originalController) {
		t.Fatal("welcome controller probe could not find generated tagline")
	}
	if err := os.WriteFile(controllerPath, controllerV4, 0o644); err != nil {
		t.Fatalf("repair ordinary Go source: %v", err)
	}
	recoveredBody := waitForDevelopmentResponse(t, baseURL, "ordinary-go-v4", &serveOutput)
	if !strings.Contains(recoveredBody, "view-with-broken-go") {
		t.Fatalf("Go recovery did not publish the pending valid view: %s", recoveredBody)
	}

	controllerV5 := bytes.Replace(controllerV4, []byte("ordinary-go-v4"), []byte("atomic-save-v5"), 1)
	temporaryController := filepath.Join(filepath.Dir(controllerPath), ".welcome_controller.go.atomic")
	if err := os.WriteFile(temporaryController, controllerV5, 0o644); err != nil {
		t.Fatalf("write atomic-save temporary source: %v", err)
	}
	if err := os.Rename(temporaryController, controllerPath); err != nil {
		t.Fatalf("atomically replace welcome controller: %v", err)
	}
	finalBody := waitForDevelopmentResponse(t, baseURL, "atomic-save-v5", &serveOutput)
	_, finalEvents, finalGeneration := developmentLiveReloadPage(t, finalBody)
	shutdownReload := openDevelopmentLiveReload(t, baseURL, finalEvents, finalGeneration)

	stopCommandProcess(t, command, true)
	serveRunning = false
	waitForDevelopmentLiveReloadShutdown(t, shutdownReload)
	if command.ProcessState == nil || !command.ProcessState.Success() {
		t.Fatalf("cancelled forge serve did not exit cleanly: %v", command.ProcessState)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("development address was not released: %v\n%s", err, serveOutput.String())
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close reused development address: %v", err)
	}
	assertNoApplicationDevelopmentBinaries(t, directory)
	if command.Process.Pid != cliProcessID {
		t.Fatal("forge CLI process identity changed during development workflow")
	}
}

type developmentLiveReloadResult struct {
	body string
	err  error
}

type developmentLiveReloadSubscription struct {
	result <-chan developmentLiveReloadResult
}

func developmentLiveReloadPage(t *testing.T, body string) (scriptPath, eventsPath, generation string) {
	t.Helper()
	matches := developmentLiveReloadClientPattern.FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		t.Fatalf("development page contains %d LiveReload clients, want exactly one:\n%s", len(matches), body)
	}
	if count := strings.Count(body, "data-goforge-generation="); count != 1 {
		t.Fatalf("development page contains %d LiveReload generation markers, want exactly one", count)
	}
	scriptPath = matches[0][1]
	eventsPath = strings.TrimSuffix(scriptPath, "/client.js") + "/events"
	return scriptPath, eventsPath, matches[0][2]
}

func assertDevelopmentLiveReloadPage(t *testing.T, body, wantScriptPath, wantGeneration string) {
	t.Helper()
	scriptPath, _, generation := developmentLiveReloadPage(t, body)
	if scriptPath != wantScriptPath {
		t.Fatalf("development LiveReload client path changed from %q to %q", wantScriptPath, scriptPath)
	}
	if generation != wantGeneration {
		t.Fatalf("development LiveReload generation = %q, want %q", generation, wantGeneration)
	}
}

func nextDevelopmentLiveReloadGeneration(t *testing.T, generation string) string {
	t.Helper()
	current, err := strconv.ParseUint(generation, 10, 64)
	if err != nil {
		t.Fatalf("parse development LiveReload generation %q: %v", generation, err)
	}
	if current == ^uint64(0) {
		t.Fatal("development LiveReload generation cannot advance")
	}
	return strconv.FormatUint(current+1, 10)
}

func assertDevelopmentLiveReloadScript(t *testing.T, baseURL, scriptPath, eventsPath string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(baseURL + scriptPath)
	if err != nil {
		t.Fatalf("request development LiveReload client: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read development LiveReload client: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("development LiveReload client status = %d: %s", response.StatusCode, body)
	}
	if got := response.Header.Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("development LiveReload client content type = %q", got)
	}
	if !strings.Contains(string(body), `new EventSource("`+eventsPath+`"`) {
		t.Fatalf("development LiveReload client does not connect to %q: %s", eventsPath, body)
	}
}

func openDevelopmentLiveReload(t *testing.T, baseURL, eventsPath, generation string) developmentLiveReloadSubscription {
	t.Helper()
	requestURL := baseURL + eventsPath + "?since=" + url.QueryEscape(generation)
	response, err := (&http.Client{}).Get(requestURL)
	if err != nil {
		t.Fatalf("subscribe to development LiveReload: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		t.Fatalf("development LiveReload subscription status = %d: %s", response.StatusCode, body)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		_ = response.Body.Close()
		t.Fatalf("development LiveReload content type = %q", got)
	}
	result := make(chan developmentLiveReloadResult, 1)
	go func() {
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		result <- developmentLiveReloadResult{body: string(body), err: readErr}
	}()
	t.Cleanup(func() { _ = response.Body.Close() })
	return developmentLiveReloadSubscription{result: result}
}

func waitForDevelopmentLiveReload(t *testing.T, subscription developmentLiveReloadSubscription, generation string) {
	t.Helper()
	select {
	case result := <-subscription.result:
		if result.err != nil {
			t.Fatalf("read development LiveReload event: %v", result.err)
		}
		if count := strings.Count(result.body, "event: reload\n"); count != 1 {
			t.Fatalf("development LiveReload emitted %d reload events, want exactly one: %q", count, result.body)
		}
		want := "event: reload\ndata: " + generation + "\n\n"
		if !strings.Contains(result.body, want) {
			t.Fatalf("development LiveReload event = %q, want generation %q", result.body, generation)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("development LiveReload did not emit generation %q", generation)
	}
}

func assertNoDevelopmentLiveReload(t *testing.T, subscription developmentLiveReloadSubscription, operation string) {
	t.Helper()
	select {
	case result := <-subscription.result:
		t.Fatalf("%s completed the LiveReload stream: body=%q err=%v", operation, result.body, result.err)
	case <-time.After(500 * time.Millisecond):
	}
}

func waitForDevelopmentLiveReloadShutdown(t *testing.T, subscription developmentLiveReloadSubscription) {
	t.Helper()
	select {
	case <-subscription.result:
	case <-time.After(2 * time.Second):
		t.Fatal("forge serve shutdown left a development LiveReload stream open")
	}
}

func developmentResponse(t *testing.T, baseURL string) string {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("request development welcome page: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read development welcome page: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("development welcome status = %d: %s", response.StatusCode, body)
	}
	return string(body)
}

func waitForDevelopmentResponse(t *testing.T, baseURL, marker string, output *synchronizedBuffer) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "/")
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK && strings.Contains(string(body), marker) {
				return string(body)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("development response never contained %q:\n%s", marker, output.String())
	return ""
}

func waitForDevelopmentOutput(t *testing.T, output *synchronizedBuffer, offset int, marker string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		current := output.String()
		if offset <= len(current) && strings.Contains(current[offset:], marker) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("development output after offset %d never contained %q:\n%s", offset, marker, output.String())
}

func assertDevelopmentArtifact(t *testing.T, path string, want []byte, operation string) {
	t.Helper()
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read compiled views after %s: %v", operation, err)
	}
	if !bytes.Equal(current, want) {
		t.Fatalf("%s replaced the last-good compiled views", operation)
	}
}

func assertNoApplicationDevelopmentBinaries(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("forge serve left an application bin directory: %v", err)
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if strings.HasPrefix(name, "server-") || strings.HasPrefix(name, ".goforge-build-") {
			return fmt.Errorf("development binary remains inside application: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
