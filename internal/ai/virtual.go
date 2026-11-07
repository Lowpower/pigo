package ai

// VirtualModelAPI marks an assistant message that names a virtual model.
// Failed routing leaves this API on the error reply so resume does not treat
// it as a physical dispatch.
const VirtualModelAPI = "pi-virtual"

// VirtualRouteError is a routing failure for a selected virtual model.
// The agent records Provider and Model on the error reply.
type VirtualRouteError struct {
	Provider string
	Model    string
	Err      error
}

func (e *VirtualRouteError) Error() string {
	if e == nil || e.Err == nil {
		return "virtual route failed"
	}
	return e.Err.Error()
}

func (e *VirtualRouteError) Unwrap() error { return e.Err }
