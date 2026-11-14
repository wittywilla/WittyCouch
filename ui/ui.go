// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

// Package ui is this module's server-driven UI (module API 5; WittyMediaServer docs/SDUI-MODULES.md):
// the Game PCs pages every app draws, the Apple TV and Android TV apps among them, which have no web
// view for the module's web part. Their sources are answered by WittyCouch's service (package api),
// and every row that does something runs one of WittyCouch's own commands, on its own routes.
// It carries its theme scope, so it takes the games colours, and an administrator can recolour it.
package ui

import (
	"github.com/wittywilla/wittymodule/sdui"
	core "github.com/wittywilla/wittymodule/sdui/core"
)

// Module is this module's ID.
const Module = "wittycouch"

// Pages.
const (
	// PageHosts is a person's Game PCs, and the PCs on this server's network to add.
	PageHosts = Module
	// PageHost is one PC (?item=<its id>&name=<its name>): wake, pair, end what's running, its games.
	PageHost = Module + "-host"
)

// Sources, answered by the service.
const (
	SourceHosts = Module + ".hosts" // the person's PCs, each opening its page
	SourceFound = Module + ".found" // PCs answering on this server's network, each with Add
	SourceHost  = Module + ".host"  // one PC: how it is, and what can be done to it
	SourceApps  = Module + ".apps"  // what one PC offers
)

// Commands: WittyCouch's own routes, which check again who is asking.
const (
	CommandAdd        = Module + ".host.add"
	CommandWake       = Module + ".host.wake"
	CommandQuit       = Module + ".host.quit"
	CommandPair       = Module + ".host.pair"
	CommandPairCancel = Module + ".host.pairCancel"
	CommandRemove     = Module + ".host.remove"
)

// Settings keys (the server's; read by the service with the host API's Settings).
const (
	SettingAccess      = "access"      // adults, everyone, admins
	SettingPrivate     = "private"     // PCs on this server's own network
	SettingInternet    = "internet"    // PCs over the internet
	SettingCustomPorts = "customPorts" // ports other than Sunshine's usual one, for people who aren't admins
	SettingMaxStreams  = "maxStreams"
	SettingUDPPort     = "udpPort"
	SettingPublicIP    = "publicIp"
	SettingSTUN        = "stun"
)

// Commands are WittyCouch's own commands, as sdui.json declares them.
func Commands() []core.CommandSpec {
	routes := "/api/modules/" + Module
	return []core.CommandSpec{
		{Op: CommandAdd, Label: "Add this PC", Method: "POST", Path: routes + "/hosts", Body: `{"address":"{item}"}`,
			Description: "Game PCs: adds a PC running Sunshine to the person's own list (item: its address, as the network found it). It has to answer as Sunshine."},
		{Op: CommandWake, Label: "Wake it up", Method: "POST", Path: routes + "/hosts/{item}/wake",
			Description: "Game PCs: sends a wake-up signal to one of the person's PCs (item: its ID)."},
		{Op: CommandQuit, Label: "End what's running", Method: "POST", Path: routes + "/hosts/{item}/quit",
			Description: "Game PCs: ends the game running on one of the person's PCs (item: its ID)."},
		{Op: CommandPair, Label: "Pair this PC", Method: "POST", Path: routes + "/hosts/{item}/pair",
			Description: "Game PCs: starts pairing one of the person's PCs (item: its ID). The PC's page shows the PIN to type into Sunshine."},
		{Op: CommandPairCancel, Label: "Stop pairing", Method: "DELETE", Path: routes + "/hosts/{item}/pair",
			Description: "Game PCs: stops waiting for a PIN (item: the PC's ID)."},
		{Op: CommandRemove, Label: "Remove", Method: "DELETE", Path: routes + "/hosts/{item}",
			Description: "Game PCs: takes a PC off the person's list (item: its ID). The PC keeps this server on its own list until someone removes it there."},
	}
}

func sources() []sdui.SourceSpec {
	host := sdui.PropSpec{Name: "host", Type: "ref", Label: "PC", Required: true, Help: "The PC's ID, $item on its page."}
	return []sdui.SourceSpec{
		{Type: SourceHosts, Label: "Your game PCs", Icon: "devices", Title: "Your PCs",
			Description: "The person's own PCs running Sunshine, each with how it is (online, running a game, asleep, needs pairing), opening its page. Nothing for a profile this isn't turned on for.",
			Yields:      []string{"view"}, CardStyle: "tile"},
		{Type: SourceFound, Label: "PCs on this network", Icon: "search", Title: "Find PCs on this network",
			Description: "The PCs running Sunshine that answer on this server's network, each with a row that adds it (or says it was added), so no address has to be typed. It looks for a few seconds.",
			Yields:      []string{"view"}, CardStyle: "tile"},
		{Type: SourceHost, Label: "A game PC", Icon: "devices", Title: "",
			Description: "One of the person's PCs: how it is, then what can be done: wake it, end what's running, pair it (with the PIN to type into Sunshine while it waits), remove it.",
			Params:      []sdui.PropSpec{host}, Yields: []string{"view"}, CardStyle: "tile"},
		{Type: SourceApps, Label: "A game PC's games", Icon: "game", Title: "Games",
			Description: "What a paired PC offers, its desktop last. Playing in the page is for browsers: on a TV or phone, a row says to open Moonlight, where the PC appears once it is paired with Moonlight too.",
			Params:      []sdui.PropSpec{host}, Yields: []string{"view"}, CardStyle: "tile"},
	}
}

// UI is the module's SDUI.
func UI() *sdui.Module {
	m := sdui.New(Module, "Game PCs").Colours(map[string]string{"accent": "#7F46B8"})
	for _, s := range sources() {
		m.AddSource(s)
	}
	m.Commands = Commands()
	list := func(id, title string, src sdui.Source, kv ...any) sdui.Section {
		return sdui.Sec(id, "List", sdui.Props(append([]any{"title", title, "focusable", true}, kv...)...), src)
	}
	page := func(id, title string, sections ...sdui.Section) sdui.Page {
		return sdui.Page{ID: id, Kind: "custom", Title: title, Icon: "devices", Scope: Module, Sections: sections}
	}

	hosts := page(PageHosts, "Game PCs",
		sdui.Sec("intro", "Text", sdui.Props("text", "Play the games on a PC running Sunshine. Each PC pairs once, with a PIN typed on that PC, and your pairings are yours alone."), nil),
		list("pcs", "Your PCs", sdui.Src(SourceHosts), "empty", "message", "emptyText", "No PCs yet. Find one on this network below."),
		list("found", "Find PCs on this network", sdui.Src(SourceFound), "empty", "message", "emptyText", "None answered. A PC elsewhere is added by its address, in a browser."),
		sdui.Notice("needs", "info", "The PC needs Sunshine, a free program its owner installs. A PC over the internet is added by its address, in a browser."))
	hosts.Description = "A person's Game PCs on every app: their PCs, opening each one's page, and the PCs on this server's network to add with a press."
	m.AddPage(hosts)

	one := page(PageHost, "{title}",
		list("pc", "", sdui.Src(SourceHost, "host", "$item"), "empty", "message", "emptyText", "This PC isn't on your list."),
		list("apps", "Games", sdui.Src(SourceApps, "host", "$item"), "empty", "message", "emptyText", "Its games appear once it's paired and on."))
	one.Description = "One of a person's Game PCs: wake it, end what's running, pair it with the PIN to type into Sunshine, remove it, and its games. Opened with ?item=<its id>&name=<its name>."
	m.AddPage(one)

	admin := func(key, kind, label, help string, def any, kv ...func(*sdui.SettingRow)) sdui.SettingRow {
		r := sdui.SettingRow{Key: key, Kind: kind, Label: label, Help: help, Default: def}
		for _, f := range kv {
			f(&r)
		}
		return r
	}
	m.AddSettings(sdui.SettingsPage{ID: Module, Title: "Game PCs", Icon: "devices", Groups: []sdui.SettingsGroup{
		{Title: "Who may connect", Footer: "This has the server connect wherever a person says. It only ever speaks this one protocol. Administrators can always.", Rows: []sdui.SettingRow{
			admin(SettingAccess, "enum", "Who can use it", "A PC's desktop is outside anything age limits can reach, so Kids and Guest profiles and limited accounts start without this.", "adults",
				func(r *sdui.SettingRow) {
					r.Options = []sdui.EnumOption{{Value: "adults", Label: "Adult profiles with no limits set"}, {Value: "everyone", Label: "Everyone, Kids and Guest profiles too"}, {Value: "admins", Label: "Admins only"}}
				}),
			admin(SettingPrivate, "bool", "PCs on this server's own network", "", true),
			admin(SettingInternet, "bool", "PCs over the internet, such as a friend's", "", true),
			admin(SettingCustomPorts, "bool", "Ports other than Sunshine's usual one", "Off makes it harder to use this to look for what's open on a network.", false),
			admin(SettingMaxStreams, "int", "Streams at once", "Each stream passes through this server at up to its full bitrate.", 2,
				func(r *sdui.SettingRow) { r.Min, r.Max = sdui.Int(1), sdui.Int(16) }),
		}},
		{Title: "Reaching browsers away from home", Footer: "Nothing needed at home. Never forward Sunshine's own ports, and never port 47990.", Rows: []sdui.SettingRow{
			admin(SettingUDPPort, "int", "UDP port", "0: any free port. Choose one to forward on your router, or to publish from a container.", 0,
				func(r *sdui.SettingRow) { r.Min, r.Max = sdui.Int(0), sdui.Int(65535) }),
			admin(SettingPublicIP, "string", "This server's public address", "", "", func(r *sdui.SettingRow) { r.MaxLength = 100 }),
			admin(SettingSTUN, "string", "STUN server", "Empty: nothing leaves your network.", "", func(r *sdui.SettingRow) { r.MaxLength = 300 }),
		}},
	}})
	return m
}
