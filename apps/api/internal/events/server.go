package events

// ServerStarted: a server process started (docs/api/observability.md#servers-and-versions).
// Server is its server_instances ID (srv_…, new at every start), Version
// its build's (observe.Version). A release shows as starts with a new
// version; a crash loop as starts without stops.
type ServerStarted struct {
	Server  string `json:"server"`
	Version string `json:"version"`
}

func (ServerStarted) EventName() string { return "server.started" }

// ServerStopped: a server stopped without crashing. Reason is ServerSignal
// (it was asked to stop) or ServerFailed (its listener failed, or startup
// failed after server.started). A server that crashed records none.
type ServerStopped struct {
	Server string `json:"server"`
	Reason string `json:"reason"`
}

func (ServerStopped) EventName() string { return "server.stopped" }

// server.stopped's reasons.
const (
	ServerSignal = "signal"
	ServerFailed = "failed"
)
