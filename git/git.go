package git

import (
	"context"
	"errors"
	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/providers"
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

	if mirror.AutoCreateConfig.Enabled {
		err := createRepo(ctx, mirror)
		if err != nil {
			return err
		}
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

func createRepo(ctx context.Context, repo config.GitSource) error {
	auth, ok := repo.AutoCreateConfig.GetAuth()
	if !ok {
		return errors.New("auto create is enabled but no auth is provided")
	}
	switch auth.AuthType {
	case config.AuthTypeGithub:
		return providers.GithubProvider.CreateRepo(ctx, repo)
	case config.AuthTypeGitlab:
		return providers.GitlabProvider.CreateRepo(ctx, repo)
	case config.AuthTypeGitea:
		return providers.GiteaProvider.CreateRepo(ctx, repo)
	case config.AuthTypeNone:
		return errors.New("auto create is enabled but no auth is provided")
	default:
		return errors.New("unknown auth type")
	}
}
