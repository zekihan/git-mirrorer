package git

import (
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

func getLatest(repo config.GitSource) error {
	if _, err := os.Stat(repo.GetRepoPath()); os.IsNotExist(err) {
		err := clone(repo)
		if err != nil {
			return err
		}
	} else {
		err = fetch(repo.GetRepoPath())
		if err != nil {
			return err
		}
	}
	return nil
}

func HandleMirror(source config.GitSource, mirror config.GitSource) error {
	if source.URL == mirror.URL {
		return nil
	}

	if mirror.AutoCreateConfig.Enabled {
		err := createRepo(mirror)
		if err != nil {
			return err
		}
	}

	err := getLatest(source)
	if err != nil {
		return err
	}
	err = push(source.GetRepoPath(), mirror.URL)
	if err != nil {
		return err
	}
	return nil
}

func createRepo(repo config.GitSource) error {
	auth, ok := repo.AutoCreateConfig.GetAuth()
	if !ok {
		return errors.New("auto create is enabled but no auth is provided")
	}
	switch auth.AuthType {
	case config.AuthTypeGithub:
		return providers.GithubProvider.CreateRepo(repo)
	case config.AuthTypeGitlab:
		return providers.GitlabProvider.CreateRepo(repo)
	case config.AuthTypeNone:
		return errors.New("auto create is enabled but no auth is provided")
	default:
		return errors.New("unknown auth type")
	}
}
