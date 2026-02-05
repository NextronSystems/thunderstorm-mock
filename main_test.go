//go:build integration

/*
 * THOR Thunderstorm Mock Server - Integration Tests
 *
 * These tests treat the mock server as a black box:
 * 1. Build and start the server as a subprocess
 * 2. Send HTTP requests to the server
 * 3. Verify responses match the OpenAPI specification
 * 4. Verify stdout logging works correctly (for testing collector clients)
 *
 * Run with: go test -tags=integration -v
 *
 * Author: Claude Opus 4.5
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

// serverProcess holds the running server process and its stdout
type serverProcess struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	stdoutMu  sync.Mutex
	stdoutBuf bytes.Buffer
	wg        sync.WaitGroup
}

// startServer builds and starts the mock server, returning stdout for log verification
func startServer(t *testing.T) *serverProcess {
	t.Helper()

	// Build the server
	buildCmd := exec.Command("go", "build", "-o", "thunderstorm-mock-test", ".")
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("failed to build server: %v", err)
	}

	// Create context for the server process
	ctx, cancel := context.WithCancel(context.Background())

	// Start the server
	cmd := exec.CommandContext(ctx, "./thunderstorm-mock-test")

	// Capture stdout for log verification
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("failed to start server: %v", err)
	}

	sp := &serverProcess{
		cmd:    cmd,
		cancel: cancel,
	}

	// Read stdout in background goroutine
	sp.wg.Add(1)
	go func() {
		defer sp.wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			sp.stdoutMu.Lock()
			sp.stdoutBuf.WriteString(line)
			sp.stdoutBuf.WriteString("\n")
			sp.stdoutMu.Unlock()
		}
	}()

	// Wait for server to be ready
	if !waitForServer(t, serverAddr, startupDelay) {
		sp.stop(t)
		t.Fatal("server failed to start in time")
	}

	return sp
}

// stop gracefully shuts down the server
func (sp *serverProcess) stop(t *testing.T) {
	t.Helper()

	// Send interrupt signal for graceful shutdown
	if sp.cmd.Process != nil {
		_ = sp.cmd.Process.Signal(syscall.SIGTERM)
	}

	// Wait briefly then force cancel
	time.Sleep(shutdownDelay)
	sp.cancel()
	_ = sp.cmd.Wait()
	sp.wg.Wait()

	// Clean up binary
	_ = os.Remove("./thunderstorm-mock-test")
}

// getStdout returns collected stdout output
func (sp *serverProcess) getStdout() string {
	// Give a moment for logs to flush
	time.Sleep(200 * time.Millisecond)
	sp.stdoutMu.Lock()
	defer sp.stdoutMu.Unlock()
	return sp.stdoutBuf.String()
}

// waitForServer polls the server until it responds or timeout
func waitForServer(t *testing.T, addr string, timeout time.Duration) bool {
	t.Helper()
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

// createTestFile creates a temporary file for upload
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

// uploadFile creates a multipart request body with a file
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

// LogEntry represents the JSON log structure written to stdout
type LogEntry struct {
	Time     string `json:"time"`
	Method   string `json:"method"`
	URI      string `json:"uri"`
	Handler  string `json:"handler"`
	Duration int64  `json:"duration"`
	Response string `json:"response"`
}

// parseLogEntries extracts log entries from stdout
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

// ==================== Integration Tests ====================

func TestServerStartsAndResponds(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	// Simple health check - server should respond
	resp, err := http.Get(apiBase + "/status")
	if err != nil {
		t.Fatalf("server not responding: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestInfoEndpointReturnsValidJSON(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	resp, err := http.Get(apiBase + "/info")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)

	// Verify it's valid JSON with expected structure
	var info map[string]interface{}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	// Check required fields per OpenAPI spec
	requiredFields := []string{"version_info", "arguments", "license_expiration", "license_owner", "threads"}
	for _, field := range requiredFields {
		if _, ok := info[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// Verify stdout logging
	stdout := server.getStdout()
	if !strings.Contains(stdout, `"handler":"Info"`) {
		t.Error("expected Info handler to be logged to stdout")
	}
	if !strings.Contains(stdout, `"method":"GET"`) {
		t.Error("expected GET method to be logged")
	}
}

func TestStatusEndpointReturnsValidJSON(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	resp, err := http.Get(apiBase + "/status")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	var status map[string]interface{}
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	// Check required fields per OpenAPI spec
	requiredFields := []string{"scanned_samples", "queued_async_requests", "avg_scan_time_milliseconds", "avg_wait_time_milliseconds"}
	for _, field := range requiredFields {
		if _, ok := status[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}
}

func TestSynchronousScanWorkflow(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	// Create and upload a test file
	testFile := createTestFile(t, "test malware content for sync scan")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := uploadFile(t, testFile)

	req, err := http.NewRequest("POST", apiBase+"/check", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)

	// Response should be an array (ThorReport)
	var report []map[string]interface{}
	if err := json.Unmarshal(respBody, &report); err != nil {
		t.Fatalf("response is not valid JSON array: %v", err)
	}

	if len(report) == 0 {
		t.Error("expected at least one finding in report")
	}

	// Verify finding structure
	if len(report) > 0 {
		finding := report[0]
		if finding["type"] != "THOR Finding" {
			t.Errorf("expected type 'THOR Finding', got '%v'", finding["type"])
		}
		if finding["hash"] == nil || finding["hash"] == "" {
			t.Error("expected non-empty hash in finding")
		}
	}

	// Verify stdout logging contains response
	stdout := server.getStdout()
	if !strings.Contains(stdout, `"handler":"Check"`) {
		t.Error("expected Check handler to be logged")
	}
	// Note: The response is embedded as a JSON string, so quotes are escaped
	if !strings.Contains(stdout, `\"THOR Finding\"`) {
		t.Error("expected finding to be logged in response")
	}
}

func TestAsynchronousScanWorkflow(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	// Step 1: Submit file for async scan
	testFile := createTestFile(t, "test malware content for async scan")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := uploadFile(t, testFile)

	req, err := http.NewRequest("POST", apiBase+"/checkAsync", body)
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

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var sampleId map[string]interface{}
	if err := json.Unmarshal(respBody, &sampleId); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	id, ok := sampleId["id"].(float64)
	if !ok || id <= 0 {
		t.Fatalf("expected positive sample ID, got %v", sampleId["id"])
	}

	t.Logf("Received sample ID: %.0f", id)

	// Step 2: Check status immediately (should be waiting)
	resultResp, err := http.Get(fmt.Sprintf("%s/getAsyncResults?id=%.0f", apiBase, id))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resultBody, _ := io.ReadAll(resultResp.Body)
	_ = resultResp.Body.Close()

	var result map[string]interface{}
	_ = json.Unmarshal(resultBody, &result)

	if result["status"] != "Waiting for execution" {
		t.Logf("Initial status: %v (expected 'Waiting for execution')", result["status"])
	}

	// Step 3: Wait for scan to complete and check again
	time.Sleep(6 * time.Second)

	resultResp, err = http.Get(fmt.Sprintf("%s/getAsyncResults?id=%.0f", apiBase, id))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resultBody, _ = io.ReadAll(resultResp.Body)
	_ = resultResp.Body.Close()

	_ = json.Unmarshal(resultBody, &result)

	if result["status"] != "Sample analysis complete" {
		t.Errorf("expected 'Sample analysis complete', got '%v'", result["status"])
	}

	// Verify result contains findings
	if result["result"] == nil {
		t.Error("expected result field in completed scan")
	}

	// Verify stdout logging
	stdout := server.getStdout()
	if !strings.Contains(stdout, `"handler":"CheckAsync"`) {
		t.Error("expected CheckAsync handler to be logged")
	}
	if !strings.Contains(stdout, `"handler":"GetAsyncResults"`) {
		t.Error("expected GetAsyncResults handler to be logged")
	}
}

func TestStdoutLoggingFormat(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	// Make a request
	resp, err := http.Get(apiBase + "/info")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	// Give time for log to be written
	stdout := server.getStdout()
	entries := parseLogEntries(stdout)

	if len(entries) == 0 {
		t.Fatal("no log entries found in stdout")
	}

	// Find the info request log entry
	var infoEntry *LogEntry
	for i := range entries {
		if entries[i].Handler == "Info" {
			infoEntry = &entries[i]
			break
		}
	}

	if infoEntry == nil {
		t.Fatal("Info handler log entry not found")
	}

	// Verify log entry structure
	if infoEntry.Method != "GET" {
		t.Errorf("expected method 'GET', got '%s'", infoEntry.Method)
	}
	if infoEntry.URI != "/api/v1/info" {
		t.Errorf("expected URI '/api/v1/info', got '%s'", infoEntry.URI)
	}
	if infoEntry.Time == "" {
		t.Error("expected non-empty time field")
	}
	if infoEntry.Response == "" {
		t.Error("expected non-empty response field")
	}

	// Verify response in log is valid JSON
	var loggedResponse map[string]interface{}
	if err := json.Unmarshal([]byte(infoEntry.Response), &loggedResponse); err != nil {
		t.Errorf("logged response is not valid JSON: %v", err)
	}
}

func TestErrorTriggers(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	testFile := createTestFile(t, "test content")
	defer func() { _ = os.Remove(testFile) }()

	tests := []struct {
		name           string
		endpoint       string
		source         string
		expectedStatus int
		expectedMsg    string
	}{
		{
			name:           "Check 400 error",
			endpoint:       "/check",
			source:         "error 400",
			expectedStatus: http.StatusBadRequest,
			expectedMsg:    "Invalid parameters given",
		},
		{
			name:           "Check 500 error",
			endpoint:       "/check",
			source:         "error 500",
			expectedStatus: http.StatusInternalServerError,
			expectedMsg:    "Internal server error",
		},
		{
			name:           "CheckAsync 400 error",
			endpoint:       "/checkAsync",
			source:         "error 400",
			expectedStatus: http.StatusBadRequest,
			expectedMsg:    "Invalid parameters given",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, contentType := uploadFile(t, testFile)

			// Properly URL-encode the source parameter
			endpoint := fmt.Sprintf("%s%s?source=%s", apiBase, tc.endpoint, url.QueryEscape(tc.source))
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
				t.Errorf("expected message '%s' in response, got: %s", tc.expectedMsg, string(respBody))
			}
		})
	}
}

func TestGetAsyncResultsErrorTriggers(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	tests := []struct {
		name           string
		id             string
		expectedStatus int
	}{
		{"Crashed trigger (id=0)", "0", http.StatusOK},
		{"Bad request trigger (id=-400)", "-400", http.StatusBadRequest},
		{"Server error trigger (id=-500)", "-500", http.StatusInternalServerError},
		{"Invalid ID", "999999999", http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(fmt.Sprintf("%s/getAsyncResults?id=%s", apiBase, tc.id))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}
		})
	}

	// Special check for crashed status message
	resp, _ := http.Get(apiBase + "/getAsyncResults?id=0")
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if !strings.Contains(string(body), "Sample analysis failed") {
		t.Error("expected 'Sample analysis failed' status for id=0")
	}
}

func TestHistoryEndpoints(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	endpoints := []struct {
		name string
		path string
	}{
		{"QueueHistory", "/queueHistory"},
		{"QueueHistory with params", "/queueHistory?aggregate=5&limit=60"},
		{"SampleHistory", "/sampleHistory"},
		{"SampleHistory with params", "/sampleHistory?aggregate=10&limit=120"},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			resp, err := http.Get(apiBase + ep.path)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200, got %d", resp.StatusCode)
			}

			body, _ := io.ReadAll(resp.Body)

			// Should be valid JSON object (TimestampMap)
			var history map[string]interface{}
			if err := json.Unmarshal(body, &history); err != nil {
				t.Errorf("response is not valid JSON: %v", err)
			}
		})
	}
}

func TestStatusReflectsScans(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	// Get initial status
	resp, _ := http.Get(apiBase + "/status")
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	var initialStatus map[string]interface{}
	_ = json.Unmarshal(body, &initialStatus)
	initialScans := initialStatus["scanned_samples"].(float64)

	// Perform a sync scan
	testFile := createTestFile(t, "content to increase scan count")
	defer func() { _ = os.Remove(testFile) }()

	uploadBody, contentType := uploadFile(t, testFile)
	req, _ := http.NewRequest("POST", apiBase+"/check", uploadBody)
	req.Header.Set("Content-Type", contentType)
	scanResp, _ := http.DefaultClient.Do(req)
	_ = scanResp.Body.Close()

	// Get updated status
	resp, _ = http.Get(apiBase + "/status")
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	var newStatus map[string]interface{}
	_ = json.Unmarshal(body, &newStatus)
	newScans := newStatus["scanned_samples"].(float64)

	if newScans <= initialScans {
		t.Errorf("expected scanned_samples to increase, was %.0f, now %.0f", initialScans, newScans)
	}
}

func TestResponseContentType(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	endpoints := []string{"/info", "/status", "/queueHistory", "/sampleHistory"}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			resp, err := http.Get(apiBase + ep)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			ct := resp.Header.Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				t.Errorf("expected Content-Type application/json, got %s", ct)
			}
		})
	}
}

func TestScanWithCustomSource(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	testFile := createTestFile(t, "test content with source")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := uploadFile(t, testFile)

	// Use a custom source parameter
	endpoint := apiBase + "/check?source=" + url.QueryEscape("my-custom-source")
	req, _ := http.NewRequest("POST", endpoint, body)
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	var report []map[string]interface{}
	_ = json.Unmarshal(respBody, &report)

	if len(report) > 0 && report[0]["source"] != "my-custom-source" {
		t.Errorf("expected source 'my-custom-source', got '%v'", report[0]["source"])
	}
}

func TestAsyncStateProgression(t *testing.T) {
	server := startServer(t)
	defer server.stop(t)

	testFile := createTestFile(t, "async state progression test")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := uploadFile(t, testFile)
	req, _ := http.NewRequest("POST", apiBase+"/checkAsync", body)
	req.Header.Set("Content-Type", contentType)

	resp, _ := http.DefaultClient.Do(req)
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	var sampleId map[string]interface{}
	_ = json.Unmarshal(respBody, &sampleId)
	id := sampleId["id"].(float64)

	// State 1: Waiting (immediate)
	resultResp, _ := http.Get(fmt.Sprintf("%s/getAsyncResults?id=%.0f", apiBase, id))
	resultBody, _ := io.ReadAll(resultResp.Body)
	_ = resultResp.Body.Close()

	var result map[string]interface{}
	_ = json.Unmarshal(resultBody, &result)

	if result["status"] != "Waiting for execution" {
		t.Errorf("expected 'Waiting for execution', got '%v'", result["status"])
	}

	// State 2: In Progress (after ~2 seconds)
	time.Sleep(2500 * time.Millisecond)

	resultResp, _ = http.Get(fmt.Sprintf("%s/getAsyncResults?id=%.0f", apiBase, id))
	resultBody, _ = io.ReadAll(resultResp.Body)
	_ = resultResp.Body.Close()

	_ = json.Unmarshal(resultBody, &result)

	if result["status"] != "Currently being scanned" {
		t.Errorf("expected 'Currently being scanned', got '%v'", result["status"])
	}

	// State 3: Complete (after ~5 seconds total)
	time.Sleep(3 * time.Second)

	resultResp, _ = http.Get(fmt.Sprintf("%s/getAsyncResults?id=%.0f", apiBase, id))
	resultBody, _ = io.ReadAll(resultResp.Body)
	_ = resultResp.Body.Close()

	_ = json.Unmarshal(resultBody, &result)

	if result["status"] != "Sample analysis complete" {
		t.Errorf("expected 'Sample analysis complete', got '%v'", result["status"])
	}

	// Should have results when complete
	if result["result"] == nil {
		t.Error("expected result field when status is complete")
	}
}
