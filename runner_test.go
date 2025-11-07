package tstr_test

import (
	"testing"

	"github.com/go-tstr/tstr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunnerOrder(t *testing.T) {
	const depCount = 3
	startCh := make(chan int, 5)
	readyCh := make(chan int, 5)
	stopCh := make(chan int, 5)

	deps := make([]tstr.Dependency, 0, depCount)
	for i := range depCount {
		deps = append(deps, &MockDep{
			num:     i,
			startCh: startCh,
			readyCh: readyCh,
			stopCh:  stopCh,
		})
	}

	r := tstr.NewRunner(deps...)
	require.NoError(t, r.Start())

	startOrder := make([]int, 0, 3)
	for v := range startCh {
		startOrder = append(startOrder, v)
		if len(startOrder) == depCount {
			close(startCh)
		}
	}
	assert.Equal(t, []int{0, 1, 2}, startOrder, "wrong start order")

	readyOrder := make([]int, 0, 3)
	for v := range readyCh {
		readyOrder = append(readyOrder, v)
		if len(readyOrder) == depCount {
			close(readyCh)
		}
	}
	assert.Equal(t, []int{0, 1, 2}, readyOrder, "wrong ready order")

	require.NoError(t, r.Stop())

	stopOrder := make([]int, 0, 3)
	for v := range stopCh {
		stopOrder = append(stopOrder, v)
		if len(stopOrder) == depCount {
			close(stopCh)
		}
	}
	assert.Equal(t, []int{2, 1, 0}, stopOrder, "wrong stop order")
}

type MockDep struct {
	num     int
	startCh chan int
	readyCh chan int
	stopCh  chan int
}

func (m *MockDep) Start() error {
	m.startCh <- m.num
	return nil
}

func (m *MockDep) Ready() error {
	m.readyCh <- m.num
	return nil
}

func (m *MockDep) Stop() error {
	m.stopCh <- m.num
	return nil
}
