package compose

import (
	"context"
	"fmt"

	"github.com/go-tstr/tstr/strerr"
	tc "github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/testcontainers/testcontainers-go/wait"
)

const ErrCreateStack = strerr.Error("failed to create compose stack")

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
	return c.stack.Up(context.Background(), c.upOpts...)
}

func (c *Compose) Ready() error {
	return c.ready(c.stack)
}

func (c *Compose) Stop() error {
	return c.stack.Down(context.Background(), c.downOpts...)
}

// WithFile creates compose stack from file.
func WithFile(file string) Opt {
	return func(c *Compose) error {
		var err error
		c.stack, err = tc.NewDockerCompose(file)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrCreateStack, err)
		}
		return nil
	}
}

// WithStack sets ComposeStack.
func WithStack(s tc.ComposeStack) Opt {
	return func(c *Compose) error {
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
	return func(c *Compose) error {
		c.stack.WaitForService(service, strategy)
		return nil
	}
}

// WithEnv sets environment variables for compose.
func WithEnv(env map[string]string) Opt {
	return func(c *Compose) error {
		c.stack.WithEnv(env)
		return nil
	}
}

// WithOsEnv passes environment from OS to compose.
func WithOsEnv() Opt {
	return func(c *Compose) error {
		c.stack.WithOsEnv()
		return nil
	}
}

// WithReadyFn sets ready function.
func WithReadyFn(fn func(tc.ComposeStack) error) Opt {
	return func(c *Compose) error {
		c.ready = fn
		return nil
	}
}
