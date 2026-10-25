package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	DefaultServer  = "http://localhost:8080"
	DefaultTimeout = 10 * time.Second
	EnvServer      = "SANDCTL_SERVER"
	EnvToken       = "SANDCTL_TOKEN"
	EnvAPIKeys     = "CONTROL_PLANE_API_KEYS"
	DefaultContext = "default"
	configFileName = "config"
	configDirName  = "sandctl"
)

// Context is a named control-plane connection context. Secrets are never
// persisted to the configuration file; the token is resolved from the
// environment at load time.
type Context struct {
	Server  string `yaml:"server"`
	Timeout string `yaml:"timeout,omitempty"`
}

// ConfigFile is the on-disk representation. Token values are intentionally
// absent.
type ConfigFile struct {
	Contexts       map[string]*Context `yaml:"contexts"`
	CurrentContext string              `yaml:"current-context"`
}

// Config is the resolved, in-memory configuration for one command invocation.
type Config struct {
	ContextName string
	Server      string
	Token       string
	Timeout     time.Duration
}

// Redacted returns a copy of the configuration safe for display: the token is
// replaced with a presence marker and never its value.
func (c Config) Redacted() Config {
	redacted := c
	if c.Token != "" {
		redacted.Token = "***"
	}
	return redacted
}

// HasToken reports whether a bearer token is available without revealing it.
func (c Config) HasToken() bool {
	return c.Token != ""
}

func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config directory: %w", err)
	}
	return filepath.Join(dir, configDirName), nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName+".yaml"), nil
}

// ConfigPath returns the path to the configuration file.
func ConfigPath() (string, error) {
	return configPath()
}

// SortedContextNames returns context names in sorted order.
func SortedContextNames(contexts map[string]*Context) []string {
	names := make([]string, 0, len(contexts))
	for name := range contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func loadFile() (*ConfigFile, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ConfigFile{
				Contexts:       map[string]*Context{},
				CurrentContext: DefaultContext,
			}, nil
		}
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	var cfg ConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]*Context{}
	}
	if cfg.CurrentContext == "" {
		cfg.CurrentContext = DefaultContext
	}
	return &cfg, nil
}

func saveFile(cfg *ConfigFile) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file %q: %w", path, err)
	}
	return nil
}

func resolveToken() string {
	if token := strings.TrimSpace(os.Getenv(EnvToken)); token != "" {
		return token
	}
	if keys := strings.TrimSpace(os.Getenv(EnvAPIKeys)); keys != "" {
		for _, part := range strings.Split(keys, ",") {
			if key := strings.TrimSpace(part); key != "" {
				return key
			}
		}
	}
	return ""
}

func normalizeServer(server string) string {
	server = strings.TrimSpace(server)
	server = strings.TrimRight(server, "/")
	return server
}

func validateServer(server string) error {
	if !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		return fmt.Errorf("server must start with http:// or https://: %q", server)
	}
	return nil
}

func parseTimeout(raw string, def time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return def, fmt.Errorf("invalid timeout: %q", raw)
	}
	return d, nil
}

// Load resolves configuration for a command. Priority (highest first):
//  1. serverOverride (from the --server flag)
//  2. SANDCTL_SERVER environment variable
//  3. the current context's server, falling back to DefaultServer
//
// The bearer token is always resolved from SANDCTL_TOKEN, falling back to the
// first key in CONTROL_PLANE_API_KEYS. It is never read from the config file.
func Load(serverOverride string) (Config, error) {
	file, err := loadFile()
	if err != nil {
		return Config{}, err
	}

	server := normalizeServer(serverOverride)
	if server == "" {
		server = strings.TrimSpace(os.Getenv(EnvServer))
	}
	if server == "" {
		if ctx, ok := file.Contexts[file.CurrentContext]; ok && ctx != nil {
			server = normalizeServer(ctx.Server)
		}
	}
	if server == "" {
		server = DefaultServer
	}
	if err := validateServer(server); err != nil {
		return Config{}, err
	}

	timeout := DefaultTimeout
	if raw := strings.TrimSpace(os.Getenv("SANDCTL_TIMEOUT")); raw != "" {
		if parsed, err := parseTimeout(raw, DefaultTimeout); err == nil {
			timeout = parsed
		}
	} else if ctx, ok := file.Contexts[file.CurrentContext]; ok && ctx != nil {
		if parsed, err := parseTimeout(ctx.Timeout, DefaultTimeout); err == nil {
			timeout = parsed
		}
	}

	return Config{
		ContextName: file.CurrentContext,
		Server:      server,
		Token:       resolveToken(),
		Timeout:     timeout,
	}, nil
}

// SetContext creates or updates a named context's server and/or timeout in the
// config file without touching credentials.
func SetContext(name, server, timeout string) error {
	if name == "" {
		return errors.New("context name is required")
	}
	file, err := loadFile()
	if err != nil {
		return err
	}
	if file.Contexts == nil {
		file.Contexts = map[string]*Context{}
	}
	ctx := file.Contexts[name]
	if ctx == nil {
		ctx = &Context{}
		file.Contexts[name] = ctx
	}
	if server != "" {
		s := normalizeServer(server)
		if err := validateServer(s); err != nil {
			return err
		}
		ctx.Server = s
	}
	if timeout != "" {
		if _, err := parseTimeout(timeout, DefaultTimeout); err != nil {
			return err
		}
		ctx.Timeout = timeout
	}
	if file.CurrentContext == "" {
		file.CurrentContext = name
	}
	return saveFile(file)
}

// UseContext switches the current context.
func UseContext(name string) error {
	file, err := loadFile()
	if err != nil {
		return err
	}
	if name != "" {
		if _, ok := file.Contexts[name]; !ok {
			return fmt.Errorf("context %q does not exist", name)
		}
		file.CurrentContext = name
	} else {
		file.CurrentContext = DefaultContext
	}
	return saveFile(file)
}

// CurrentContext returns the name of the current context.
func CurrentContext() (string, error) {
	file, err := loadFile()
	if err != nil {
		return "", err
	}
	if file.CurrentContext == "" {
		return DefaultContext, nil
	}
	return file.CurrentContext, nil
}

// DeleteContext removes a named context. It refuses to delete the current
// context unless force is true.
func DeleteContext(name string, force bool) error {
	file, err := loadFile()
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New("context name is required")
	}
	if _, ok := file.Contexts[name]; !ok {
		return fmt.Errorf("context %q does not exist", name)
	}
	if !force && file.CurrentContext == name {
		return fmt.Errorf("refusing to delete current context %q; use --force or switch contexts first", name)
	}
	delete(file.Contexts, name)
	if file.CurrentContext == name {
		if len(file.Contexts) > 0 {
			for k := range file.Contexts {
				file.CurrentContext = k
				break
			}
		} else {
			file.CurrentContext = DefaultContext
		}
	}
	return saveFile(file)
}

// Init creates a default configuration file with a single "default" context.
func Init(force bool) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("config file already exists at %q", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect config file %q: %w", path, err)
	}
	file := &ConfigFile{
		Contexts: map[string]*Context{
			DefaultContext: {Server: DefaultServer, Timeout: DefaultTimeout.String()},
		},
		CurrentContext: DefaultContext,
	}
	return saveFile(file)
}

// Show returns the resolved config for the current context, including whether
// a token is present, without revealing its value.
func Show() (*Config, *ConfigFile, error) {
	file, err := loadFile()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := Load("")
	if err != nil {
		return nil, nil, err
	}
	return &cfg, file, nil
}
