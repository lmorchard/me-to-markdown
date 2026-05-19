package envfile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadEmptyPath(t *testing.T) {
	got, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil entries for empty path, got %v", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if got != nil {
		t.Fatalf("missing file should yield nil entries, got %v", got)
	}
}

func TestLoadParsesEntries(t *testing.T) {
	path := writeEnv(t, `# comment
MASTODON_SERVER=https://mastodon.social
MASTODON_ACCESS_TOKEN=token-with-=-and-#-inside

  GITHUB_TOKEN=ghp_xxx
`)
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	want := []string{
		"MASTODON_SERVER=https://mastodon.social",
		"MASTODON_ACCESS_TOKEN=token-with-=-and-#-inside",
		"GITHUB_TOKEN=ghp_xxx",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestLoadRejectsMalformedLines(t *testing.T) {
	cases := map[string]string{
		"no equals":  "FOO\n",
		"empty key":  "=value\n",
		"only equal": "=\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeEnv(t, body)
			_, err := Load(path)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", body)
			}
		})
	}
}

func writeEnv(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write tmp env: %v", err)
	}
	return path
}
