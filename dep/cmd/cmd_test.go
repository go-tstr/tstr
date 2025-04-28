package cmd_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/go-tstr/tstr/dep/cmd"
	"github.com/go-tstr/tstr/dep/deptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmd(t *testing.T) {
	waitPkg := prepareCode(t)
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
			err: cmd.ErrNilCmdRegexp,
		},
		{
			name: "WithEnv",
			cmd: cmd.New(
				cmd.WithCommand("go", "env", "GOPRIVATE"),
				cmd.WithEnvSet("GOPRIVATE=foo"),
				cmd.WithWaitMatchingLine("foo"),
				cmd.WithStopFn(func(c *exec.Cmd) error { return nil }),
			),
		},
		{
			name: "WithEnv",
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
			name: "BadRegexp",
			cmd: cmd.New(
				cmd.WithCommand("./"+filepath.Base(waitBin)),
				cmd.WithWaitMatchingLine(`)_(*&(^*)^_*(&)^&(*%^($%^&*())))`),
			),
			err: cmd.ErrBadRegexp,
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

func TestCmd_WithGoCode_Coverage(t *testing.T) {
	waitPkg := prepareCode(t)
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

func blockForever(context.Context, *exec.Cmd) error {
	select {}
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

	modFile = `module test-code

go 1.23.2
`
)

func prepareCode(t *testing.T) string {
	dir, err := os.MkdirTemp("", "cmd-test-bin_")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(dir)) })
	require.NoError(t, os.WriteFile(dir+"/main.go", []byte(code), 0o644))
	require.NoError(t, os.WriteFile(dir+"/go.mod", []byte(modFile), 0o644))
	return dir
}
