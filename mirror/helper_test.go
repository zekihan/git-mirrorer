package mirror

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zekihan/go-commons/utils"

	"github.com/zekihan/git-mirrorer/config"
)

func setupRepos(t *testing.T) {
	t.Helper()

	remotes := []string{
		"remote0",
		"remote1",
		"remote2",
		"remote3",
	}

	repos := []string{
		"repo1",
		"repo2",
	}

	paths := make(map[string]map[string]string)
	for _, remote := range remotes {
		paths[remote] = make(map[string]string)
		for _, repo := range repos {
			paths[remote][repo] = t.TempDir()
		}
	}

	remotes = remotes[1:] // Exclude remote0 from the remotes list for cloning

	for _, repo := range repos {
		remote := "remote0"
		setupRemoteRepo(t, paths, remote, repo)
	}

	cfg := config.GetConfig()
	cfg.Values = make([]config.Value, 0)
	cfg.RepoPath = t.TempDir()

	for _, repo := range repos {
		val := config.Value{
			Source: config.GitSource{
				URL: paths["remote0"][repo],
			},
			Mirrors: make([]config.GitSource, 0),
		}
		for _, remote := range remotes {
			repoPath := paths[remote][repo]

			err := utils.RunCmdInPath(
				t.Context(),
				repoPath,
				"git",
				nil,
				"clone",
				paths["remote0"][repo],
				".",
			)
			if err != nil {
				t.Fatalf("Failed to clone repository %s from remote %s: %v", repo, remote, err)
			}

			val.Mirrors = append(val.Mirrors, config.GitSource{
				URL: repoPath,
			})
		}

		cfg.Values = append(cfg.Values, val)
	}
}

func setupRemoteRepo(t *testing.T, paths map[string]map[string]string, remote, repo string) {
	t.Helper()

	repoPath := paths[remote][repo]

	err := os.MkdirAll(repoPath, 0o600)
	if err != nil {
		t.Fatalf("Failed to create repository directory %s: %v", repoPath, err)
	}

	err = utils.RunCmdInPath(t.Context(), repoPath, "git", nil, "init")
	if err != nil {
		t.Fatalf("Failed to initialize git repository %s: %v", repoPath, err)
	}

	err = utils.RunCmdInPath(
		t.Context(),
		repoPath,
		"git",
		nil,
		"config",
		"user.email",
		"you@example.com",
	)
	if err != nil {
		t.Fatalf("Failed to set global git user email: %v", err)
	}

	err = utils.RunCmdInPath(
		t.Context(),
		repoPath,
		"git",
		nil,
		"config",
		"user.name",
		"Your Name",
	)
	if err != nil {
		t.Fatalf("Failed to set global git user name: %v", err)
	}

	err = utils.RunCmdInPath(
		t.Context(),
		repoPath,
		"git",
		nil,
		"config",
		"init.defaultBranch",
		"main",
	)
	if err != nil {
		t.Fatalf("Failed to set global git default branch: %v", err)
	}

	err = utils.RunCmdInPath(
		t.Context(),
		repoPath,
		"git",
		nil,
		"remote",
		"add",
		remote,
		"https://example.com/"+repo+"/"+remote+".git",
	)
	if err != nil {
		t.Fatalf("Failed to add remote %s to repository %s: %v", remote, repoPath, err)
	}

	err = utils.RunCmdInPath(
		t.Context(),
		repoPath,
		"git",
		nil,
		"commit",
		"--allow-empty",
		"-m",
		"Initial commit",
	)
	if err != nil {
		t.Fatalf("Failed to create initial commit in repository %s: %v", repoPath, err)
	}

	filePath := filepath.Join(repoPath, "file.txt")

	err = os.WriteFile(filePath, []byte("Hello, World!"), 0o600)
	if err != nil {
		t.Fatalf("Failed to create file in repository %s: %v", repoPath, err)
	}

	err = utils.RunCmdInPath(t.Context(), repoPath, "git", nil, "add", filePath)
	if err != nil {
		t.Fatalf("Failed to add file to repository %s: %v", repoPath, err)
	}

	err = utils.RunCmdInPath(t.Context(), repoPath, "git", nil, "commit", "-m", "Add file.txt")
	if err != nil {
		t.Fatalf("Failed to commit file in repository %s: %v", repoPath, err)
	}
}
