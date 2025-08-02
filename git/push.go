package git

import (
	"context"
	"fmt"

	"github.com/zekihan/go-commons/utils"

	log "github.com/sirupsen/logrus"
)

func push(ctx context.Context, repoPath, mirrorURL string) error {
	repoURLFromPath := getRepoURLFromPath(repoPath) //nolint:contextcheck // This is a local path
	log.Debug(fmt.Sprintf("Pushing to mirror(%s) origin in %s", mirrorURL, repoURLFromPath))

	err := utils.RunCmdInPath(
		ctx,
		repoPath,
		"git",
		nil,
		"remote",
		"set-url",
		"--push",
		"origin",
		mirrorURL,
	)
	if err != nil {
		return fmt.Errorf("failed to set push URL for origin in %s: %w", repoPath, err)
	}

	err = utils.RunCmdInPath(ctx, repoPath, "git", nil, "push", "--mirror")
	if err != nil {
		return fmt.Errorf("failed to push to mirror %s: %w", mirrorURL, err)
	}

	log.Debug(fmt.Sprintf("Pushed to mirror(%s) origin in %s", mirrorURL, repoURLFromPath))

	return nil
}
