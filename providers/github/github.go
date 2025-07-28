package github

import (
	"context"
	"errors"
	"github.com/google/go-github/v74/github"
	"github.com/zekihan/git-mirrorer/config"
	"sync"
)

type Provider struct {
	ProviderType config.AuthType
}

func NewProvider() *Provider {
	return &Provider{
		ProviderType: config.AuthTypeGithub,
	}
}

var clients = sync.Map{}

func (p *Provider) CreateRepo(ctx context.Context, repo config.GitSource) error {
	auth, err := p.getAuth(repo)
	if err != nil {
		return err
	}
	if err := p.checkIfRepoExists(ctx, repo); err == nil {
		return err
	}
	client := p.getClientCached(auth.Token, auth)
	org, repoName := repo.GetOwnerEntity()
	if !repo.AutoCreateConfig.IsOrg {
		org = ""
	}
	_, _, err = client.Repositories.Create(ctx, org, &github.Repository{
		Name:    github.String(repoName),
		Private: github.Bool(repo.AutoCreateConfig.Private),
	})
	if err != nil {
		return err
	}
	return nil
}

func (p *Provider) checkIfRepoExists(ctx context.Context, repo config.GitSource) error {
	auth, err := p.getAuth(repo)
	if err != nil {
		return err
	}
	client := p.getClientCached(auth.Token, auth)
	owner, repoName := repo.GetOwnerEntity()
	_, res, err := client.Repositories.Get(ctx, owner, repoName)
	if err != nil {
		return err
	}
	if res.StatusCode == 404 {
		return errors.New("repo not found")
	}
	return nil
}

func (p *Provider) getClientCached(key string, auth *config.AuthConfig) *github.Client {
	value, ok := clients.Load(key)
	if ok {
		client, ok := value.(*github.Client)
		if ok {
			return client
		}
	}
	client := github.NewClient(nil).WithAuthToken(auth.Token)
	clients.Store(key, client)
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
