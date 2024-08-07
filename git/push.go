package git

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/util"
)

func push(repoPath string, mirrorURL string) error {
	log.Debug(fmt.Sprintf("Pushing to mirror(%s) origin in %s", mirrorURL, getRepoURLFromPath(repoPath)))
	err := util.RunCmdInPath(repoPath, "git", nil, "remote", "set-url", "--push", "origin", mirrorURL)
	if err != nil {
		return err
	}
	err = util.RunCmdInPath(repoPath, "git", nil, "push", "--mirror")
	if err != nil {
		return err
	}
	return nil
}
