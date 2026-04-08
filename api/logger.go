package thunderstormmock

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// logOutput is the destination for request logs
var (
	logOutput   io.Writer = os.Stdout
	logOutputMu sync.RWMutex
)

// SetLogOutput sets the output destination for request logs.
// This is safe to call concurrently.
func SetLogOutput(w io.Writer) {
	logOutputMu.Lock()
	defer logOutputMu.Unlock()
	logOutput = w
}

// getLogOutput returns the current log output writer.
func getLogOutput() io.Writer {
	logOutputMu.RLock()
	defer logOutputMu.RUnlock()
	return logOutput
}

type JSONLoggingWriter struct {
	http.ResponseWriter
	JSONResponse []byte
}

func (w *JSONLoggingWriter) Write(b []byte) (int, error) {
	w.JSONResponse = append(w.JSONResponse, b...)
	return w.ResponseWriter.Write(b)
}

// handlerNames maps URL path suffixes to handler names for logging.
var handlerNames = map[string]string{
	"check":           "Check",
	"checkAsync":      "CheckAsync",
	"getAsyncResults": "GetAsyncResults",
	"info":            "Info",
	"queueHistory":    "QueueHistory",
	"sampleHistory":   "SampleHistory",
	"status":          "Status",
}

func handlerNameFromPath(path string) string {
	// Extract the last path segment (e.g., "/api/v1/check" → "check")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		suffix := path[idx+1:]
		if name, ok := handlerNames[suffix]; ok {
			return name
		}
	}
	return "Unknown"
}

func Logger(inner http.Handler, name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		jw := &JSONLoggingWriter{ResponseWriter: w}

		inner.ServeHTTP(jw, r)

		info := struct {
			Time     time.Time     `json:"time"`
			Method   string        `json:"method"`
			URI      string        `json:"uri"`
			Handler  string        `json:"handler"`
			Duration time.Duration `json:"duration"`
			Response string        `json:"response"`
		}{
			Time:     start,
			Method:   r.Method,
			URI:      r.RequestURI,
			Handler:  name,
			Duration: time.Since(start),
			Response: string(jw.JSONResponse),
		}

		var msg string
		if jsonBytes, err := json.Marshal(info); err != nil {
			msg = "{\"error\": \"failed to marshal log info\"}"
		} else {
			msg = string(jsonBytes)
		}
		fmt.Fprintln(getLogOutput(), msg)
	})
}

// LoggingMiddleware is a MiddlewareFunc that logs each request with the
// handler name inferred from the URL path.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := handlerNameFromPath(r.URL.Path)
		start := time.Now()

		jw := &JSONLoggingWriter{ResponseWriter: w}

		next.ServeHTTP(jw, r)

		info := struct {
			Time     time.Time     `json:"time"`
			Method   string        `json:"method"`
			URI      string        `json:"uri"`
			Handler  string        `json:"handler"`
			Duration time.Duration `json:"duration"`
			Response string        `json:"response"`
		}{
			Time:     start,
			Method:   r.Method,
			URI:      r.RequestURI,
			Handler:  name,
			Duration: time.Since(start),
			Response: string(jw.JSONResponse),
		}

		var msg string
		if jsonBytes, err := json.Marshal(info); err != nil {
			msg = "{\"error\": \"failed to marshal log info\"}"
		} else {
			msg = string(jsonBytes)
		}
		fmt.Fprintln(getLogOutput(), msg)
	})
}
