package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"time"

	"github.com/go-tstr/tstr/strerr"
	"golang.org/x/sync/errgroup"
)

const (
	ErrMissingCmd     = strerr.Error("missing command")
	ErrStartFailed    = strerr.Error("failed to start command")
	ErrReadyFailed    = strerr.Error("failed to verify readiness")
	ErrStopFailed     = strerr.Error("command didn't stop successfully")
	ErrOptApply       = strerr.Error("failed apply Opt")
	ErrNoMatchingLine = strerr.Error("no matching line found")
	ErrNilCmd         = strerr.Error("command has to be set before this option can be applied, check the order of options")
	// Deprecated: use ErrNilCmd.
	ErrNilCmdRegexp   = ErrNilCmd
	ErrPreCmdFailed   = strerr.Error("pre command failed")
	ErrBadRegexp      = strerr.Error("bad regular expression for matching line")
	ErrOutputPipe     = strerr.Error("failed to acquire output pipe for command")
	ErrBuildFailed    = strerr.Error("failed to build go binary")
	ErrCreateCoverDir = strerr.Error("failed create coverage dir")
	ErrExitedEarly    = strerr.Error("command exited before becoming ready")
)

const exitGrace = time.Second

type Cmd struct {
	opts         []Opt
	ready        func(context.Context, *exec.Cmd) error
	stop         func(*exec.Cmd) error
	cmd          *exec.Cmd
	envSet       []string
	envIsSet     bool
	envAppend    []string
	readyTimeout time.Duration
	readyOnExit  bool
	exitReported bool
	done         chan struct{}
	waitErr      error
	cleanup      []func()
}

type Opt func(*Cmd) error

func New(opts ...Opt) *Cmd {
	return &Cmd{
		opts:         opts,
		ready:        func(context.Context, *exec.Cmd) error { return nil },
		stop:         StopWithSignal(os.Interrupt),
		readyTimeout: 30 * time.Second,
	}
}

func (c *Cmd) Start() error {
	if err := c.start(); err != nil {
		c.runCleanup()
		return err
	}
	return nil
}

func (c *Cmd) Ready() error {
	if c.done == nil {
		return fmt.Errorf("%w: command not started", ErrReadyFailed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.readyTimeout)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- c.ready(ctx, c.cmd) }()

	exited := c.done
	if c.readyOnExit {
		exited = nil
	}

	for {
		select {
		case err := <-errCh:
			c.exitReported = c.readyOnExit && err != nil
			return c.wrapErr(ErrReadyFailed, err)
		case <-ctx.Done():
			return c.wrapErr(ErrReadyFailed, fmt.Errorf("timeout after %s: %w", c.readyTimeout, ctx.Err()))
		case <-exited:
			if c.waitErr != nil {
				return c.wrapErr(ErrReadyFailed, c.exitedEarly(ctx, errCh))
			}
			// daemonizing wrappers exit 0 right away and the service comes up later
			exited = nil
		}
	}
}

// Stop tolerates a missing command because the runner stops dependencies whose Start failed.
func (c *Cmd) Stop() error {
	if c.cmd == nil {
		return nil
	}
	defer c.runCleanup()
	err := c.stop(c.cmd)
	if c.done == nil {
		return c.wrapErr(ErrStopFailed, err)
	}
	waitErr := c.wait()
	if c.exitReported {
		waitErr = nil
	}
	return c.wrapErr(ErrStopFailed, errors.Join(err, waitErr))
}

func (c *Cmd) start() error {
	c.envSet, c.envIsSet, c.envAppend = nil, false, nil
	for _, opt := range c.opts {
		if err := opt(c); err != nil {
			return fmt.Errorf("%w: %w", ErrOptApply, err)
		}
	}

	if c.cmd == nil {
		return ErrMissingCmd
	}

	// Resolved here so Environ sees the final Dir and the env options work in any order.
	if c.envIsSet || len(c.envAppend) > 0 {
		env := c.cmd.Environ()
		if c.envIsSet {
			env = c.envSet
		}
		env = append(env, c.envAppend...)
		c.cmd.Env = env
	}

	if err := c.cmd.Start(); err != nil {
		return c.wrapErr(ErrStartFailed, err)
	}

	cmd, done := c.cmd, make(chan struct{})
	c.done, c.waitErr, c.exitReported = done, nil, false
	go func() {
		c.waitErr = cmd.Wait()
		close(done)
	}()
	return nil
}

func (c *Cmd) exitedEarly(ctx context.Context, errCh <-chan error) error {
	c.exitReported = true
	if ctx.Err() != nil {
		return errors.Join(ErrExitedEarly, c.waitErr)
	}

	// the ready fn may still succeed on output the command left behind, so give it a moment
	var err error
	select {
	case err = <-errCh:
		if err == nil {
			c.exitReported = false
			return nil
		}
	case <-time.After(min(exitGrace, c.readyTimeout)):
	}
	return errors.Join(ErrExitedEarly, c.waitErr, err)
}

func (c *Cmd) wait() error {
	<-c.done
	return c.waitErr
}

func (c *Cmd) runCleanup() {
	for _, fn := range c.cleanup {
		fn()
	}
	c.cleanup = nil
}

func (c *Cmd) wrapErr(wErr, err error) error {
	if err == nil {
		return nil
	}
	if c.cmd == nil {
		return fmt.Errorf("%w: %w", wErr, err)
	}
	return fmt.Errorf("cmd '%s' %w: %w", c.cmd.String(), wErr, err)
}

// WithCommand creates a new command with the given name and arguments.
func WithCommand(name string, args ...string) Opt {
	return func(c *Cmd) error {
		c.cmd = exec.Command(name, args...)
		c.cmd.Stdout = os.Stdout
		c.cmd.Stderr = os.Stderr
		return nil
	}
}

// WithCommandFn creates a new command using the given function.
// This is useful for lazy loading of the command and using arguments from other dependencies.
func WithCommandFn(fn func() (*exec.Cmd, error)) Opt {
	return func(c *Cmd) error {
		cmd, err := fn()
		if err != nil {
			return err
		}
		c.cmd = cmd
		return nil
	}
}

// WithReadyFn allows user to provide custom readiness function.
// Given fn should block until the command is ready.
func WithReadyFn(fn func(context.Context, *exec.Cmd) error) Opt {
	return func(c *Cmd) error {
		c.readyOnExit = false
		c.ready = fn
		return nil
	}
}

// WithReadyHTTP sets the ready function to wait for url to return 200 OK.
func WithReadyHTTP(url string) Opt {
	const delay = 100 * time.Millisecond
	return func(c *Cmd) error {
		c.readyOnExit = false
		c.ready = func(ctx context.Context, cmd *exec.Cmd) error {
			client := &http.Client{
				Timeout: 1 * time.Second,
			}
			for {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
				if err != nil {
					return err
				}
				resp, err := client.Do(req)
				if err == nil {
					_ = resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						return nil
					}
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
		}
		return nil
	}
}

// WithStopFn allows user to provide custom stop function.
// The fn may receive a command that never started (Process == nil) and must tolerate it.
// The function must only signal or kill the command and must not call Wait, Cmd already waits on the process and a concurrent Wait is a data race.
func WithStopFn(fn func(*exec.Cmd) error) Opt {
	return func(c *Cmd) error {
		c.stop = fn
		return nil
	}
}

// WithEnvSet replaces the inherited environment with the given variables.
// Calling it with no arguments gives the command an empty environment.
func WithEnvSet(env ...string) Opt {
	return func(c *Cmd) error {
		c.envSet = append([]string{}, env...)
		c.envIsSet = true
		return nil
	}
}

// WithEnvAppend appends environment variables to the inherited environment or to the env set by WithEnvSet.
// If the same variable appears multiple times, the last value wins.
func WithEnvAppend(env ...string) Opt {
	return func(c *Cmd) error {
		c.envAppend = append(c.envAppend, env...)
		return nil
	}
}

// WithArgsSet sets arguments for the command.
func WithArgsSet(args ...string) Opt {
	return withCmd(func(c *Cmd) error {
		c.cmd.Args = args
		return nil
	})
}

// WithArgsAppend adds arguments to commands current argument list.
func WithArgsAppend(args ...string) Opt {
	return withCmd(func(c *Cmd) error {
		c.cmd.Args = append(c.cmd.Args, args...)
		return nil
	})
}

// WithDir sets the working directory for the command.
func WithDir(dir string) Opt {
	return withCmd(func(c *Cmd) error {
		c.cmd.Dir = dir
		return nil
	})
}

// WithWaitMatchingLine sets the ready function so that it waits for the command to output a line that matches the given regular expression.
func WithWaitMatchingLine(exp string) Opt {
	return withCmd(func(c *Cmd) error {
		fn, closePipe, err := matchingLine(exp, c.cmd)
		if err != nil {
			return err
		}
		c.cleanup = append(c.cleanup, closePipe)
		return WithReadyFn(fn)(c)
	})
}

// WithReadyTimeout overrides the default 30s timeout for the ready function.
func WithReadyTimeout(d time.Duration) Opt {
	return func(c *Cmd) error {
		c.readyTimeout = d
		return nil
	}
}

// WithWaitExit sets the ready function so that it waits for the command to exit successfully.
// This is useful for commands that exit on their own and don't need to be stopped manually.
func WithWaitExit() Opt {
	return func(c *Cmd) error {
		c.readyOnExit = true
		c.ready = func(context.Context, *exec.Cmd) error { return c.wait() }
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

// WithGoCode builds the given Go projects and sets the main package as the command.
// By default the output binary is instrumented to collect coverage data
// Working directory for build command is set to modulePath which means that the mainPkg should be relative to it.
// Building the binary is done in a separate goroutine and the command is started only after the build is finished.
// Also building is done only once which allows to reuse the reusing the same Cmd instance without rebuilding the binary.
func WithGoCode(modulePath, mainPkg string) Opt {
	var target string
	eg := &errgroup.Group{}
	eg.Go(func() error {
		dir, err := os.MkdirTemp("", "go-tstr")
		if err != nil {
			return fmt.Errorf("failed to create tmp dir for go binary: %w", err)
		}

		target = dir + "/" + "go-app"
		buildCmd := exec.Command("go", "build", "-race", "-cover", "-covermode", "atomic", "-o", target, mainPkg)
		buildCmd.Env = append(os.Environ(), "CGO_ENABLED=1") // Required for -race flag
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		buildCmd.Dir = modulePath
		err = buildCmd.Run()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrBuildFailed, err)
		}
		return nil
	})

	return func(c *Cmd) error {
		if err := eg.Wait(); err != nil {
			return err
		}

		c.cmd = exec.Command(target)
		c.cmd.Stdout = os.Stdout
		c.cmd.Stderr = os.Stderr
		return nil
	}
}

// WithGoCover calls WithGoCoverDir with the os.Getenv("GOCOVERDIR") value if it's set.
// Otherwise it's a no-op.
func WithGoCover() Opt {
	dir := os.Getenv("GOCOVERDIR")
	if dir == "" {
		return func(c *Cmd) error { return nil }
	}

	return WithGoCoverDir(dir)
}

// WithGoCoverDir creates the dir if it doesn't exist and
// appends the GOCOVERDIR env variable into the commands env.
func WithGoCoverDir(dir string) Opt {
	return func(c *Cmd) error {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("%w: %w", ErrCreateCoverDir, err)
		}
		return WithEnvAppend("GOCOVERDIR=" + dir)(c)
	}
}

func withCmd(fn func(*Cmd) error) Opt {
	return func(c *Cmd) error {
		if c.cmd == nil {
			return ErrNilCmd
		}
		return fn(c)
	}
}

// StopWithSignal returns a stop function that sends the given signal to the command if it is still running.
// It does not call Wait because Cmd already waits on the process and a concurrent Wait is a data race.
func StopWithSignal(s os.Signal) func(*exec.Cmd) error {
	return func(c *exec.Cmd) error {
		if c == nil || c.Process == nil {
			return nil
		}
		err := c.Process.Signal(s)
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
}

// MatchLine waits for the command to output a line that matches the given regular expression.
func MatchingLine(exp string, cmd *exec.Cmd) (func(context.Context, *exec.Cmd) error, error) {
	fn, _, err := matchingLine(exp, cmd)
	return fn, err
}

func matchingLine(exp string, cmd *exec.Cmd) (func(context.Context, *exec.Cmd) error, func(), error) {
	if cmd == nil {
		return nil, nil, ErrNilCmd
	}

	re, err := regexp.Compile(exp)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrBadRegexp, err)
	}

	// StdoutPipe is closed by Wait, which runs in the background, so use a pipe that outlives it
	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrOutputPipe, err)
	}
	cmd.Stdout = w
	closePipe := func() {
		_ = w.Close()
		_ = r.Close()
	}

	return func(ctx context.Context, _ *exec.Cmd) error {
		// the child has its own copy of the write end after Start
		_ = w.Close()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if re.Match(scanner.Bytes()) {
				// drain the rest of the output on background
				go func() {
					for scanner.Scan() {
					}
					_ = r.Close()
				}()
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		_ = r.Close()
		return errors.Join(ErrNoMatchingLine, scanner.Err())
	}, closePipe, nil
}
