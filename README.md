[![Go Reference](https://pkg.go.dev/badge/github.com/go-tstr/tstr.svg)](https://pkg.go.dev/github.com/go-tstr/tstr) [![codecov](https://codecov.io/github/go-tstr/tstr/graph/badge.svg?token=H3u7Ui9PfC)](https://codecov.io/github/go-tstr/tstr) ![main](https://github.com/go-tstr/tstr/actions/workflows/go.yml/badge.svg?branch=main)

# TSTR: your ultimate testing library!

tstr is testing library allows you to write integration and black-box tests like normal unit tests in Go.

You can declare the test dependencies like:

- compose files
- single containers
- cli commands
- main package of Go program

and let tstr take care of the rest.

## Usage

This library is build on top of two concepts:

- tstr.Tester
- tstr.Dependency

### tstr.Tester

There's two common ways to use tester, either from `func TestMain` or from `func TestXXX`. For both of these approaches there's helper function; `tstr.RunMain` and `tstr.Run`, which make it easy to setup and run `tstr.Tester`.

#### tstr.RunMain

```go
func TestMain(m *testing.M) {
	tstr.RunMain(m, tstr.WithDeps(
    // Pass test dependencies here.
    ))
}
```

With `TestMain` approach you will have single test env within the packge.
`tstr.RunMain` will setup the test env you defined, call `m.Run()`, cleanup test env and finally call `os.Exit` with returned exit code.

#### tstr.Run

This approach allows more granular control over test env. For example you can have single test env for each top level test. This can be usefull when you want to avoid any side effects and shared state between tests. Also this approach allows more advaced usage like creating a pool of test envs for parallel testing.

##### tstr.WithFn

Simplest way to use `tstr.Run` is with the `tstr.WithFn` option:

```go
func TestMyFunc(t *testing.T) {
	err := tstr.Run(
		tstr.WithDeps(
		// Pass test dependencies here.
		),
		tstr.WithFn(func() {
			const (
				input    = 1
				expected = 1
			)
			got := MyFunc(input)
			assert.Equal(t, expected, got)
		}),
	)
	require.NoError(t, err)
}
```

##### tstr.WithTable

For table driven tests you can use `tstr.WithTable` which loops over the given test table and executes test function for each element using `t.Run`:

```go
func TestMyFunc(t *testing.T) {
	type test struct {
		Name     string
		input    int
		expected int
	}

	tests := []test{
		{Name: "test-1", input: 1, expected: 1},
		{Name: "test-2", input: 2, expected: 2},
		{Name: "test-3", input: 3, expected: 3},
	}

	err := tstr.Run(
		tstr.WithDeps(
		// Add dependencies here.
		),
		tstr.WithTable(t, tests, func(t *testing.T, tt test) {
			got := MyFunc(tt.input)
			assert.Equal(t, tt.expected, got)
		}),
	)
	require.NoError(t, err)
}
```

### tstr.Dependency

`tstr.Dependency` declares an interface for test dependency which can be then controlled by `tstr.Tester`. This repo provides the most commonly used dependecies that user can use within their tests. Since `tstr.Dependency` is just an interface users can also implement their own custom dependencies.

#### Compose

Compose dependecy allows you to define and manage Docker Compose stacks as test dependencies. You can create a Compose stack from projects compose file and control its lifecycle within your tests.

```go
func TestMain(m *testing.M) {
	tstr.RunMain(m, tstr.WithDeps(
		compose.New(
			compose.WithFile("../docker-compose.yaml"),
			compose.WithUpOptions(tc.Wait(true)),
			compose.WithDownOptions(tc.RemoveVolumes(true)),
			compose.WithEnv(map[string]string{"DB_PORT": "5432"}),
			compose.WithWaitForService("postgres", wait.ForLog("ready to accept connections")),
		),
	))
}
```

#### Container

Container dependecy allows you to define and manage single containers as test dependencies. You can use predefined modules from testcontainer-go or create generic container.

```go
func TestMain(m *testing.M) {
	tstr.RunMain(m, tstr.WithDeps(
		container.New(
			container.WithModule(postgres.Run, "postgres:16-alpine",
				postgres.WithDatabase("test"),
				postgres.WithUsername("user"),
				postgres.WithPassword("password"),
			),
		),
	))
}
```

#### Cmd

Cmd dependecy is the most versatile one. It can be used for running any binary or even compiling a Go application and running it as dependency.

This example compiles `my-app` Go application, instruments it for coverage collections, waits for it to be ready and finally starts running tests.

```go
func TestMain(m *testing.M) {
	tstr.RunMain(m, tstr.WithDeps(
		cmd.New(
			cmd.WithGoCode("../", "./cmd/my-app"),
			cmd.WithReadyHTTP("http://localhost:8080/ready"),
			cmd.WithEnvAppend("GOCOVERDIR=./cover"),
		),
	))
}
```

#### Custom Dependencies

You can also create your own custom dependencies by implementing the `tstr.Dependency` interface.

```go
type Custom struct{}

func New() *Custom {
	return &Custom{}
}

func (c *Custom) Start() error { return nil }

func (c *Custom) Ready() error { return nil }

func (c *Custom) Stop() error { return nil }
```
