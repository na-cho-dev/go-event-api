package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

// Error codes returned in the error envelope. Clients can branch on these
// without parsing messages.
const (
	CodeValidation         = "validation_error"
	CodeInvalidJSON        = "invalid_json"
	CodeInvalidID          = "invalid_id"
	CodeUnauthorized       = "unauthorized"
	CodeTokenExpired       = "token_expired"
	CodeInvalidCredentials = "invalid_credentials"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeConflict           = "conflict"
	CodeRateLimited        = "rate_limited"
	CodePayloadTooLarge    = "payload_too_large"
	CodeInternal           = "internal_error"
)

// ErrorResponse is the envelope every error uses.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes one failure.
type ErrorDetail struct {
	Code      string       `json:"code" example:"validation_error"`
	Message   string       `json:"message" example:"The request body is invalid."`
	Details   []FieldError `json:"details,omitempty"`
	RequestID string       `json:"requestId,omitempty" example:"3f1c2a9b0d4e"`
}

// FieldError points at one invalid request field.
type FieldError struct {
	Field   string `json:"field" example:"email"`
	Message string `json:"message" example:"must be a valid email address"`
}

// PageMeta describes a page of results.
type PageMeta struct {
	Page       int `json:"page" example:"1"`
	Limit      int `json:"limit" example:"20"`
	Total      int `json:"total" example:"42"`
	TotalPages int `json:"totalPages" example:"3"`
}

// ListResponse wraps a page of items.
type ListResponse[T any] struct {
	Data []T      `json:"data"`
	Meta PageMeta `json:"meta"`
}

// CollectionResponse wraps an unpaged list.
type CollectionResponse[T any] struct {
	Data []T `json:"data"`
}

func newPageMeta(page, limit, total int) PageMeta {
	pages := 0
	if limit > 0 {
		pages = (total + limit - 1) / limit
	}
	return PageMeta{Page: page, Limit: limit, Total: total, TotalPages: pages}
}

// fail writes the error envelope and aborts the handler chain.
func fail(c *gin.Context, status int, code, message string, details ...FieldError) {
	c.AbortWithStatusJSON(status, ErrorResponse{Error: ErrorDetail{
		Code:      code,
		Message:   message,
		Details:   details,
		RequestID: requestIDFrom(c),
	}})
}

// bindJSON decodes and validates the body. It writes the error response
// itself and returns false when the body is unusable.
func bindJSON(c *gin.Context, dst any) bool {
	err := c.ShouldBindJSON(dst)
	if err == nil {
		return true
	}

	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		fail(c, http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
			fmt.Sprintf("The request body may not exceed %d bytes.", maxBytes.Limit))
		return false
	}

	var fieldErrs validator.ValidationErrors
	if errors.As(err, &fieldErrs) {
		details := make([]FieldError, 0, len(fieldErrs))
		for _, fe := range fieldErrs {
			details = append(details, FieldError{Field: fe.Field(), Message: describeRule(fe)})
		}
		fail(c, http.StatusBadRequest, CodeValidation, "The request body failed validation.", details...)
		return false
	}

	var parseErr *time.ParseError
	if errors.As(err, &parseErr) {
		fail(c, http.StatusBadRequest, CodeValidation, "The request body failed validation.",
			FieldError{Field: "date", Message: "must be an RFC 3339 timestamp such as 2026-11-05T18:00:00Z"})
		return false
	}

	if errors.Is(err, io.EOF) {
		fail(c, http.StatusBadRequest, CodeInvalidJSON, "The request body is empty.")
		return false
	}
	fail(c, http.StatusBadRequest, CodeInvalidJSON, "The request body is not valid JSON.")
	return false
}

func describeRule(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		if fe.Kind().String() == "string" {
			return "must be at least " + fe.Param() + " characters"
		}
		return "must be at least " + fe.Param()
	case "max":
		if fe.Kind().String() == "string" {
			return "must be at most " + fe.Param() + " characters"
		}
		return "must be at most " + fe.Param()
	case "gte":
		return "must be at least " + fe.Param()
	case "lte":
		return "must be at most " + fe.Param()
	default:
		return "is invalid"
	}
}

// useJSONFieldNames makes validation errors report json tag names instead
// of Go struct field names.
func useJSONFieldNames() {
	v, ok := binding.Validator.Engine().(*validator.Validate)
	if !ok {
		return
	}
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})
}

// pathID parses a positive integer path parameter or writes a 400.
func pathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, CodeInvalidID, fmt.Sprintf("The %s must be a positive integer.", name))
		return 0, false
	}
	return id, true
}
