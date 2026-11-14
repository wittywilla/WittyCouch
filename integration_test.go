// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

package wittycouch

import (
	"os"
	"testing"

	"github.com/wittywilla/wittymodule"
)

// integration.json is what a server reads strictly at install: it passes WittyModule's own check.
func TestIntegrationFile(t *testing.T) {
	m, err := wittymodule.ParseManifest(ManifestJSON)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("integration.json")
	if err != nil {
		t.Fatal(err)
	}
	in, err := wittymodule.ParseIntegration(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range in.Check(m) {
		t.Errorf("%v", p)
	}
	if err := in.Validate(m); err != nil {
		t.Error(err)
	}
}
