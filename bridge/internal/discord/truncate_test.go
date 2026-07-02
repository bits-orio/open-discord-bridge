package discord

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateContent(t *testing.T) {
	t.Run("under limit is unchanged", func(t *testing.T) {
		short := "hello world"
		if got := truncateContent(short); got != short {
			t.Errorf("truncateContent(%q) = %q, want unchanged", short, got)
		}
	})

	t.Run("exactly at limit is unchanged", func(t *testing.T) {
		exact := strings.Repeat("a", maxContentLen)
		if got := truncateContent(exact); got != exact {
			t.Errorf("truncateContent of exact-length content was modified")
		}
	})

	t.Run("over limit is truncated with suffix and stays within the cap", func(t *testing.T) {
		long := strings.Repeat("a", maxContentLen+500)
		got := truncateContent(long)
		if n := utf8.RuneCountInString(got); n > maxContentLen {
			t.Fatalf("truncated content length = %d, want <= %d", n, maxContentLen)
		}
		if !strings.HasSuffix(got, truncatedSuffix) {
			t.Errorf("truncated content = %q, want suffix %q", got, truncatedSuffix)
		}
	})

	t.Run("multi-byte runes are not split", func(t *testing.T) {
		long := strings.Repeat("é", maxContentLen+500) // 2 bytes/rune in UTF-8
		got := truncateContent(long)
		if !utf8.ValidString(got) {
			t.Fatalf("truncateContent produced invalid UTF-8: %q", got)
		}
		if n := utf8.RuneCountInString(got); n > maxContentLen {
			t.Fatalf("truncated content length = %d, want <= %d", n, maxContentLen)
		}
	})
}
