package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/go-tstr/tstr/strerr"
)

const (
	ErrMissingCmd     = strerr.Error("missing command")
	ErrStartFailed    = strerr.Error("failed to start command")
	ErrReadyFailed    = strerr.Error("failed to verify readiness")
	ErrStopFailed     = strerr.Error("command didn't stop successfully")
	ErrOptApply       = strerr.Error("failed apply Opt")
	ErrNoMatchingLine = strerr.Error("no matching line found")
	ErrNilCmdRegexp   = strerr.Error("command has to be set before this option can be applied, check the order of options")
)

type Cmd struct {
	opts         []Opt
	ready        func(*exec.Cmd) error
	stop         func(*exec.Cmd) error
	cmd          *exec.Cmd
	readyTimeout time.Duration
}

type Opt func(*Cmd) error

func New(opts ...Opt) *Cmd {
	return &Cmd{
		opts:         opts,
		ready:        func(*exec.Cmd) error { return nil },
		stop:         StopWithSignal(os.Interrupt),
		readyTimeout: 30 * time.Second,
	}
}

func (c *Cmd) Start() error {
	for _, opt := range c.opts {
		if err := opt(c); err != nil {
			return fmt.Errorf("failed to apply option %s: %w", getFnName(opt), err)
		}
	}

	if c.cmd == nil {
		return ErrMissingCmd
	}

	return c.wrapErr(ErrStartFailed, c.cmd.Start())
}

func (c *Cmd) Ready() error {
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		errCh <- c.ready(c.cmd)
	}()

	select {
	case <-time.After(c.readyTimeout):
		return c.wrapErr(ErrReadyFailed, fmt.Errorf("timeout after %s", c.readyTimeout))
	case err := <-errCh:
		return c.wrapErr(ErrReadyFailed, err)
	}
}

func (c *Cmd) Stop() error {
	return c.wrapErr(ErrStopFailed, c.stop(c.cmd))
}

func (c *Cmd) wrapErr(wErr, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("cmd '%s' %w: %w", c.cmd.String(), wErr, err)
}

// WithCommand creates a new command with the given name and arguments.
func WithCommand(name string, args ...string) Opt {
	return func(c *Cmd) error {
		c.cmd = exec.Command(name, args...)
		return nil
	}
}

// WithReadyFn allows user to provide custom ready function.
func WithReadyFn(fn func(*exec.Cmd) error) Opt {
	return func(c *Cmd) error {
		c.ready = fn
		return nil
	}
}

// WithStopFn allows user to provide custom stop function.
func WithStopFn(fn func(*exec.Cmd) error) Opt {
	return func(c *Cmd) error {
		c.stop = fn
		return nil
	}
}

// WithDir sets environment variables for the command.
// By default, the command inherits the environment of the current process and setting this option will override it.
func WithEnv(env ...string) Opt {
	return func(c *Cmd) error {
		c.cmd.Env = env
		return nil
	}
}

// WithDir sets the working directory for the command.
func WithDir(dir string) Opt {
	return func(c *Cmd) error {
		c.cmd.Dir = dir
		return nil
	}
}

// WithWaitRegexp sets the ready function so that it waits for the command to output a line that matches the given regular expression.
func WithWaitMatchingLine(exp string) Opt {
	return func(c *Cmd) error {
		re, err := regexp.Compile(exp)
		if err != nil {
			return err
		}

		if c.cmd == nil {
			return ErrNilCmdRegexp
		}

		stdout, err := c.cmd.StdoutPipe()
		if err != nil {
			return err
		}

		return WithReadyFn(func(cmd *exec.Cmd) error {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				if re.Match(scanner.Bytes()) {
					return nil
				}
			}
			return errors.Join(ErrNoMatchingLine, scanner.Err())
		})(c)
	}
}

// WithReadyTimeout overrides the default 30s timeout for the ready function.
func WithReadyTimeout(d time.Duration) Opt {
	return func(c *Cmd) error {
		c.readyTimeout = d
		return nil
	}
}

// WithWaitExit sets the ready and stop functions so that ready waits for the command to exit successfully and stop returns nil immediately.
// This is useful for commands that exit on their own and don't need to be stopped manually.
func WithWaitExit() Opt {
	return func(c *Cmd) error {
		c.ready = func(cmd *exec.Cmd) error { return cmd.Wait() }
		c.stop = func(*exec.Cmd) error { return nil }
		return nil
	}
}

// WithExecCmd allows user to construct the command with custom exec.Cmd.
func WithExecCmd(cmd *exec.Cmd) Opt {
	return func(c *Cmd) error {
		c.cmd = cmd
		return nil
	}
}

// StopWithSignal returns a stop function that sends the given signal to the command and waits for it to exit.
// This can be used with WithStopFn to stop the command with a specific signal.
func StopWithSignal(s os.Signal) func(*exec.Cmd) error {
	return func(c *exec.Cmd) error {
		if c == nil || c.Process == nil {
			return nil
		}
		var err error
		if c.Process != nil && c.ProcessState == nil {
			err = c.Process.Signal(s)
		}
		return errors.Join(err, c.Wait())
	}
}

func getFnName(fn any) string {
	strs := strings.Split((runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()), ".")
	return strs[len(strs)-1]
}
