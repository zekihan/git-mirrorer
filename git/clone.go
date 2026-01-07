package git

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zekihan/go-commons/utils"

	"github.com/zekihan/git-mirrorer/config"
)

func clone(ctx context.Context, repo config.GitSource) error {
	repoPath := repo.GetRepoPath() //nolint:contextcheck // This is a local path, so context is not needed.

	slog.DebugContext(
		ctx,
		"Cloning repository",
		slog.String("url", repo.URL),
		slog.String("path", repoPath),
	)

	err := utils.RunCmd(ctx, "git", nil, "clone", "--mirror", repo.URL, repoPath)
	if err != nil {
		return fmt.Errorf("failed to clone repository %s: %w", repo.URL, err)
	}

	return nil
}
