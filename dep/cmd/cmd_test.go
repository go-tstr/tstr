package cmd_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/go-tstr/tstr/dep/cmd"
	"github.com/go-tstr/tstr/dep/deptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmd(t *testing.T) {
	waitPkg := writeProgram(t, code)
	waitBin := waitPkg + "/main"

	tests := []struct {
		name string
		cmd  *cmd.Cmd
		err  error
	}{
		{
			name: "MissingCommand",
			cmd:  cmd.New(),
			err:  cmd.ErrMissingCmd,
		},
		{
			name: "CommandNotFound",
			cmd: cmd.New(
				cmd.WithCommand("non-existing-command"),
			),
			err: cmd.ErrStartFailed,
		},
		{
			name: "WaitForExitError",
			cmd: cmd.New(
				cmd.WithCommand("go", "foo"),
				cmd.WithWaitExit(),
			),
			err: cmd.ErrReadyFailed,
		},
		{
			name: "WaitForExit",
			cmd: cmd.New(
				cmd.WithCommand("go", "version"),
				cmd.WithWaitExit(),
			),
		},
		{
			name: "NoMatchingLine",
			cmd: cmd.New(
				cmd.WithCommand("go", "version"),
				cmd.WithWaitMatchingLine("not matching line"),
			),
			err: cmd.ErrNoMatchingLine,
		},
		{
			name: "ReadyFailed",
			cmd: cmd.New(
				cmd.WithCommand("go", "version"),
				cmd.WithWaitMatchingLine("not matching line"),
			),
			err: cmd.ErrReadyFailed,
		},
		{
			name: "ReadyTimeoutExceeded",
			cmd: cmd.New(
				cmd.WithCommand("go", "env"),
				cmd.WithReadyFn(blockForever),
				cmd.WithReadyTimeout(1),
			),
			err: cmd.ErrReadyFailed,
		},
		{
			name: "OptionError",
			cmd: cmd.New(
				cmd.WithWaitMatchingLine("not matching line"),
			),
			err: cmd.ErrNilCmd,
		},
		{
			name: "WithEnvSet",
			cmd: cmd.New(
				cmd.WithCommand("go", "env", "GOPRIVATE"),
				cmd.WithEnvSet("GOPRIVATE=foo"),
				cmd.WithWaitMatchingLine("foo"),
				cmd.WithStopFn(func(c *exec.Cmd) error { return nil }),
			),
		},
		{
			name: "WithEnvAppend",
			cmd: cmd.New(
				cmd.WithCommand("go", "env", "GOPRIVATE"),
				cmd.WithEnvAppend("GOPRIVATE=foo"),
				cmd.WithWaitMatchingLine("foo"),
				cmd.WithStopFn(func(c *exec.Cmd) error { return nil }),
			),
		},
		{
			name: "WithExecCmd",
			cmd: cmd.New(
				cmd.WithExecCmd(exec.Command("go", "build", "-o", waitBin, waitPkg+"/main.go")),
				cmd.WithWaitExit(),
			),
		},
		{
			name: "DefaultReady",
			cmd: cmd.New(
				cmd.WithCommand("go", "version"),
				cmd.WithStopFn(func(c *exec.Cmd) error { return nil }),
			),
		},
		{
			name: "CustomStopFn",
			cmd: cmd.New(
				cmd.WithCommand(waitBin),
				cmd.WithWaitMatchingLine("Waiting for signal"),
				cmd.WithStopFn(cmd.StopWithSignal(syscall.SIGTERM)),
			),
		},
		{
			name: "WithDir",
			cmd: cmd.New(
				cmd.WithCommand("./"+filepath.Base(waitBin)),
				cmd.WithDir(filepath.Dir(waitBin)),
				cmd.WithWaitMatchingLine("Waiting for signal"),
			),
		},
		{
			name: "MatchingLineStderr",
			cmd: cmd.New(
				cmd.WithCommand("sh", "-c", `trap "exit 0" INT; echo ready >&2; sleep 30 >/dev/null 2>&1 & wait`),
				cmd.WithWaitMatchingLine("ready"),
			),
		},
		{
			name: "MatchingLineAfterLongLine",
			cmd: cmd.New(
				cmd.WithCommand("sh", "-c", `printf "%0100000d\nready\n" 0`),
				cmd.WithWaitMatchingLine("ready"),
			),
		},
		{
			name: "MatchingLineOversizedDiscarded",
			cmd: cmd.New(
				cmd.WithCommand("sh", "-c", `trap "" INT; printf "%065531dready%d\n" 0 0`),
				cmd.WithWaitMatchingLine("ready$"),
			),
			err: cmd.ErrNoMatchingLine,
		},
		{
			name: "MatchingLineNoTrailingNewline",
			cmd: cmd.New(
				cmd.WithCommand("sh", "-c", `trap "" INT; printf ready`),
				cmd.WithWaitMatchingLine("^ready$"),
			),
		},
		{
			name: "MatchingLineCRLF",
			cmd: cmd.New(
				cmd.WithCommand("sh", "-c", `trap "" INT; printf "ready\r\n"`),
				cmd.WithWaitMatchingLine("^ready$"),
			),
		},
		{
			name: "BadRegexp",
			cmd: cmd.New(
				cmd.WithCommand("./"+filepath.Base(waitBin)),
				cmd.WithWaitMatchingLine(`)_(*&(^*)^_*(&)^&(*%^($%^&*())))`),
			),
			err: cmd.ErrBadRegexp,
		},
		{
			name: "WithArgsSet_NilCmd",
			cmd: cmd.New(
				cmd.WithArgsSet("version"),
			),
			err: cmd.ErrNilCmd,
		},
		{
			name: "WithArgsAppend_NilCmd",
			cmd: cmd.New(
				cmd.WithArgsAppend("version"),
			),
			err: cmd.ErrNilCmd,
		},
		{
			name: "WithDir_NilCmd",
			cmd: cmd.New(
				cmd.WithDir(waitPkg),
			),
			err: cmd.ErrNilCmd,
		},
		{
			name: "WithGoCode_BuildFailure",
			cmd: cmd.New(
				cmd.WithGoCode(waitPkg, "./non/existing/pkg"),
				cmd.WithWaitMatchingLine("Waiting for signal"),
			),
			err: cmd.ErrBuildFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deptest.ErrorIs(t, tt.cmd, nil, tt.err)
		})
	}
}

func TestWithReadyHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	c := cmd.New(
		cmd.WithReadyHTTP(srv.URL),
		cmd.WithCommand("go", "version"),
		cmd.WithStopFn(func(c *exec.Cmd) error { return nil }),
	)
	deptest.ErrorIs(t, c, nil, nil)
}

func TestCmd_StopWithNilCmd(t *testing.T) {
	called := false
	c := cmd.New(
		cmd.WithStopFn(func(ec *exec.Cmd) error { called = true; return ec.Process.Kill() }),
	)
	deptest.ErrorIs(t, c, nil, cmd.ErrMissingCmd)
	assert.False(t, called)
	assert.NoError(t, c.Stop())
}

func TestCmd_StopWithUnstartedProcess(t *testing.T) {
	called := false
	c := cmd.New(
		cmd.WithCommand("non-existing-command"),
		cmd.WithStopFn(func(c *exec.Cmd) error {
			called = true
			assert.Nil(t, c.Process)
			return nil
		}),
	)
	deptest.ErrorIs(t, c, nil, cmd.ErrStartFailed)
	assert.True(t, called)
	assert.NoError(t, c.Stop())
}

func TestWithEnvAppend_InheritsEnv(t *testing.T) {
	t.Setenv("TSTR_TEST_INHERIT", "inherited")
	c := cmd.New(
		cmd.WithCommand("sh", "-c", "echo $TSTR_TEST_INHERIT $FOO"),
		cmd.WithEnvAppend("FOO=bar"),
		cmd.WithWaitMatchingLine("^inherited bar$"),
		cmd.WithStopFn(func(c *exec.Cmd) error { return nil }),
	)
	deptest.ErrorIs(t, c, nil, nil)
}

func TestWithEnvAppend_LastValueWins(t *testing.T) {
	t.Setenv("FOO", "old")
	c := cmd.New(
		cmd.WithCommand("sh", "-c", "echo $FOO"),
		cmd.WithEnvAppend("FOO=bar"),
		cmd.WithWaitMatchingLine("^bar$"),
		cmd.WithStopFn(func(c *exec.Cmd) error { return nil }),
	)
	deptest.ErrorIs(t, c, nil, nil)
}

func TestWithEnvAppend_PWDFollowsDir(t *testing.T) {
	dir := t.TempDir()
	c := cmd.New(
		cmd.WithCommand("sh", "-c", "echo $PWD"),
		cmd.WithEnvAppend("FOO=bar"),
		cmd.WithDir(dir),
		cmd.WithWaitMatchingLine("^"+regexp.QuoteMeta(dir)+"$"),
		cmd.WithStopFn(func(c *exec.Cmd) error { return nil }),
	)
	deptest.ErrorIs(t, c, nil, nil)
}

func TestWithEnvSet_BeforeCommand(t *testing.T) {
	c := cmd.New(
		cmd.WithEnvSet("FOO=bar"),
		cmd.WithCommand("sh", "-c", "echo $FOO"),
		cmd.WithWaitMatchingLine("^bar$"),
		cmd.WithStopFn(func(c *exec.Cmd) error { return nil }),
	)
	deptest.ErrorIs(t, c, nil, nil)
}

func TestWithEnvSet_ReplacesEnv(t *testing.T) {
	t.Setenv("TSTR_TEST_INHERIT", "inherited")
	c := cmd.New(
		cmd.WithCommand("sh", "-c", "echo x$TSTR_TEST_INHERIT"),
		cmd.WithEnvSet("FOO=bar"),
		cmd.WithWaitMatchingLine("^x$"),
		cmd.WithStopFn(func(c *exec.Cmd) error { return nil }),
	)
	deptest.ErrorIs(t, c, nil, nil)
}

func TestCmd_ExitedEarly(t *testing.T) {
	c := cmd.New(
		cmd.WithCommand("sh", "-c", "exit 1"),
		cmd.WithReadyHTTP("http://127.0.0.1:1/"),
		cmd.WithReadyTimeout(10*time.Second),
	)
	start := time.Now()
	deptest.ErrorIs(t, c, nil, cmd.ErrExitedEarly)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestCmd_ExitedEarlyAfterWaitExit(t *testing.T) {
	c := cmd.New(
		cmd.WithCommand("sh", "-c", "exit 1"),
		cmd.WithWaitExit(),
		cmd.WithReadyHTTP("http://127.0.0.1:1/"),
		cmd.WithReadyTimeout(10*time.Second),
	)
	start := time.Now()
	deptest.ErrorIs(t, c, nil, cmd.ErrExitedEarly)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestCmd_ExitZeroBeforeReady(t *testing.T) {
	c := cmd.New(
		cmd.WithCommand("sh", "-c", "true"),
		cmd.WithReadyFn(func(context.Context, *exec.Cmd) error {
			time.Sleep(200 * time.Millisecond)
			return nil
		}),
	)
	deptest.ErrorIs(t, c, nil, nil)
}

func TestCmd_WaitExitErrorReportedOnce(t *testing.T) {
	c := cmd.New(
		cmd.WithCommand("go", "foo"),
		cmd.WithWaitExit(),
	)
	require.NoError(t, c.Start())
	readyErr := c.Ready()
	stopErr := c.Stop()
	require.ErrorIs(t, readyErr, cmd.ErrReadyFailed)
	require.NoError(t, stopErr)
	assert.Equal(t, 1, strings.Count(errors.Join(readyErr, stopErr).Error(), "exit status"))
}

func TestCmd_StopAfterReadyTimeout(t *testing.T) {
	waitBin := buildProgram(t, writeProgram(t, code))

	c := cmd.New(
		cmd.WithCommand(waitBin),
		cmd.WithReadyFn(blockForever),
		cmd.WithReadyTimeout(100*time.Millisecond),
	)
	require.NoError(t, c.Start())
	t.Cleanup(func() { _ = c.Stop() })
	require.ErrorIs(t, c.Ready(), cmd.ErrReadyFailed)
	require.NoError(t, c.Stop())
}

func TestCmd_WithGoCode_Coverage(t *testing.T) {
	waitPkg := writeProgram(t, code)
	coverDir, err := os.MkdirTemp("", "coverdir_")
	require.NoError(t, err)

	c := cmd.New(
		cmd.WithGoCode(waitPkg, "./"),
		cmd.WithWaitMatchingLine("Waiting for signal"),
		cmd.WithGoCoverDir(coverDir),
	)
	deptest.ErrorIs(t, c, nil, nil)

	files, err := filepath.Glob(coverDir + "/*")
	require.NoError(t, err)

	// should have covcounters.* and covmeta.* file
	require.Len(t, files, 2)
}

func TestStopTimeout(t *testing.T) {
	ignoreBin := buildProgram(t, writeProgram(t, ignoreCode))

	c := cmd.New(
		cmd.WithCommand(ignoreBin),
		cmd.WithWaitMatchingLine("Ignoring signals"),
		cmd.WithStopTimeout(500*time.Millisecond),
	)
	require.NoError(t, c.Start())
	require.NoError(t, c.Ready())

	start := time.Now()
	err := c.Stop()
	require.ErrorIs(t, err, cmd.ErrStopFailed)
	require.ErrorIs(t, err, cmd.ErrStopTimeout)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestStopAlreadyExited(t *testing.T) {
	c := cmd.New(
		cmd.WithCommand("sh", "-c", "exit 3"),
		cmd.WithReadyFn(waitExited),
	)
	require.NoError(t, c.Start())
	require.NoError(t, c.Ready())

	err := c.Stop()
	require.ErrorIs(t, err, cmd.ErrStopFailed)
	assert.Contains(t, err.Error(), "exit status 3")
	assert.NotContains(t, err.Error(), "process already finished")
}

func TestStopDaemonizer(t *testing.T) {
	ec := exec.Command("sh", "-c", "sleep 2 & echo ready")
	ec.Stdout = &bytes.Buffer{}

	c := cmd.New(
		cmd.WithExecCmd(ec),
		cmd.WithReadyFn(waitExited),
		cmd.WithStopTimeout(500*time.Millisecond),
	)
	require.NoError(t, c.Start())
	require.NoError(t, c.Ready())
	require.NoError(t, c.Stop())
}

func TestStopGrandchildHoldsPipe(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ready")
	ec := exec.Command("sh", "-c", `trap '' INT TERM; sleep 3 & touch "$MARKER"; sleep 3`)
	ec.Env = append(os.Environ(), "MARKER="+marker)
	ec.Stdout = &bytes.Buffer{}

	c := cmd.New(
		cmd.WithExecCmd(ec),
		cmd.WithReadyFn(func(ctx context.Context, _ *exec.Cmd) error {
			for {
				if _, err := os.Stat(marker); err == nil {
					return nil
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				time.Sleep(10 * time.Millisecond)
			}
		}),
		cmd.WithStopTimeout(300*time.Millisecond),
	)
	require.NoError(t, c.Start())
	require.NoError(t, c.Ready())

	start := time.Now()
	err := c.Stop()
	require.ErrorIs(t, err, cmd.ErrStopTimeout)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestWithWaitMatchingLine_PassThrough(t *testing.T) {
	var stdout, stderr bytes.Buffer
	c := exec.Command("sh", "-c", `trap "" INT; echo ready; echo after; echo err >&2`)
	c.Stdout = &stdout
	c.Stderr = &stderr

	deptest.ErrorIs(t, cmd.New(
		cmd.WithExecCmd(c),
		cmd.WithWaitMatchingLine("ready"),
	), nil, nil)

	assert.Equal(t, "ready\nafter\n", stdout.String())
	assert.Equal(t, "err\n", stderr.String())
}

func TestWithWaitMatchingLine_SharedWriter(t *testing.T) {
	var out bytes.Buffer
	c := exec.Command("sh", "-c", `trap "" INT; echo ready; echo err >&2; echo after`)
	c.Stdout = &out
	c.Stderr = &out

	deptest.ErrorIs(t, cmd.New(
		cmd.WithExecCmd(c),
		cmd.WithWaitMatchingLine("ready"),
	), nil, nil)

	assert.Equal(t, "ready\nerr\nafter\n", out.String())
}

func TestWithWaitMatchingLine_GrandchildOutlivesParent(t *testing.T) {
	out := &syncBuffer{}
	c := exec.Command("sh", "-c", `trap "" INT; (sleep 1; echo late) & echo ready; exit 0`)
	c.Stdout = out

	deptest.ErrorIs(t, cmd.New(
		cmd.WithExecCmd(c),
		cmd.WithWaitMatchingLine("ready"),
	), func() {
		assert.Eventually(t, func() bool { return strings.Contains(out.String(), "late") }, 3*time.Second, 10*time.Millisecond)
	}, nil)
}

func TestWithWaitMatchingLine_StopDoesNotWaitForGrandchild(t *testing.T) {
	c := cmd.New(
		cmd.WithCommand("sh", "-c", `trap "exit 0" INT; echo ready; sleep 3 & wait`),
		cmd.WithWaitMatchingLine("ready"),
	)
	require.NoError(t, c.Start())
	require.NoError(t, c.Ready())

	start := time.Now()
	require.NoError(t, c.Stop())
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestWithWaitMatchingLine_BlockingWriter(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pr.Close() })
	ec := exec.Command("sh", "-c", "echo ready; sleep 30")
	ec.Stdout = pw

	c := cmd.New(
		cmd.WithExecCmd(ec),
		cmd.WithWaitMatchingLine("ready"),
		cmd.WithStopTimeout(500*time.Millisecond),
	)
	require.NoError(t, c.Start())
	require.NoError(t, c.Ready())

	start := time.Now()
	_ = c.Stop()
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestWithWaitMatchingLine_GrandchildHoldsStdout(t *testing.T) {
	c := cmd.New(
		cmd.WithCommand("sh", "-c", "echo ready; sleep 30 & wait"),
		cmd.WithWaitMatchingLine("ready"),
		cmd.WithStopTimeout(500*time.Millisecond),
	)
	require.NoError(t, c.Start())
	require.NoError(t, c.Ready())

	start := time.Now()
	_ = c.Stop()
	assert.Less(t, time.Since(start), 5*time.Second)
}

func blockForever(context.Context, *exec.Cmd) error {
	select {}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Signal fails with ErrProcessDone only once the background Wait has reaped the process
func waitExited(ctx context.Context, c *exec.Cmd) error {
	for !errors.Is(c.Process.Signal(syscall.Signal(0)), os.ErrProcessDone) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

const (
	code = `
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	fmt.Println("Waiting for signal")
	s := <-c
	fmt.Println("Got signal:", s)
}`

	ignoreCode = `
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	signal.Ignore(os.Interrupt, syscall.SIGTERM)
	fmt.Println("Ignoring signals")
	for {
		time.Sleep(time.Hour)
	}
}`

	modFile = `module test-code

go 1.23.2
`
)

func writeProgram(t *testing.T, src string) string {
	dir, err := os.MkdirTemp("", "cmd-test-bin_")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(dir)) })
	require.NoError(t, os.WriteFile(dir+"/main.go", []byte(src), 0o600))
	require.NoError(t, os.WriteFile(dir+"/go.mod", []byte(modFile), 0o600))
	return dir
}

func buildProgram(t *testing.T, dir string) string {
	bin := dir + "/main"
	build := exec.Command("go", "build", "-o", bin, dir+"/main.go")
	build.Dir = dir
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	require.NoError(t, build.Run())
	return bin
}
