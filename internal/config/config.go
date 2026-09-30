package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Routes []Route `json:"routes"`
}

type Route struct {
	Path   string `json:"path"`
	Script string `json:"script"`
	// Branch limits deploys to pushes on this branch. Empty accepts any branch.
	Branch string `json:"branch,omitempty"`
}

func Load(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("config path is required")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	if len(cfg.Routes) == 0 {
		return nil, fmt.Errorf("routes must not be empty")
	}

	seen := map[string]struct{}{}
	for _, route := range cfg.Routes {
		if route.Path == "" {
			return nil, fmt.Errorf("route path is required")
		}
		if route.Script == "" {
			return nil, fmt.Errorf("route script is required: %s", route.Path)
		}
		if !strings.HasPrefix(route.Path, "/") {
			return nil, fmt.Errorf("route path must start with /: %s", route.Path)
		}
		if !filepath.IsAbs(route.Script) {
			return nil, fmt.Errorf("route script must be absolute path: %s", route.Script)
		}
		if strings.HasPrefix(route.Branch, "refs/") {
			return nil, fmt.Errorf("route branch must not start with refs/ (use the branch name only): %s", route.Path)
		}
		if _, ok := seen[route.Path]; ok {
			return nil, fmt.Errorf("duplicate route path: %s", route.Path)
		}
		seen[route.Path] = struct{}{}
	}

	return &cfg, nil
}
