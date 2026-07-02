package discord

import "unicode/utf8"

// maxContentLen is Discord's hard limit on message content length, in characters.
// Sending more causes the API call to fail outright (HTTP 400) and the message is
// lost, so every outbound content path caps at this before calling the API.
const maxContentLen = 2000

const truncatedSuffix = "...(truncated)"

// truncateContent caps content at Discord's message-length limit. Content within the
// limit is returned unchanged; longer content is cut and marked with truncatedSuffix so
// the loss is visible instead of the whole message silently failing to send.
func truncateContent(content string) string {
	if utf8.RuneCountInString(content) <= maxContentLen {
		return content
	}

	cut := maxContentLen - utf8.RuneCountInString(truncatedSuffix)
	if cut < 0 {
		cut = 0
	}

	runes := []rune(content)
	if cut > len(runes) {
		cut = len(runes)
	}
	return string(runes[:cut]) + truncatedSuffix
}
