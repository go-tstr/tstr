package depfn

type DepFn struct {
	start func() error
	ready func() error
	stop  func() error
}

// New creates a new DepFn with the provided start, ready, and stop functions.
// If any of the functions is nil, it will be treated as a no-op and nil is returned when they are called.
// This provides a simple way to create dependencies without needing to implement the full Dependency interface.
func New(start, ready, stop func() error) DepFn {
	return DepFn{
		start: start,
		ready: ready,
		stop:  stop,
	}
}

func (f DepFn) Start() error {
	if f.start == nil {
		return nil
	}
	return f.start()
}

func (f DepFn) Ready() error {
	if f.ready == nil {
		return nil
	}
	return f.ready()
}

func (f DepFn) Stop() error {
	if f.stop == nil {
		return nil
	}
	return f.stop()
}
