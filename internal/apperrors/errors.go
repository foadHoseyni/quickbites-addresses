// Package apperrors defines errors that carry an HTTP status code, so the
// service layer can say "this is a 404" without importing net/http handlers.
package apperrors

import "net/http"

type AppError struct {
	Status  int    `json:"-"`
	Message string `json:"error"`
}

func (e *AppError) Error() string { return e.Message }

func NewBadRequest(msg string) *AppError {
	return &AppError{Status: http.StatusBadRequest, Message: msg}
}

func NewUnauthorized(msg string) *AppError {
	return &AppError{Status: http.StatusUnauthorized, Message: msg}
}

func NewNotFound(msg string) *AppError {
	return &AppError{Status: http.StatusNotFound, Message: msg}
}

func NewServerError(msg string) *AppError {
	return &AppError{Status: http.StatusInternalServerError, Message: msg}
}
