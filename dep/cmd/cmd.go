package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
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
	ErrPreCmdFailed   = strerr.Error("pre command failed")
	ErrBadRegexp      = strerr.Error("bad regular expression for matching line")
	ErrOutputPipe     = strerr.Error("failed to aquire output pipe for command")
	ErrBuildFailed    = strerr.Error("failed to build go binary")
	ErrCreateCoverDir = strerr.Error("failed create coverage dir")
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
			return fmt.Errorf("%w: %w", ErrOptApply, err)
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

// WithReadyFn allows user to provide custom readiness function.
// Given fn should block until the command is ready.
func WithReadyFn(fn func(*exec.Cmd) error) Opt {
	return func(c *Cmd) error {
		c.ready = fn
		return nil
	}
}

// WithReadyHTTP sets the ready function to wait for url to return 200 OK.
func WithReadyHTTP(url string) Opt {
	return func(c *Cmd) error {
		c.ready = func(cmd *exec.Cmd) error {
			client := &http.Client{
				Timeout: 10 * time.Second,
			}
			for {
				resp, err := client.Get(url)
				if err != nil {
					continue
				}
				if resp.StatusCode == http.StatusOK {
					return nil
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
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

// WithEnvSet sets environment variables for the command.
// By default the command inherits the environment of the current process and setting this option will override it.
func WithEnvSet(env ...string) Opt {
	return func(c *Cmd) error {
		c.cmd.Env = env
		return nil
	}
}

// WithEnvAppend adds environment variables to commands current env.
// By default the command inherits the environment of the current process and setting this option will override it.
func WithEnvAppend(env ...string) Opt {
	return func(c *Cmd) error {
		c.cmd.Env = append(c.cmd.Env, env...)
		return nil
	}
}

// WithArgsSet sets arguments for the command.
func WithArgsSet(args ...string) Opt {
	return func(c *Cmd) error {
		c.cmd.Args = args
		return nil
	}
}

// WithArgsAppend adds arguments to commands current argument list.
func WithArgsAppend(args ...string) Opt {
	return func(c *Cmd) error {
		c.cmd.Args = append(c.cmd.Args, args...)
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

// WithWaitMatchingLine sets the ready function so that it waits for the command to output a line that matches the given regular expression.
func WithWaitMatchingLine(exp string) Opt {
	return func(c *Cmd) error {
		fn, err := MatchingLine(exp, c.cmd)
		if err != nil {
			return err
		}
		return WithReadyFn(fn)(c)
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

// WithPreCmd runs the given command as part of the setup.
// This can be used to prepare the actual main command.
func WithPreCmd(cmd *exec.Cmd) Opt {
	return func(c *Cmd) error {
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%w: %w", ErrPreCmdFailed, err)
		}
		return nil
	}
}

// WithGoCode builds the given Go projects and sets the main package as the command.
// By default the command is set to collect coverage data.
// Working directory for build command is set to modulePath which means that the mainPkg should be relative to it.
func WithGoCode(modulePath, mainPkg string) Opt {
	return func(c *Cmd) error {
		dir, err := os.MkdirTemp("", "go-tstr")
		if err != nil {
			return fmt.Errorf("failed to create tmp dir for go binary: %w", err)
		}

		target := dir + "/" + "go-app"
		buildCmd := exec.Command("go", "build", "-race", "-cover", "-covermode", "atomic", "-o", target, mainPkg)
		buildCmd.Env = append(os.Environ(), "CGO_ENABLED=1") // Required for -race flag
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		buildCmd.Dir = modulePath
		err = buildCmd.Run()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrBuildFailed, err)
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
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("%w: %w", ErrCreateCoverDir, err)
		}
		c.cmd.Env = append(c.cmd.Env, "GOCOVERDIR="+dir)
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

// MatchLine waits for the command to output a line that matches the given regular expression.
func MatchingLine(exp string, cmd *exec.Cmd) (func(*exec.Cmd) error, error) {
	if cmd == nil {
		return nil, ErrNilCmdRegexp
	}

	re, err := regexp.Compile(exp)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadRegexp, err)
	}

	cmd.Stdout = nil
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOutputPipe, err)
	}

	return func(cmd *exec.Cmd) error {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if re.Match(scanner.Bytes()) {
				// drain the rest of the output on background
				go func() {
					for scanner.Scan() {
					}
				}()
				return nil
			}
		}
		return errors.Join(ErrNoMatchingLine, scanner.Err())
	}, nil
}
