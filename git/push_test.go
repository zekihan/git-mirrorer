package git

import (
	"os"
	"testing"

	"github.com/zekihan/git-mirrorer/config"
)

func Test_push(t *testing.T) {
	setupRepos(t)

	type args struct {
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
				repo: config.GetConfig().Values[0],
			},
			wantErr: false,
		},
		{
			name: "Push non-existing repository",
			args: args{
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
			_, err := os.Stat(tt.args.repo.Source.GetRepoPath())
			if os.IsNotExist(err) {
				err = clone(t.Context(), tt.args.repo.Source)
				if (err != nil) != tt.wantErr {
					t.Errorf("Failed to clone repository: %v", err)
				} else {
					t.Logf("Cloned repository: %s", tt.args.repo.Source.GetRepoPath())
				}
			}

			err = push(t.Context(), tt.args.repo.Source.GetRepoPath(), tt.args.repo.Mirrors[0].URL)

			if (err != nil) != tt.wantErr {
				t.Errorf("push() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
