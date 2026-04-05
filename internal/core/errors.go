package core

import "fmt"

// AppError is the common interface for all application errors
type AppError interface {
	error
	Code() int    // HTTP status code
	Slug() string // Machine-readable error type
}

// ConfigError represents a configuration validation error
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config error: %s — %s", e.Field, e.Message)
}

func (e *ConfigError) Code() int    { return 400 }
func (e *ConfigError) Slug() string { return "config_error" }

// NotFoundError represents a missing resource error
type NotFoundError struct {
	Entity string
	ID     string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Entity, e.ID)
}

func (e *NotFoundError) Code() int    { return 404 }
func (e *NotFoundError) Slug() string { return "not_found" }

// ValidationError represents an input validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s — %s", e.Field, e.Message)
}

func (e *ValidationError) Code() int    { return 400 }
func (e *ValidationError) Slug() string { return "validation_error" }

// ConflictError represents a resource conflict
type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string {
	return e.Message
}

func (e *ConflictError) Code() int    { return 409 }
func (e *ConflictError) Slug() string { return "conflict" }

// UnauthorizedError represents authentication failure
type UnauthorizedError struct {
	Message string
}

func (e *UnauthorizedError) Error() string {
	if e.Message == "" {
		return "unauthorized"
	}
	return e.Message
}

func (e *UnauthorizedError) Code() int    { return 401 }
func (e *UnauthorizedError) Slug() string { return "unauthorized" }

// ForbiddenError represents permission denied
type ForbiddenError struct {
	Message string
}

func (e *ForbiddenError) Error() string {
	if e.Message == "" {
		return "forbidden"
	}
	return e.Message
}

func (e *ForbiddenError) Code() int    { return 403 }
func (e *ForbiddenError) Slug() string { return "forbidden" }

// InternalError represents an unexpected internal error
type InternalError struct {
	Message string
	Cause   error
}

func (e *InternalError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("internal error: %s — %v", e.Message, e.Cause)
	}
	return fmt.Sprintf("internal error: %s", e.Message)
}

func (e *InternalError) Code() int    { return 500 }
func (e *InternalError) Slug() string { return "internal_error" }
