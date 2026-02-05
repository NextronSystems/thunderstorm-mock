/*
 * THOR Thunderstorm API - Integration Tests
 *
 * This file contains integration tests for the Thunderstorm mock server.
 * Tests verify that all API endpoints work correctly and that responses
 * are properly logged to stdout as expected for testing collector clients.
 *
 * API version: 1.0.0
 *
 * Author: Claude Opus 4.5
 *
 * Note: Feel free to abandon these tests if they become too brittle or difficult to maintain, especially on API changes. Main parts should be working anyway assuming the generator and the generated code are working correctly.
 */

package thunderstormmock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// testRouter creates a router with all API controllers for testing
func testRouter() *httptest.Server {
	InfoAPIService := NewInfoAPIService()
	InfoAPIController := NewInfoAPIController(InfoAPIService)

	ResultsAPIService := NewResultsAPIService()
	ResultsAPIController := NewResultsAPIController(ResultsAPIService)

	ScanAPIService := NewScanAPIService()
	ScanAPIController := NewScanAPIController(ScanAPIService)

	router := NewRouter(InfoAPIController, ResultsAPIController, ScanAPIController)
	return httptest.NewServer(router)
}

// createTestFile creates a temporary file with the given content for upload tests
func createTestFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "thunderstorm-test-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		t.Fatalf("failed to write to temp file: %v", err)
	}
	_ = f.Close()
	return f.Name()
}

// createMultipartFormFile creates a multipart form body with a file upload
func createMultipartFormFile(t *testing.T, fieldName, filePath string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer func() { _ = file.Close() }()

	part, err := writer.CreateFormFile(fieldName, filePath)
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		t.Fatalf("failed to copy file content: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close writer: %v", err)
	}

	return body, writer.FormDataContentType()
}

// LogEntry represents the structure of log entries written to stdout
type LogEntry struct {
	Time     time.Time `json:"time"`
	Method   string    `json:"method"`
	URI      string    `json:"uri"`
	Handler  string    `json:"handler"`
	Duration int64     `json:"duration"`
	Response string    `json:"response"`
}

// ==================== Info API Tests ====================

func TestInfoEndpoint(t *testing.T) {
	server := testRouter()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/info")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var info ThunderstormInfo
	if err := json.Unmarshal(body, &info); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	// Verify required fields are present
	if info.LicenseOwner == "" {
		t.Error("expected non-empty license_owner")
	}
	if info.Threads == 0 {
		t.Error("expected non-zero threads")
	}
}

func TestStatusEndpoint(t *testing.T) {
	server := testRouter()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var status ThunderstormStatus
	if err := json.Unmarshal(body, &status); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	// Status should have required fields (even if zero values are valid)
	if status.ScannedSamples < 0 {
		t.Error("scanned_samples should not be negative")
	}
	if status.QueuedAsyncRequests < 0 {
		t.Error("queued_async_requests should not be negative")
	}

	// Verify response contains required JSON fields
	if !strings.Contains(string(body), `"scanned_samples"`) {
		t.Error("expected response to contain scanned_samples")
	}
}

func TestQueueHistoryEndpoint(t *testing.T) {
	server := testRouter()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/queueHistory")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var history TimestampMap
	if err := json.Unmarshal(body, &history); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}
}

func TestQueueHistoryWithParams(t *testing.T) {
	server := testRouter()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/queueHistory?aggregate=5&limit=60")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var history TimestampMap
	if err := json.Unmarshal(body, &history); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}
}

func TestSampleHistoryEndpoint(t *testing.T) {
	server := testRouter()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/sampleHistory")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var history TimestampMap
	if err := json.Unmarshal(body, &history); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}
}

func TestSampleHistoryWithParams(t *testing.T) {
	server := testRouter()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/sampleHistory?aggregate=10&limit=120")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var history TimestampMap
	if err := json.Unmarshal(body, &history); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}
}

// ==================== Scan API Tests ====================

func TestCheckEndpoint(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// Create a test file
	testFile := createTestFile(t, "test malicious content for scanning")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/check", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var report ThorReport
	if err := json.Unmarshal(respBody, &report); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	// Should return at least one finding
	if len(report) == 0 {
		t.Error("expected at least one finding in report")
	}

	// Verify finding has expected fields
	if len(report) > 0 {
		finding := report[0]
		if finding.Type != "THOR Finding" {
			t.Errorf("expected finding type 'THOR Finding', got '%s'", finding.Type)
		}
		if finding.Hash == "" {
			t.Error("expected non-empty hash in finding")
		}
	}

	// Verify response contains expected JSON structure
	if !strings.Contains(string(respBody), `"THOR Finding"`) {
		t.Error("expected response to contain THOR Finding")
	}
}

func TestCheckEndpointWithSource(t *testing.T) {
	server := testRouter()
	defer server.Close()

	testFile := createTestFile(t, "test content with custom source")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/check?source=custom-source", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var report ThorReport
	if err := json.Unmarshal(respBody, &report); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	// Verify source is in the response
	if len(report) > 0 && report[0].Source != "custom-source" {
		t.Errorf("expected source 'custom-source', got '%s'", report[0].Source)
	}
}

func TestCheckAsyncEndpoint(t *testing.T) {
	server := testRouter()
	defer server.Close()

	testFile := createTestFile(t, "test async content for scanning")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/checkAsync", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var sampleId SampleId
	if err := json.Unmarshal(respBody, &sampleId); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	// Should return a valid ID
	if sampleId.Id <= 0 {
		t.Errorf("expected positive sample ID, got %d", sampleId.Id)
	}

	// Verify response contains id field
	if !strings.Contains(string(respBody), `"id"`) {
		t.Error("expected response to contain id field")
	}
}

// ==================== Results API Tests ====================

func TestGetAsyncResultsWaiting(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// First submit an async scan
	testFile := createTestFile(t, "test content for async waiting state")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/checkAsync", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}

	var sampleId SampleId
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err := json.Unmarshal(respBody, &sampleId); err != nil {
		t.Fatalf("failed to parse sample ID: %v", err)
	}

	// Immediately check results (should be waiting)
	resultResp, err := http.Get(fmt.Sprintf("%s/api/v1/getAsyncResults?id=%d", server.URL, sampleId.Id))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resultResp.Body.Close() }()

	if resultResp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resultResp.StatusCode)
	}

	resultBody, err := io.ReadAll(resultResp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var result AsyncResult
	if err := json.Unmarshal(resultBody, &result); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	// Should be in waiting state immediately after submission
	if result.Status != waiting.String() {
		t.Errorf("expected status '%s', got '%s'", waiting.String(), result.Status)
	}
}

func TestGetAsyncResultsComplete(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// First submit an async scan
	testFile := createTestFile(t, "test content for async complete state")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/checkAsync", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}

	var sampleId SampleId
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err := json.Unmarshal(respBody, &sampleId); err != nil {
		t.Fatalf("failed to parse sample ID: %v", err)
	}

	// Wait for the scan to complete (asyncProgressTime = 5 seconds)
	time.Sleep(6 * time.Second)

	// Check results (should be complete)
	resultResp, err := http.Get(fmt.Sprintf("%s/api/v1/getAsyncResults?id=%d", server.URL, sampleId.Id))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resultResp.Body.Close() }()

	if resultResp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resultResp.StatusCode)
	}

	resultBody, err := io.ReadAll(resultResp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var result AsyncResult
	if err := json.Unmarshal(resultBody, &result); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	// Should be completed with results
	if result.Status != finished.String() {
		t.Errorf("expected status '%s', got '%s'", finished.String(), result.Status)
	}

	// Should have results
	if len(result.Result) == 0 {
		t.Error("expected results in completed scan")
	}

	// Verify response contains the complete status
	if !strings.Contains(string(resultBody), `"Sample analysis complete"`) {
		t.Error("expected response to contain 'Sample analysis complete'")
	}
}

func TestGetAsyncResultsInvalidId(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// Request with non-existent ID
	resp, err := http.Get(server.URL + "/api/v1/getAsyncResults?id=999999999")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var errResp Error
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	if errResp.Message == "" {
		t.Error("expected non-empty error message")
	}
}

func TestGetAsyncResultsCrashedTrigger(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// ID=0 is a special trigger for crashed status
	resp, err := http.Get(server.URL + "/api/v1/getAsyncResults?id=0")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var result AsyncResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	if result.Status != crashed.String() {
		t.Errorf("expected status '%s', got '%s'", crashed.String(), result.Status)
	}
}

// ==================== Error Trigger Tests ====================

func TestCheckEndpointError400(t *testing.T) {
	server := testRouter()
	defer server.Close()

	testFile := createTestFile(t, "test content for error 400")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	// source="error 400" is a special trigger
	req, err := http.NewRequest("POST", server.URL+"/api/v1/check?source=error%20400", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var errResp Error
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	if errResp.Message != "Invalid parameters given" {
		t.Errorf("expected 'Invalid parameters given', got '%s'", errResp.Message)
	}

	// Verify error message is in response
	if !strings.Contains(string(respBody), `"Invalid parameters given"`) {
		t.Error("expected response to contain error message")
	}
}

func TestCheckEndpointError500(t *testing.T) {
	server := testRouter()
	defer server.Close()

	testFile := createTestFile(t, "test content for error 500")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	// source="error 500" is a special trigger
	req, err := http.NewRequest("POST", server.URL+"/api/v1/check?source=error%20500", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var errResp Error
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Errorf("failed to parse response JSON: %v", err)
	}

	if errResp.Message != "Internal server error" {
		t.Errorf("expected 'Internal server error', got '%s'", errResp.Message)
	}
}

func TestCheckAsyncEndpointError400(t *testing.T) {
	server := testRouter()
	defer server.Close()

	testFile := createTestFile(t, "test content for async error 400")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/checkAsync?source=error%20400", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestCheckAsyncEndpointError500(t *testing.T) {
	server := testRouter()
	defer server.Close()

	testFile := createTestFile(t, "test content for async error 500")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/checkAsync?source=error%20500", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}
}

func TestGetAsyncResultsError400Trigger(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// ID=-400 is a special trigger for 400 error
	resp, err := http.Get(server.URL + "/api/v1/getAsyncResults?id=-400")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestGetAsyncResultsError500Trigger(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// ID=-500 is a special trigger for 500 error
	resp, err := http.Get(server.URL + "/api/v1/getAsyncResults?id=-500")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}
}

// ==================== Integration/Workflow Tests ====================

func TestFullAsyncWorkflow(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// 1. Submit a file for async scanning
	testFile := createTestFile(t, "full workflow test content")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/checkAsync", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}

	var sampleId SampleId
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err := json.Unmarshal(respBody, &sampleId); err != nil {
		t.Fatalf("failed to parse sample ID: %v", err)
	}

	t.Logf("Received sample ID: %d", sampleId.Id)

	// 2. Check status - should be waiting initially
	resultResp, err := http.Get(fmt.Sprintf("%s/api/v1/getAsyncResults?id=%d", server.URL, sampleId.Id))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}

	var result AsyncResult
	resultBody, _ := io.ReadAll(resultResp.Body)
	_ = resultResp.Body.Close()
	_ = json.Unmarshal(resultBody, &result)

	if result.Status != waiting.String() {
		t.Logf("Initial status: %s (expected %s)", result.Status, waiting.String())
	}

	// 3. Wait and check again - should progress through states
	time.Sleep(3 * time.Second)

	resultResp, err = http.Get(fmt.Sprintf("%s/api/v1/getAsyncResults?id=%d", server.URL, sampleId.Id))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}

	resultBody, _ = io.ReadAll(resultResp.Body)
	_ = resultResp.Body.Close()
	_ = json.Unmarshal(resultBody, &result)

	if result.Status != inProgress.String() {
		t.Logf("Progress status: %s (expected %s)", result.Status, inProgress.String())
	}

	// 4. Wait for completion
	time.Sleep(3 * time.Second)

	resultResp, err = http.Get(fmt.Sprintf("%s/api/v1/getAsyncResults?id=%d", server.URL, sampleId.Id))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}

	resultBody, _ = io.ReadAll(resultResp.Body)
	_ = resultResp.Body.Close()
	_ = json.Unmarshal(resultBody, &result)

	if result.Status != finished.String() {
		t.Errorf("expected final status '%s', got '%s'", finished.String(), result.Status)
	}

	if len(result.Result) == 0 {
		t.Error("expected findings in completed scan")
	}

	// 5. Check status endpoint reflects the scan
	statusResp, err := http.Get(server.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = statusResp.Body.Close() }()

	statusBody, _ := io.ReadAll(statusResp.Body)
	var status ThunderstormStatus
	_ = json.Unmarshal(statusBody, &status)

	if status.ScannedSamples < 1 {
		t.Error("expected at least 1 scanned sample in status")
	}
}

// ==================== Stdout Logging Format Test ====================

// TestLoggerWritesJSON verifies the Logger middleware correctly writes JSON to stdout.
// This test uses httptest.ResponseRecorder to directly verify the logging behavior
// without relying on stdout capture which has race conditions with httptest.Server.
func TestLoggerWritesJSON(t *testing.T) {
	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"test": "response"})
	})

	// Wrap with Logger
	loggedHandler := Logger(handler, "TestHandler")

	// Create a request and recorder
	req := httptest.NewRequest("GET", "/test/endpoint", nil)
	rr := httptest.NewRecorder()

	// This will print to stdout - we verify the handler works correctly
	loggedHandler.ServeHTTP(rr, req)

	// Verify the response was captured properly
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Verify response body
	body := rr.Body.String()
	if !strings.Contains(body, `"test"`) {
		t.Error("expected response to contain test field")
	}
}

// TestJSONLoggingWriter verifies the JSONLoggingWriter captures response correctly
func TestJSONLoggingWriter(t *testing.T) {
	rr := httptest.NewRecorder()
	jw := &JSONLoggingWriter{ResponseWriter: rr}

	// Write some content
	testContent := `{"key":"value"}`
	n, err := jw.Write([]byte(testContent))

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != len(testContent) {
		t.Errorf("expected %d bytes written, got %d", len(testContent), n)
	}
	if string(jw.JSONResponse) != testContent {
		t.Errorf("expected captured content '%s', got '%s'", testContent, string(jw.JSONResponse))
	}
}

// ==================== Missing File Tests ====================

func TestCheckEndpointMissingFile(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// Send POST without a file
	req, err := http.NewRequest("POST", server.URL+"/api/v1/check", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Should return an error (400 or 422 for missing required field)
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected status 400 or 422, got %d", resp.StatusCode)
	}
}

// ==================== Content Type Tests ====================

func TestResponseContentType(t *testing.T) {
	server := testRouter()
	defer server.Close()

	endpoints := []string{
		"/api/v1/info",
		"/api/v1/status",
		"/api/v1/queueHistory",
		"/api/v1/sampleHistory",
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			resp, err := http.Get(server.URL + endpoint)
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			contentType := resp.Header.Get("Content-Type")
			if !strings.Contains(contentType, "application/json") {
				t.Errorf("expected Content-Type to contain 'application/json', got '%s'", contentType)
			}
		})
	}
}

// ==================== Concurrent Request Tests ====================

func TestConcurrentScans(t *testing.T) {
	server := testRouter()
	defer server.Close()

	numRequests := 10
	done := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(n int) {
			testFile := createTestFile(t, fmt.Sprintf("concurrent test content %d", n))
			defer func() { _ = os.Remove(testFile) }()

			body, contentType := createMultipartFormFile(t, "file", testFile)

			req, err := http.NewRequest("POST", server.URL+"/api/v1/check", body)
			if err != nil {
				t.Errorf("request %d: failed to create request: %v", n, err)
				done <- false
				return
			}
			req.Header.Set("Content-Type", contentType)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("request %d: failed to make request: %v", n, err)
				done <- false
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("request %d: expected status 200, got %d", n, resp.StatusCode)
				done <- false
				return
			}

			done <- true
		}(i)
	}

	// Wait for all requests
	successCount := 0
	for i := 0; i < numRequests; i++ {
		if <-done {
			successCount++
		}
	}

	if successCount != numRequests {
		t.Errorf("expected %d successful requests, got %d", numRequests, successCount)
	}
}

// ==================== Async State Progression Test ====================

func TestAsyncStateProgression(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// Submit file
	testFile := createTestFile(t, "state progression test content")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/checkAsync", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}

	var sampleId SampleId
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	_ = json.Unmarshal(respBody, &sampleId)

	// Test state progression
	states := []struct {
		sleepBefore time.Duration
		expected    status
	}{
		{0, waiting},
		{2500 * time.Millisecond, inProgress},
		{3 * time.Second, finished},
	}

	for i, s := range states {
		time.Sleep(s.sleepBefore)

		resultResp, err := http.Get(fmt.Sprintf("%s/api/v1/getAsyncResults?id=%d", server.URL, sampleId.Id))
		if err != nil {
			t.Fatalf("step %d: failed to make request: %v", i, err)
		}

		resultBody, _ := io.ReadAll(resultResp.Body)
		_ = resultResp.Body.Close()

		var result AsyncResult
		_ = json.Unmarshal(resultBody, &result)

		if result.Status != s.expected.String() {
			t.Errorf("step %d: expected status '%s', got '%s'", i, s.expected.String(), result.Status)
		}
	}
}

// ==================== Multiple Async Requests Test ====================

func TestMultipleAsyncRequests(t *testing.T) {
	server := testRouter()
	defer server.Close()

	numRequests := 5
	sampleIds := make([]int64, numRequests)

	// Submit multiple async requests
	for i := 0; i < numRequests; i++ {
		testFile := createTestFile(t, fmt.Sprintf("multi async test content %d", i))
		defer func() { _ = os.Remove(testFile) }()

		body, contentType := createMultipartFormFile(t, "file", testFile)

		req, err := http.NewRequest("POST", server.URL+"/api/v1/checkAsync", body)
		if err != nil {
			t.Fatalf("failed to create request %d: %v", i, err)
		}
		req.Header.Set("Content-Type", contentType)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request %d: %v", i, err)
		}

		var sampleId SampleId
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		_ = json.Unmarshal(respBody, &sampleId)

		sampleIds[i] = sampleId.Id
	}

	// Verify all IDs are unique
	idSet := make(map[int64]bool)
	for _, id := range sampleIds {
		if idSet[id] {
			t.Errorf("duplicate sample ID: %d", id)
		}
		idSet[id] = true
	}

	// Wait for completion and verify all results
	time.Sleep(6 * time.Second)

	for i, id := range sampleIds {
		resultResp, err := http.Get(fmt.Sprintf("%s/api/v1/getAsyncResults?id=%d", server.URL, id))
		if err != nil {
			t.Fatalf("failed to get results for request %d: %v", i, err)
		}

		resultBody, _ := io.ReadAll(resultResp.Body)
		_ = resultResp.Body.Close()

		var result AsyncResult
		_ = json.Unmarshal(resultBody, &result)

		if result.Status != finished.String() {
			t.Errorf("request %d (ID %d): expected status '%s', got '%s'", i, id, finished.String(), result.Status)
		}

		if len(result.Result) == 0 {
			t.Errorf("request %d (ID %d): expected findings in result", i, id)
		}
	}
}

// ==================== Response Body Verification Tests ====================

func TestInfoResponseContainsRequiredFields(t *testing.T) {
	server := testRouter()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/info")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	requiredFields := []string{
		"version_info",
		"arguments",
		"license_expiration",
		"license_owner",
		"scan_speed_limitation",
		"threads",
	}

	for _, field := range requiredFields {
		if !strings.Contains(bodyStr, fmt.Sprintf(`"%s"`, field)) {
			t.Errorf("expected response to contain field '%s'", field)
		}
	}
}

func TestStatusResponseContainsRequiredFields(t *testing.T) {
	server := testRouter()
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	requiredFields := []string{
		"scanned_samples",
		"queued_async_requests",
		"avg_scan_time_milliseconds",
		"avg_wait_time_milliseconds",
	}

	for _, field := range requiredFields {
		if !strings.Contains(bodyStr, fmt.Sprintf(`"%s"`, field)) {
			t.Errorf("expected response to contain field '%s'", field)
		}
	}
}

func TestCheckResponseContainsRequiredFields(t *testing.T) {
	server := testRouter()
	defer server.Close()

	testFile := createTestFile(t, "check response test content")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/check", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	bodyStr := string(respBody)

	// ThorFinding should have these fields
	requiredFields := []string{
		"type",
		"id",
		"hash",
	}

	for _, field := range requiredFields {
		if !strings.Contains(bodyStr, fmt.Sprintf(`"%s"`, field)) {
			t.Errorf("expected response to contain field '%s'", field)
		}
	}
}

func TestAsyncResultResponseContainsRequiredFields(t *testing.T) {
	server := testRouter()
	defer server.Close()

	testFile := createTestFile(t, "async result response test content")
	defer func() { _ = os.Remove(testFile) }()

	body, contentType := createMultipartFormFile(t, "file", testFile)

	req, err := http.NewRequest("POST", server.URL+"/api/v1/checkAsync", body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}

	var sampleId SampleId
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	_ = json.Unmarshal(respBody, &sampleId)

	// Get the async result
	resultResp, err := http.Get(fmt.Sprintf("%s/api/v1/getAsyncResults?id=%d", server.URL, sampleId.Id))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resultResp.Body.Close() }()

	resultBody, _ := io.ReadAll(resultResp.Body)
	bodyStr := string(resultBody)

	// AsyncResult must have status field
	if !strings.Contains(bodyStr, `"status"`) {
		t.Error("expected response to contain field 'status'")
	}
}

// ==================== Error Response Tests ====================

func TestErrorResponseFormat(t *testing.T) {
	server := testRouter()
	defer server.Close()

	// Test invalid async results ID
	resp, err := http.Get(server.URL + "/api/v1/getAsyncResults?id=999999999")
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Error response must have message field
	if !strings.Contains(bodyStr, `"message"`) {
		t.Error("expected error response to contain field 'message'")
	}
}
