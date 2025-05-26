package tstr

import (
	"errors"
	"fmt"

	"github.com/go-tstr/tstr/strerr"
)

const (
	ErrStartFailed = strerr.Error("failed to start test dependencies")
	ErrStopFailed  = strerr.Error("failed to stop test dependencies")
)

type Runner struct {
	runnables  []Dependency
	stoppables []Stoppable
}

// NewRunner creates a new Runner with the given dependencies.
// The dependencies will be started in the order they are provided and stopped in reverse order.
// If any of the dependencies fail to start, the rest of the dependencies will not be started.
// If any of the dependencies fail to stop, the rest of the dependencies will still be stopped.
// Start method should be non-blocking and return immediately after starting the dependency.
// Ready method should block until the dependency is ready to be used.
// Stop method should block until the dependency is stopped.
func NewRunner(rr ...Dependency) *Runner {
	return &Runner{
		runnables: rr,
	}
}

// Start starts the dependencies in the order they were provided and waits for them to be ready.
// Next dependency will not be started if the previous one fails to start or become ready.
// Stop should be always called after Start, even if Start fails. That way all the started dependencies will be stopped.
func (t *Runner) Start() error {
	for i := range t.runnables {
		r := t.runnables[i]
		t.stoppables = append(t.stoppables, r)
		if err := r.Start(); err != nil {
			return fmt.Errorf("%w: %w", ErrStartFailed, err)
		}
		if err := r.Ready(); err != nil {
			return fmt.Errorf("%w: %w", ErrStartFailed, err)
		}
	}
	return nil
}

// Stop stops all started dependencies in the reverse order they were started.
func (t *Runner) Stop() error {
	var err error
	for _, s := range t.stoppables {
		err = errors.Join(err, s.Stop())
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrStopFailed, err)
	}
	return nil
}

type Dependency interface {
	Startable
	Stoppable
}

type Startable interface {
	Start() error
	Ready() error
}

type Stoppable interface {
	Stop() error
}
