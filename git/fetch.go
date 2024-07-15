package git

import (
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/util"
)

func fetch(repoPath string) error {
	log.Debug("Fetching repository in ", getRepoURLFromPath(repoPath))
	err := util.RunCmdInPath(repoPath, "git", nil, "fetch", "-p", "origin")
	if err != nil {
		return err
	}
	return nil
}
