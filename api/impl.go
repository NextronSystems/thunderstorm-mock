package thunderstormmock

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
)

// MockServer implements the StrictServerInterface for the Thunderstorm mock server.
type MockServer struct{}

// NewMockServer creates a new mock server instance.
func NewMockServer() *MockServer {
	return &MockServer{}
}

// Ensure MockServer implements StrictServerInterface at compile time.
var _ StrictServerInterface = (*MockServer)(nil)

// getSpecialTriggerCheck checks source for special testing-related triggers on scan endpoints.
// Returns the response object and true if a trigger matched, or nil and false otherwise.
func getSpecialTriggerCheck(source string) (CheckResponseObject, bool) {
	switch source {
	case "error 400":
		return Check400JSONResponse{BadRequestJSONResponse{Message: "Invalid parameters given"}}, true
	case "error 500":
		return Check500JSONResponse{InternalServerErrorJSONResponse{Message: "Internal server error"}}, true
	}
	return nil, false
}

// getSpecialTriggerCheckAsync checks source for special testing-related triggers on async scan endpoints.
func getSpecialTriggerCheckAsync(source string) (CheckAsyncResponseObject, bool) {
	switch source {
	case "error 400":
		return CheckAsync400JSONResponse{BadRequestJSONResponse{Message: "Invalid parameters given"}}, true
	case "error 500":
		return CheckAsync500JSONResponse{InternalServerErrorJSONResponse{Message: "Internal server error"}}, true
	}
	return nil, false
}

// readFileFromMultipart reads the first "file" part from a multipart reader.
func readFileFromMultipart(reader *multipart.Reader) (io.ReadCloser, error) {
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return nil, fmt.Errorf("missing 'file' field in multipart body")
		}
		if err != nil {
			return nil, err
		}
		if part.FormName() == "file" {
			return part, nil
		}
		_ = part.Close()
	}
}

// Check handles synchronous file scanning.
// (POST /check)
func (s *MockServer) Check(_ context.Context, request CheckRequestObject) (CheckResponseObject, error) {
	source := ""
	if request.Params.Source != nil {
		source = *request.Params.Source
	}

	if resp, matched := getSpecialTriggerCheck(source); matched {
		return resp, nil
	}

	file, err := readFileFromMultipart(request.Body)
	if err != nil {
		return Check400JSONResponse{BadRequestJSONResponse{Message: fmt.Sprintf("Missing or invalid file: %v", err)}}, nil
	}
	defer func() { _ = file.Close() }()

	req, err := StoreScanRequest(true, file, source)
	if err != nil {
		return Check500JSONResponse{InternalServerErrorJSONResponse{Message: fmt.Sprintf("Internal server error: %v", err)}}, nil
	}
	return Check200JSONResponse(req.ToThorReport()), nil
}

// CheckAsync handles asynchronous file scanning.
// (POST /checkAsync)
func (s *MockServer) CheckAsync(_ context.Context, request CheckAsyncRequestObject) (CheckAsyncResponseObject, error) {
	source := ""
	if request.Params.Source != nil {
		source = *request.Params.Source
	}

	if resp, matched := getSpecialTriggerCheckAsync(source); matched {
		return resp, nil
	}

	file, err := readFileFromMultipart(request.Body)
	if err != nil {
		return CheckAsync400JSONResponse{BadRequestJSONResponse{Message: fmt.Sprintf("Missing or invalid file: %v", err)}}, nil
	}
	defer func() { _ = file.Close() }()

	req, err := StoreScanRequest(false, file, source)
	if err != nil {
		return CheckAsync500JSONResponse{InternalServerErrorJSONResponse{Message: fmt.Sprintf("Internal server error: %v", err)}}, nil
	}
	return CheckAsync200JSONResponse(SampleIdObj{Id: int64(req.ID)}), nil
}

// GetAsyncResults retrieves the results of an asynchronous file check.
// (GET /getAsyncResults)
func (s *MockServer) GetAsyncResults(_ context.Context, request GetAsyncResultsRequestObject) (GetAsyncResultsResponseObject, error) {
	id := request.Params.Id

	switch id {
	case 0:
		return GetAsyncResults200JSONResponse(ScanRequest{ID: 0}.ToResult()), nil
	case -400:
		return GetAsyncResults400JSONResponse{BadRequestJSONResponse{Message: "Invalid parameters given"}}, nil
	case -500:
		return GetAsyncResults500JSONResponse{InternalServerErrorJSONResponse{Message: "Internal server error"}}, nil
	}

	if req, ok := LoadScanRequest(id); ok {
		return GetAsyncResults200JSONResponse(req.ToResult()), nil
	}
	return GetAsyncResults400JSONResponse{BadRequestJSONResponse{Message: "Invalid sample ID"}}, nil
}

// Info returns static information about the running THOR instance.
// (GET /info)
func (s *MockServer) Info(_ context.Context, _ InfoRequestObject) (InfoResponseObject, error) {
	return Info200JSONResponse(MockInfo()), nil
}

// QueueHistory returns a history of how many asynchronous requests were queued.
// (GET /queueHistory)
func (s *MockServer) QueueHistory(_ context.Context, request QueueHistoryRequestObject) (QueueHistoryResponseObject, error) {
	var aggregate, limit int64
	if request.Params.Aggregate != nil {
		aggregate = *request.Params.Aggregate
	}
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	return QueueHistory200JSONResponse{QueueHistoryJSONResponse(History(queueHistoryType, aggregate, limit))}, nil
}

// SampleHistory returns a history of how many samples were scanned.
// (GET /sampleHistory)
func (s *MockServer) SampleHistory(_ context.Context, request SampleHistoryRequestObject) (SampleHistoryResponseObject, error) {
	var aggregate, limit int64
	if request.Params.Aggregate != nil {
		aggregate = *request.Params.Aggregate
	}
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	return SampleHistory200JSONResponse{SampleHistoryJSONResponse(History(sampleHistoryType, aggregate, limit))}, nil
}

// Status returns live information about the running THOR instance.
// (GET /status)
func (s *MockServer) Status(_ context.Context, _ StatusRequestObject) (StatusResponseObject, error) {
	return Status200JSONResponse(MockStatus()), nil
}
