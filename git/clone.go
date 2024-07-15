package git

import (
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/util"
)

func clone(repo config.GitSource) error {
	log.Debug("Cloning repository from ", repo.URL, " to ", repo.GetRepoPath())
	err := util.RunCmd("git", nil, "clone", "--mirror", repo.URL, repo.GetRepoPath())
	if err != nil {
		return err
	}
	return nil
}
