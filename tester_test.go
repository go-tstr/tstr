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
		{
			name: "wrong name field type",
			opts: []tstr.Opt{
				tstr.WithTable(MockTestingT{}, []struct{ Name int }{{Name: 1}}, func(*testing.T, struct{ Name int }) {}),
			},
			expectedErr: tstr.ErrWrongNameFieldType,
		},
		{
			name: "nil embedded pointer name field",
			opts: []tstr.Opt{
				tstr.WithTable(MockTestingT{}, []embedsNilBase{{}}, func(*testing.T, embedsNilBase) {}),
			},
			expectedErr: tstr.ErrNilNameField,
		},
		{
			name: "missing name field with empty table",
			opts: []tstr.Opt{
				tstr.WithTable(MockTestingT{}, []struct{ foo int }{}, func(*testing.T, struct{ foo int }) {}),
			},
			expectedErr: tstr.ErrMissingNameField,
		},
		{
			name: "ambiguous name field",
			opts: []tstr.Opt{
				tstr.WithTable(MockTestingT{}, []embedsTwoNames{{}}, func(*testing.T, embedsTwoNames) {}),
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

func TestWithTable_NamedStringType(t *testing.T) {
	type name string
	type test struct {
		Name name
	}

	var got string
	err := tstr.Run(
		tstr.WithTable(t,
			[]test{{Name: "named"}},
			func(t *testing.T, _ test) {
				got = t.Name()
			},
		),
	)
	assert.NoError(t, err)
	assert.Equal(t, "TestWithTable_NamedStringType/named", got)
}

func TestWithTable_NilNameFailsInit(t *testing.T) {
	rec := &recordingTestingT{}
	tester := tstr.NewTester(
		tstr.WithTable(rec,
			[]embedsNilBase{{base: &base{Name: "a"}}, {}, {base: &base{Name: "b"}}},
			func(*testing.T, embedsNilBase) {},
		),
	)
	err := tester.Init()
	assert.ErrorIs(t, err, tstr.ErrNilNameField)
	assert.Empty(t, rec.names)
}

func TestWithTable_EmptyTable(t *testing.T) {
	rec := &recordingTestingT{}
	err := tstr.Run(
		tstr.WithTable(rec, []struct{ Name string }{}, func(*testing.T, struct{ Name string }) {}),
	)
	assert.NoError(t, err)
	assert.Empty(t, rec.names)
}

type (
	base           struct{ Name string }
	other          struct{ Name string }
	embedsNilBase  struct{ *base }
	embedsTwoNames struct {
		base
		other
	}
)

type recordingTestingT struct{ names []string }

func (r *recordingTestingT) Run(name string, _ func(*testing.T)) bool {
	r.names = append(r.names, name)
	return true
}

type MockTestingT struct{}

func (MockTestingT) Run(string, func(*testing.T)) bool { return true }
