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
	Short: "Download release binaries for one or more registered tools",
	Long: `Download pre-built release binaries from each tool's GitHub releases
into the managed binary directory ($XDG_DATA_HOME/me-to-markdown/bin).

With no arguments, installs every registered tool. With one or more
slugs (see `+"`me-to-markdown list`"+`), installs only those.

By default each tool is fetched at its own latest tagged release. Pass
--version to pin a specific tag (applied to every tool installed in this
invocation, so prefer installing one at a time when pinning).`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().String("version", "latest", "release tag to install (default: each tool's latest)")
}

func runInstall(cmd *cobra.Command, args []string) error {
	log := GetLogger()
	versionFlag, _ := cmd.Flags().GetString("version")

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
