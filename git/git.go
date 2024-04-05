package git

import (
	"github.com/zekihan/git-mirrorer/config"
	"net/url"
	"os"
	"path"
	"strings"
)

func getRepoPath(repoURL string) string {
	return path.Join(config.GetConfig().RepoPath, url.PathEscape(repoURL))
}

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

func getLatest(repoURL string) error {
	repoPath := getRepoPath(repoURL)
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		err := clone(repoPath, repoURL)
		if err != nil {
			return err
		}
	} else {
		err = fetch(repoPath)
		if err != nil {
			return err
		}
	}
	return nil
}

func HandleMirror(sourceURL string, mirrorURL string) error {
	err := getLatest(sourceURL)
	if err != nil {
		return err
	}
	repoPath := getRepoPath(sourceURL)
	err = push(repoPath, mirrorURL)
	if err != nil {
		return err
	}
	return nil
}
