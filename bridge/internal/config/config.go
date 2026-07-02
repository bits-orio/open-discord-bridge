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
	LogFile      string           `yaml:"log_file"` // also write logs here (default: bridge.log next to events; "-" = stderr only)
	Discord      DiscordConfig    `yaml:"discord"`
	ControlAPI   ControlAPIConfig `yaml:"control_api"`

	// FilePath is the config file this was loaded from ("" when loaded from env vars —
	// see LoadFromEnv). Used to persist runtime config changes (e.g. POST /v1/config
	// routing updates) back to disk so they survive a restart; env-var mode has no file
	// to write to, so such changes stay in-memory-only until the env vars are updated too.
	FilePath string `yaml:"-"`
}

type FactorioConfig struct {
	RCON               RCONConfig `yaml:"rcon"`
	EventsFile         string     `yaml:"events_file"` // local path, or remote path for sftp
	LinksFile          string     `yaml:"links_file"`  // local path for persistent links (default: links.json next to binary)
	RequiredModVersion string     `yaml:"required_mod_version"`
	SFTP               SFTPConfig `yaml:"sftp"`
}

// SFTPConfig is used when transport is "sftp" (bridge on separate infra from Factorio).
type SFTPConfig struct {
	Host           string `yaml:"host"` // host:port
	User           string `yaml:"user"`
	KeyPath        string `yaml:"key_path"`
	PasswordEnv    string `yaml:"password_env"`
	Password       string `yaml:"-"` // resolved from env at load time
	KnownHostsPath string `yaml:"known_hosts_path"`
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
	TokenEnv                string      `yaml:"token_env"`
	Token                   string      `yaml:"-"` // resolved from env at load time
	GuildID                 string      `yaml:"guild_id"`
	Embed                   bool        `yaml:"embed"`                      // color integrator-event category labels via an ANSI code block
	AnnounceStatus          bool        `yaml:"announce_status"`            // post bridge.established/disconnected to Discord
	ChannelTopicStatus      *bool       `yaml:"channel_topic_status"`       // keep channel topic in sync with server state (default true)
	StatusPlayerJoinedEvent string      `yaml:"status_player_joined_event"` // event key for player joins (default: vanilla.player_joined)
	StatusPlayerLeftEvent   string      `yaml:"status_player_left_event"`   // event key for player leaves (default: vanilla.player_left)
	LinkedRoleID            string      `yaml:"linked_role_id"`             // role kept in sync with linked players
	LinkedNickname          string      `yaml:"linked_nickname"`            // nickname format for linked members ({factorio}/{discord})
	Routes                  []Route     `yaml:"routes"`
	Admins                  AdminConfig `yaml:"admins"`
	Commands                []Command   `yaml:"commands"`
}

type Route struct {
	Source    string `yaml:"source"`
	ChannelID string `yaml:"channel_id"`
}

// AdminConfig defines who counts as a Discord admin (for admin-only commands). A user is
// an admin if their ID is in Users, OR they hold any role in Roles, OR (when
// UseDiscordPermission is unset/true) they have Discord's Administrator permission.
type AdminConfig struct {
	Roles                []string `yaml:"roles"` // role IDs
	Users                []string `yaml:"users"` // user IDs
	UseDiscordPermission *bool    `yaml:"use_discord_permission"`
}

// PermissionFallback reports whether Discord's Administrator permission counts as admin
// (default true when unset).
func (a AdminConfig) PermissionFallback() bool {
	return a.UseDiscordPermission == nil || *a.UseDiscordPermission
}

// Command maps a Discord message trigger (matched on the first word) to an RCON command
// the bridge runs, posting the reply back. Admins choose exactly which commands exist.
// Rcon may be multiline (e.g. a /silent-command script). Admin gates it to Discord admins.
type Command struct {
	Trigger     string `yaml:"trigger"`
	Rcon        string `yaml:"rcon"`
	Admin       bool   `yaml:"admin"`
	Args        bool   `yaml:"args"`         // opt-in: interpolate {args}/{1}.../{user} from the message
	UsageHint   string `yaml:"usage_hint"`   // shown instead of generic "Usage:" when args are missing
	DiscordLink bool   `yaml:"discord_link"` // when typed with no code, initiate the Discord→game reverse linking flow
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

	// Allow ${ENV} and a leading ~/ in the events path and RCON address so configs stay
	// portable and don't need to hardcode machine-specific values.
	c.Factorio.EventsFile = expandPath(c.Factorio.EventsFile)
	c.Factorio.RCON.Address = os.ExpandEnv(c.Factorio.RCON.Address)
	c.LogFile = expandPath(c.LogFile)

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
	if c.Factorio.SFTP.PasswordEnv != "" {
		c.Factorio.SFTP.Password = os.Getenv(c.Factorio.SFTP.PasswordEnv)
	}
	if c.ControlAPI.Enabled && c.ControlAPI.Listen == "" {
		c.ControlAPI.Listen = "127.0.0.1:7777"
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	c.FilePath = path
	return &c, nil
}

func (c *Config) validate() error {
	if c.Transport != "local" && c.Transport != "sftp" {
		return fmt.Errorf("transport %q not supported (use \"local\" or \"sftp\")", c.Transport)
	}
	if c.Factorio.EventsFile == "" {
		return fmt.Errorf("factorio.events_file is required")
	}
	if c.Transport == "sftp" {
		s := c.Factorio.SFTP
		if s.Host == "" || s.User == "" {
			return fmt.Errorf("sftp transport requires factorio.sftp.host and factorio.sftp.user")
		}
		if s.KeyPath == "" && s.Password == "" {
			return fmt.Errorf("sftp transport requires factorio.sftp.key_path or a password")
		}
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
	for i, cmd := range c.Discord.Commands {
		if cmd.Trigger == "" || cmd.Rcon == "" {
			return fmt.Errorf("discord.commands[%d] needs both trigger and rcon", i)
		}
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
		LogFile:   expandPath(os.Getenv("ODB_LOG_FILE")),
		Factorio: FactorioConfig{
			RCON: RCONConfig{
				Address:     os.Getenv("ODB_RCON_ADDRESS"),
				PasswordEnv: "FACTORIO_RCON_PASSWORD",
				Password:    os.Getenv("FACTORIO_RCON_PASSWORD"),
			},
			EventsFile:         expandPath(os.Getenv("ODB_EVENTS_FILE")),
			LinksFile:          expandPath(os.Getenv("ODB_LINKS_FILE")),
			RequiredModVersion: os.Getenv("ODB_REQUIRED_MOD_VERSION"),
			SFTP: SFTPConfig{
				Host:           os.Getenv("ODB_SFTP_HOST"),
				User:           os.Getenv("ODB_SFTP_USER"),
				KeyPath:        os.Getenv("ODB_SFTP_KEY_PATH"),
				PasswordEnv:    "SFTP_PASSWORD",
				Password:       os.Getenv("SFTP_PASSWORD"),
				KnownHostsPath: os.Getenv("ODB_SFTP_KNOWN_HOSTS"),
			},
		},
		Discord: DiscordConfig{
			TokenEnv:                "DISCORD_BOT_TOKEN",
			Token:                   os.Getenv("DISCORD_BOT_TOKEN"),
			GuildID:                 os.Getenv("ODB_DISCORD_GUILD_ID"),
			Embed:                   parseBool(os.Getenv("ODB_EMBED")),
			AnnounceStatus:          parseBool(os.Getenv("ODB_ANNOUNCE_STATUS")),
			ChannelTopicStatus:      optBool(os.Getenv("ODB_CHANNEL_TOPIC_STATUS")),
			StatusPlayerJoinedEvent: os.Getenv("ODB_STATUS_PLAYER_JOINED_EVENT"),
			StatusPlayerLeftEvent:   os.Getenv("ODB_STATUS_PLAYER_LEFT_EVENT"),
			LinkedRoleID:            os.Getenv("ODB_LINKED_ROLE_ID"),
			LinkedNickname:          os.Getenv("ODB_LINKED_NICKNAME"),
			Admins: AdminConfig{
				Roles:                splitCSV(os.Getenv("ODB_ADMIN_ROLES")),
				Users:                splitCSV(os.Getenv("ODB_ADMIN_USERS")),
				UseDiscordPermission: optBool(os.Getenv("ODB_ADMIN_USE_DISCORD_PERMISSION")),
			},
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

	cmds, err := commandsFromEnv()
	if err != nil {
		return nil, err
	}
	c.Discord.Commands = cmds

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

// commandsFromEnv parses ODB_COMMANDS ("!trigger=/rcon command;!t2=/cmd2").
func commandsFromEnv() ([]Command, error) {
	raw := os.Getenv("ODB_COMMANDS")
	if raw == "" {
		return nil, nil
	}
	var cmds []Command
	for _, entry := range strings.Split(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		trig, rc, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("ODB_COMMANDS entry %q must be trigger=rcon_command", entry)
		}
		cmds = append(cmds, Command{Trigger: strings.TrimSpace(trig), Rcon: strings.TrimSpace(rc)})
	}
	return cmds, nil
}

// splitCSV splits a comma-separated env value into trimmed, non-empty items.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// optBool parses an optional bool env value into *bool (nil if unset/invalid).
func optBool(s string) *bool {
	if s == "" {
		return nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return nil
	}
	return &b
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
