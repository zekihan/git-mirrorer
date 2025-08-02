package mirror

import (
	"testing"
	"time"

	"github.com/zekihan/git-mirrorer/config"
)

func TestReconcile_NoValues(t *testing.T) {
	orig := config.GetConfig().Values
	config.GetConfig().Values = nil

	Reconcile(t.Context())

	config.GetConfig().Values = orig
}

func TestReconcile_SingleMirror(t *testing.T) {
	setupRepos(t)
	ctx := t.Context()

	vals := config.GetConfig().Values
	if len(vals) == 0 {
		t.Fatal("setupRepos did not create any values")
	}

	config.GetConfig().Values = vals[:1]

	Reconcile(ctx)
}

func TestReconcile_ErrorHandling(t *testing.T) {
	// Use a non-existent repo to force error
	config.GetConfig().Values = []config.Value{
		{
			Source: config.GitSource{URL: "/non/existent/source"},
			Mirrors: []config.GitSource{
				{URL: "/non/existent/mirror"},
			},
		},
	}

	Reconcile(t.Context())
}

func TestStartAndStopReconcileLoop(t *testing.T) {
	// Set a short interval for fast test
	cfg := config.GetConfig()
	cfg.Schedule.Interval = 100 * time.Millisecond
	done := make(chan struct{})

	go func() {
		StartReconcileLoop()
		time.Sleep( //nolint:forbidigo // Not a production code, just for testing
			250 * time.Millisecond,
		)
		StopReconcileLoop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Reconcile loop did not stop in time")
	}
}
