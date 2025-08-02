package git

import (
	"context"
	"github.com/zekihan/git-mirrorer/config"
	"os"
	"testing"
)

func Test_push(t *testing.T) {
	setupRepos(t)

	type args struct {
		ctx  context.Context
		repo config.Value
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Push valid repository",
			args: args{
				ctx:  t.Context(),
				repo: config.GetConfig().Values[0],
			},
			wantErr: false,
		},
		{
			name: "Push non-existing repository",
			args: args{
				ctx: t.Context(),
				repo: config.Value{
					Source: config.GitSource{
						URL: config.GetConfig().Values[0].Source.URL,
					},
					Mirrors: []config.GitSource{
						{
							URL: "mirror-url",
						},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := os.Stat(tt.args.repo.Source.GetRepoPath()); os.IsNotExist(err) {
				err := clone(tt.args.ctx, tt.args.repo.Source)
				if (err != nil) != tt.wantErr {
					t.Errorf("Failed to clone repository: %v", err)
				} else {
					t.Logf("Cloned repository: %s", tt.args.repo.Source.GetRepoPath())
				}
			}
			if err := push(tt.args.ctx, tt.args.repo.Source.GetRepoPath(), tt.args.repo.Mirrors[0].URL); (err != nil) != tt.wantErr {
				t.Errorf("push() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
