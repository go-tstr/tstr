package compose

import (
	"context"
	"fmt"

	"github.com/go-tstr/tstr/strerr"
	tc "github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	ErrCreateStack  = strerr.Error("failed to create compose stack")
	ErrMissingStack = strerr.Error("missing compose stack")
	ErrNilStack     = strerr.Error("compose stack has to be set before this option can be applied, check the order of options")
)

// Opt is option type for OptCompose.
type Opt func(*Compose) error

type Compose struct {
	stack    tc.ComposeStack
	opts     []Opt
	upOpts   []tc.StackUpOption
	downOpts []tc.StackDownOption
	ready    func(tc.ComposeStack) error
}

// New creates new Compose dependency.
// By default it applies tc.Wait(true) and tc.RemoveOrphans(true) options.
// Those can be overwritten by WithUpOptions and WithDownOptions.
func New(opts ...Opt) *Compose {
	return &Compose{
		opts:     opts,
		ready:    func(cs tc.ComposeStack) error { return nil },
		upOpts:   []tc.StackUpOption{tc.Wait(true)},
		downOpts: []tc.StackDownOption{tc.RemoveOrphans(true)},
	}
}

func (c *Compose) Start() error {
	for _, opt := range c.opts {
		if err := opt(c); err != nil {
			return fmt.Errorf("failed to apply option: %w", err)
		}
	}
	if c.stack == nil {
		return ErrMissingStack
	}
	return c.stack.Up(context.Background(), c.upOpts...)
}

func (c *Compose) Ready() error {
	if c.stack == nil {
		return ErrMissingStack
	}
	return c.ready(c.stack)
}

// Stop tolerates a nil stack because the runner stops dependencies whose Start failed.
func (c *Compose) Stop() error {
	if c.stack == nil {
		return nil
	}
	return c.stack.Down(context.Background(), c.downOpts...)
}

// WithFile creates compose stack from file.
func WithFile(file string) Opt {
	return func(c *Compose) error {
		stack, err := tc.NewDockerCompose(file)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrCreateStack, err)
		}
		c.stack = stack
		return nil
	}
}

// WithStack sets ComposeStack.
func WithStack(s tc.ComposeStack) Opt {
	return func(c *Compose) error {
		if s == nil {
			return ErrNilStack
		}
		c.stack = s
		return nil
	}
}

// WithUpOptions sets options for compose.Up().
func WithUpOptions(opts ...tc.StackUpOption) Opt {
	return func(c *Compose) error {
		c.upOpts = opts
		return nil
	}
}

// WithDownOptions sets options for compose.Down().
func WithDownOptions(opts ...tc.StackDownOption) Opt {
	return func(c *Compose) error {
		c.downOpts = opts
		return nil
	}
}

// WithWaitForService makes compose up wait for specific service with given strategy.
func WithWaitForService(service string, strategy wait.Strategy) Opt {
	return withStack(func(s tc.ComposeStack) error {
		s.WaitForService(service, strategy)
		return nil
	})
}

// WithEnv sets environment variables for compose.
func WithEnv(env map[string]string) Opt {
	return withStack(func(s tc.ComposeStack) error {
		s.WithEnv(env)
		return nil
	})
}

// WithOsEnv passes environment from OS to compose.
func WithOsEnv() Opt {
	return withStack(func(s tc.ComposeStack) error {
		s.WithOsEnv()
		return nil
	})
}

// WithReadyFn sets ready function.
func WithReadyFn(fn func(tc.ComposeStack) error) Opt {
	return func(c *Compose) error {
		c.ready = fn
		return nil
	}
}

func withStack(fn func(tc.ComposeStack) error) Opt {
	return func(c *Compose) error {
		if c.stack == nil {
			return ErrNilStack
		}
		return fn(c.stack)
	}
}
