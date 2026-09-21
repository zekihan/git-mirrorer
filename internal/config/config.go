package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

type Config struct {
	StateDir         string        `yaml:"stateDir"`
	Concurrency      int           `yaml:"concurrency"`
	OperationTimeout time.Duration `yaml:"operationTimeout"`
	RunTimeout       time.Duration `yaml:"runTimeout"`
	LogLevel         string        `yaml:"logLevel"`
	Repositories     []Repository  `yaml:"repositories"`
}

type Repository struct {
	Name         string        `yaml:"name"`
	Source       string        `yaml:"source"`
	LFS          bool          `yaml:"lfs"`
	Destinations []Destination `yaml:"destinations"`
}

type Destination struct {
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	Atomic *bool  `yaml:"atomic"`
}

func (d Destination) AtomicEnabled() bool { return d.Atomic == nil || *d.Atomic }

func Load(filename string) (Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()
	base, err := filepath.Abs(filepath.Dir(filename))
	if err != nil {
		return Config{}, fmt.Errorf("resolve configuration directory: %w", err)
	}
	return Decode(file, base)
}

func Decode(reader io.Reader, base string) (Config, error) {
	cfg := Config{StateDir: ".git-mirrorer", Concurrency: 4, OperationTimeout: 10 * time.Minute, LogLevel: "info"}
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		// YAML errors may include a scalar containing a credential. Do not echo them.
		return Config{}, errors.New("invalid YAML: check syntax, field names, duplicate keys, and value types")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, errors.New("configuration must contain exactly one YAML document")
	}
	if err := cfg.validate(base); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validName(name string) bool {
	return name != "" && strings.TrimSpace(name) == name && !strings.ContainsFunc(name, unicode.IsControl)
}

func (c *Config) validate(base string) error {
	if c.Concurrency < 1 || c.OperationTimeout <= 0 || c.RunTimeout < 0 {
		return errors.New("concurrency and operationTimeout must be positive; runTimeout must be nonnegative")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("logLevel must be debug, info, warn, or error")
	}
	if strings.TrimSpace(c.StateDir) == "" || strings.ContainsFunc(c.StateDir, unicode.IsControl) {
		return errors.New("stateDir must be a nonempty filesystem path")
	}
	var err error
	c.StateDir, err = resolvePath(base, c.StateDir)
	if err != nil {
		return errors.New("cannot resolve stateDir")
	}
	if len(c.Repositories) == 0 {
		return errors.New("repositories must not be empty")
	}
	names, sources, destinations := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i := range c.Repositories {
		repo := &c.Repositories[i]
		field := fmt.Sprintf("repositories[%d]", i)
		if !validName(repo.Name) || names[repo.Name] {
			return fmt.Errorf("%s.name must be nonempty and unique, without surrounding whitespace or control characters", field)
		}
		names[repo.Name] = true
		repo.Source, err = c.address(base, repo.Source)
		if err != nil {
			return fmt.Errorf("%s.source: %w", field, err)
		}
		if sources[repo.Source] {
			return fmt.Errorf("%s.source duplicates another source; combine its destinations", field)
		}
		sources[repo.Source] = true
		if len(repo.Destinations) == 0 {
			return fmt.Errorf("%s.destinations must not be empty", field)
		}
		destNames := map[string]bool{}
		for j := range repo.Destinations {
			dest := &repo.Destinations[j]
			if !validName(dest.Name) || destNames[dest.Name] {
				return fmt.Errorf("%s.destinations[%d].name must be nonempty and unique, without surrounding whitespace or control characters", field, j)
			}
			destNames[dest.Name] = true
			dest.URL, err = c.address(base, dest.URL)
			if err != nil {
				return fmt.Errorf("%s.destinations[%d].url: %w", field, j, err)
			}
			if destinations[dest.URL] {
				return fmt.Errorf("%s.destinations[%d].url is already owned by a destination", field, j)
			}
			destinations[dest.URL] = true
		}
	}
	for source := range sources {
		if destinations[source] {
			return errors.New("a destination is also a configured source; self-mirroring, chains, and cycles are unsupported")
		}
	}
	return nil
}

func (c Config) address(base, value string) (string, error) {
	address, local, err := Address(base, value)
	if err != nil {
		return "", err
	}
	if local && (within(address, c.StateDir) || within(c.StateDir, address)) {
		return "", errors.New("repository path overlaps stateDir")
	}
	return address, nil
}

func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Address returns a conservative identity that is also a usable Git address.
func Address(base, value string) (string, bool, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.HasPrefix(value, "-") || strings.ContainsFunc(value, unicode.IsControl) {
		return "", false, errors.New("address is empty, option-like, or contains invalid whitespace")
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return "", false, errors.New("invalid URL or forbidden query/fragment")
		}
		if strings.ContainsFunc(u.Path, unicode.IsControl) || (u.User != nil && strings.ContainsFunc(u.User.Username(), unicode.IsControl)) {
			return "", false, errors.New("URL contains encoded control characters")
		}
		u.Scheme = strings.ToLower(u.Scheme)
		if u.User != nil {
			if _, password := u.User.Password(); password || u.Scheme != "ssh" {
				return "", false, errors.New("embedded credentials are forbidden; use SSH or a credential helper")
			}
		}
		switch u.Scheme {
		case "file":
			if u.Host != "" && u.Host != "localhost" || !filepath.IsAbs(u.Path) {
				return "", false, errors.New("file URL must identify an absolute local path")
			}
			path, err := resolvePath(base, u.Path)
			return path, true, err
		case "ssh", "https":
			if u.Hostname() == "" || u.Path == "" || u.Path == "/" || strings.HasPrefix(u.Host, "-") {
				return "", false, errors.New("URL must contain a host and repository path")
			}
			u.Host = strings.ToLower(u.Host)
			return u.String(), false, nil
		default:
			return "", false, errors.New("unsupported URL scheme")
		}
	}
	// A colon before any slash denotes SCP-style SSH, except for explicit paths.
	colon, slash := strings.IndexByte(value, ':'), strings.IndexByte(value, '/')
	if bracket := strings.IndexByte(value, '['); bracket >= 0 && (slash < 0 || bracket < slash) {
		end := strings.IndexByte(value, ']')
		if end < bracket || end+1 >= len(value) || value[end+1] != ':' {
			return "", false, errors.New("invalid bracketed SCP-style SSH address")
		}
		colon = end + 1
	}
	if colon >= 0 && (slash < 0 || colon < slash) {
		host, path := value[:colon], value[colon+1:]
		if host == "" || path == "" || strings.HasPrefix(path, ":") || strings.ContainsAny(host, " ?#\\") || strings.Count(host, "@") > 1 {
			return "", false, errors.New("invalid SCP-style SSH address")
		}
		user, hostname := "", host
		if at := strings.IndexByte(host, '@'); at >= 0 {
			user, hostname = host[:at+1], host[at+1:]
		}
		if hostname == "" || strings.HasPrefix(hostname, "-") || user == "@" {
			return "", false, errors.New("invalid SSH host or username")
		}
		return user + strings.ToLower(hostname) + ":" + path, false, nil
	}
	path, err := resolvePath(base, value)
	return path, true, err
}

// Resolve existing ancestors as well, so a not-yet-created child of a symlink is normalized.
func resolvePath(base, value string) (string, error) {
	if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	value, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	ancestor := value
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) || ancestor == filepath.Dir(ancestor) {
			return "", errors.New("cannot resolve filesystem path")
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = filepath.Dir(ancestor)
	}
}
