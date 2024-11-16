package tstr

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunMain(t *testing.T) {
	tests := []struct {
		name         string
		opts         []Opt
		m            TestingM
		expectedCode int
	}{
		{
			name:         "success",
			opts:         []Opt{},
			m:            MockTestingM(0),
			expectedCode: 0,
		},
		{
			name:         "run failure",
			opts:         []Opt{},
			m:            MockTestingM(1),
			expectedCode: 1,
		},
		{
			name:         "init failure",
			opts:         []Opt{func(t *Tester) error { return errors.New("testing") }},
			m:            MockTestingM(0),
			expectedCode: 1,
		},
		{
			name: "start failure",
			opts: []Opt{WithDeps(&RunnableFn{
				start: func() error { return errors.New("testing") },
				ready: func() error { return nil },
				stop:  func() error { return nil },
			})},
			m:            MockTestingM(0),
			expectedCode: 1,
		},
		{
			name: "ready failure",
			opts: []Opt{WithDeps(&RunnableFn{
				start: func() error { return nil },
				ready: func() error { return errors.New("testing") },
				stop:  func() error { return nil },
			})},
			m:            MockTestingM(0),
			expectedCode: 1,
		},
		{
			name: "stop failure",
			opts: []Opt{WithDeps(&RunnableFn{
				start: func() error { return nil },
				ready: func() error { return nil },
				stop:  func() error { return errors.New("testing") },
			})},
			m:            MockTestingM(0),
			expectedCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotCode int
			exit = func(code int) { gotCode = code }
			RunMain(tt.m, tt.opts...)
			assert.Equal(t, tt.expectedCode, gotCode)
		})
	}
}

type MockTestingM int

func (m MockTestingM) Run() int { return int(m) }
