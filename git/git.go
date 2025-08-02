package git

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/zekihan/git-mirrorer/config"
)

func getRepoURLFromPath(repoPath string) string {
	unescape, err := url.PathUnescape(repoPath)
	if err != nil {
		return ""
	}

	partNumber := 2

	splitN := strings.SplitN(unescape, config.GetConfig().RepoPath, partNumber)
	if len(splitN) < partNumber {
		return ""
	}

	return splitN[1]
}

func getLatest(ctx context.Context, repo config.GitSource) error {
	repoPath := repo.GetRepoPath() //nolint:contextcheck // This is a local path, so context is not needed.

	_, err := os.Stat(repoPath)
	if os.IsNotExist(err) {
		err = clone(ctx, repo)
		if err != nil {
			return fmt.Errorf("failed to clone repository %s: %w", repo.URL, err)
		}
	} else {
		err = fetch(ctx, repoPath)
		if err != nil {
			return fmt.Errorf("failed to fetch repository %s: %w", repo.URL, err)
		}
	}

	return nil
}

func HandleMirror(ctx context.Context, source, mirror config.GitSource) error {
	if source.URL == mirror.URL {
		return nil
	}

	err := getLatest(ctx, source)
	if err != nil {
		return fmt.Errorf("failed to get latest from source %s: %w", source.URL, err)
	}

	repoPath := source.GetRepoPath() //nolint:contextcheck // This is a local path, so context is not needed.

	err = push(ctx, repoPath, mirror.URL)
	if err != nil {
		return fmt.Errorf("failed to push to mirror %s: %w", mirror.URL, err)
	}

	return nil
}
