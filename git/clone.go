package git

import (
	"context"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/go-commons/utils"
)

func clone(ctx context.Context, repo config.GitSource) error {
	log.Debug("Cloning repository from ", repo.URL, " to ", repo.GetRepoPath())
	err := utils.RunCmd(ctx, "git", nil, "clone", "--mirror", repo.URL, repo.GetRepoPath())
	if err != nil {
		return err
	}
	return nil
}
