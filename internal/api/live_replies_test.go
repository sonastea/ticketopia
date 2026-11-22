package api

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Each account has its own window; the window rolls over; concurrent checks
// never exceed the limit; and memory stops growing at the account cap.
func TestReplyChecksLimitEachAccountPerWindow(t *testing.T) {
	r := &replyChecks{limit: 2}
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	for _, account := range []string{"a", "a"} {
		if ok, _ := r.allow(account, now); !ok {
			t.Fatal("within the limit")
		}
	}
	if ok, wait := r.allow("a", now.Add(10*time.Second)); ok || wait != 50 {
		t.Fatal("over the limit should wait out the window", ok, wait)
	}
	if ok, _ := r.allow("b", now.Add(10*time.Second)); !ok {
		t.Fatal("another account shares no budget")
	}
	if ok, _ := r.allow("a", now.Add(time.Minute)); !ok {
		t.Fatal("a new window should reset the account")
	}

	r = &replyChecks{limit: 10}
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range 100 {
		wg.Go(func() {
			if ok, _ := r.allow("busy", now); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if allowed != 10 {
		t.Fatal("concurrent checks allowed", allowed)
	}

	r = &replyChecks{limit: 1}
	for i := range maxReplyCheckAccounts {
		r.allow(fmt.Sprint(i), now)
	}
	if ok, _ := r.allow("late", now); ok || len(r.counts) != maxReplyCheckAccounts {
		t.Fatal("the account map grew past its cap", len(r.counts))
	}
	if ok, _ := r.allow("late", now.Add(time.Minute)); !ok {
		t.Fatal("the next window should admit new accounts again")
	}
}

// Unset, empty or blank uses the default; anything else must be in range.
func TestLiveReplyChecksFromEnv(t *testing.T) {
	for raw, want := range map[string]int{"": DefaultLiveReplyChecks, "  ": DefaultLiveReplyChecks, "0": 0, " 45 ": 45, "6000": 6000} {
		t.Setenv("DISCUSSION_LIVE_CHECKS_PER_MINUTE", raw)
		if got, err := LiveReplyChecksFromEnv(); err != nil || got != want {
			t.Errorf("%q: got %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"-1", "6001", "thirty"} {
		t.Setenv("DISCUSSION_LIVE_CHECKS_PER_MINUTE", raw)
		if _, err := LiveReplyChecksFromEnv(); err == nil {
			t.Errorf("%q was accepted", raw)
		}
	}
}
