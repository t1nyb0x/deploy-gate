package main

import (
	"fmt"
	"runtime/debug"
)

// Set at build time, e.g. -ldflags "-X main.version=v0.4.0 -X main.commit=abc1234".
// When empty, values are taken from the build info embedded by the Go toolchain.
var (
	version = ""
	commit  = ""
)

const (
	unknown        = "unknown"
	shortRevLength = 12
)

type buildDetails struct {
	Version   string
	Commit    string
	GoVersion string
}

func (d buildDetails) String() string {
	return fmt.Sprintf("deploy-gate version=%s commit=%s go=%s", d.Version, d.Commit, d.GoVersion)
}

func currentBuildInfo() buildDetails {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		info = nil
	}
	return resolveBuildInfo(version, commit, info)
}

func resolveBuildInfo(ldVersion, ldCommit string, info *debug.BuildInfo) buildDetails {
	d := buildDetails{Version: ldVersion, Commit: shortRev(ldCommit), GoVersion: unknown}

	if info != nil {
		d.GoVersion = info.GoVersion
		if d.Version == "" && info.Main.Version != "(devel)" {
			d.Version = info.Main.Version
		}
		if d.Commit == "" {
			d.Commit = vcsRevision(info.Settings)
		}
	}

	if d.Version == "" {
		d.Version = "dev"
	}
	if d.Commit == "" {
		d.Commit = unknown
	}
	return d
}

func vcsRevision(settings []debug.BuildSetting) string {
	var rev string
	var modified bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if rev == "" {
		return ""
	}
	rev = shortRev(rev)
	if modified {
		rev += "-dirty"
	}
	return rev
}

func shortRev(rev string) string {
	if len(rev) > shortRevLength {
		return rev[:shortRevLength]
	}
	return rev
}
