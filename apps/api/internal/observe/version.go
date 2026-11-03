package observe

import (
	"runtime/debug"
	"sync"
)

// Version is this build's release: the first 12 characters of the VCS
// revision Go recorded when it was built in a checkout (with "-dirty" for
// uncommitted changes), or "dev" without one (go run, go test, a build from
// an exported tree). It is in ValidRequestID's shape. Grouped errors store
// it as their last_version.
func Version() string {
	versionOnce.Do(func() { version = readVersion() })
	return version
}

var (
	versionOnce sync.Once
	version     string
)

func readVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	revision, modified := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" || !ValidRequestID(revision) {
		return "dev"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		revision += "-dirty"
	}
	return revision
}
