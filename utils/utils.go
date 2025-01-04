package utils

import (
	"fmt"
)

var (
	Version      = "dev"
	CommitHash   = "none"
	BuildDate    = "I don't remember exactly"
	GitTreeState = "not a git repo"
)

func GetStartupInfo() string {
	dirty := ""
	if GitTreeState == "dirty" {
		dirty = " (dirty)"
	}
	return fmt.Sprintf("Version: %s, Build Date: %s, CommitHash: %s%s", Version, BuildDate, CommitHash, dirty)
}
