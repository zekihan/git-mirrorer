package git

import (
	"os"
	"testing"

	"github.com/zekihan/git-mirrorer/config"
)

func Test_fetch(t *testing.T) {
	setupRepos(t)

	type args struct {
		repo config.GitSource
	}

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Fetch valid repository",
			args: args{
				repo: config.GetConfig().Values[0].Source,
			},
			wantErr: false,
		},
		{
			name: "Fetch non-existing repository",
			args: args{
				repo: config.GitSource{
					URL: "invalid-repo-url",
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := os.Stat(tt.args.repo.GetRepoPath())
			if os.IsNotExist(err) {
				err = clone(t.Context(), tt.args.repo)
				if (err != nil) != tt.wantErr {
					t.Errorf("Failed to clone repository: %v", err)
				} else {
					t.Logf("Cloned repository: %s", tt.args.repo.GetRepoPath())
				}
			}

			err = fetch(t.Context(), tt.args.repo.GetRepoPath())
			if (err != nil) != tt.wantErr {
				t.Errorf("fetch() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
