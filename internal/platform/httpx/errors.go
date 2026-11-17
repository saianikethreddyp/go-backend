// Package httpx contains the HTTP plumbing shared by all feature packages:
// error types, JSON responses, request decoding and validation.
package httpx

import "net/http"

// FieldError describes one invalid request field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// AppError is an error that knows how to render itself as an HTTP response.
type AppError struct {
	Status  int
	Code    string
	Message string
	Details any
	Err     error // underlying cause, never sent to clients
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Err.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *AppError) Unwrap() error { return e.Err }

func BadRequest(msg string) *AppError {
	return &AppError{Status: http.StatusBadRequest, Code: "bad_request", Message: msg}
}

func Unauthorized(msg string) *AppError {
	return &AppError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: msg}
}

func Forbidden(msg string) *AppError {
	return &AppError{Status: http.StatusForbidden, Code: "forbidden", Message: msg}
}

func NotFound(what string) *AppError {
	return &AppError{Status: http.StatusNotFound, Code: "not_found", Message: what + " not found"}
}

func Conflict(msg string) *AppError {
	return &AppError{Status: http.StatusConflict, Code: "conflict", Message: msg}
}

func PreconditionRequired(code, msg string) *AppError {
	return &AppError{Status: http.StatusPreconditionRequired, Code: code, Message: msg}
}

func TooManyRequests(msg string) *AppError {
	return &AppError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: msg}
}

func ServiceUnavailable(msg string) *AppError {
	return &AppError{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: msg}
}

func PayloadTooLarge() *AppError {
	return &AppError{Status: http.StatusRequestEntityTooLarge, Code: "payload_too_large", Message: "request body too large"}
}

func Validation(details []FieldError) *AppError {
	return &AppError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "request validation failed", Details: details}
}

// Internal wraps an unexpected error; the cause is logged, never returned.
func Internal(err error) *AppError {
	return &AppError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "internal server error", Err: err}
}
