package git

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/zekihan/go-commons/utils"
)

func push(ctx context.Context, repoPath, mirrorURL string) error {
	repoURLFromPath := getRepoURLFromPath(repoPath) //nolint:contextcheck // This is a local path
	slog.DebugContext(
		ctx,
		"Setting push URL for mirror",
		slog.String("mirrorUrl", mirrorURL),
		slog.String("repoUrlFromPath", repoURLFromPath),
	)

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

	slog.DebugContext(
		ctx,
		"Successfully pushed to mirror",
		slog.String("mirrorUrl", mirrorURL),
		slog.String("repoUrlFromPath", repoURLFromPath),
	)

	return nil
}
