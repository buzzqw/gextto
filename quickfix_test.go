package gextto

import (
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/utils"
)

func TestRecoverGoroutineKeepsDaemonAlive(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer recoverGoroutine("test")
		defer wg.Done()
		panic("boom")
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deferred cleanup did not run after the panic")
	}
}

func TestHookTitleStaysOneArgument(t *testing.T) {
	hook := DefaultEventHook()
	hook.Program = "/usr/bin/printf"
	hook.Args = `"%s|" {title}`
	run, err := RunHook(hook, "download_started", map[string]any{
		"title": `Show "Name" --rm S01E01`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Stdout != `Show "Name" --rm S01E01|` {
		t.Fatalf("title split or altered: %q", run.Stdout)
	}
}

func TestHookTimeoutKillsChildren(t *testing.T) {
	hook := DefaultEventHook()
	hook.Program = "/usr/bin/sh"
	// The child keeps stdout open after the shell itself is killed.
	hook.Args = `-c "sleep 30 & sleep 30"`
	hook.TimeoutSecs = 1
	started := time.Now()
	if _, err := RunHook(hook, "download_started", map[string]any{}); err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("timeout not enforced: %s", elapsed)
	}
}

func TestNotificationErrorsHideTokens(t *testing.T) {
	err := redactRequestError(&url.Error{
		Op:  "Post",
		URL: "https://api.telegram.org/bot123456:ABC-def_secret/sendMessage",
		Err: errors.New("dial tcp: i/o timeout"),
	})
	if strings.Contains(err.Error(), "ABC-def_secret") || !strings.Contains(err.Error(), "api.telegram.org") {
		t.Fatalf("unexpected redaction: %v", err)
	}
	redacted := utils.RedactURLSecrets(`Post "https://api.telegram.org/bot123456:ABC-def_secret/sendMessage": EOF`)
	if strings.Contains(redacted, "ABC-def_secret") {
		t.Fatalf("token not redacted: %s", redacted)
	}
}

func TestSMTPTimeoutOnSilentServer(t *testing.T) {
	// 10.255.255.1 is a non-routable address: the dial hangs until the timeout.
	started := time.Now()
	err := sendMailWithTimeout("10.255.255.1:25", "10.255.255.1", nil, "a@b", []string{"c@d"}, []byte("x"), time.Second)
	if err == nil {
		t.Fatal("expected an error")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("SMTP delivery not bounded: %s", time.Since(started))
	}
}

func TestWorkerRestartBackoffGrowsAndCaps(t *testing.T) {
	previous := workerRestartDelay
	workerRestartDelay = 5 * time.Second
	defer func() { workerRestartDelay = previous }()
	if got := workerRestartBackoff(0); got != 5*time.Second {
		t.Fatalf("first restart delay = %s", got)
	}
	if got := workerRestartBackoff(3); got != 40*time.Second {
		t.Fatalf("fourth restart delay = %s", got)
	}
	if got := workerRestartBackoff(50); got != workerRestartMaxDelay {
		t.Fatalf("delay not capped: %s", got)
	}
}
