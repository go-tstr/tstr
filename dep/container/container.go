// Package container provides a wrapper around the testcontainers-go library to simplify container management in tests.
package container

import (
	"context"
	"fmt"

	"github.com/go-tstr/tstr/strerr"
	"github.com/testcontainers/testcontainers-go"
)

const (
	ErrCreateWithModule           = strerr.Error("failed to create container with testcontainers module")
	ErrCreateWithGenericContainer = strerr.Error("failed to create generic container")
)

type Container struct {
	opts  []Opt
	c     testcontainers.Container
	ready func(testcontainers.Container) error
}

type Opt func(*Container) error

func New(opts ...Opt) *Container {
	return &Container{
		opts:  opts,
		ready: func(c testcontainers.Container) error { return nil },
	}
}

func (c *Container) Start() error {
	for _, opt := range c.opts {
		if err := opt(c); err != nil {
			return fmt.Errorf("failed to apply option: %w", err)
		}
	}
	return nil
}

func (c *Container) Ready() error {
	return c.ready(c.c)
}

func (c *Container) Stop() error {
	return testcontainers.TerminateContainer(c.c)
}

// Container returns the underlying testcontainers.Container.
func (c *Container) Container() testcontainers.Container {
	return c.c
}

// WithReadyFn sets a custom readiness function which should block until ready.
func WithReadyFn(fn func(testcontainers.Container) error) Opt {
	return func(c *Container) error {
		c.ready = fn
		return nil
	}
}

// WithModule creates a container using the testcontainers-go modules.
func WithModule[T testcontainers.Container](
	runFn func(ctx context.Context, img string, opts ...testcontainers.ContainerCustomizer) (T, error),
	img string,
	opts ...testcontainers.ContainerCustomizer,
) Opt {
	return func(c *Container) error {
		var err error
		c.c, err = runFn(context.Background(), img, opts...)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrCreateWithModule, err)
		}
		return nil
	}
}

// WithGenericContainer creates a container using the testcontainers.GenericContainer function.
func WithGenericContainer(req testcontainers.GenericContainerRequest) Opt {
	return func(c *Container) (err error) {
		c.c, err = testcontainers.GenericContainer(context.Background(), req)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrCreateWithGenericContainer, err)
		}
		return nil
	}
}
