package completion

// ControlResult is an operation that outlived its caller's wait. Done alone
// means the transport returned; Reaped distinguishes a final kernel result
// from an uncertain transport failure. A timeout is not cancellation.
type ControlResult interface {
	error
	Done() <-chan struct{}
	Result() error
	Reaped() bool
}

// ReconcileControl is deliberately nonblocking. Only a reaped final result
// permits a lifecycle transition or a new conflicting command.
func ReconcileControl(p ControlResult) (resolved bool, result error) {
	select {
	case <-p.Done():
		if p.Reaped() {
			return true, p.Result()
		}
	default:
	}
	return false, p
}
