package tstr_test

import (
	"os"
	"strings"
	"testing"

	"github.com/go-tstr/tstr"
	"github.com/go-tstr/tstr/dep/cmd"
	"github.com/go-tstr/tstr/dep/compose"
	"github.com/stretchr/testify/assert"
	tc "github.com/testcontainers/testcontainers-go/modules/compose"
)

func ExampleWithTable() {
	var t *testing.T

	type test struct {
		Name           string
		input          string
		expectedOutput string
	}

	testCases := []test{
		{
			Name:           "test-1",
			input:          "foo",
			expectedOutput: "FOO",
		},
		{
			Name:           "test-2",
			input:          "bar",
			expectedOutput: "BAR",
		},
		{
			Name:           "test-3",
			input:          "baz",
			expectedOutput: "BAZ",
		},
	}

	// Each test case will be executed as a sub-test using t.Run
	tstr.Run(tstr.WithTable(t, testCases, func(t *testing.T, tc test) {
		output := strings.ToUpper(tc.input)
		assert.Equal(t, tc.expectedOutput, output)
	}))
}

func ExampleRun() {
	const (
		modulePath = "../"
		mainPkg    = "./cmd/app"
	)

	tstr.Run(
		tstr.WithDeps(
			compose.New(
				compose.WithFile("docker-compose.yaml"),
				compose.WithUpOptions(tc.Wait(true)),
			),
			cmd.New(
				cmd.WithGoCode(modulePath, mainPkg),
				cmd.WithEnv(append(os.Environ(), "GOCOVERDIR=../coverage")...),
			),
		),
		tstr.WithFn(func() error {
			// Run test code here
			return nil
		}),
	)
}
