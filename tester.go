// Package tstr provides testing framework for Go programs that simplifies testing with test dependencies.
package tstr

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/go-tstr/tstr/strerr"
)

const (
	ErrMissingTestFn      = strerr.Error("missing Opt for test function")
	ErrOverwritingTestFn  = strerr.Error("trying to overwrite test function")
	ErrMissingNameField   = strerr.Error("missing or ambiguous field Name in test case struct")
	ErrWrongTestCaseType  = strerr.Error("wrong test case type")
	ErrWrongNameFieldType = strerr.Error("field Name in test case struct must be a string")
	ErrNilNameField       = strerr.Error("cannot read field Name in test case")
)

// TestingM contains required methods from *testing.M.
type TestingM interface {
	Run() int
}

// TestingT contains required methods from *testing.T.
type TestingT interface {
	Run(name string, fn func(*testing.T)) bool
}

// exit allows monkey-patching os.Exit in tests.
var exit = os.Exit

// Run runs the test with the given options.
// Options are applied in the order they are passed.
// One of the options must provide the test function.
//
// Options that provide the test function:
//   - WithM
//   - WithFn
//   - WithTable
func Run(opts ...Opt) error {
	t := NewTester(opts...)
	if err := t.Init(); err != nil {
		return err
	}
	return t.Run()
}

// RunMain is a convinience wrapper around Run that can be used inside TestMain.
// RunMain applies automatically WithM option which calls m.Run.
// Also os.Exit is called with non-zero exit code if Run returns any error.
//
// Example:
//
//	func TestMain(m *testing.M) {
//		tstr.RunMain(m, tstr.WithDeps(MyDependency()))
//	}
func RunMain(m TestingM, opts ...Opt) {
	err := Run(append(opts, WithM(m))...)
	if err == nil {
		return
	}

	exitCode := 1
	var eErr ExitError
	if errors.As(err, &eErr) {
		exitCode = int(eErr)
	}

	fmt.Println(err)
	exit(exitCode)
}

type Tester struct {
	opts []Opt
	deps []Dependency
	test func() error
}

// NewTester creates a new Tester with the given options.
// In most cases you should use Run function instead of creating Tester manually,
// Using NewTester can be useful if you need more control over the test execution
// or if you want to reuse same Tester instance.
func NewTester(opts ...Opt) *Tester {
	return &Tester{
		opts: opts,
	}
}

// Init applies all options to the Tester.
func (t *Tester) Init() error {
	for _, opt := range t.opts {
		if err := opt(t); err != nil {
			return fmt.Errorf("failed to apply option: %w", err)
		}
	}
	if t.test == nil {
		return ErrMissingTestFn
	}
	return nil
}

// Run starts the test dependencies, executes the test function and finally stops the dependencies.
func (t *Tester) Run() error {
	r := NewRunner(t.deps...)
	if err := r.Start(); err != nil {
		return errors.Join(err, r.Stop())
	}

	err := t.test()
	return errors.Join(err, r.Stop())
}

func (t *Tester) setTest(fn func() error) error {
	if t.test != nil {
		return ErrOverwritingTestFn
	}
	t.test = fn
	return nil
}

type Opt func(*Tester) error

// WithDeps adds dependencies to the tester.
func WithDeps(deps ...Dependency) Opt {
	return func(t *Tester) error {
		t.deps = append(t.deps, deps...)
		return nil
	}
}

type ExitError int

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e) }

// WithM uses the given testing.M as the test function.
func WithM(m TestingM) Opt {
	return func(t *Tester) error {
		return t.setTest(func() error {
			if code := m.Run(); code != 0 {
				return ExitError(code)
			}
			return nil
		})
	}
}

// WithFn uses the given function as the test function.
func WithFn(fn func()) Opt {
	return func(t *Tester) error {
		return t.setTest(func() error {
			fn()
			return nil
		})
	}
}

// WithTable runs the given test function for each test case in the table.
// T must be a struct type with a string field Name which is used as the subtest name.
func WithTable[T any](tt TestingT, cases []T, test func(*testing.T, T)) Opt {
	return func(t *Tester) error {
		typ := reflect.TypeFor[T]()
		if typ.Kind() != reflect.Struct {
			return fmt.Errorf("%w: expected struct, got %s", ErrWrongTestCaseType, typ.Kind())
		}

		field, ok := typ.FieldByName("Name")
		if !ok {
			return ErrMissingNameField
		}
		if field.Type.Kind() != reflect.String {
			return fmt.Errorf("%w: got %s", ErrWrongNameFieldType, field.Type)
		}

		names := make([]string, len(cases))
		for i, tc := range cases {
			name, err := reflect.ValueOf(tc).FieldByIndexErr(field.Index)
			if err != nil {
				return fmt.Errorf("%w at index %d: %w", ErrNilNameField, i, err)
			}
			names[i] = name.String()
		}

		return t.setTest(func() error {
			for i, tc := range cases {
				tt.Run(names[i], func(t *testing.T) {
					test(t, tc)
				})
			}
			return nil
		})
	}
}
