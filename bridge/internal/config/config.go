package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration that unmarshals from a YAML string like "2s".
type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

type Config struct {
	Factorio     FactorioConfig   `yaml:"factorio"`
	Transport    string           `yaml:"transport"`
	PollInterval Duration         `yaml:"poll_interval"`
	Discord      DiscordConfig    `yaml:"discord"`
	ControlAPI   ControlAPIConfig `yaml:"control_api"`
}

type FactorioConfig struct {
	RCON               RCONConfig `yaml:"rcon"`
	EventsFile         string     `yaml:"events_file"`
	RequiredModVersion string     `yaml:"required_mod_version"`
}

type ControlAPIConfig struct {
	Enabled      bool   `yaml:"enabled"`
	Listen       string `yaml:"listen"`
	AuthTokenEnv string `yaml:"auth_token_env"`
	AuthToken    string `yaml:"-"` // resolved from env at load time
}

type RCONConfig struct {
	Address     string `yaml:"address"`
	PasswordEnv string `yaml:"password_env"`
	Password    string `yaml:"-"` // resolved from env at load time
}

type DiscordConfig struct {
	TokenEnv string  `yaml:"token_env"`
	Token    string  `yaml:"-"` // resolved from env at load time
	GuildID  string  `yaml:"guild_id"`
	Routes   []Route `yaml:"routes"`
}

type Route struct {
	Source    string `yaml:"source"`
	ChannelID string `yaml:"channel_id"`
}

// Load reads and validates the config file, resolving secrets from the environment.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Allow ${ENV} and a leading ~/ in the events path so configs stay portable and
	// don't need to hardcode an absolute home directory.
	c.Factorio.EventsFile = expandPath(c.Factorio.EventsFile)

	if c.Transport == "" {
		c.Transport = "local"
	}
	if c.PollInterval == 0 {
		c.PollInterval = Duration(time.Second)
	}

	// Resolve secrets from the environment; never store them in the YAML.
	if c.Discord.TokenEnv != "" {
		c.Discord.Token = os.Getenv(c.Discord.TokenEnv)
	}
	if c.Factorio.RCON.PasswordEnv != "" {
		c.Factorio.RCON.Password = os.Getenv(c.Factorio.RCON.PasswordEnv)
	}
	if c.ControlAPI.AuthTokenEnv != "" {
		c.ControlAPI.AuthToken = os.Getenv(c.ControlAPI.AuthTokenEnv)
	}
	if c.ControlAPI.Enabled && c.ControlAPI.Listen == "" {
		c.ControlAPI.Listen = "127.0.0.1:7777"
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) validate() error {
	if c.Transport != "local" {
		return fmt.Errorf("transport %q not supported in this MVP (only \"local\")", c.Transport)
	}
	if c.Factorio.EventsFile == "" {
		return fmt.Errorf("factorio.events_file is required")
	}
	if c.Factorio.RCON.Address == "" {
		return fmt.Errorf("factorio.rcon.address is required")
	}
	if c.Factorio.RCON.Password == "" {
		return fmt.Errorf("RCON password is empty; set env var %q", c.Factorio.RCON.PasswordEnv)
	}
	if c.Discord.Token == "" {
		return fmt.Errorf("Discord token is empty; set env var %q", c.Discord.TokenEnv)
	}
	if len(c.Discord.Routes) == 0 {
		return fmt.Errorf("discord.routes is empty; add at least one route")
	}
	for i, r := range c.Discord.Routes {
		if r.Source == "" || r.ChannelID == "" {
			return fmt.Errorf("discord.routes[%d] needs both source and channel_id", i)
		}
	}
	if c.ControlAPI.Enabled && c.ControlAPI.AuthToken == "" {
		return fmt.Errorf("control_api.enabled but auth token empty; set env var %q", c.ControlAPI.AuthTokenEnv)
	}
	return nil
}

func (c *Config) Interval() time.Duration { return time.Duration(c.PollInterval) }

func expandPath(p string) string {
	p = os.ExpandEnv(p)
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	return p
}
