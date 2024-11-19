package compose_test

import (
	"context"
	"os"
	"testing"

	"github.com/go-tstr/tstr/dep/compose"
	"github.com/go-tstr/tstr/dep/deptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestCompose(t *testing.T) {
	file := prepareFile(t)
	c, err := tc.NewDockerCompose(file)
	require.NoError(t, err)

	tests := []struct {
		name    string
		fn      func() error
		compose *compose.Compose
		err     error
	}{
		{
			name: "WithFile",
			compose: compose.New(
				compose.WithFile(file),
				compose.WithUpOptions(tc.Wait(true)),
				compose.WithDownOptions(tc.RemoveVolumes(true)),
				compose.WithEnv(map[string]string{"DB_PORT": "5432"}),
				compose.WithWaitForService("postgres", wait.ForLog("ready to accept connections")),
			),
		},
		{
			name: "WithStack",
			compose: compose.New(
				compose.WithStack(c),
				compose.WithUpOptions(tc.Wait(true)),
				compose.WithDownOptions(tc.RemoveVolumes(true)),
				compose.WithOsEnv(),
				compose.WithReadyFn(func(stack tc.ComposeStack) error {
					pc, err := stack.ServiceContainer(context.Background(), "postgres")
					if err != nil {
						return err
					}
					_, _, err = pc.Exec(context.Background(), []string{"psql", "-U", "root", "-d", "root", "-c", "SELECT 1"})
					return err
				}),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deptest.ErrorIs(t, tt.compose, tt.fn, tt.err)
		})
	}
}

const composeFile = `
services:
  postgres:
    image: postgres:16-alpine
    restart: always
    environment:
      - POSTGRES_USER=root
      - POSTGRES_PASSWORD=root
      - POSTGRES_DB=root
    ports:
      - "${DB_PORT:-5432}:5432"
    command: ["postgres", "-c", "log_statement=all"]
    healthcheck:
      test: ["CMD-SHELL", "sh -c 'pg_isready -U health -d health'"]
      interval: 1s
      timeout: 10s
      retries: 10`

func prepareFile(t *testing.T) string {
	dir, err := os.MkdirTemp("", "compose-test_")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(dir)) })

	file := dir + "/docker-compose.yaml"
	require.NoError(t, os.WriteFile(file, []byte(composeFile), 0o644))
	return file
}
