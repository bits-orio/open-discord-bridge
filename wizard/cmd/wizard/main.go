// Command wizard is the interactive setup CLI for the Open Discord Bridge: it validates
// your bot token, helps you add the bot and pick a channel, and writes bridge.yaml + .env.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bits-orio/open-discord-bridge/wizard"
)

func main() {
	out := flag.String("out", "bridge", "directory to write bridge.yaml and .env into")
	flag.Parse()

	in := bufio.NewScanner(os.Stdin)

	fmt.Println("== Open Discord Bridge — setup wizard ==")

	// 1. Token (env or prompt) → validate.
	token := os.Getenv("DISCORD_BOT_TOKEN")
	if token == "" {
		token = prompt(in, "Paste your Discord bot token")
	}
	bot, err := wizard.Connect(token)
	if err != nil {
		fail("%v", err)
	}
	fmt.Printf("✓ Connected as bot %q\n\n", bot.Name)

	// 2. Invite the bot.
	fmt.Println("Add the bot to your server (open this URL, pick the server, Authorize):")
	fmt.Println("  " + bot.InviteURL())
	prompt(in, "Press Enter once you've added it")

	// 3. Pick the guild (poll until the bot is in one).
	guild := pickGuild(in, bot)

	// 4. Pick the channel.
	channels, err := bot.TextChannels(guild.ID)
	if err != nil {
		fail("list channels: %v", err)
	}
	if len(channels) == 0 {
		fail("no text channels found in %q", guild.Name)
	}
	ch := channels[pickIndex(in, "Pick a channel", channelLabels(channels))]

	// 5. Factorio details.
	transport := strings.ToLower(promptDefault(in, "Transport (local/sftp)", "local"))
	if transport != "sftp" {
		transport = "local"
	}

	rcon := promptDefault(in, "Factorio RCON address", "127.0.0.1:27015")

	var (
		sftpHost, sftpUser, sftpKeyPath, sftpKnownHosts, sftpPassword string
		eventsLabel, eventsDefault                                    string
	)
	if transport == "sftp" {
		sftpHost = prompt(in, "SFTP host:port (e.g. panel.host:2022)")
		sftpUser = prompt(in, "SFTP username (e.g. user.serverid)")
		useKey := strings.ToLower(promptDefault(in, "Use an SSH private key instead of a password? (y/N)", "n"))
		if useKey == "y" || useKey == "yes" {
			sftpKeyPath = prompt(in, "Path to private key file")
		} else {
			sftpPassword = prompt(in, "SFTP password")
		}
		sftpKnownHosts = promptDefault(in, "known_hosts file (blank to skip host-key check)", "")
		eventsLabel = "Events file path (relative to SFTP root)"
		eventsDefault = "script-output/open-discord-bridge/events.jsonl"
	} else {
		eventsLabel = "Events file path"
		eventsDefault = "${HOME}/factorio/script-output/open-discord-bridge/events.jsonl"
	}
	events := promptDefault(in, eventsLabel, eventsDefault)
	rconPass := prompt(in, "Factorio RCON password")

	// 6. Write config + env.
	yamlText, err := wizard.RenderBridgeYAML(wizard.ConfigParams{
		Transport:   transport,
		RconAddress: rcon, EventsFile: events,
		GuildID: guild.ID, ChannelID: ch.ID,
		SFTPHost: sftpHost, SFTPUser: sftpUser,
		SFTPKeyPath: sftpKeyPath, SFTPKnownHosts: sftpKnownHosts,
	})
	if err != nil {
		fail("render config: %v", err)
	}
	cfgPath := filepath.Join(*out, "bridge.yaml")
	envPath := filepath.Join(*out, ".env")
	writeFile(in, cfgPath, yamlText, 0o644)
	envContent := fmt.Sprintf("DISCORD_BOT_TOKEN=%s\nFACTORIO_RCON_PASSWORD=%s\n", token, rconPass)
	if sftpPassword != "" {
		envContent += fmt.Sprintf("SFTP_PASSWORD=%s\n", sftpPassword)
	}
	writeFile(in, envPath, envContent, 0o600)

	fmt.Printf("\n✓ Wrote %s and %s\n", cfgPath, envPath)
	fmt.Println("\nNext:")
	fmt.Println("  1. Install the companion mod on your Factorio server: drop the")
	fmt.Println("     open-discord-bridge_<version>.zip into its mods/ folder (or install it")
	fmt.Println("     from the in-game mod browser).")
	fmt.Println("  2. Make sure the server has RCON enabled (--rcon-port / --rcon-password).")
	fmt.Println("  3. Run the bridge — it does NOT auto-load .env, so source it first:")
	fmt.Printf("       set -a; . %s; set +a\n", envPath)
	fmt.Printf("       ./odb-bridge -config %s\n", cfgPath)
	fmt.Println("  4. Type in your channel / in-game to confirm the bridge works.")
	fmt.Println("\n  Full step-by-step guide: QUICKSTART.md")
}

func pickGuild(in *bufio.Scanner, bot *wizard.Bot) wizard.Guild {
	for {
		guilds, err := bot.Guilds()
		if err != nil {
			fail("list guilds: %v", err)
		}
		if len(guilds) == 0 {
			fmt.Println("The bot isn't in any server yet.")
			prompt(in, "Add it with the URL above, then press Enter to retry")
			continue
		}
		labels := make([]string, len(guilds))
		for i, g := range guilds {
			labels[i] = g.Name
		}
		return guilds[pickIndex(in, "Pick your server", labels)]
	}
}

func channelLabels(chs []wizard.Channel) []string {
	out := make([]string, len(chs))
	for i, c := range chs {
		out[i] = "#" + c.Name
	}
	return out
}

// ── small prompt helpers ──────────────────────────────────────────────────────

func prompt(in *bufio.Scanner, label string) string {
	fmt.Printf("%s: ", label)
	if !in.Scan() {
		os.Exit(1)
	}
	return strings.TrimSpace(in.Text())
}

func promptDefault(in *bufio.Scanner, label, def string) string {
	fmt.Printf("%s [%s]: ", label, def)
	if !in.Scan() {
		os.Exit(1)
	}
	if v := strings.TrimSpace(in.Text()); v != "" {
		return v
	}
	return def
}

func pickIndex(in *bufio.Scanner, label string, options []string) int {
	for i, o := range options {
		fmt.Printf("  %d) %s\n", i+1, o)
	}
	for {
		n, err := strconv.Atoi(prompt(in, label+" (number)"))
		if err == nil && n >= 1 && n <= len(options) {
			return n - 1
		}
		fmt.Println("  enter a number from the list")
	}
}

func writeFile(in *bufio.Scanner, path, content string, mode os.FileMode) {
	if _, err := os.Stat(path); err == nil {
		if a := strings.ToLower(promptDefault(in, "Overwrite "+path+"? (y/N)", "n")); a != "y" && a != "yes" {
			fmt.Printf("  skipped %s\n", path)
			return
		}
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		fail("write %s: %v", path, err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
