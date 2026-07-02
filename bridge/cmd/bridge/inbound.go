package main

import (
	"encoding/json"

	"github.com/bits-orio/open-discord-bridge/bridge/internal/rcon"
)

// incomingCommands builds one or more "/odb-incoming {json}" RCON commands for a
// Discord message. gorcon rejects any command over rcon.MaxCommandLen outright (see
// bridge/internal/rcon), so a long Discord message would otherwise be silently dropped.
// Instead, the message is split across multiple /odb-incoming calls, each within budget.
//
// Splitting (rather than truncating) is safe here because the companion mod's
// odb-incoming handler (control.lua's handle_incoming) treats every call as an
// independent, complete chat line — a long message becomes consecutive
// "[Discord] user: ..." lines in-game instead of losing content to truncation.
func incomingCommands(user, userID, message, channel string) []string {
	const prefix = "/odb-incoming "

	build := func(msg string) string {
		payload, _ := json.Marshal(map[string]string{
			"user":    user,
			"user_id": userID,
			"message": msg,
			"channel": channel,
		})
		return prefix + string(payload)
	}

	if full := build(message); len(full) <= rcon.MaxCommandLen {
		return []string{full}
	}

	// Fixed cost of everything but the message text (prefix, JSON structure, user/
	// user_id/channel). Used as a first guess for each chunk's size; shrunk below if
	// JSON-escaping (quotes, backslashes, control characters) pushes a chunk over budget.
	overhead := len(build(""))
	budget := rcon.MaxCommandLen - overhead
	if budget <= 0 {
		// user/channel alone exceed the budget; nothing sane to split. Return the
		// oversized command as-is — Execute already treats "too long" as a non-fatal,
		// single-send failure rather than tearing down the connection.
		return []string{build(message)}
	}

	var out []string
	remaining := []rune(message)
	for len(remaining) > 0 {
		n := budget
		if n > len(remaining) {
			n = len(remaining)
		}
		for n > 0 && len(build(string(remaining[:n]))) > rcon.MaxCommandLen {
			n--
		}
		if n == 0 {
			break
		}
		out = append(out, build(string(remaining[:n])))
		remaining = remaining[n:]
	}
	return out
}
