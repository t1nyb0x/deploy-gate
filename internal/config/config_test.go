package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantErr    string
		wantBranch string
	}{
		{
			name:    "valid route without branch",
			content: `{"routes":[{"path":"/deploy/a","script":"/scripts/a.sh"}]}`,
		},
		{
			name:       "valid route with branch",
			content:    `{"routes":[{"path":"/deploy/a","script":"/scripts/a.sh","branch":"main"}]}`,
			wantBranch: "main",
		},
		{
			name:    "branch must not include refs prefix",
			content: `{"routes":[{"path":"/deploy/a","script":"/scripts/a.sh","branch":"refs/heads/main"}]}`,
			wantErr: "branch must not start with refs/",
		},
		{
			name:    "empty routes",
			content: `{"routes":[]}`,
			wantErr: "routes must not be empty",
		},
		{
			name:    "missing path",
			content: `{"routes":[{"script":"/scripts/a.sh"}]}`,
			wantErr: "route path is required",
		},
		{
			name:    "missing script",
			content: `{"routes":[{"path":"/deploy/a"}]}`,
			wantErr: "route script is required",
		},
		{
			name:    "path without leading slash",
			content: `{"routes":[{"path":"deploy/a","script":"/scripts/a.sh"}]}`,
			wantErr: "route path must start with /",
		},
		{
			name:    "relative script",
			content: `{"routes":[{"path":"/deploy/a","script":"scripts/a.sh"}]}`,
			wantErr: "route script must be absolute path",
		},
		{
			name:    "duplicate path",
			content: `{"routes":[{"path":"/deploy/a","script":"/a.sh"},{"path":"/deploy/a","script":"/b.sh"}]}`,
			wantErr: "duplicate route path",
		},
		{
			name:    "malformed json",
			content: `{`,
			wantErr: "decode config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, tt.content))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := cfg.Routes[0].Branch; got != tt.wantBranch {
				t.Errorf("branch = %q, want %q", got, tt.wantBranch)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadEmptyPath(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}
