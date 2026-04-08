package thunderstormmock

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// MockServer implements the ServerInterface for the Thunderstorm mock server.
type MockServer struct{}

// NewMockServer creates a new mock server instance.
func NewMockServer() *MockServer {
	return &MockServer{}
}

// Ensure MockServer implements ServerInterface at compile time.
var _ ServerInterface = (*MockServer)(nil)

// writeJSON encodes the given body as JSON and writes it to the response with the given status code.
func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

// getSpecialTriggerResponse checks source for special testing-related triggers.
func getSpecialTriggerResponse(w http.ResponseWriter, source string) bool {
	switch source {
	case "error 400":
		writeJSON(w, http.StatusBadRequest, Error{Message: "Invalid parameters given"})
		return true
	case "error 500":
		writeJSON(w, http.StatusInternalServerError, Error{Message: "Internal server error"})
		return true
	}
	return false
}

// Check handles synchronous file scanning.
// (POST /check)
func (s *MockServer) Check(w http.ResponseWriter, r *http.Request, params CheckParams) {
	source := ""
	if params.Source != nil {
		source = *params.Source
	}

	if getSpecialTriggerResponse(w, source) {
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Error{Message: fmt.Sprintf("Missing or invalid file: %v", err)})
		return
	}
	defer func() { _ = file.Close() }()

	req, err := StoreScanRequest(true, file, source)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, Error{Message: fmt.Sprintf("Internal server error: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, req.ToThorReport())
}

// CheckAsync handles asynchronous file scanning.
// (POST /checkAsync)
func (s *MockServer) CheckAsync(w http.ResponseWriter, r *http.Request, params CheckAsyncParams) {
	source := ""
	if params.Source != nil {
		source = *params.Source
	}

	if getSpecialTriggerResponse(w, source) {
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Error{Message: fmt.Sprintf("Missing or invalid file: %v", err)})
		return
	}
	defer func() { _ = file.Close() }()

	req, err := StoreScanRequest(false, file, source)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, Error{Message: fmt.Sprintf("Internal server error: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, SampleIdObj{Id: int64(req.ID)})
}

// GetAsyncResults retrieves the results of an asynchronous file check.
// (GET /getAsyncResults)
func (s *MockServer) GetAsyncResults(w http.ResponseWriter, r *http.Request, params GetAsyncResultsParams) {
	id := params.Id

	switch id {
	// Handle some special triggers for testing purposes
	case 0:
		writeJSON(w, http.StatusOK, ScanRequest{ID: 0}.ToResult())
		return
	case -400:
		writeJSON(w, http.StatusBadRequest, Error{Message: "Invalid parameters given"})
		return
	case -500:
		writeJSON(w, http.StatusInternalServerError, Error{Message: "Internal server error"})
		return
	}

	if req, ok := LoadScanRequest(id); ok {
		writeJSON(w, http.StatusOK, req.ToResult())
		return
	}
	writeJSON(w, http.StatusBadRequest, Error{Message: "Invalid sample ID"})
}

// Info returns static information about the running THOR instance.
// (GET /info)
func (s *MockServer) Info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, MockInfo())
}

// QueueHistory returns a history of how many asynchronous requests were queued.
// (GET /queueHistory)
func (s *MockServer) QueueHistory(w http.ResponseWriter, r *http.Request, params QueueHistoryParams) {
	var aggregate, limit int64
	if params.Aggregate != nil {
		aggregate = *params.Aggregate
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	writeJSON(w, http.StatusOK, History(queueHistoryType, aggregate, limit))
}

// SampleHistory returns a history of how many samples were scanned.
// (GET /sampleHistory)
func (s *MockServer) SampleHistory(w http.ResponseWriter, r *http.Request, params SampleHistoryParams) {
	var aggregate, limit int64
	if params.Aggregate != nil {
		aggregate = *params.Aggregate
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	writeJSON(w, http.StatusOK, History(sampleHistoryType, aggregate, limit))
}

// Status returns live information about the running THOR instance.
// (GET /status)
func (s *MockServer) Status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, MockStatus())
}
