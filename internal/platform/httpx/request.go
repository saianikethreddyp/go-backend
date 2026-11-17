package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

const maxBodyBytes = 1 << 20 // 1 MiB

var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	// Report errors using JSON field names rather than Go field names.
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
		if name == "-" || name == "" {
			return f.Name
		}
		return name
	})
	return v
}

// DecodeJSON reads a single JSON object from the body into dst, rejecting
// unknown fields and trailing data, then validates dst using struct tags.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return DecodeJSONLimit(w, r, dst, maxBodyBytes)
}

// DecodeJSONLimit is DecodeJSON with a custom body size cap in bytes, for the
// few endpoints (such as contact sync) that legitimately send more data.
func DecodeJSONLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			return PayloadTooLarge()
		case errors.Is(err, io.EOF):
			return BadRequest("request body is required")
		default:
			return BadRequest("request body is not valid JSON or contains unknown fields")
		}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return BadRequest("request body must contain a single JSON object")
	}
	return Validate(dst)
}

// Validate runs struct-tag validation and converts failures to an AppError.
func Validate(v any) error {
	err := validate.Struct(v)
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return Internal(err)
	}
	out := make([]FieldError, 0, len(verrs))
	for _, fe := range verrs {
		out = append(out, FieldError{Field: fe.Namespace()[strings.Index(fe.Namespace(), ".")+1:], Message: describe(fe)})
	}
	return Validation(out)
}

func describe(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		if fe.Kind() == reflect.String {
			return fmt.Sprintf("must be at least %s characters", fe.Param())
		}
		return fmt.Sprintf("must be at least %s", fe.Param())
	case "max":
		if fe.Kind() == reflect.String {
			return fmt.Sprintf("must be at most %s characters", fe.Param())
		}
		return fmt.Sprintf("must be at most %s", fe.Param())
	case "gte":
		return fmt.Sprintf("must be greater than or equal to %s", fe.Param())
	case "lte":
		return fmt.Sprintf("must be less than or equal to %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", fe.Param())
	case "https_url", "url":
		return "must be a valid https URL"
	default:
		return "is invalid"
	}
}

// BearerToken extracts the token from an "Authorization: Bearer <token>" header.
func BearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

// PathUUID parses a UUID URL parameter; malformed ids are a 404, not a 400,
// so callers cannot probe the id format.
func PathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, NotFound("resource")
	}
	return id, nil
}

// QueryUUID parses an optional UUID query parameter.
func QueryUUID(r *http.Request, name string) (*uuid.UUID, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, BadRequest(name + " must be a valid UUID")
	}
	return &id, nil
}

// Pagination parses limit/offset query parameters with sane bounds.
func Pagination(r *http.Request) (limit, offset int, err error) {
	limit, offset = 20, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 || limit > 100 {
			return 0, 0, BadRequest("limit must be between 1 and 100")
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if offset, err = strconv.Atoi(v); err != nil || offset < 0 {
			return 0, 0, BadRequest("offset must be 0 or greater")
		}
	}
	return limit, offset, nil
}

// Page is the standard list envelope.
type Page[T any] struct {
	Data   []T `json:"data"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}
