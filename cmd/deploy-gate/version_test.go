package main

import (
	"runtime/debug"
	"testing"
)

func buildInfo(mainVersion string, settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{
		GoVersion: "go1.27.3",
		Main:      debug.Module{Path: "github.com/t1nyb0x/deploy-gate", Version: mainVersion},
		Settings:  settings,
	}
}

func TestResolveBuildInfo(t *testing.T) {
	const fullRev = "0123456789abcdef0123456789abcdef01234567"

	tests := []struct {
		name      string
		ldVersion string
		ldCommit  string
		info      *debug.BuildInfo
		want      buildDetails
	}{
		{
			name:      "ldflags take precedence",
			ldVersion: "v0.4.0",
			ldCommit:  "abc1234",
			info:      buildInfo("v0.3.0", debug.BuildSetting{Key: "vcs.revision", Value: fullRev}),
			want:      buildDetails{Version: "v0.4.0", Commit: "abc1234", GoVersion: "go1.27.3"},
		},
		{
			name:      "full commit from ldflags is shortened",
			ldVersion: "v0.4.0",
			ldCommit:  fullRev,
			info:      buildInfo("(devel)"),
			want:      buildDetails{Version: "v0.4.0", Commit: "0123456789ab", GoVersion: "go1.27.3"},
		},
		{
			name: "module version from go install",
			info: buildInfo("v0.3.0"),
			want: buildDetails{Version: "v0.3.0", Commit: "unknown", GoVersion: "go1.27.3"},
		},
		{
			name: "local build falls back to dev and vcs revision",
			info: buildInfo("(devel)", debug.BuildSetting{Key: "vcs.revision", Value: fullRev}),
			want: buildDetails{Version: "dev", Commit: "0123456789ab", GoVersion: "go1.27.3"},
		},
		{
			name: "uncommitted changes are marked dirty",
			info: buildInfo("(devel)",
				debug.BuildSetting{Key: "vcs.revision", Value: fullRev},
				debug.BuildSetting{Key: "vcs.modified", Value: "true"},
			),
			want: buildDetails{Version: "dev", Commit: "0123456789ab-dirty", GoVersion: "go1.27.3"},
		},
		{
			name: "short revision is kept as is",
			info: buildInfo("", debug.BuildSetting{Key: "vcs.revision", Value: "abc"}),
			want: buildDetails{Version: "dev", Commit: "abc", GoVersion: "go1.27.3"},
		},
		{
			name:      "no build info",
			ldVersion: "v0.4.0",
			info:      nil,
			want:      buildDetails{Version: "v0.4.0", Commit: "unknown", GoVersion: "unknown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveBuildInfo(tt.ldVersion, tt.ldCommit, tt.info)
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBuildDetailsString(t *testing.T) {
	d := buildDetails{Version: "v0.4.0", Commit: "abc1234", GoVersion: "go1.27.3"}
	want := "deploy-gate version=v0.4.0 commit=abc1234 go=go1.27.3"
	if got := d.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
