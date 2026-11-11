// Package apperr is the one error type services return to handlers.
// Handlers turn it into the JSON envelope (API) or a flash message (web).
package apperr

import (
	"errors"
	"net/http"
)

type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func New(status int, code, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

func BadRequest(code, msg string) *Error { return New(http.StatusBadRequest, code, msg) }
func Unauthorized(msg string) *Error     { return New(http.StatusUnauthorized, "unauthorized", msg) }
func Forbidden(msg string) *Error        { return New(http.StatusForbidden, "forbidden", msg) }
func NotFound(what string) *Error        { return New(http.StatusNotFound, "not_found", what+" not found") }
func Conflict(code, msg string) *Error   { return New(http.StatusConflict, code, msg) }
func Invalid(fields map[string]string) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "invalid", Message: "Some fields are not valid", Fields: fields}
}

// As returns the *Error inside err, if any.
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}
