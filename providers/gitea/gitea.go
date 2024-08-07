package gitea

import (
	"code.gitea.io/sdk/gitea"
	"errors"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/config"
)

type Provider struct {
	ProviderType config.AuthType
}

func NewProvider() *Provider {
	return &Provider{
		ProviderType: config.AuthTypeGitea,
	}
}

var clients = make(map[string]*gitea.Client)

func (p *Provider) CreateRepo(repo config.GitSource) error {
	auth, err := p.getAuth(repo)
	if err != nil {
		return err
	}
	if err := p.checkIfRepoExists(repo); err == nil {
		return err
	}
	client := p.getClientCached(auth.Token, auth)
	org, repoName := repo.GetOwnerEntity()
	if !repo.AutoCreateConfig.IsOrg {
		_, _, err = client.CreateRepo(gitea.CreateRepoOption{
			Name:     repoName,
			Private:  repo.AutoCreateConfig.Private,
			AutoInit: false,
		})
	} else {
		_, _, err = client.CreateOrgRepo(org, gitea.CreateRepoOption{
			Name:     repoName,
			Private:  repo.AutoCreateConfig.Private,
			AutoInit: false,
		})
	}
	if err != nil {
		log.Error(err)
	}
	for {
		if err := p.checkIfRepoExists(repo); err == nil {
			break
		}
	}
	return nil
}

func (p *Provider) checkIfRepoExists(repo config.GitSource) error {
	auth, err := p.getAuth(repo)
	if err != nil {
		return err
	}
	client := p.getClientCached(auth.Token, auth)
	owner, repoName := repo.GetOwnerEntity()
	_, res, err := client.GetRepo(owner, repoName)
	if err != nil {
		return err
	}
	if res.StatusCode == 404 {
		return errors.New("repo not found")
	}
	return nil
}

func (p *Provider) getClientCached(key string, auth *config.AuthConfig) *gitea.Client {
	if client, ok := clients[key]; ok {
		return client
	}
	client, err := gitea.NewClient(auth.URL, gitea.SetToken(auth.Token))
	if err != nil {
		panic(err)
	}
	clients[key] = client
	return client
}

func (p *Provider) getAuth(repo config.GitSource) (*config.AuthConfig, error) {
	auth, ok := repo.AutoCreateConfig.GetAuth()
	if !ok {
		return nil, errors.New("auto create is enabled but no auth is provided")
	}
	if auth.AuthType != p.ProviderType {
		return nil, errors.New("auth type is not github")
	}
	return &auth, nil
}
