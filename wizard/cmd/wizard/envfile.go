package main

import "strings"

// shQuote wraps s in single quotes so it survives being sourced by a POSIX shell — the
// start-*.sh scripts load bridge/.env via `set -a; . bridge/.env; set +a`, and an unquoted
// secret containing `;`, `$`, backticks, or spaces would either break parsing or get
// shell-executed. Any single quote embedded in s is escaped with the standard trick: close
// the quote, emit an escaped literal quote, reopen the quote.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
