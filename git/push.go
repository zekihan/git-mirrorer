package git

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/go-commons/utils"
)

func push(ctx context.Context, repoPath string, mirrorURL string) error {
	log.Debug(fmt.Sprintf("Pushing to mirror(%s) origin in %s", mirrorURL, getRepoURLFromPath(repoPath)))
	err := utils.RunCmdInPath(ctx, repoPath, "git", nil, "remote", "set-url", "--push", "origin", mirrorURL)
	if err != nil {
		return err
	}
	err = utils.RunCmdInPath(ctx, repoPath, "git", nil, "push", "--mirror")
	if err != nil {
		return err
	}
	log.Debug(fmt.Sprintf("Pushed to mirror(%s) origin in %s", mirrorURL, getRepoURLFromPath(repoPath)))
	return nil
}
