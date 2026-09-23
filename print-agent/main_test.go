package main

import (
	"testing"
	"time"
)

func TestParseStatusReply(t *testing.T) {
	tests := []struct {
		name  string
		query []byte
		reply []byte
		want  byte
		ok    bool
	}{
		{name: "dle eot 2", query: []byte{0x1D, 0x04, 0x02}, reply: []byte{0x1D, 0x04, 0x02, 0x20}, want: 0x20, ok: true},
		{name: "dle eot 3", query: []byte{0x1D, 0x04, 0x03}, reply: []byte{0x1D, 0x04, 0x03, 0x00}, want: 0x00, ok: true},
		{name: "dle eot 4", query: []byte{0x1D, 0x04, 0x04}, reply: []byte{0x1D, 0x04, 0x04, 0x44}, want: 0x44, ok: true},
		{name: "short reply", query: []byte{0x1D, 0x04, 0x02}, reply: []byte{0x1D, 0x04, 0x02}, ok: false},
		{name: "prefix mismatch", query: []byte{0x1D, 0x04, 0x02}, reply: []byte{0x1D, 0x04, 0x03, 0x20}, ok: false},
		{name: "empty input", query: nil, reply: nil, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseStatusReply(tt.query, tt.reply)
			if ok != tt.ok {
				t.Fatalf("ok=%v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("status=0x%02x, want 0x%02x", got, tt.want)
			}
		})
	}
}

func TestBuildStatusReply(t *testing.T) {
	tests := []struct {
		name         string
		replies      [3][]byte
		queried      bool
		raw          string
		paperOut     bool
		paperNearEnd bool
		coverOpen    bool
		paused       bool
		errFatal     bool
	}{
		{
			name: "all normal",
			replies: [3][]byte{
				{0x1D, 0x04, 0x02, 0x00},
				{0x1D, 0x04, 0x03, 0x00},
				{0x1D, 0x04, 0x04, 0x00},
			},
			queried: true,
			raw:     "0200/0300/0400",
		},
		{
			name: "n2 paper out",
			replies: [3][]byte{
				{0x1D, 0x04, 0x02, 0x20},
				{0x1D, 0x04, 0x03, 0x00},
				{0x1D, 0x04, 0x04, 0x00},
			},
			queried:  true,
			raw:      "0220/0300/0400",
			paperOut: true,
		},
		{
			name: "cover open and paused",
			replies: [3][]byte{
				{0x1D, 0x04, 0x02, 0x0c},
				{0x1D, 0x04, 0x03, 0x00},
				{0x1D, 0x04, 0x04, 0x00},
			},
			queried:   true,
			raw:       "020c/0300/0400",
			coverOpen: true,
			paused:    true,
		},
		{
			name: "fatal error",
			replies: [3][]byte{
				{0x1D, 0x04, 0x02, 0x00},
				{0x1D, 0x04, 0x03, 0x20},
				{0x1D, 0x04, 0x04, 0x00},
			},
			queried:  true,
			raw:      "0200/0320/0400",
			errFatal: true,
		},
		{
			name: "paper near end",
			replies: [3][]byte{
				{0x1D, 0x04, 0x02, 0x00},
				{0x1D, 0x04, 0x03, 0x00},
				{0x1D, 0x04, 0x04, 0x0c},
			},
			queried:      true,
			raw:          "0200/0300/040c",
			paperNearEnd: true,
		},
		{
			name: "n4 paper out",
			replies: [3][]byte{
				{0x1D, 0x04, 0x02, 0x00},
				{0x1D, 0x04, 0x03, 0x00},
				{0x1D, 0x04, 0x04, 0x60},
			},
			queried:  true,
			raw:      "0200/0300/0460",
			paperOut: true,
		},
		{
			name: "partial invalid replies",
			replies: [3][]byte{
				{0x1D, 0x04, 0x02, 0x00},
				nil,
				{0x1D, 0x04, 0x02, 0x00},
			},
			queried: true,
			raw:     "0200/--/--",
		},
		{
			name:    "all invalid",
			queried: false,
			raw:     "--/--/--",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildStatusReply(tt.replies)
			if got.queried != tt.queried || got.raw != tt.raw || got.paperOut != tt.paperOut ||
				got.paperNearEnd != tt.paperNearEnd || got.coverOpen != tt.coverOpen ||
				got.paused != tt.paused || got.errFatal != tt.errFatal {
				t.Fatalf("status=%+v", got)
			}
		})
	}
}

func TestDialCooldown(t *testing.T) {
	resetDialCooldownForTest()
	addr := "192.0.2.10:9100"

	markDialCooldown(addr)
	remain, ok := isDialCooling(addr)
	if !ok {
		t.Fatalf("dial cooldown should be active")
	}
	if remain <= 0 || remain > dialCooldownTTL {
		t.Fatalf("remain=%s, want within (0,%s]", remain, dialCooldownTTL)
	}

	dialCooldown.Lock()
	dialCooldown.m[addr] = time.Now().Add(-time.Second)
	dialCooldown.Unlock()
	if _, ok := isDialCooling(addr); ok {
		t.Fatalf("expired dial cooldown should be cleared")
	}
}

func TestStatusBlacklist(t *testing.T) {
	resetStatusBlacklistForTest()
	addr := "192.0.2.11:9100"

	if !markStatusBlacklisted(addr) {
		t.Fatalf("first blacklist should report newly added")
	}
	if !isStatusBlacklisted(addr) {
		t.Fatalf("status blacklist should be active")
	}
	if markStatusBlacklisted(addr) {
		t.Fatalf("second blacklist within ttl should not report newly added")
	}

	statusBlacklist.Lock()
	statusBlacklist.m[addr] = time.Now().Add(-time.Second)
	statusBlacklist.Unlock()
	if isStatusBlacklisted(addr) {
		t.Fatalf("expired status blacklist should be cleared")
	}
}

func resetDialCooldownForTest() {
	dialCooldown.Lock()
	defer dialCooldown.Unlock()
	dialCooldown.m = map[string]time.Time{}
}

func resetStatusBlacklistForTest() {
	statusBlacklist.Lock()
	defer statusBlacklist.Unlock()
	statusBlacklist.m = map[string]time.Time{}
}
