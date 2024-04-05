package git

import (
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/util"
)

func clone(repoPath string, repoURL string) error {
	log.Debug("Cloning repository from ", repoURL, " to ", getRepoURLFromPath(repoPath))
	err := util.RunCmd("git", "clone", "--mirror", repoURL, repoPath)
	if err != nil {
		return err
	}
	return nil
}
