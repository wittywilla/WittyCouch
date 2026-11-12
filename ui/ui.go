// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

// Package ui is this module's server-driven UI (module API 4; WittyMediaServer
// docs/SDUI-MODULES.md). It adds no pages, rows or menu entries of its own: its screens are the
// server's (Settings, Game PCs, and the game player). It carries its theme scope, so anything of
// it the apps draw takes the games colours, and an administrator can recolour it.
package ui

import "github.com/wittywilla/wittymodule/sdui"

// Module is this module's ID.
const Module = "wittycouch"

// UI is the module's SDUI.
func UI() *sdui.Module {
	return sdui.New(Module, "Game PCs").Colours(map[string]string{"accent": "#7F46B8"})
}
