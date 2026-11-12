// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

package relay

import "testing"

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
