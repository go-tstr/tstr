package deptest_test

import (
	"testing"

	"github.com/go-tstr/tstr/dep/deptest"
	"github.com/stretchr/testify/assert"
)

func TestErrorIs_NilErr(t *testing.T) {
	got := deptest.ErrorIs(t, MockDep{}, func() {}, nil)
	assert.True(t, got)
}

func TestErrorIs_NoFn(t *testing.T) {
	got := deptest.ErrorIs(t, MockDep{}, nil, nil)
	assert.True(t, got)
}

type MockDep struct{}

func (MockDep) Start() error { return nil }
func (MockDep) Ready() error { return nil }
func (MockDep) Stop() error  { return nil }
