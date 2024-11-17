package strerr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-tstr/tstr/strerr"
	"github.com/stretchr/testify/assert"
)

func TestError_Error(t *testing.T) {
	const (
		msg     = "test error"
		SomeErr = strerr.Error(msg)
	)

	err := fmt.Errorf("more info: %w", SomeErr)
	assert.ErrorIs(t, err, SomeErr)
	assert.Equal(t, msg, errors.Unwrap(err).Error())
}
