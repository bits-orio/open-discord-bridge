package discord

import (
	"errors"
	"strings"
	"testing"
)

// TestRunSlashInteractionDefersBeforeRunningCommand covers fix #2(a): the interaction must
// be acknowledged (deferred) before the (possibly slow) command runs, so a slow RCON
// round-trip can never blow Discord's 3s initial-response window.
func TestRunSlashInteractionDefersBeforeRunningCommand(t *testing.T) {
	var order []string

	ack := func() error { order = append(order, "ack"); return nil }
	runCmd := func(SlashInvocation) (string, error) {
		order = append(order, "run")
		return "ok", nil
	}
	edit := func(content string) error { order = append(order, "edit:"+content); return nil }

	runSlashInteraction(SlashInvocation{Name: "test"}, runCmd, ack, edit)

	want := []string{"ack", "run", "edit:ok"}
	if len(order) != len(want) {
		t.Fatalf("got %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("got %v, want %v", order, want)
		}
	}
}

// TestRunSlashInteractionPropagatesError covers fix #2(b): a real RCON/command error must
// reach the user-visible reply, not get collapsed into a generic "Done." that masks the
// failure as a success.
func TestRunSlashInteractionPropagatesError(t *testing.T) {
	var got string
	ack := func() error { return nil }
	runCmd := func(SlashInvocation) (string, error) { return "", errors.New("factorio unreachable") }
	edit := func(content string) error { got = content; return nil }

	runSlashInteraction(SlashInvocation{Name: "test"}, runCmd, ack, edit)

	if got == "Done." {
		t.Fatal("error was collapsed into a generic \"Done.\" — real failure is masked as success")
	}
	if want := "factorio unreachable"; !strings.Contains(got, want) {
		t.Fatalf("reply %q does not surface the underlying error %q", got, want)
	}
}

// TestRunSlashInteractionEmptyReplyBecomesDone covers the legitimate "ran fine, nothing to
// report" case, which should still show something instead of leaving Discord's deferred ack
// unresolved.
func TestRunSlashInteractionEmptyReplyBecomesDone(t *testing.T) {
	var got string
	ack := func() error { return nil }
	runCmd := func(SlashInvocation) (string, error) { return "", nil }
	edit := func(content string) error { got = content; return nil }

	runSlashInteraction(SlashInvocation{Name: "test"}, runCmd, ack, edit)

	if got != "Done." {
		t.Fatalf("got %q, want \"Done.\"", got)
	}
}

// TestRunSlashInteractionBailsIfAckFails covers the case where even the deferred ack fails
// (e.g. the interaction already expired): the command must not run and no edit should be
// attempted, since an edit against a failed ack would itself just fail.
func TestRunSlashInteractionBailsIfAckFails(t *testing.T) {
	ranCmd, ranEdit := false, false
	ack := func() error { return errors.New("ack failed") }
	runCmd := func(SlashInvocation) (string, error) { ranCmd = true; return "", nil }
	edit := func(content string) error { ranEdit = true; return nil }

	runSlashInteraction(SlashInvocation{Name: "test"}, runCmd, ack, edit)

	if ranCmd || ranEdit {
		t.Fatalf("want no command run and no edit attempted after a failed ack (ranCmd=%v ranEdit=%v)", ranCmd, ranEdit)
	}
}
