package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validYAML = `repositories:
  - name: example
    source: ./source.git
    destinations:
      - name: backup
        url: ./backup.git
`

func TestDefaultsAndRelativePaths(t *testing.T) {
	base := canonicalTemp(t)
	cfg, err := Decode(strings.NewReader(validYAML), base)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Concurrency != 4 || cfg.OperationTimeout.String() != "10m0s" || cfg.RunTimeout != 0 || cfg.LogLevel != "info" {
		t.Fatalf("defaults: %+v", cfg)
	}
	if cfg.StateDir != filepath.Join(base, ".git-mirrorer") || cfg.Repositories[0].Source != filepath.Join(base, "source.git") {
		t.Fatalf("paths: %+v", cfg)
	}
	if !cfg.Repositories[0].Destinations[0].AtomicEnabled() {
		t.Fatal("atomic default must be true")
	}
	cfg, err = Decode(strings.NewReader(validYAML+"        atomic: false\n"), base)
	if err != nil || cfg.Repositories[0].Destinations[0].AtomicEnabled() {
		t.Fatalf("explicit false: %v", err)
	}
}

func TestRejectInvalidConfiguration(t *testing.T) {
	cases := map[string]string{
		"unknown":               validYAML + "schedule: 1m\n",
		"nested unknown":        validYAML + "        typo: true\n",
		"duplicate":             "concurrency: 1\nconcurrency: 2\n" + validYAML,
		"document":              validYAML + "---\n{}\n",
		"empty":                 "repositories: []\n",
		"zero workers":          "concurrency: 0\n" + validYAML,
		"timeout":               "operationTimeout: 0s\n" + validYAML,
		"negative run":          "runTimeout: -1s\n" + validYAML,
		"level":                 "logLevel: verbose\n" + validYAML,
		"self":                  strings.ReplaceAll(validYAML, "./backup.git", "./source.git"),
		"cache overlap":         strings.ReplaceAll(validYAML, "./backup.git", "./.git-mirrorer/repos/a"),
		"missing name":          strings.ReplaceAll(validYAML, "name: example", "name: ''"),
		"duplicate source":      validYAML + "  - name: other\n    source: ./source.git\n    destinations:\n      - name: b\n        url: ./other.git\n",
		"duplicate destination": validYAML + "  - name: other\n    source: ./other.git\n    destinations:\n      - name: b\n        url: ./backup.git\n",
		"chain":                 validYAML + "  - name: other\n    source: ./backup.git\n    destinations:\n      - name: b\n        url: ./other.git\n",
		"credential":            strings.ReplaceAll(validYAML, "./source.git", "https://user:SUPERSECRET@example.com/a"),
		"typed secret":          strings.ReplaceAll(validYAML, "./source.git", "[SUPERSECRET]"),
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(value), t.TempDir())
			if err == nil {
				t.Fatal("accepted invalid config")
			}
			if strings.Contains(err.Error(), "SUPERSECRET") {
				t.Fatalf("leaked input: %v", err)
			}
		})
	}
}

func TestAddresses(t *testing.T) {
	base := canonicalTemp(t)
	cases := []struct {
		in, want string
		local    bool
	}{
		{"git@EXAMPLE.com:Owner/repo.git", "git@example.com:Owner/repo.git", false},
		{"git@[::1]:Owner/repo.git", "git@[::1]:Owner/repo.git", false},
		{"ssh://git@EXAMPLE.com:2222/Owner/repo.git", "ssh://git@example.com:2222/Owner/repo.git", false},
		{"https://EXAMPLE.com/Owner/repo.git", "https://example.com/Owner/repo.git", false},
		{"./repo.git", filepath.Join(base, "repo.git"), true},
		{"repo[1].git", filepath.Join(base, "repo[1].git"), true},
		{"file:///tmp/repo.git", "/private/tmp/repo.git", true},
	}
	for _, tc := range cases {
		got, local, err := Address(base, tc.in)
		if tc.in == "file:///tmp/repo.git" {
			tc.want, _ = resolvePath(base, "/tmp/repo.git")
		}
		if err != nil || got != tc.want || local != tc.local {
			t.Errorf("%q => %q %v %v", tc.in, got, local, err)
		}
	}
	for _, input := range []string{"", "-evil", " user@host:a", "ext::cmd", "git://host/repo", "https://host/repo?token=secret", "https://token@host/repo", "ssh://git:secret@host/repo", "ssh://host", "host:", "@host:repo", "file://remote/path", "a\nb"} {
		if _, _, err := Address(base, input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestSymlinkAncestorsAndConfigDirectory(t *testing.T) {
	base := canonicalTemp(t)
	actual := filepath.Join(base, "actual")
	if err := os.Mkdir(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	got, _, err := Address(base, "link/missing/repo")
	if err != nil || got != filepath.Join(actual, "missing/repo") {
		t.Fatalf("symlink: %q %v", got, err)
	}
	filename := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(filename, []byte(validYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filename)
	if err != nil || cfg.Repositories[0].Source != filepath.Join(base, "source.git") {
		t.Fatalf("load: %+v %v", cfg, err)
	}
}

func canonicalTemp(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocalPathChangesAfterLoad(t *testing.T) {
	for _, endpoint := range []string{"source.git", "backup.git"} {
		for _, change := range []string{"endpoint", "ancestor", "writable ancestor", "missing endpoint"} {
			t.Run(endpoint+"/"+change, func(t *testing.T) {
				base := filepath.Join(canonicalTemp(t), "namespace")
				if err := os.Mkdir(base, 0o700); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"source.git", "backup.git"} {
					if change == "missing endpoint" && name == endpoint {
						continue
					}
					if err := os.Mkdir(filepath.Join(base, name), 0o700); err != nil {
						t.Fatal(err)
					}
				}
				cfg, err := Decode(strings.NewReader(validYAML), base)
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(base, endpoint)
				switch change {
				case "endpoint":
					if err := os.Rename(target, target+".old"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(target, 0o700); err != nil {
						t.Fatal(err)
					}
				case "ancestor":
					if err := os.Rename(base, base+".old"); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(target, 0o700); err != nil {
						t.Fatal(err)
					}
				case "writable ancestor":
					if err := os.Chmod(base, 0o777); err != nil {
						t.Fatal(err)
					}
				case "missing endpoint":
					if err := os.Mkdir(target, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				if err := cfg.CheckLocalPath(target); err == nil {
					t.Fatal("accepted changed path")
				}
			})
		}
	}
}

func TestTrustedLocalHooksPolicy(t *testing.T) {
	cfg, err := Decode(strings.NewReader(validYAML+"        trustedLocalHooks: true\n"), canonicalTemp(t))
	if err != nil || !cfg.Repositories[0].Destinations[0].TrustedLocalHooks {
		t.Fatalf("local opt-in: %v", err)
	}
	for _, remote := range []string{"ssh://git@example.invalid/backup.git", "https://example.invalid/backup.git"} {
		input := strings.ReplaceAll(validYAML, "./backup.git", remote)
		if _, err := Decode(strings.NewReader(input), canonicalTemp(t)); err != nil {
			t.Fatalf("remote default: %v", err)
		}
		if _, err := Decode(strings.NewReader(input+"        trustedLocalHooks: true\n"), canonicalTemp(t)); err == nil {
			t.Fatal("accepted remote-only opt-in")
		}
	}
}

func TestCachePolicyDefaultsOverridesAndValidation(t *testing.T) {
	cfg, err := Decode(strings.NewReader(validYAML), canonicalTemp(t))
	if err != nil || cfg.Cache.SourceBytes != 5<<30 || cfg.Cache.TotalBytes != 20<<30 || cfg.Cache.SourceEntries != 250_000 || cfg.Cache.ValidationTimeout.String() != "10s" {
		t.Fatalf("cache defaults: %+v %v", cfg.Cache, err)
	}
	cfg, err = Decode(strings.NewReader("cache:\n  sourceBytes: 1024\n  validationTimeout: 50ms\n"+validYAML), canonicalTemp(t))
	if err != nil || cfg.Cache.SourceBytes != 1024 || cfg.Cache.TotalBytes != 20<<30 || cfg.Cache.ValidationTimeout.String() != "50ms" {
		t.Fatalf("cache overrides: %+v %v", cfg.Cache, err)
	}
	for _, setting := range []string{"sourceBytes: 0", "sourceEntries: -1", "totalBytes: 1", "totalEntries: 1", "totalObjects: 1", "sourceObjects: 0", "reserveBytes: 0", "validationTimeout: 0s", "unknown: 1"} {
		if _, err := Decode(strings.NewReader("cache:\n  "+setting+"\n"+validYAML), canonicalTemp(t)); err == nil {
			t.Errorf("accepted %s", setting)
		}
	}
}
