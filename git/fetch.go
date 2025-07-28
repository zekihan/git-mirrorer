package git

import (
	"context"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/go-commons/utils"
)

func fetch(ctx context.Context, repoPath string) error {
	log.Debug("Fetching repository in ", getRepoURLFromPath(repoPath))
	err := utils.RunCmdInPath(ctx, repoPath, "git", nil, "fetch", "-p", "origin")
	if err != nil {
		return err
	}
	return nil
}
