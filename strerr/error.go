// Package strerr provides string error type that allows declaring errors as constants unlike Go's own errors.New which uses privete struct type.
package strerr

// Error adds Error method to string type.
type Error string

func (e Error) Error() string { return string(e) }
