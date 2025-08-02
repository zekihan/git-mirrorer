package git

import (
	"context"
	"github.com/zekihan/git-mirrorer/config"
	"net/url"
	"os"
	"strings"
)

func getRepoURLFromPath(repoPath string) string {
	unescape, err := url.PathUnescape(repoPath)
	if err != nil {
		return ""
	}
	splitN := strings.SplitN(unescape, config.GetConfig().RepoPath, 2)
	if len(splitN) < 2 {
		return ""
	}
	return splitN[1]
}

func getLatest(ctx context.Context, repo config.GitSource) error {
	if _, err := os.Stat(repo.GetRepoPath()); os.IsNotExist(err) {
		err := clone(ctx, repo)
		if err != nil {
			return err
		}
	} else {
		err = fetch(ctx, repo.GetRepoPath())
		if err != nil {
			return err
		}
	}
	return nil
}

func HandleMirror(ctx context.Context, source config.GitSource, mirror config.GitSource) error {
	if source.URL == mirror.URL {
		return nil
	}

	err := getLatest(ctx, source)
	if err != nil {
		return err
	}
	err = push(ctx, source.GetRepoPath(), mirror.URL)
	if err != nil {
		return err
	}
	return nil
}
