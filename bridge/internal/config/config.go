package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
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

// Load reads and validates configuration. If the config file is absent, it builds the
// config entirely from environment variables (env-var config mode) — see LoadFromEnv.
func Load(path string) (*Config, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return LoadFromEnv()
	}

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

// LoadFromEnv builds config entirely from environment variables — used when no config
// file is present (containers, panels like Pterodactyl). Non-secret settings use ODB_*
// vars; secrets reuse the same names as file mode (DISCORD_BOT_TOKEN,
// FACTORIO_RCON_PASSWORD, BRIDGE_CONTROL_TOKEN).
func LoadFromEnv() (*Config, error) {
	c := &Config{
		Transport: getenvDefault("ODB_TRANSPORT", "local"),
		Factorio: FactorioConfig{
			RCON: RCONConfig{
				Address:     os.Getenv("ODB_RCON_ADDRESS"),
				PasswordEnv: "FACTORIO_RCON_PASSWORD",
				Password:    os.Getenv("FACTORIO_RCON_PASSWORD"),
			},
			EventsFile:         expandPath(os.Getenv("ODB_EVENTS_FILE")),
			RequiredModVersion: os.Getenv("ODB_REQUIRED_MOD_VERSION"),
		},
		Discord: DiscordConfig{
			TokenEnv: "DISCORD_BOT_TOKEN",
			Token:    os.Getenv("DISCORD_BOT_TOKEN"),
			GuildID:  os.Getenv("ODB_DISCORD_GUILD_ID"),
		},
		ControlAPI: ControlAPIConfig{
			Enabled:      parseBool(os.Getenv("ODB_CONTROL_API_ENABLED")),
			Listen:       os.Getenv("ODB_CONTROL_API_LISTEN"),
			AuthTokenEnv: "BRIDGE_CONTROL_TOKEN",
			AuthToken:    os.Getenv("BRIDGE_CONTROL_TOKEN"),
		},
	}

	if v := os.Getenv("ODB_POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("ODB_POLL_INTERVAL: %w", err)
		}
		c.PollInterval = Duration(d)
	} else {
		c.PollInterval = Duration(time.Second)
	}

	routes, err := routesFromEnv()
	if err != nil {
		return nil, err
	}
	c.Discord.Routes = routes

	if c.ControlAPI.Enabled && c.ControlAPI.Listen == "" {
		c.ControlAPI.Listen = "127.0.0.1:7777"
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// routesFromEnv parses ODB_ROUTES ("source=channel_id,source=channel_id"), or falls back
// to ODB_DISCORD_CHANNEL_ID as a single catch-all route.
func routesFromEnv() ([]Route, error) {
	if raw := os.Getenv("ODB_ROUTES"); raw != "" {
		var routes []Route
		for _, pair := range strings.Split(raw, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			src, ch, ok := strings.Cut(pair, "=")
			if !ok {
				return nil, fmt.Errorf("ODB_ROUTES entry %q must be source=channel_id", pair)
			}
			routes = append(routes, Route{Source: strings.TrimSpace(src), ChannelID: strings.TrimSpace(ch)})
		}
		return routes, nil
	}
	if ch := os.Getenv("ODB_DISCORD_CHANNEL_ID"); ch != "" {
		return []Route{{Source: "*", ChannelID: ch}}, nil
	}
	return nil, nil // validate reports the empty-routes error
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseBool(s string) bool {
	b, _ := strconv.ParseBool(s)
	return b
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
