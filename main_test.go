//go:build integration

/*
 * THOR Thunderstorm Mock Server - Integration Tests
 *
 * These tests treat the mock server as a black box:
 * 1. Build and start the server once as a subprocess (via TestMain)
 * 2. Send HTTP requests to the server
 * 3. Verify responses match the OpenAPI specification
 * 4. Verify stdout logging works correctly
 *
 * Run with: go test -tags=integration -v
 */

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	serverAddr    = "http://localhost:8080"
	apiBase       = serverAddr + "/api/v1"
	startupDelay  = 2 * time.Second
	shutdownDelay = 500 * time.Millisecond
)

// server is the shared server process for all tests.
var server *serverProcess

// serverProcess holds the running server process and its stdout.
type serverProcess struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	stdoutMu  sync.Mutex
	stdoutBuf bytes.Buffer
	wg        sync.WaitGroup
}

func TestMain(m *testing.M) {
	// Build the server binary once.
	buildCmd := exec.Command("go", "build", "-o", "thunderstorm-mock-test", ".")
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build server: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "./thunderstorm-mock-test")

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		fmt.Fprintf(os.Stderr, "failed to create stdout pipe: %v\n", err)
		os.Exit(1)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		fmt.Fprintf(os.Stderr, "failed to start server: %v\n", err)
		os.Exit(1)
	}

	server = &serverProcess{cmd: cmd, cancel: cancel}

	server.wg.Add(1)
	go func() {
		defer server.wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			server.stdoutMu.Lock()
			server.stdoutBuf.WriteString(line)
			server.stdoutBuf.WriteString("\n")
			server.stdoutMu.Unlock()
		}
	}()

	// Wait for server to become ready.
	if !waitForServer(serverAddr, startupDelay) {
		server.stop()
		fmt.Fprintln(os.Stderr, "server failed to start in time")
		os.Exit(1)
	}

	code := m.Run()

	server.stop()
	os.Exit(code)
}

func (sp *serverProcess) stop() {
	if sp.cmd.Process != nil {
		_ = sp.cmd.Process.Signal(syscall.SIGTERM)
	}
	time.Sleep(shutdownDelay)
	sp.cancel()
	_ = sp.cmd.Wait()
	sp.wg.Wait()
	_ = os.Remove("./thunderstorm-mock-test")
}

func (sp *serverProcess) getAndClearStdout() string {
	time.Sleep(200 * time.Millisecond)
	sp.stdoutMu.Lock()
	defer sp.stdoutMu.Unlock()
	s := sp.stdoutBuf.String()
	sp.stdoutBuf.Reset()
	return s
}

func waitForServer(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(addr + "/api/v1/status")
		if err == nil {
			_ = resp.Body.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// --- helpers ---

func createTestFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "thunderstorm-integration-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		t.Fatalf("failed to write temp file: %v", err)
	}
	_ = f.Close()
	return f.Name()
}

func uploadFile(t *testing.T, filePath string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer func() { _ = file.Close() }()

	part, err := writer.CreateFormFile("file", filePath)
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		t.Fatalf("failed to copy file: %v", err)
	}

	_ = writer.Close()
	return body, writer.FormDataContentType()
}

func getJSON(t *testing.T, path string) (int, map[string]interface{}) {
	t.Helper()
	resp, err := http.Get(apiBase + path)
	if err != nil {
		t.Fatalf("request to %s failed: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("response from %s is not valid JSON object: %v\nbody: %s", path, err, body)
	}
	return resp.StatusCode, result
}

func postFile(t *testing.T, endpoint, source string) (*http.Response, []byte) {
	t.Helper()
	testFile := createTestFile(t, "test content")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := uploadFile(t, testFile)

	u := apiBase + endpoint
	if source != "" {
		u += "?source=" + url.QueryEscape(source)
	}
	req, err := http.NewRequest("POST", u, body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, respBody
}

// LogEntry represents the JSON log structure written to stdout.
type LogEntry struct {
	Time     string `json:"time"`
	Method   string `json:"method"`
	URI      string `json:"uri"`
	Handler  string `json:"handler"`
	Duration int64  `json:"duration"`
	Response string `json:"response"`
}

func parseLogEntries(stdout string) []LogEntry {
	var entries []LogEntry
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

// ==================== Tests ====================

func TestGETEndpoints(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		handler        string // expected handler name in stdout log
		requiredFields []string
	}{
		{
			name:           "Info",
			path:           "/info",
			handler:        "Info",
			requiredFields: []string{"version_info", "arguments", "license_expiration", "license_owner", "threads"},
		},
		{
			name:           "Status",
			path:           "/status",
			handler:        "Status",
			requiredFields: []string{"scanned_samples", "queued_async_requests", "avg_scan_time_milliseconds", "avg_wait_time_milliseconds"},
		},
		{
			name:    "QueueHistory",
			path:    "/queueHistory",
			handler: "QueueHistory",
		},
		{
			name:    "QueueHistory with params",
			path:    "/queueHistory?aggregate=5&limit=60",
			handler: "QueueHistory",
		},
		{
			name:    "SampleHistory",
			path:    "/sampleHistory",
			handler: "SampleHistory",
		},
		{
			name:    "SampleHistory with params",
			path:    "/sampleHistory?aggregate=10&limit=120",
			handler: "SampleHistory",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server.getAndClearStdout() // drain previous output

			resp, err := http.Get(apiBase + tc.path)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200, got %d", resp.StatusCode)
			}

			ct := resp.Header.Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				t.Errorf("expected Content-Type application/json, got %s", ct)
			}

			body, _ := io.ReadAll(resp.Body)
			var result map[string]interface{}
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatalf("response is not valid JSON: %v", err)
			}

			for _, field := range tc.requiredFields {
				if _, ok := result[field]; !ok {
					t.Errorf("missing required field: %s", field)
				}
			}

			// Verify that the stdout log matches the HTTP exchange.
			stdout := server.getAndClearStdout()
			entries := parseLogEntries(stdout)
			if len(entries) == 0 {
				t.Fatal("no log entry found on stdout")
			}

			entry := entries[len(entries)-1]

			if entry.Handler != tc.handler {
				t.Errorf("stdout: expected handler %q, got %q", tc.handler, entry.Handler)
			}
			if entry.Method != "GET" {
				t.Errorf("stdout: expected method GET, got %q", entry.Method)
			}
			expectedURI := "/api/v1" + tc.path
			if entry.URI != expectedURI {
				t.Errorf("stdout: expected URI %q, got %q", expectedURI, entry.URI)
			}
			if entry.Time == "" {
				t.Error("stdout: expected non-empty time field")
			}

			// The logged response must match the actual HTTP response body.
			var loggedResponse map[string]interface{}
			if err := json.Unmarshal([]byte(entry.Response), &loggedResponse); err != nil {
				t.Fatalf("stdout: response is not valid JSON: %v", err)
			}

			// Re-serialize both to compare (avoids whitespace/key-order issues).
			actualJSON, _ := json.Marshal(result)
			loggedJSON, _ := json.Marshal(loggedResponse)
			if string(actualJSON) != string(loggedJSON) {
				t.Errorf("stdout response does not match HTTP response\nHTTP:   %s\nstdout: %s", actualJSON, loggedJSON)
			}
		})
	}
}

func TestSynchronousScan(t *testing.T) {
	resp, respBody := postFile(t, "/check", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var report []map[string]interface{}
	if err := json.Unmarshal(respBody, &report); err != nil {
		t.Fatalf("response is not valid JSON array: %v", err)
	}
	if len(report) == 0 {
		t.Fatal("expected at least one finding in report")
	}

	finding := report[0]
	if finding["type"] != "THOR Finding" {
		t.Errorf("expected type 'THOR Finding', got '%v'", finding["type"])
	}
	if finding["hash"] == nil || finding["hash"] == "" {
		t.Error("expected non-empty hash in finding")
	}

	// Verify custom source is reflected when provided.
	t.Run("CustomSource", func(t *testing.T) {
		resp, respBody := postFile(t, "/check", "my-custom-source")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		var r []map[string]interface{}
		_ = json.Unmarshal(respBody, &r)
		if len(r) > 0 && r[0]["source"] != "my-custom-source" {
			t.Errorf("expected source 'my-custom-source', got '%v'", r[0]["source"])
		}
	})

	// Verify stdout logging for the scan.
	stdout := server.getAndClearStdout()
	if !strings.Contains(stdout, `"handler":"Check"`) {
		t.Error("expected Check handler to be logged")
	}
}

func TestAsynchronousScan(t *testing.T) {
	// Step 1: Submit file.
	resp, respBody := postFile(t, "/checkAsync", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var sampleID map[string]interface{}
	if err := json.Unmarshal(respBody, &sampleID); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	id, ok := sampleID["id"].(float64)
	if !ok || id <= 0 {
		t.Fatalf("expected positive sample ID, got %v", sampleID["id"])
	}

	asyncURL := fmt.Sprintf("%s/getAsyncResults?id=%.0f", apiBase, id)

	// Step 2: Immediately check — should be waiting.
	_, result := getJSON(t, fmt.Sprintf("/getAsyncResults?id=%.0f", id))
	if result["status"] != "Waiting for execution" {
		t.Errorf("expected 'Waiting for execution', got '%v'", result["status"])
	}

	// Step 3: After ~2.5s — should be in progress.
	time.Sleep(2500 * time.Millisecond)
	resp2, err := http.Get(asyncURL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	var inProgress map[string]interface{}
	_ = json.Unmarshal(body2, &inProgress)
	if inProgress["status"] != "Currently being scanned" {
		t.Errorf("expected 'Currently being scanned', got '%v'", inProgress["status"])
	}

	// Step 4: After ~5s total — should be complete.
	time.Sleep(3 * time.Second)
	_, complete := getJSON(t, fmt.Sprintf("/getAsyncResults?id=%.0f", id))
	if complete["status"] != "Sample analysis complete" {
		t.Errorf("expected 'Sample analysis complete', got '%v'", complete["status"])
	}
	if complete["result"] == nil {
		t.Error("expected result field when status is complete")
	}

	// Verify stdout logging.
	stdout := server.getAndClearStdout()
	if !strings.Contains(stdout, `"handler":"CheckAsync"`) {
		t.Error("expected CheckAsync handler to be logged")
	}
	if !strings.Contains(stdout, `"handler":"GetAsyncResults"`) {
		t.Error("expected GetAsyncResults handler to be logged")
	}
}

func TestStatusReflectsScans(t *testing.T) {
	_, initial := getJSON(t, "/status")
	initialScans := initial["scanned_samples"].(float64)

	// Perform a sync scan.
	resp, _ := postFile(t, "/check", "")
	_ = resp.Body.Close()

	_, updated := getJSON(t, "/status")
	newScans := updated["scanned_samples"].(float64)

	if newScans <= initialScans {
		t.Errorf("expected scanned_samples to increase, was %.0f, now %.0f", initialScans, newScans)
	}
}

func TestSpecialSyncErrorTriggers(t *testing.T) {
	testFile := createTestFile(t, "test content")
	defer func() { _ = os.Remove(testFile) }()

	tests := []struct {
		name                 string
		endpoint             string
		specialSourceTrigger string
		expectedStatus       int
		expectedMsg          string
	}{
		{"Check 400", "/check", "error 400", http.StatusBadRequest, "Invalid parameters given"},
		{"Check 500", "/check", "error 500", http.StatusInternalServerError, "Internal server error"},
		{"CheckAsync 400", "/checkAsync", "error 400", http.StatusBadRequest, "Invalid parameters given"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, contentType := uploadFile(t, testFile)
			endpoint := fmt.Sprintf("%s%s?source=%s", apiBase, tc.endpoint, url.QueryEscape(tc.specialSourceTrigger))
			req, _ := http.NewRequest("POST", endpoint, body)
			req.Header.Set("Content-Type", contentType)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}

			respBody, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(respBody), tc.expectedMsg) {
				t.Errorf("expected message '%s' in response, got: %s", tc.expectedMsg, respBody)
			}
		})
	}
}

func TestSpecialAsyncErrorTriggers(t *testing.T) {
	tests := []struct {
		name             string
		specialIDTrigger string
		expectedStatus   int
		expectedMsg      string // empty means skip body check
	}{
		{"Crashed (id=0)", "0", http.StatusOK, "Sample analysis failed"},
		{"Bad request (id=-400)", "-400", http.StatusBadRequest, ""},
		{"Server error (id=-500)", "-500", http.StatusInternalServerError, ""},
		{"Invalid ID", "999999999", http.StatusBadRequest, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(fmt.Sprintf("%s/getAsyncResults?id=%s", apiBase, tc.specialIDTrigger))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}

			if tc.expectedMsg != "" {
				body, _ := io.ReadAll(resp.Body)
				if !strings.Contains(string(body), tc.expectedMsg) {
					t.Errorf("expected '%s' in body, got: %s", tc.expectedMsg, body)
				}
			}
		})
	}
}
