package config

// Config holds application configuration for me-to-markdown.
//
// Phase 0 carries only the standard logging fields. Phase 2 will extend this
// with orchestrator-specific options (include/exclude defaults, default
// --since window, omit_errors, managed-bin-dir override).
type Config struct {
	Verbose bool
	Debug   bool
	LogJSON bool
}
