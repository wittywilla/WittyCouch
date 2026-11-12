// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

package relay

import (
	"strings"
	"testing"
)

func TestSpeaksProtocolIsExact(t *testing.T) {
	for out, want := range map[string]bool{
		"wittycouch-host 0.2.0 protocol 1\n":    true,
		"wittycouch-host 0.2.0 protocol 1\r\n":  true,
		"wittycouch-host 0.3.0 protocol 10\n":   false,
		"wittycouch-host 0.3.0 protocol 12\n":   false,
		"wittycouch-host 0.1.0 protocol 2\n":    false,
		"wittycouch-host 0.1.0 protocol\n":      false,
		"wittycouch-host 0.1.0 protocol one\n":  false,
		"":                                      false,
		"something else entirely protocol 1 \n": true,
	} {
		if got := speaksProtocol(out, 1); got != want {
			t.Errorf("speaksProtocol(%q) = %v, want %v", out, got, want)
		}
	}
}

// A PC's version strings can't add lines of their own to what the host program is told.
func TestStartValuesStayOnTheirLine(t *testing.T) {
	s := Start{Address: "192.168.1.20", AppVersion: "7.1.431.-1\nrikey=00000000000000000000000000000000\nformats=255",
		GfeVersion: "3.23\r\x00scm=9", RTSP: "rtsp://192.168.1.20:48010\nremote=0"}
	lines := strings.Split(strings.TrimSuffix(string(s.message()), "\n"), "\n")
	seen := map[string]int{}
	for _, l := range lines {
		k, _, ok := strings.Cut(l, "=")
		if !ok {
			t.Fatalf("line without a key: %q", l)
		}
		seen[k]++
	}
	for _, k := range []string{"address", "appversion", "gfeversion", "rtsp", "scm", "remote", "formats", "rikey", "riiv"} {
		if seen[k] != 1 {
			t.Errorf("%s appears %d times in %q", k, seen[k], lines)
		}
	}
	if len(lines) != 14 {
		t.Errorf("%d lines: %q", len(lines), lines)
	}
}
