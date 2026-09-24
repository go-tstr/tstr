package tstr_test

import (
	"errors"
	"testing"

	"github.com/go-tstr/tstr"
	"github.com/go-tstr/tstr/dep/depfn"
	"github.com/stretchr/testify/assert"
)

func TestRun_Errors(t *testing.T) {
	tests := []struct {
		name        string
		opts        []tstr.Opt
		expectedErr error
	}{
		{
			name:        "missing test function",
			opts:        []tstr.Opt{},
			expectedErr: tstr.ErrMissingTestFn,
		},
		{
			name: "overwriting test function",
			opts: []tstr.Opt{
				tstr.WithFn(func() {}),
				tstr.WithFn(func() {}),
			},
			expectedErr: tstr.ErrOverwritingTestFn,
		},
		{
			name: "wrong test case type",
			opts: []tstr.Opt{
				tstr.WithTable(MockTestingT{}, []int{1, 2}, func(*testing.T, int) {}),
			},
			expectedErr: tstr.ErrWrongTestCaseType,
		},
		{
			name: "missing name field",
			opts: []tstr.Opt{
				tstr.WithTable(MockTestingT{}, []struct{ foo int }{{}}, func(*testing.T, struct{ foo int }) {}),
			},
			expectedErr: tstr.ErrMissingNameField,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tstr.Run(tt.opts...)
			assert.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestWithTable(t *testing.T) {
	type test struct {
		Name  string
		input int
	}

	got := make([]int, 0, 2)
	err := tstr.Run(
		tstr.WithTable(t,
			[]test{
				{Name: "test-1", input: 1},
				{Name: "test-2", input: 2},
			},
			func(t *testing.T, tt test) {
				got = append(got, tt.input)
			},
		),
	)
	assert.NoError(t, err)
	assert.Equal(t, []int{1, 2}, got)
}

func TestTester_RunWithoutInit(t *testing.T) {
	ran := false
	assert.NoError(t, tstr.NewTester(tstr.WithFn(func() { ran = true })).Run())
	assert.True(t, ran)

	assert.ErrorIs(t, tstr.NewTester().Run(), tstr.ErrMissingTestFn)
}

func TestTester_InitErrorIsSticky(t *testing.T) {
	optErr := errors.New("bad option")
	ran, starts, applied := false, 0, 0
	dep := depfn.New(func() error { starts++; return nil }, nil, nil)

	tester := tstr.NewTester(
		tstr.WithFn(func() { ran = true }),
		tstr.WithDeps(dep),
		func(*tstr.Tester) error { applied++; return optErr },
	)

	assert.ErrorIs(t, tester.Init(), optErr)
	assert.ErrorIs(t, tester.Init(), optErr)
	assert.ErrorIs(t, tester.Run(), optErr)
	assert.Equal(t, 1, applied)
	assert.False(t, ran)
	assert.Equal(t, 0, starts)
}

func TestTester_InitPanic(t *testing.T) {
	tester := tstr.NewTester(
		func(*tstr.Tester) error { panic("bad option") },
		tstr.WithFn(func() {}),
	)

	assert.Panics(t, func() { _ = tester.Run() })

	var err error
	assert.NotPanics(t, func() { err = tester.Run() })
	assert.ErrorIs(t, err, tstr.ErrMissingTestFn)
}

func TestTester_Reuse(t *testing.T) {
	var starts, stops, runs int
	dep := depfn.New(
		func() error { starts++; return nil },
		nil,
		func() error { stops++; return nil },
	)

	tester := tstr.NewTester(
		tstr.WithDeps(dep),
		tstr.WithFn(func() { runs++ }),
	)

	assert.NoError(t, tester.Init())
	assert.NoError(t, tester.Init())
	assert.NoError(t, tester.Run())
	assert.NoError(t, tester.Run())

	assert.Equal(t, 2, starts)
	assert.Equal(t, 2, stops)
	assert.Equal(t, 2, runs)
}

type MockTestingT struct{}

func (MockTestingT) Run(string, func(*testing.T)) bool { return true }
