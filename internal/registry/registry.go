// Package registry holds the static list of tools the orchestrator knows
// about. Adding a new *-to-markdown tool is three lines here plus a rebuild.
//
// Each tool conforms to the canonical `export --since/--until/-o` contract;
// see docs/dev-sessions/.../spec.md for the contract definition.
package registry

import "fmt"

// Tool describes one *-to-markdown source the orchestrator can invoke.
type Tool struct {
	// Binary is the executable name on disk (e.g. "mastodon-to-markdown").
	Binary string

	// Slug is the short user-facing identifier used in --include/--exclude
	// (e.g. "mastodon"). By convention it's Binary minus the
	// "-to-markdown" suffix.
	Slug string

	// Label is the human-friendly label used in section headers
	// (e.g. "## Mastodon").
	Label string

	// Repo is the GitHub "owner/name" used by `install` to download
	// release assets (e.g. "lmorchard/mastodon-to-markdown").
	Repo string
}

// Tools is the registered set of *-to-markdown tools, in the order they
// appear in concatenated output and in `list` output.
var Tools = []Tool{
	{Binary: "mastodon-to-markdown", Slug: "mastodon", Label: "Mastodon", Repo: "lmorchard/mastodon-to-markdown"},
	{Binary: "linkding-to-markdown", Slug: "linkding", Label: "Linkding", Repo: "lmorchard/linkding-to-markdown"},
	{Binary: "github-to-markdown", Slug: "github", Label: "GitHub", Repo: "lmorchard/github-to-markdown"},
	{Binary: "spotify-to-markdown", Slug: "spotify", Label: "Spotify", Repo: "lmorchard/spotify-to-markdown"},
	{Binary: "pocketcasts-to-markdown", Slug: "pocketcasts", Label: "Pocket Casts", Repo: "lmorchard/pocketcasts-to-markdown"},
}

// BySlug returns the tool whose Slug matches s, or false if none does.
func BySlug(s string) (Tool, bool) {
	for _, t := range Tools {
		if t.Slug == s {
			return t, true
		}
	}
	return Tool{}, false
}

// AssetName returns the conventional release asset filename for the given
// goos/goarch pair, matching the workflow used by every *-to-markdown tool
// (`{binary}-{goos}-{goarch}.tar.gz`, or `.zip` on Windows).
func (t Tool) AssetName(goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("%s-%s-%s.%s", t.Binary, goos, goarch, ext)
}
