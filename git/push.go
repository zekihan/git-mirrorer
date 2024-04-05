package git

import (
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/util"
)

func push(repoPath string, mirrorURL string) error {
	log.Debug("Pushing to mirror origin in ", getRepoURLFromPath(repoPath))
	err := util.RunCmdInPath(repoPath, "git", "remote", "set-url", "--push", "origin", mirrorURL)
	if err != nil {
		return err
	}
	err = util.RunCmdInPath(repoPath, "git", "push", "--mirror")
	if err != nil {
		return err
	}
	return nil
}
