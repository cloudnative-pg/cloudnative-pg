/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
)

const (
	// DefaultReadTimeout is the default value to be used by the webservers
	DefaultReadTimeout = 20 * time.Second
	// DefaultReadHeaderTimeout is the default value to be used by the webservers
	DefaultReadHeaderTimeout = 3 * time.Second
)

// Error an error response from http webserver
type Error struct {
	// One of a server-defined set of error codes
	Code string `json:"code"`
	// A human-readable representation of the error.
	Message string `json:"message"`
	// An array of details about specific errors that led to this reported error.
	Details []Error `json:"details,omitempty"`
}

// Response a response from the http webserver
type Response[T interface{}] struct {
	Data  *T     `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}

// EnsureDataIsPresent returns an error if the data is field is nil
func (body Response[T]) EnsureDataIsPresent() error {
	status := body.Data
	if status != nil {
		return nil
	}

	if body.Error != nil {
		return fmt.Errorf("encountered a body error while preparing, code: '%s', message: %s",
			body.Error.Code, body.Error.Message)
	}

	return fmt.Errorf("encounteered an empty body while expecting it to not be empty")
}

// Webserver wraps a webserver to make it a kubernetes Runnable
type Webserver struct {
	server *http.Server
}

// NewWebServer creates a Webserver as a Kubernetes Runnable, given a http.Server
func NewWebServer(server *http.Server) *Webserver {
	return &Webserver{
		server: server,
	}
}

// Start starts a webserver listener, implementing the K8s runnable interface
func (ws *Webserver) Start(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	errChan := make(chan error, 1)
	go func() {
		contextLogger.Info("Starting webserver", "address", ws.server.Addr, "hasTLS", ws.server.TLSConfig != nil)

		var err error
		if ws.server.TLSConfig != nil {
			err = ws.server.ListenAndServeTLS("", "")
		} else {
			err = ws.server.ListenAndServe()
		}
		if err != nil {
			errChan <- err
		}
	}()

	select {
	// we exit with error code, potentially we could do a retry logic, but rarely a webserver that doesn't start will run
	// on subsequent tries
	case err := <-errChan:
		if errors.Is(err, http.ErrServerClosed) {
			contextLogger.Error(err, "Closing the web server", "address", ws.server.Addr)
		} else {
			contextLogger.Error(err, "Error while running the web server", "address", ws.server.Addr)
		}
		return err
	case <-ctx.Done():
		if err := ws.server.Shutdown(context.Background()); err != nil {
			contextLogger.Error(err, "Error while shutting down the web server", "address", ws.server.Addr)
			return err
		}
	}

	contextLogger.Info("Webserver exited", "address", ws.server.Addr)

	return nil
}

// writeJSONResponse handles writing a JSON HTTP response with proper error handling and logging.
// It takes a pre-marshaled JSON byte slice and handles setting the correct content type.
// It returns an error if the write operation fails or if the number of bytes written
// doesn't match the expected count.
func writeJSONResponse(w http.ResponseWriter, endpoint string, js []byte) error {
	w.Header().Set("Content-Type", "application/json")

	expectedBytes := len(js)
	n, err := w.Write(js)

	if err != nil {
		log.Warning(fmt.Sprintf("%s: failed to write JSON response", endpoint),
			"error", err.Error(),
			"bytesWritten", n,
			"expectedBytes", expectedBytes)
		return fmt.Errorf("%s: failed to write JSON response: %w", endpoint, err)
	}

	if n != expectedBytes {
		err := fmt.Errorf("incomplete JSON write: wrote %d of %d bytes", n, expectedBytes)
		log.Warning(fmt.Sprintf("%s: incomplete JSON response write", endpoint),
			"error", err.Error(),
			"bytesWritten", n,
			"expectedBytes", expectedBytes)
		return err
	}

	log.Trace(fmt.Sprintf("%s: successfully wrote JSON response", endpoint),
		"bytesWritten", n)

	return nil
}

// writeResponse handles writing an HTTP response with proper error handling and logging.
// It returns an error if the write operation fails or if the number of bytes written
// doesn't match the expected count.
func writeTextResponse(w http.ResponseWriter, endpoint, value string) error {
	expectedBytes := len(value)
	n, err := fmt.Fprint(w, value)

	if err != nil {
		log.Warning(fmt.Sprintf("%s: failed to write response", endpoint),
			"error", err.Error(),
			"bytesWritten", n,
			"expectedBytes", expectedBytes)
		return fmt.Errorf("%s: failed to write response: %w", endpoint, err)
	}

	if n != expectedBytes {
		err := fmt.Errorf("incomplete write: wrote %d of %d bytes", n, expectedBytes)
		log.Warning(fmt.Sprintf("%s: incomplete response write", endpoint),
			"error", err.Error(),
			"bytesWritten", n,
			"expectedBytes", expectedBytes)
		return err
	}

	log.Trace(fmt.Sprintf("%s: successfully wrote response", endpoint),
		"bytesWritten", n)

	return nil
}

func sendJSONResponse[T any](w http.ResponseWriter, statusCode int, resp Response[T], endpoint string) {
	log.Trace("sendJSONResponse: entry", "status", statusCode, "endpoint", endpoint)
	defer log.Trace("sendJSONResponse: deferred exit", "status", statusCode, "endpoint", endpoint)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	log.Trace("sendJSONResponse: headers set", "status", statusCode, "endpoint", endpoint)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Trace("sendJSONResponse: Failed to write JSON response", "error", err, "response", resp, "status", statusCode, "endpoint", endpoint)
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
	}

	log.Trace("sendJSONResponse: exit", "status", statusCode, "endpoint", endpoint)
}

func sendBadRequestJSONResponse(w http.ResponseWriter, errorCode string, message string, endpoint string) {
	sendJSONResponse(w, http.StatusBadRequest, Response[any]{
		Error: &Error{
			Code:    errorCode,
			Message: message,
		},
	}, endpoint)
}

func sendUnprocessableEntityJSONResponse(w http.ResponseWriter, errorCode string, message string, endpoint string) {
	sendJSONResponse(w, http.StatusUnprocessableEntity, Response[any]{
		Error: &Error{
			Code:    errorCode,
			Message: message,
		},
	}, endpoint)
}

func sendJSONResponseWithData[T interface{}](w http.ResponseWriter, statusCode int, data T, endpoint string) {
	sendJSONResponse(w, statusCode, Response[T]{
		Data: &data,
	},
		endpoint)
}
