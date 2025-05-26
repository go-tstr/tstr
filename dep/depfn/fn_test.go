package depfn_test

import (
	"errors"
	"testing"

	"github.com/go-tstr/tstr/dep/depfn"
	"github.com/go-tstr/tstr/dep/deptest"
	"github.com/stretchr/testify/require"
)

func TestNew_Nil_Fn(t *testing.T) {
	dep := depfn.New(nil, nil, nil)
	require.NoError(t, dep.Start())
	require.NoError(t, dep.Ready())
	require.NoError(t, dep.Stop())
}

func TestNew_NoErr_Fn(t *testing.T) {
	dep := depfn.New(
		func() error { return nil },
		func() error { return nil },
		func() error { return nil },
	)

	require.NoError(t, dep.Start())
	require.NoError(t, dep.Ready())
	require.NoError(t, dep.Stop())
}

func TestNew_Err_Fn(t *testing.T) {
	var (
		startErr = errors.New("start error")
		readyErr = errors.New("ready error")
		stopErr  = errors.New("stop error")
	)

	dep := depfn.New(
		func() error {
			return startErr
		},
		func() error {
			return readyErr
		},
		func() error {
			return stopErr
		},
	)
	require.ErrorIs(t, dep.Start(), startErr)
	require.ErrorIs(t, dep.Ready(), readyErr)
	require.ErrorIs(t, dep.Stop(), stopErr)
}

func TestNew_ValidDep(t *testing.T) {
	ok := deptest.ErrorIs(t, depfn.New(nil, nil, nil), func() {}, nil)
	require.True(t, ok)
}
