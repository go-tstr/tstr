package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
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
	ErrStopTimeout    = strerr.Error("command did not exit within stop timeout")
)

// DefaultStopTimeout is how long Stop waits for the command to exit after the stop function before killing it.
const DefaultStopTimeout = 10 * time.Second

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
	stopTimeout  time.Duration
	ownWaitDelay bool
	readyOnExit  bool
	exitReported bool
	done         chan struct{}
	waitErr      error
	cleanup      []func() error
}

type Opt func(*Cmd) error

func New(opts ...Opt) *Cmd {
	return &Cmd{
		opts:         opts,
		ready:        func(context.Context, *exec.Cmd) error { return nil },
		stop:         StopWithSignal(os.Interrupt),
		readyTimeout: 30 * time.Second,
		stopTimeout:  DefaultStopTimeout,
	}
}

func (c *Cmd) Start() error {
	if err := c.start(); err != nil {
		return errors.Join(err, c.runCleanup())
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
// Stop calls the stop function and waits for the command to exit, killing it if it is still running after the stop timeout.
func (c *Cmd) Stop() error {
	if c.cmd == nil {
		return nil
	}
	err := c.stopAndWait()
	return c.wrapErr(ErrStopFailed, errors.Join(err, c.runCleanup()))
}

func (c *Cmd) stopAndWait() error {
	err := c.stop(c.cmd)
	if c.done == nil {
		return err
	}

	// a nil channel never fires, so a zero timeout waits forever
	var timeout <-chan time.Time
	if c.stopTimeout > 0 {
		timeout = time.After(c.stopTimeout)
	}

	select {
	case <-c.done:
	case <-timeout:
		err = errors.Join(err, c.kill())
	}

	waitErr := c.wait()
	if c.exitReported {
		waitErr = nil
	}
	return errors.Join(err, waitErr)
}

// kill is a no-op when the command already exited, done only closes once Wait has also drained the pipes
func (c *Cmd) kill() error {
	if errors.Is(c.cmd.Process.Signal(syscall.Signal(0)), os.ErrProcessDone) {
		return nil
	}
	killErr := StopWithSignal(os.Kill)(c.cmd)
	<-c.done
	// the command may have honoured the stop signal just before the kill landed
	if ps := c.cmd.ProcessState; ps != nil {
		if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() && ws.Signal() != syscall.SIGKILL {
			return killErr
		}
	}
	return errors.Join(ErrStopTimeout, killErr)
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

	// Wait blocks on pipe EOF when output is not an *os.File, and grandchildren of a killed command keep the pipe open
	c.ownWaitDelay = c.cmd.WaitDelay == 0 && c.stopTimeout > 0
	if c.ownWaitDelay {
		c.cmd.WaitDelay = c.stopTimeout
	}

	if err := c.cmd.Start(); err != nil {
		return c.wrapErr(ErrStartFailed, err)
	}

	cmd, done := c.cmd, make(chan struct{})
	c.done, c.waitErr, c.exitReported = done, nil, false
	go func() {
		c.waitErr = c.waitExit(cmd)
		close(done)
	}()
	return nil
}

// pipes left open by grandchildren are not a failure of the command itself
func (c *Cmd) waitExit(cmd *exec.Cmd) error {
	err := cmd.Wait()
	if c.ownWaitDelay && errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}
	return err
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

func (c *Cmd) runCleanup() error {
	var err error
	for _, fn := range c.cleanup {
		err = errors.Join(err, fn())
	}
	c.cleanup = nil
	return err
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
// Cmd kills the command if it is still running after the stop timeout, see WithStopTimeout.
func WithStopFn(fn func(*exec.Cmd) error) Opt {
	return func(c *Cmd) error {
		c.stop = fn
		return nil
	}
}

// WithStopTimeout overrides DefaultStopTimeout for both the exit wait and the output pipe drain, zero waits forever.
// Only the direct child is killed, grandchildren are left running.
func WithStopTimeout(d time.Duration) Opt {
	return func(c *Cmd) error {
		c.stopTimeout = d
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
		m, err := matchingLine(exp, c.cmd)
		if err != nil {
			return err
		}
		c.cleanup = append(c.cleanup, func() error { return m.cleanup(c.stopTimeout) })
		return WithReadyFn(m.ready)(c)
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

// MatchingLine returns a ready function that waits for the command to output a line matching the given regular expression.
// Both stdout and stderr are scanned and passed through to the writers already set on the command.
// The pipes and drain goroutines are released once the ready function has been called after Start and the process
// and its descendants have closed their output. Use WithWaitMatchingLine inside cmd.New for deterministic cleanup on Stop.
func MatchingLine(exp string, cmd *exec.Cmd) (func(context.Context, *exec.Cmd) error, error) {
	m, err := matchingLine(exp, cmd)
	if err != nil {
		return nil, err
	}
	return m.ready, nil
}

func matchingLine(exp string, cmd *exec.Cmd) (*lineMatcher, error) {
	if cmd == nil {
		return nil, ErrNilCmd
	}

	re, err := regexp.Compile(exp)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadRegexp, err)
	}

	m := &lineMatcher{re: re, matched: make(chan struct{}), eof: make(chan struct{})}
	if err := m.attach(cmd); err != nil {
		return nil, err
	}
	return m, nil
}

type lineMatcher struct {
	re        *regexp.Regexp
	matchOnce sync.Once
	matched   chan struct{}
	pipes     []*matchPipe
	writeOnce sync.Once
	open      atomic.Int32
	eof       chan struct{}
	mu        sync.Mutex
	err       error
}

func (m *lineMatcher) attach(cmd *exec.Cmd) error {
	stdout, err := m.pipe(cmd.Stdout)
	if err != nil {
		return err
	}
	stderr := stdout
	// A shared pipe keeps both streams in the order the user asked for when they already point at the same writer.
	if !interfaceEqual(cmd.Stdout, cmd.Stderr) {
		if stderr, err = m.pipe(cmd.Stderr); err != nil {
			stdout.close()
			return err
		}
	}
	cmd.Stdout, cmd.Stderr = stdout.w, stderr.w

	m.open.Store(int32(len(m.pipes)))
	for _, p := range m.pipes {
		go p.drain()
	}
	return nil
}

func (m *lineMatcher) ready(ctx context.Context, _ *exec.Cmd) error {
	m.closeWriters()
	select {
	case <-m.matched:
		return nil
	case <-m.eof:
		if m.isMatched() {
			return nil
		}
		return errors.Join(ErrNoMatchingLine, m.readErr())
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Closing a read end drops whatever the kernel still buffers, so the drains are woken to flush it first.
func (m *lineMatcher) cleanup(timeout time.Duration) error {
	m.closeWriters()
	for _, p := range m.pipes {
		if err := p.r.SetReadDeadline(time.Now()); err != nil {
			_ = p.r.Close()
		}
	}

	// a nil channel never fires, so a zero timeout waits forever
	var expired <-chan time.Time
	if timeout > 0 {
		expired = time.After(timeout)
	}
	select {
	case <-m.eof:
		return m.readErr()
	case <-expired:
		for _, p := range m.pipes {
			_ = p.r.Close()
		}
		return errors.Join(m.readErr(), fmt.Errorf("output not drained within %s", timeout))
	}
}

// The child has its own copies of the write ends after Start, ours would keep EOF from ever arriving.
func (m *lineMatcher) closeWriters() {
	m.writeOnce.Do(func() {
		for _, p := range m.pipes {
			_ = p.w.Close()
		}
	})
}

func (m *lineMatcher) pipe(dst io.Writer) (*matchPipe, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOutputPipe, err)
	}
	p := &matchPipe{m: m, r: r, w: w, dst: dst}
	m.pipes = append(m.pipes, p)
	return p, nil
}

func (m *lineMatcher) match(line []byte) {
	if m.isMatched() {
		return
	}
	line = bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
	if m.re.Match(line) {
		m.matchOnce.Do(func() { close(m.matched) })
	}
}

func (m *lineMatcher) isMatched() bool {
	select {
	case <-m.matched:
		return true
	default:
		return false
	}
}

func (m *lineMatcher) fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err == nil {
		m.err = err
	}
}

func (m *lineMatcher) readErr() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

func (m *lineMatcher) drained(err error) {
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
		m.fail(err)
	}
	if m.open.Add(-1) == 0 {
		close(m.eof)
	}
}

// matchPipe is handed to the child as a plain file, so Wait returns when the child exits even if grandchildren keep writing.
type matchPipe struct {
	m    *lineMatcher
	r, w *os.File
	dst  io.Writer
}

func (p *matchPipe) close() {
	_ = p.w.Close()
	_ = p.r.Close()
}

func (p *matchPipe) drain() {
	br := bufio.NewReaderSize(p.r, 64*1024)
	skipping := false
	var err error
	for err == nil {
		var chunk []byte
		chunk, err = br.ReadSlice('\n')
		// Matching happens before the tee so a blocked destination writer cannot stall readiness.
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			// Oversized lines are skipped entirely, matching a chunk could turn its boundary into a false anchored match.
			skipping, err = true, nil
		case skipping:
			skipping = false
		case len(chunk) > 0:
			p.m.match(chunk)
		}
		p.tee(chunk)
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		err = p.flush()
	}
	_ = p.r.Close()
	p.m.drained(err)
}

// flush tees what the kernel already buffered without blocking, a grandchild still holding the write end must not stall Stop.
func (p *matchPipe) flush() error {
	if err := p.r.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	rc, err := p.r.SyscallConn()
	if err != nil {
		return err
	}
	buf := make([]byte, 32*1024)
	for {
		var n int
		var rerr error
		if err := rc.Read(func(fd uintptr) bool {
			n, rerr = syscall.Read(int(fd), buf)
			return true
		}); err != nil {
			return err
		}
		switch {
		case n > 0:
			p.tee(buf[:n])
		case rerr == nil, errors.Is(rerr, syscall.EAGAIN):
			return nil
		case errors.Is(rerr, syscall.EINTR):
		default:
			return rerr
		}
	}
}

func (p *matchPipe) tee(chunk []byte) {
	if p.dst == nil || len(chunk) == 0 {
		return
	}
	if _, err := p.dst.Write(chunk); err != nil {
		p.m.fail(err)
	}
}

func interfaceEqual(a, b any) bool {
	defer func() { _ = recover() }()
	return a == b
}
