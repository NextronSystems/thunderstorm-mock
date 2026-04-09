/*
 * THOR Thunderstorm Mock Server
 *
 * A mock implementation of the THOR Thunderstorm API for testing purposes.
 *
 * API version: 1.0.0
 */

package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	thunderstormmock "github.com/NextronSystems/thunderstorm-mock/api"
)

// Version is the current version of the mock server.
// This should match the latest release tag (e.g., "v1.0.0").
// During build, this can be overridden via -ldflags.
var Version = "v0.0.0-dev"

const (
	envPrefix = "THUNDERSTORM_MOCK_"

	defaultPort    = "8080"
	defaultAddress = ""
	defaultOutput  = "-"
)

// Config holds the server configuration
type Config struct {
	Port    string
	Address string
	Output  string
	Version bool
}

// getEnv returns the environment variable value or the default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(envPrefix + key); value != "" {
		return value
	}
	return defaultValue
}

// parseFlags parses command-line flags with environment variable defaults
func parseFlags() *Config {
	cfg := &Config{}

	// Define flags with environment variable defaults
	flag.StringVar(&cfg.Port, "port", getEnv("PORT", defaultPort),
		"Port to listen on (env: THUNDERSTORM_MOCK_PORT)")
	flag.StringVar(&cfg.Port, "p", getEnv("PORT", defaultPort),
		"Port to listen on (shorthand)")

	flag.StringVar(&cfg.Address, "address", getEnv("ADDRESS", defaultAddress),
		"Network address to bind to (env: THUNDERSTORM_MOCK_ADDRESS)")
	flag.StringVar(&cfg.Address, "a", getEnv("ADDRESS", defaultAddress),
		"Network address to bind to (shorthand)")

	flag.StringVar(&cfg.Output, "output", getEnv("OUTPUT", defaultOutput),
		"Output destination: '-' for stdout, or a file path (env: THUNDERSTORM_MOCK_OUTPUT)")
	flag.StringVar(&cfg.Output, "o", getEnv("OUTPUT", defaultOutput),
		"Output destination (shorthand)")

	flag.BoolVar(&cfg.Version, "version", false, "Print version and exit")
	flag.BoolVar(&cfg.Version, "v", false, "Print version and exit (shorthand)")

	// Custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `THOR Thunderstorm Mock Server %s

A mock implementation of the THOR Thunderstorm API for testing collector clients.

Usage:
  thunderstorm-mock [flags]

Flags:
  -p, --port string      Port to listen on (default "%s")
                         Environment: THUNDERSTORM_MOCK_PORT
  -a, --address string   Network address to bind to (default "" = all interfaces)
                         Environment: THUNDERSTORM_MOCK_ADDRESS
  -o, --output string    Output destination for request logs:
                         '-' for stdout (default), or a file path
                         Environment: THUNDERSTORM_MOCK_OUTPUT
  -v, --version          Print version and exit
  -h, --help             Show this help message

Examples:
  # Start on default port 8080
  thunderstorm-mock

  # Start on port 9090
  thunderstorm-mock --port 9090

  # Bind to localhost only and log to file
  thunderstorm-mock -a 127.0.0.1 -p 8080 -o /var/log/thunderstorm-mock.log

  # Using environment variables
  THUNDERSTORM_MOCK_PORT=9090 THUNDERSTORM_MOCK_OUTPUT=/tmp/mock.log thunderstorm-mock

`, Version, defaultPort)
	}

	flag.Parse()

	return cfg
}

// setupOutput configures the output writer based on the output flag
// Returns the writer, a cleanup function to close any resources, and an error if setup fails.
func setupOutput(output string) (io.Writer, func(), error) {
	if output == "-" {
		return os.Stdout, func() {}, nil
	}

	file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open output file %q: %w", output, err)
	}

	cleanup := func() {
		_ = file.Close()
	}

	return file, cleanup, nil
}

func main() {
	cfg := parseFlags()

	// Handle --version
	if cfg.Version {
		fmt.Println(Version)
		os.Exit(0)
	}

	// Build the listen address
	listenAddr := cfg.Address + ":" + cfg.Port
	if _, _, err := net.SplitHostPort(listenAddr); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid listen address and port combination: %v\n", err)
		os.Exit(1)
	}

	// Setup output
	output, cleanup, err := setupOutput(cfg.Output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer cleanup()

	// Configure the logger to use the specified output
	thunderstormmock.SetLogOutput(output)

	// Create the mock server and set up the handler
	mockServer := thunderstormmock.NewMockServer()
	strictHandler := thunderstormmock.NewStrictHandlerWithOptions(mockServer, nil, thunderstormmock.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"message":"%s"}`, err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"message":"%s"}`, err.Error())
		},
	})
	handler := thunderstormmock.HandlerWithOptions(strictHandler, thunderstormmock.StdHTTPServerOptions{
		BaseURL: "/api/v1",
		Middlewares: []thunderstormmock.MiddlewareFunc{
			thunderstormmock.LoggingMiddleware,
		},
	})

	// Log startup info to stderr (always visible)
	fmt.Fprintf(os.Stderr, "Thunderstorm Mock Server %s starting on %s\n", Version, listenAddr)
	if cfg.Output != "-" {
		fmt.Fprintf(os.Stderr, "Logging requests to: %s\n", cfg.Output)
	}

	// Start the server (ListenAndServe always returns a non-nil error)
	fmt.Fprintf(os.Stderr, "Server exit: %v\n", http.ListenAndServe(listenAddr, handler))
}
