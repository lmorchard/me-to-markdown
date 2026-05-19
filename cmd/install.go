package cmd

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/registry"
	"github.com/lmorchard/me-to-markdown/internal/runner"
	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install [tool-slug ...]",
	Short: "Download (or build) binaries for one or more registered tools",
	Long: `Install registered tools into the managed binary directory
($XDG_DATA_HOME/me-to-markdown/bin).

Default mode downloads pre-built release tarballs from each tool's
GitHub releases and verifies SHA-256 against checksums.txt.

With --from-source, the tools are built locally from sibling
repository checkouts via ` + "`go build`" + ` — useful for dev iteration
without waiting for CI / releases. The default source root is the
parent directory of the running me-to-markdown binary's source tree
(auto-detected); pass an explicit path to override.

With no positional args, installs every registered tool. With one or
more slugs (see ` + "`me-to-markdown list`" + `), installs only those.

By default each tool is fetched at its own latest tagged release. Pass
--version to pin a specific tag (applied to every tool installed in
this invocation, so prefer installing one at a time when pinning).
--version is mutually exclusive with --from-source.`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().String("version", "latest", "release tag to install (default: each tool's latest)")
	installCmd.Flags().String("from-source", "", "build from sibling repos under DIR instead of fetching releases (DIR defaults to the auto-detected sibling layout)")
	installCmd.Flag("from-source").NoOptDefVal = "auto" // bare --from-source means "auto-detect"
}

func runInstall(cmd *cobra.Command, args []string) error {
	log := GetLogger()
	versionFlag, _ := cmd.Flags().GetString("version")
	fromSourceFlag, _ := cmd.Flags().GetString("from-source")
	fromSourceSet := cmd.Flags().Changed("from-source")
	versionSet := cmd.Flags().Changed("version")

	if fromSourceSet && versionSet {
		return errors.New("--from-source and --version are mutually exclusive")
	}

	tools, err := selectInstallTargets(args)
	if err != nil {
		return err
	}

	binDir := runner.ManagedBinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", binDir, err)
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
	defer cancel()

	if fromSourceSet {
		sourceRoot, err := resolveSourceRoot(fromSourceFlag)
		if err != nil {
			return err
		}
		log.Infof("installing from source root: %s", sourceRoot)
		var firstErr error
		for _, t := range tools {
			if err := installFromSource(ctx, log, t, sourceRoot, binDir); err != nil {
				log.Errorf("%s: %v", t.Slug, err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		return firstErr
	}

	client := &http.Client{Timeout: 5 * time.Minute}

	var firstErr error
	for _, t := range tools {
		tag := versionFlag
		if tag == "" || tag == "latest" {
			tag, err = latestTag(ctx, client, t.Repo)
			if err != nil {
				log.Errorf("%s: resolve latest tag: %v", t.Slug, err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
		}
		log.Infof("%s: installing %s", t.Slug, tag)
		if err := installOne(ctx, log, client, t, tag, binDir); err != nil {
			log.Errorf("%s: %v", t.Slug, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}

	return firstErr
}

// resolveSourceRoot returns the directory that contains each
// `<binary>/` sibling repo. With value == "auto" (set by bare
// --from-source), it auto-detects by walking up from the running
// binary's path looking for a directory whose immediate children
// include at least one registered tool's binary name. With an
// explicit path, returns that path after a sanity check that it
// contains the expected sibling layout.
func resolveSourceRoot(value string) (string, error) {
	if value != "auto" {
		abs, err := filepath.Abs(value)
		if err != nil {
			return "", fmt.Errorf("resolve --from-source path: %w", err)
		}
		if _, err := sniffSiblingLayout(abs); err != nil {
			return "", fmt.Errorf("%s: %w", abs, err)
		}
		return abs, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate running binary for auto-detect: %w", err)
	}
	// Walk upward from the binary's directory looking for a parent that
	// contains a sibling-layout. Stop at the filesystem root.
	dir, _ := filepath.EvalSymlinks(filepath.Dir(exe))
	for {
		// Probe the directory itself and its parent (the binary may live
		// at <root>/me-to-markdown or at <root>/me-to-markdown/.worktrees/<branch>).
		if _, err := sniffSiblingLayout(dir); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not auto-detect sibling-repo layout; pass --from-source <dir> explicitly")
		}
		dir = parent
	}
}

// sniffSiblingLayout returns nil if dir contains at least one
// `<tool.Binary>/` subdirectory that looks like a Go module
// (has a go.mod).
func sniffSiblingLayout(dir string) (string, error) {
	for _, t := range registry.Tools {
		candidate := filepath.Join(dir, t.Binary)
		if hasGoMod(candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("no sibling-repo with go.mod found here")
}

func hasGoMod(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil && !info.IsDir()
}

// installFromSource builds a single tool from its sibling-repo
// checkout and copies the binary into the managed bin dir. Streams
// `go build` output through the orchestrator's stderr so build errors
// are visible.
func installFromSource(ctx context.Context, log loggerLike, t registry.Tool, sourceRoot, binDir string) error {
	sourceDir := filepath.Join(sourceRoot, t.Binary)
	if !hasGoMod(sourceDir) {
		return fmt.Errorf("source dir %s missing go.mod", sourceDir)
	}

	target := filepath.Join(binDir, t.Binary)
	log.Infof("%s: building %s -> %s", t.Slug, sourceDir, target)

	build := exec.CommandContext(ctx, "go", "build", "-o", target, ".")
	build.Dir = sourceDir
	build.Stderr = os.Stderr
	build.Stdout = os.Stderr // go build is normally quiet; route any noise to stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build in %s: %w", sourceDir, err)
	}
	return nil
}

func selectInstallTargets(args []string) ([]registry.Tool, error) {
	if len(args) == 0 {
		return registry.Tools, nil
	}
	out := make([]registry.Tool, 0, len(args))
	for _, a := range args {
		t, ok := registry.BySlug(strings.TrimSpace(a))
		if !ok {
			return nil, fmt.Errorf("unknown tool slug %q (run `me-to-markdown list`)", a)
		}
		out = append(out, t)
	}
	return out, nil
}

// latestTag queries the GitHub releases API for the latest non-prerelease
// tag on the given repo.
func latestTag(ctx context.Context, client *http.Client, repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API %s: %s — %s", url, resp.Status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode releases response: %w", err)
	}
	if payload.TagName == "" {
		return "", errors.New("no tag_name in releases response")
	}
	return payload.TagName, nil
}

// installOne downloads, verifies, and extracts the release binary for one
// tool at the given tag.
func installOne(ctx context.Context, log loggerLike, client *http.Client, t registry.Tool, tag, binDir string) error {
	asset := t.AssetName(runtime.GOOS, runtime.GOARCH)
	baseURL := fmt.Sprintf("https://github.com/%s/releases/download/%s", t.Repo, tag)

	// Download checksums.txt for this release.
	checksums, err := fetchChecksums(ctx, client, baseURL+"/checksums.txt")
	if err != nil {
		return fmt.Errorf("checksums: %w", err)
	}
	expected, ok := checksums[asset]
	if !ok {
		return fmt.Errorf("no checksum entry for %s in checksums.txt", asset)
	}

	// Download the asset.
	log.Debugf("%s: downloading %s/%s", t.Slug, baseURL, asset)
	assetBytes, err := fetchURL(ctx, client, baseURL+"/"+asset)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}

	// Verify SHA-256.
	sum := sha256.Sum256(assetBytes)
	got := hex.EncodeToString(sum[:])
	if got != expected {
		return fmt.Errorf("checksum mismatch for %s: got %s, expected %s", asset, got, expected)
	}

	// Extract the binary.
	binaryName := t.Binary
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binData, err := extractBinary(assetBytes, asset, binaryName)
	if err != nil {
		return fmt.Errorf("extract %s from %s: %w", binaryName, asset, err)
	}

	// Write into the managed bin dir.
	target := filepath.Join(binDir, binaryName)
	if err := os.WriteFile(target, binData, 0o755); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	log.Infof("%s: installed %s -> %s", t.Slug, tag, target)
	return nil
}

// fetchChecksums downloads a checksums.txt file and returns a map of
// basename → sha256 hex digest. The expected line format is the output of
// `sha256sum`: `<hex>  <path>`, where the path may include directory
// components (the release workflow runs `find . -name ... -exec sha256sum`).
func fetchChecksums(ctx context.Context, client *http.Client, url string) (map[string]string, error) {
	body, err := fetchURL(ctx, client, url)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		base := path.Base(fields[len(fields)-1])
		out[base] = hash
	}
	if len(out) == 0 {
		return nil, errors.New("checksums.txt was empty or unparseable")
	}
	return out, nil
}

func fetchURL(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// extractBinary pulls the named binary out of a release archive
// (`.tar.gz` or `.zip`).
func extractBinary(archiveBytes []byte, assetName, binaryName string) ([]byte, error) {
	switch {
	case strings.HasSuffix(assetName, ".tar.gz"):
		return extractTarGz(archiveBytes, binaryName)
	case strings.HasSuffix(assetName, ".zip"):
		return extractZip(archiveBytes, binaryName)
	default:
		return nil, fmt.Errorf("unsupported archive format: %s", assetName)
	}
}

func extractTarGz(data []byte, binaryName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if path.Base(hdr.Name) == binaryName {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", binaryName)
}

func extractZip(data []byte, binaryName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("zip: %w", err)
	}
	for _, f := range zr.File {
		if path.Base(f.Name) == binaryName {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open zip entry: %w", err)
			}
			defer func() { _ = rc.Close() }()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", binaryName)
}
