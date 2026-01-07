package git

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zekihan/go-commons/utils"
)

func fetch(ctx context.Context, repoPath string) error {
	repoURLFromPath := getRepoURLFromPath(repoPath) //nolint:contextcheck // This is a local path

	slog.DebugContext(ctx, "Fetching repository", slog.String("repoUrlFromPath", repoURLFromPath))

	err := utils.RunCmdInPath(ctx, repoPath, "git", nil, "fetch", "-p", "origin")
	if err != nil {
		return fmt.Errorf("failed to fetch repository %s: %w", repoPath, err)
	}

	return nil
}
