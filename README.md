# me-to-markdown

Orchestrate the family of `*-to-markdown` tools into a single combined Markdown
report over a shared time window.

> **Status:** Bootstrap / Phase 0 — repo scaffold in place, no functional
> commands yet beyond `version`. See `docs/dev-sessions/` for the current
> design and plan.

## What this will be

`me-to-markdown` runs the configured set of `*-to-markdown` tools in parallel
over a single `--since/--until` window and concatenates their output into one
Markdown document. Useful for weeknotes, journals, and other periodic
summaries assembled from multiple personal data sources.

The orchestrator deliberately stays thin: each underlying tool keeps its own
config, state, and authentication. `me-to-markdown` is a coordinator, not an
abstraction layer.

## Development

```sh
make setup     # install gofumpt + golangci-lint
make build     # build ./me-to-markdown
make format    # go fmt + gofumpt
make lint      # golangci-lint
make test      # go test ./...
make clean     # remove binary
```

## License

MIT — see `LICENSE`.
