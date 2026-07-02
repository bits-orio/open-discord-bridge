package discord

import "unicode/utf8"

// MaxContentLen is Discord's hard limit on message content length, in characters.
// Sending more causes the API call to fail outright (HTTP 400) and the message is
// lost, so every outbound content path caps at this before calling the API.
const MaxContentLen = 2000

const maxContentLen = MaxContentLen

const truncatedSuffix = "...(truncated)"

// Truncate caps content at max runes, marking cuts with a visible suffix. Callers that
// wrap content in fixed decoration (e.g. ``` fences) use this to cap the inner text so
// the decoration survives the outer MaxContentLen cap intact.
func Truncate(content string, max int) string {
	if utf8.RuneCountInString(content) <= max {
		return content
	}

	cut := max - utf8.RuneCountInString(truncatedSuffix)
	if cut < 0 {
		cut = 0
	}

	runes := []rune(content)
	if cut > len(runes) {
		cut = len(runes)
	}
	return string(runes[:cut]) + truncatedSuffix
}

// truncateContent caps content at Discord's message-length limit. Content within the
// limit is returned unchanged; longer content is cut and marked with truncatedSuffix so
// the loss is visible instead of the whole message silently failing to send.
func truncateContent(content string) string {
	return Truncate(content, MaxContentLen)
}
