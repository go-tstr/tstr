package tstr_test

import (
	"testing"

	"github.com/go-tstr/tstr"
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
				tstr.WithFn(func() error { return nil }),
				tstr.WithFn(func() error { return nil }),
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

type MockTestingT struct{}

func (MockTestingT) Run(string, func(*testing.T)) bool { return true }
