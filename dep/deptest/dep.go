package deptest

import (
	"testing"

	"github.com/go-tstr/tstr"
	"github.com/stretchr/testify/assert"
)

// ErrorIs is a convinience wrapper around tstr.Run that can be used to test single dependency.
func ErrorIs(t *testing.T, d tstr.Dependency, fn func() error, err error) bool {
	return assert.ErrorIs(t, tstr.Run(
		tstr.WithDeps(d),
		tstr.WithFn(func() error {
			if fn == nil {
				return nil
			}
			return fn()
		}),
	), err)
}