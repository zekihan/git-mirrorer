package git

import (
	"testing"

	"github.com/zekihan/git-mirrorer/config"
)

func Test_clone(t *testing.T) {
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
			name: "Clone valid repository",
			args: args{
				repo: config.GetConfig().Values[0].Source,
			},
			wantErr: false,
		},
		{
			name: "Clone invalid repository",
			args: args{
				repo: config.GitSource{
					URL: "invalid-url",
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := clone(t.Context(), tt.args.repo)
			if (err != nil) != tt.wantErr {
				t.Errorf("clone() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
