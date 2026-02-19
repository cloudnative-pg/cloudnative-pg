/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
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
type Response[T any] struct {
	Data  *T     `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}

// GetError returns an error if an error response is detected or if the data
// field is nil
func (body Response[T]) GetError() error {
	if body.Error != nil {
		return fmt.Errorf("encountered a body error while preparing, code: '%s', message: %s",
			body.Error.Code, body.Error.Message)
	}

	status := body.Data
	if status != nil {
		return nil
	}

	return fmt.Errorf("encounteered an empty body while expecting it to not be empty")
}

// Webserver wraps a webserver to make it a kubernetes Runnable
type Webserver struct {
	server   *http.Server
	routines []func(ctx context.Context)
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

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, routine := range ws.routines {
		go routine(subCtx)
	}

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

// sendJSONResponse sends a generic JSON response.
func sendJSONResponse[T any](w http.ResponseWriter, statusCode int, data Response[T]) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
	}
}

func sendBadRequestJSONResponse(w http.ResponseWriter, errorCode string, message string) {
	sendJSONResponse(w, http.StatusBadRequest, Response[any]{
		Error: &Error{
			Code:    errorCode,
			Message: message,
		},
	})
}

func sendUnprocessableEntityJSONResponse(w http.ResponseWriter, errorCode string, message string) {
	sendJSONResponse(w, http.StatusUnprocessableEntity, Response[any]{
		Error: &Error{
			Code:    errorCode,
			Message: message,
		},
	})
}

func sendJSONResponseWithData[T any](w http.ResponseWriter, statusCode int, data T) {
	sendJSONResponse(w, statusCode, Response[T]{
		Data: &data,
	})
}
