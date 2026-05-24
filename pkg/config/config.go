package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Defaults
const (
	DefaultListen       = ":8080"
	DefaultIPCSocket    = "/run/webadmin/ipc.sock"
	DefaultSessionTTL   = 3600
	DefaultWSMaxConns   = 10
	DefaultRateLimitRPS = 20
)

// Config holds runtime configuration loaded from /etc/webadmin/config.json.
type Config struct {
	Listen         string   `json:"listen"`
	TLSCert        string   `json:"tls_cert,omitempty"`
	TLSKey         string   `json:"tls_key,omitempty"`
	IPCSocket      string   `json:"ipc_socket"`
	SessionTTL     int      `json:"session_ttl"`
	WSMaxConns     int      `json:"ws_max_conns"`
	RateLimitRPS   int      `json:"rate_limit_rps"`
	AllowedHelpers []string `json:"allowed_helpers"`
}

// Load reads and validates a JSON config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse json: %w", err)
	}

	if err := cfg.applyDefaults(); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() error {
	if c.Listen == "" {
		c.Listen = DefaultListen
	}
	if c.IPCSocket == "" {
		c.IPCSocket = DefaultIPCSocket
	}
	if c.SessionTTL == 0 {
		c.SessionTTL = DefaultSessionTTL
	}
	if c.WSMaxConns == 0 {
		c.WSMaxConns = DefaultWSMaxConns
	}
	if c.RateLimitRPS == 0 {
		c.RateLimitRPS = DefaultRateLimitRPS
	}
	return nil
}

func (c *Config) validate() error {
	if c.SessionTTL < 300 || c.SessionTTL > 86400 {
		return fmt.Errorf("config: session_ttl must be between 300 and 86400 seconds")
	}
	if c.WSMaxConns < 1 || c.WSMaxConns > 100 {
		return fmt.Errorf("config: ws_max_conns must be between 1 and 100")
	}
	if c.RateLimitRPS < 1 || c.RateLimitRPS > 1000 {
		return fmt.Errorf("config: rate_limit_rps must be between 1 and 1000")
	}
	if !strings.Contains(c.Listen, ":") {
		return fmt.Errorf("config: listen must contain a colon (e.g. :8080)")
	}
	if !filepath.IsAbs(c.IPCSocket) {
		return fmt.Errorf("config: ipc_socket must be an absolute path")
	}

	for _, helper := range c.AllowedHelpers {
		if !filepath.IsAbs(helper) {
			return fmt.Errorf("config: allowed_helpers must contain absolute paths, got %q", helper)
		}
	}

	if c.TLSCert != "" || c.TLSKey != "" {
		if c.TLSCert == "" || c.TLSKey == "" {
			return fmt.Errorf("config: both tls_cert and tls_key must be set or both omitted")
		}
	}

	return nil
}
