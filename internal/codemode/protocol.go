package codemode

// workerEnv marks the re-exec'd child. Parents do not set it.
const workerEnv = "PIGO_CODEMODE_WORKER"

// wire is one JSON message on the child's stdin or stdout.
type wire struct {
	Op    string   `json:"op"`
	Code  string   `json:"code,omitempty"`
	Hosts []string `json:"hosts,omitempty"`
	ID    int      `json:"id"`
	Name  string   `json:"name,omitempty"`
	Args  []any    `json:"args,omitempty"`
	OK    bool     `json:"ok"`
	Value string   `json:"value,omitempty"`
	Error string   `json:"error,omitempty"`
}
