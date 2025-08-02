package git

import (
	"context"
	"github.com/zekihan/git-mirrorer/config"
	"testing"
)

func Test_clone(t *testing.T) {
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
			name: "Clone valid repository",
			args: args{
				ctx:  t.Context(),
				repo: config.GetConfig().Values[0].Source,
			},
			wantErr: false,
		},
		{
			name: "Clone invalid repository",
			args: args{
				ctx: t.Context(),
				repo: config.GitSource{
					URL: "invalid-url",
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := clone(tt.args.ctx, tt.args.repo); (err != nil) != tt.wantErr {
				t.Errorf("clone() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
