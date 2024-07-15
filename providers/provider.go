package providers

import (
	"errors"
	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/providers/github"
	"github.com/zekihan/git-mirrorer/providers/gitlab"
)

type Provider struct {
	ProviderType config.AuthType
}

func (p *Provider) CreateRepo() error {
	return errors.New("not implemented")
}

var GithubProvider = github.NewProvider()
var GitlabProvider = gitlab.NewProvider()
