package git

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/utils"
)

func push(repoPath string, mirrorURL string) error {
	log.Debug(fmt.Sprintf("Pushing to mirror(%s) origin in %s", mirrorURL, getRepoURLFromPath(repoPath)))
	err := utils.RunCmdInPath(repoPath, "git", nil, "remote", "set-url", "--push", "origin", mirrorURL)
	if err != nil {
		return err
	}
	err = utils.RunCmdInPath(repoPath, "git", nil, "push", "--mirror")
	if err != nil {
		return err
	}
	log.Debug(fmt.Sprintf("Pushed to mirror(%s) origin in %s", mirrorURL, getRepoURLFromPath(repoPath)))
	return nil
}
