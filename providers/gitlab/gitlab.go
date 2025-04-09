package gitlab

import (
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/xanzy/go-gitlab"
	"github.com/zekihan/git-mirrorer/config"
	"sync"
)

type Provider struct {
	ProviderType config.AuthType
}

func NewProvider() *Provider {
	return &Provider{
		ProviderType: config.AuthTypeGitlab,
	}
}

var clients = sync.Map{}

func (p *Provider) CreateRepo(repo config.GitSource) error {
	auth, err := p.getAuth(repo)
	if err != nil {
		return err
	}
	if err := p.checkIfRepoExists(repo); err == nil {
		return err
	}
	client := p.getClientCached(auth.Token, auth)
	_, repoName := repo.GetOwnerEntity()
	visibility := gitlab.PublicVisibility
	if repo.AutoCreateConfig.Private {
		visibility = gitlab.PrivateVisibility
	}
	_, _, err = client.Projects.CreateProject(&gitlab.CreateProjectOptions{
		Name:                     gitlab.Ptr(repoName),
		MergeRequestsAccessLevel: gitlab.Ptr(gitlab.EnabledAccessControl),
		SnippetsAccessLevel:      gitlab.Ptr(gitlab.EnabledAccessControl),
		Visibility:               gitlab.Ptr(visibility),
	})
	if err != nil {
		log.Error(err)
	}
	for {
		if err := p.checkIfRepoExists(repo); err == nil {
			break
		}
	}
	if p.ProtectBranch(repo, "main") != nil {
		log.Error("failed to protect branch")
	}
	if p.ProtectBranch(repo, "master") != nil {
		log.Error("failed to protect branch")
	}
	return nil
}

func (p *Provider) ProtectBranch(repo config.GitSource, branch string) error {
	auth, err := p.getAuth(repo)
	if err != nil {
		return err
	}
	client := p.getClientCached(auth.Token, auth)
	owner, repoName := repo.GetOwnerEntity()
	_, res, err := client.ProtectedBranches.ProtectRepositoryBranches(fmt.Sprintf("%s/%s", owner, repoName), &gitlab.ProtectRepositoryBranchesOptions{
		Name:                      gitlab.Ptr(branch),
		PushAccessLevel:           gitlab.Ptr(gitlab.MaintainerPermissions),
		MergeAccessLevel:          gitlab.Ptr(gitlab.MaintainerPermissions),
		AllowForcePush:            gitlab.Ptr(true),
		CodeOwnerApprovalRequired: gitlab.Ptr(false),
	})
	if err != nil {
		return err
	}
	if res.StatusCode == 404 {
		return errors.New("repo not found")
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
	_, res, err := client.Projects.GetProject(fmt.Sprintf("%s/%s", owner, repoName), nil)
	if err != nil {
		return err
	}
	if res.StatusCode == 404 {
		return errors.New("repo not found")
	}
	return nil
}

func (p *Provider) getClientCached(key string, auth *config.AuthConfig) *gitlab.Client {
	value, ok := clients.Load(key)
	if ok {
		client, ok := value.(*gitlab.Client)
		if ok {
			return client
		}
	}
	client, err := gitlab.NewClient(auth.Token)
	if err != nil {
		panic(err)
	}
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
