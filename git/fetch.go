package git

import (
	"context"
	"fmt"

	"github.com/zekihan/go-commons/utils"

	log "github.com/sirupsen/logrus"
)

func fetch(ctx context.Context, repoPath string) error {
	repoURLFromPath := getRepoURLFromPath(repoPath) //nolint:contextcheck // This is a local path

	log.Debug("Fetching repository in ", repoURLFromPath)

	err := utils.RunCmdInPath(ctx, repoPath, "git", nil, "fetch", "-p", "origin")
	if err != nil {
		return fmt.Errorf("failed to fetch repository %s: %w", repoPath, err)
	}

	return nil
}
