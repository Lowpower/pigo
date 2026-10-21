package ext

import "strings"

// Tool exposure values. An empty exposure is direct, which is what extensions
// that do not send the field already do.
const (
	ExposureDirect    = "direct"
	ExposureModelOnly = "model-only"
	ExposureCodeMode  = "codemode"
	ExposureDeferred  = "deferred"
	ExposureHidden    = "hidden"
)

// NormalizeExposure maps missing and unknown values to direct.
func NormalizeExposure(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", ExposureDirect:
		return ExposureDirect
	case ExposureModelOnly:
		return ExposureModelOnly
	case ExposureCodeMode:
		return ExposureCodeMode
	case ExposureDeferred:
		return ExposureDeferred
	case ExposureHidden:
		return ExposureHidden
	default:
		return ExposureDirect
	}
}

// ModelVisible reports whether a tool belongs in the model tool list.
// model-only is visible to the model; codemode and hidden are not.
// deferred is visible only after it has been loaded.
func ModelVisible(exposure string, loaded bool) bool {
	switch NormalizeExposure(exposure) {
	case ExposureDeferred:
		return loaded
	case ExposureCodeMode, ExposureHidden:
		return false
	default:
		return true
	}
}

// Callable reports whether executeTool may run the tool.
// Hidden tools stay invisible to the model and to nested calls.
func Callable(exposure string) bool {
	return NormalizeExposure(exposure) != ExposureHidden
}
