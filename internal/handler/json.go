package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gi8lino/screendeck/internal/requestid"
)

// validator validates a decoded request and returns problems keyed by JSON field path.
type validator interface {
	Valid(context.Context) map[string]string
}

// validationError contains all field-level problems found in a request.
type validationError struct {
	Problems map[string]string
}

// Error implements error.
func (e validationError) Error() string {
	return "request validation failed"
}

// decode reads a strictly formatted JSON request body into T.
func decode[T any](r *http.Request) (T, error) {
	var value T
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&value); err != nil {
		return value, invalidRequestf(err, "invalid request: %v", err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return value, invalidRequest("invalid request: body must contain exactly one JSON value", nil)
		}
		return value, invalidRequestf(err, "invalid request: %v", err)
	}

	return value, nil
}

// decodeValid decodes a request and reports all field-level validation problems.
func decodeValid[T validator](r *http.Request) (T, error) {
	value, err := decode[T](r)
	if err != nil {
		return value, err
	}
	if problems := value.Valid(r.Context()); len(problems) > 0 {
		return value, validationError{Problems: problems}
	}
	return value, nil
}

// encode writes a JSON API response and reports encoding failures.
func encode[T any](w http.ResponseWriter, status int, value T) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return fmt.Errorf("encode JSON response: %w", err)
	}
	return nil
}

// respond writes a JSON API response and logs encoding failures.
func respond[T any](logger *slog.Logger, w http.ResponseWriter, status int, value T) {
	if err := encode(w, status, value); err != nil {
		requestid.WithLogger(logger, nil).Error("encode response",
			"event", "encode_response_failed",
			"error", err,
		)
	}
}
