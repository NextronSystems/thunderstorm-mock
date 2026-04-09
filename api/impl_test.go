/*
 * THOR Thunderstorm API - Unit Tests
 *
 * These tests cover the custom server implementation code (state management,
 * mock data generation, logging, helpers). Generated routing and parameter
 * binding code is assumed correct and tested via integration tests instead.
 *
 * API version: 1.0.0
 */

package thunderstormmock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== Scan ID Tests ====================

func TestNextScanIDIncrementing(t *testing.T) {
	id1 := NextScanID()
	id2 := NextScanID()
	id3 := NextScanID()

	assert.Greater(t, id2, id1)
	assert.Greater(t, id3, id2)
}

func TestNextScanIDConcurrent(t *testing.T) {
	const n = 100
	ids := make(chan ScanID, n)

	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			ids <- NextScanID()
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[ScanID]bool)
	for id := range ids {
		assert.False(t, seen[id], "duplicate scan ID: %d", id)
		seen[id] = true
	}
}

// ==================== StoreScanRequest / LoadScanRequest Tests ====================

func TestStoreScanRequestAndLoad(t *testing.T) {
	file := newTestReader(t, "hello world")

	req, err := StoreScanRequest(true, file, "test-source")
	require.NoError(t, err)

	assert.Positive(t, req.ID)
	assert.True(t, req.Synchronous)
	assert.Equal(t, "test-source", req.Source)
	assert.NotEmpty(t, req.FileHash)
	assert.False(t, req.SubmissionTime.IsZero())

	// Load it back
	loaded, ok := LoadScanRequest(int64(req.ID))
	require.True(t, ok, "expected to find stored scan request")
	assert.Equal(t, req.ID, loaded.ID)
}

func TestLoadScanRequestNotFound(t *testing.T) {
	_, ok := LoadScanRequest(999999999)
	assert.False(t, ok)
}

func TestStoreScanRequestHashDeterministic(t *testing.T) {
	req1, _ := StoreScanRequest(true, newTestReader(t, "same content"), "")
	req2, _ := StoreScanRequest(true, newTestReader(t, "same content"), "")

	assert.Equal(t, req1.FileHash, req2.FileHash)
}

func TestStoreScanRequestHashDiffers(t *testing.T) {
	req1, _ := StoreScanRequest(true, newTestReader(t, "content A"), "")
	req2, _ := StoreScanRequest(true, newTestReader(t, "content B"), "")

	assert.NotEqual(t, req1.FileHash, req2.FileHash)
}

// ==================== ToResult State Machine Tests ====================

func TestToResultCrashed(t *testing.T) {
	result := ScanRequest{ID: 0}.ToResult()

	assert.Equal(t, crashed.String(), result.Status)
	assert.Nil(t, result.Result)
}

func TestToResultSynchronous(t *testing.T) {
	result := ScanRequest{
		ID:          42,
		Synchronous: true,
		FileHash:    "abc123",
		Source:      "src",
	}.ToResult()

	assert.Equal(t, finished.String(), result.Status)
	require.NotNil(t, result.Result)
	require.NotEmpty(t, *result.Result)

	finding := (*result.Result)[0]
	assert.Equal(t, "THOR Finding", finding["type"])
	assert.Equal(t, "abc123", finding["hash"])
	assert.Equal(t, "src", finding["source"])
}

func TestToResultAsyncWaiting(t *testing.T) {
	result := ScanRequest{
		ID:             1,
		Synchronous:    false,
		SubmissionTime: time.Now(),
	}.ToResult()

	assert.Equal(t, waiting.String(), result.Status)
	assert.Nil(t, result.Result)
}

func TestToResultAsyncInProgress(t *testing.T) {
	result := ScanRequest{
		ID:             1,
		Synchronous:    false,
		SubmissionTime: time.Now().Add(-3 * time.Second),
	}.ToResult()

	assert.Equal(t, inProgress.String(), result.Status)
	assert.Nil(t, result.Result)
}

func TestToResultAsyncFinished(t *testing.T) {
	result := ScanRequest{
		ID:             1,
		Synchronous:    false,
		FileHash:       "deadbeef",
		SubmissionTime: time.Now().Add(-10 * time.Second),
	}.ToResult()

	assert.Equal(t, finished.String(), result.Status)
	require.NotNil(t, result.Result)
	assert.NotEmpty(t, *result.Result)
}

// ==================== ThorFinding / ThorReport Tests ====================

func TestToThorFinding(t *testing.T) {
	f := ScanRequest{ID: 7, FileHash: "abc", Source: "my-src"}.ToThorFinding()

	assert.Equal(t, "THOR Finding", f["type"])
	assert.Equal(t, int64(7), f["id"])
	assert.Equal(t, "abc", f["hash"])
	assert.Equal(t, "my-src", f["source"])
}

func TestToThorReport(t *testing.T) {
	report := ScanRequest{ID: 1, FileHash: "x"}.ToThorReport()
	assert.Len(t, report, 1)
}

// ==================== MockInfo / MockStatus Tests ====================

func TestMockInfoPopulated(t *testing.T) {
	info := MockInfo()

	assert.NotEmpty(t, info.LicenseOwner)
	assert.NotZero(t, info.Threads)
	assert.NotEmpty(t, info.Arguments)
}

func TestMockStatusReflectsScans(t *testing.T) {
	_, _ = StoreScanRequest(true, newTestReader(t, "status test"), "")

	status := MockStatus()
	assert.GreaterOrEqual(t, status.ScannedSamples, int64(1))
}

// ==================== History Tests ====================

func TestHistoryContainsBuckets(t *testing.T) {
	_, _ = StoreScanRequest(true, newTestReader(t, "history test"), "")

	h := History(sampleHistoryType, 1, 0)
	assert.NotEmpty(t, h)
}

func TestHistoryDefaultAggregate(t *testing.T) {
	// aggregate < 1 should be treated as 1
	h1 := History(sampleHistoryType, 0, 0)
	h2 := History(sampleHistoryType, 1, 0)

	assert.Equal(t, len(h1), len(h2), "aggregate=0 and aggregate=1 should produce same bucket count")
}

func TestHistoryLimitReducesBuckets(t *testing.T) {
	hUnlimited := History(sampleHistoryType, 1, 0)
	hLimited := History(sampleHistoryType, 1, 2)

	assert.LessOrEqual(t, len(hLimited), len(hUnlimited))
}

// ==================== Status Enum Tests ====================

func TestStatusStrings(t *testing.T) {
	tests := []struct {
		s    status
		want string
	}{
		{waiting, "Waiting for execution"},
		{inProgress, "Currently being scanned"},
		{finished, "Sample analysis complete"},
		{crashed, "Sample analysis failed"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, tc.s.String())
	}
}

// ==================== getSpecialTriggerCheck Tests ====================

func TestGetSpecialTriggerCheck(t *testing.T) {
	tests := []struct {
		source      string
		expectMatch bool
	}{
		{"error 400", true},
		{"error 500", true},
		{"normal", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.source, func(t *testing.T) {
			resp, matched := getSpecialTriggerCheck(tc.source)
			assert.Equal(t, tc.expectMatch, matched)
			if matched {
				assert.NotNil(t, resp)
			} else {
				assert.Nil(t, resp)
			}
		})
	}
}

func TestGetSpecialTriggerCheckAsync(t *testing.T) {
	tests := []struct {
		source      string
		expectMatch bool
	}{
		{"error 400", true},
		{"error 500", true},
		{"normal", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.source, func(t *testing.T) {
			resp, matched := getSpecialTriggerCheckAsync(tc.source)
			assert.Equal(t, tc.expectMatch, matched)
			if matched {
				assert.NotNil(t, resp)
			} else {
				assert.Nil(t, resp)
			}
		})
	}
}

// ==================== Logger Tests ====================

func TestHandlerNameFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/v1/check", "Check"},
		{"/api/v1/checkAsync", "CheckAsync"},
		{"/api/v1/getAsyncResults", "GetAsyncResults"},
		{"/api/v1/info", "Info"},
		{"/api/v1/queueHistory", "QueueHistory"},
		{"/api/v1/sampleHistory", "SampleHistory"},
		{"/api/v1/status", "Status"},
		{"/unknown/path", "Unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			assert.Equal(t, tc.want, handlerNameFromPath(tc.path))
		})
	}
}

func TestJSONLoggingWriter(t *testing.T) {
	rr := httptest.NewRecorder()
	jw := &JSONLoggingWriter{ResponseWriter: rr}

	content := `{"key":"value"}`
	n, err := jw.Write([]byte(content))

	require.NoError(t, err)
	assert.Equal(t, len(content), n)
	assert.Equal(t, content, string(jw.JSONResponse))
	assert.Equal(t, content, rr.Body.String(), "underlying writer should receive same content")
}

func TestLoggingMiddleware(t *testing.T) {
	var logBuf bytes.Buffer
	SetLogOutput(&logBuf)
	defer SetLogOutput(nil)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	})

	handler := LoggingMiddleware(inner)

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var entry struct {
		Method   string `json:"method"`
		Handler  string `json:"handler"`
		Response string `json:"response"`
	}
	require.NoError(t, json.Unmarshal(logBuf.Bytes(), &entry))
	assert.Equal(t, "GET", entry.Method)
	assert.Equal(t, "Status", entry.Handler)
	assert.Contains(t, entry.Response, `"ok"`)
}

// ==================== Test Helpers ====================

// newTestReader creates an io.Reader from in-memory content for testing.
func newTestReader(t *testing.T, content string) io.Reader {
	t.Helper()
	return strings.NewReader(content)
}
