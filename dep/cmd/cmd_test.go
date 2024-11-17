package cmd_test

import (
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
	testBin := prepareBin(t)

	tests := []struct {
		name string
		fn   func() error
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
			name: "DefaultStopFn",
			cmd: cmd.New(
				cmd.WithCommand(testBin),
				cmd.WithWaitMatchingLine("Waiting for signal"),
			),
		},
		{
			name: "CustomStopFn",
			cmd: cmd.New(
				cmd.WithCommand(testBin),
				cmd.WithWaitMatchingLine("Waiting for signal"),
				cmd.WithStopFn(cmd.StopWithSignal(syscall.SIGTERM)),
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
				cmd.WithCommand("sleep", "100"),
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
			name: "WithDir",
			cmd: cmd.New(
				cmd.WithCommand("./"+filepath.Base(testBin)),
				cmd.WithDir(filepath.Dir(testBin)),
				cmd.WithWaitMatchingLine("Waiting for signal"),
			),
		},
		{
			name: "WithEnv",
			cmd: cmd.New(
				cmd.WithCommand("go", "env", "GOPRIVATE"),
				cmd.WithEnv("GOPRIVATE=foo"),
				cmd.WithWaitMatchingLine("foo"),
			),
		},

		{
			name: "WithExecCmd",
			cmd: cmd.New(
				cmd.WithExecCmd(func() *exec.Cmd { return exec.Command("go", "version") }()),
				cmd.WithWaitExit(),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deptest.ErrorIs(t, tt.cmd, tt.fn, tt.err)
		})
	}
}

func blockForever(*exec.Cmd) error {
	select {}
}

func prepareBin(t *testing.T) string {
	const code = `
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

	dir, err := os.MkdirTemp("", "cmd-test-bin_")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(dir)) })

	require.NoError(t, os.WriteFile(dir+"/main.go", []byte(code), 0o644))
	buildCmd := exec.Command("go", "build", dir+"/main.go")
	buildCmd.Dir = dir
	require.NoError(t, buildCmd.Run())
	return dir + "/main"
}
