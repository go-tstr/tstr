package container_test

import (
	"context"
	"testing"

	"github.com/go-tstr/tstr/dep/container"
	"github.com/go-tstr/tstr/dep/deptest"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestContainer(t *testing.T) {
	tests := []struct {
		name      string
		container *container.Container
		err       error
	}{
		{
			name: "WithModule_error",
			container: container.New(
				container.WithModule(minio.Run, "minio/minio:non-existing-tag"),
			),
			err: container.ErrCreateWithModule,
		},
		{
			name: "WithModule_minio",
			container: container.New(
				container.WithModule(minio.Run, "minio/minio:RELEASE.2024-01-16T16-07-38Z"),
				container.WithReadyFn(func(c testcontainers.Container) error {
					_, err := c.ContainerIP(context.Background())
					return err
				}),
			),
		},
		{
			name: "WithModule_postgres",
			container: container.New(
				container.WithModule(postgres.Run, "postgres:16-alpine",
					postgres.WithDatabase("test"),
					postgres.WithUsername("user"),
					postgres.WithPassword("password"),
				),
			),
		},
		{
			name: "WithGenericContainer",
			container: container.New(
				container.WithGenericContainer(
					testcontainers.GenericContainerRequest{
						ContainerRequest: testcontainers.ContainerRequest{
							Image: "postgres:16-alpine",
							Env: map[string]string{
								"POSTGRES_USER":     "root",
								"POSTGRES_PASSWORD": "root",
								"POSTGRES_DB":       "root",
							},
							ExposedPorts: []string{"5432/tcp"},
							Cmd:          []string{"postgres", "-c", "fsync=off"},
						},
						Started: true,
					},
				),
			),
		},
		{
			name: "WithGenericContainer_error",
			container: container.New(
				container.WithGenericContainer(testcontainers.GenericContainerRequest{}),
			),
			err: container.ErrCreateWithGenericContainer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deptest.ErrorIs(t, tt.container, nil, tt.err)
		})
	}
}
