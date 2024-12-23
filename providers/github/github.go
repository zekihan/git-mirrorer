package github

import (
	"context"
	"errors"
	"github.com/google/go-github/v68/github"
	"github.com/zekihan/git-mirrorer/config"
)

type Provider struct {
	ProviderType config.AuthType
}

func NewProvider() *Provider {
	return &Provider{
		ProviderType: config.AuthTypeGithub,
	}
}

var clients = make(map[string]*github.Client)

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
		org = ""
	}
	_, _, err = client.Repositories.Create(context.Background(), org, &github.Repository{
		Name:    github.String(repoName),
		Private: github.Bool(repo.AutoCreateConfig.Private),
	})
	if err != nil {
		return err
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
	_, res, err := client.Repositories.Get(context.Background(), owner, repoName)
	if err != nil {
		return err
	}
	if res.StatusCode == 404 {
		return errors.New("repo not found")
	}
	return nil
}

func (p *Provider) getClientCached(key string, auth *config.AuthConfig) *github.Client {
	if client, ok := clients[key]; ok {
		return client
	}
	client := github.NewClient(nil).WithAuthToken(auth.Token)
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
