package errors

import (
	"fmt"
)

var ErrFileChanged = New("file contents changed during sync")

// MissingFieldError represents a missing required field.
type MissingFieldError struct {
	Field string
}

func (err MissingFieldError) Error() string {
	return fmt.Sprintf("missing required field: %s", err.Field)
}

// FileNotFound represents when we were unable to access a file
// because the path didn't exist.
type FileNotFound struct {
	Path string
}

func (err FileNotFound) Error() string {
	return fmt.Sprintf("%q does not exist", err.Path)
}
