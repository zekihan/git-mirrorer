package git

import (
	"context"
	"github.com/zekihan/git-mirrorer/config"
	"os"
	"testing"
)

func Test_fetch(t *testing.T) {
	setupRepos(t)

	type args struct {
		ctx  context.Context
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
				ctx:  t.Context(),
				repo: config.GetConfig().Values[0].Source,
			},
			wantErr: false,
		},
		{
			name: "Fetch non-existing repository",
			args: args{
				ctx: t.Context(),
				repo: config.GitSource{
					URL: "invalid-repo-url",
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := os.Stat(tt.args.repo.GetRepoPath()); os.IsNotExist(err) {
				err := clone(tt.args.ctx, tt.args.repo)
				if (err != nil) != tt.wantErr {
					t.Errorf("Failed to clone repository: %v", err)
				} else {
					t.Logf("Cloned repository: %s", tt.args.repo.GetRepoPath())
				}
			}
			if err := fetch(tt.args.ctx, tt.args.repo.GetRepoPath()); (err != nil) != tt.wantErr {
				t.Errorf("fetch() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
