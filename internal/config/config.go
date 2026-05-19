package config

// Config holds application configuration for me-to-markdown.
type Config struct {
	Verbose bool
	Debug   bool
	LogJSON bool

	// Since is the default --since value used by `export` when no flag
	// is provided. Accepts Go duration (e.g. "168h") or YYYY-MM-DD; the
	// command parses it the same way as the per-tool export contract.
	Since string

	// Include / Exclude let the user pre-select which tools `export`
	// runs. Either may be set but not both. Empty Include means "all
	// registered tools." Each entry matches a Tool.Slug.
	Include []string
	Exclude []string

	// OmitErrors, when true, suppresses the per-tool error section the
	// orchestrator otherwise renders in the combined output. Errors are
	// still logged to stderr and the overall exit code is non-zero.
	OmitErrors bool

	// EnvFile is the path to a KEY=VALUE file whose entries are merged
	// into every subprocess's environment. Empty = no override; falls
	// back to the conventional default ($XDG_CONFIG_HOME/me-to-markdown/env)
	// at load time if that file exists.
	EnvFile string
}
