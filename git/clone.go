package git

import (
	"context"
	"fmt"

	"github.com/zekihan/go-commons/utils"

	log "github.com/sirupsen/logrus"

	"github.com/zekihan/git-mirrorer/config"
)

func clone(ctx context.Context, repo config.GitSource) error {
	repoPath := repo.GetRepoPath() //nolint:contextcheck // This is a local path, so context is not needed.

	log.Debug("Cloning repository from ", repo.URL, " to ", repoPath)

	err := utils.RunCmd(ctx, "git", nil, "clone", "--mirror", repo.URL, repoPath)
	if err != nil {
		return fmt.Errorf("failed to clone repository %s: %w", repo.URL, err)
	}

	return nil
}
