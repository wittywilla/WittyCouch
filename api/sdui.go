// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

package api

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/wittywilla/wittycouch/gamestream"
	"github.com/wittywilla/wittycouch/ui"
	"github.com/wittywilla/wittymodule/sdui"
	core "github.com/wittywilla/wittymodule/sdui/core"
)

// The sources behind the Game PCs pages every app draws (sdui.json: wittycouch, wittycouch-host),
// the Apple TV and Android TV apps among them, which have no web view for the React pages. Every
// row that does something runs one of WittyCouch's own commands, on its own routes, which check
// again who is asking. Typing an address, and playing in the page, are a browser's.

// SDUIItems answers the server's sdui.items call for WittyCouch's sources.
func SDUIItems(req sdui.ItemsRequest) (sdui.ItemsAnswer, error) {
	if req.Viewer.User <= 0 {
		return sdui.ItemsAnswer{}, nil
	}
	a, err := Get()
	if err != nil {
		return sdui.ItemsAnswer{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return a.items(ctx, req)
}

func (a *App) items(ctx context.Context, req sdui.ItemsRequest) (sdui.ItemsAnswer, error) {
	// The server says what the profile can't do; one with anything held back counts as limited,
	// as the routes are told.
	u := person{ID: req.Viewer.User, Admin: req.Viewer.Admin, Kind: req.Viewer.ProfileKind, Limited: len(req.Viewer.Cannot) > 0}
	if !a.allowed(ctx, u) {
		return sdui.ItemsAnswer{}, nil
	}
	var rows []sdui.Item
	switch req.Source.Type() {
	case ui.SourceHosts:
		hosts, err := a.hostsOf(ctx, u.ID)
		if err != nil {
			return sdui.ItemsAnswer{}, err
		}
		for _, v := range a.viewsOf(ctx, u, hosts) {
			q := url.Values{"item": {strconv.FormatInt(v.ID, 10)}, "name": {v.Name}}
			sub := v.Message
			if v.State == "busy" && v.CurrentGame != "" {
				sub = "Running: " + v.CurrentGame
			}
			rows = append(rows, sdui.Item{ID: "host-" + strconv.FormatInt(v.ID, 10), Kind: "view", Title: v.Name, Subtitle: sub, Trailing: stateWord(v),
				Icon: "devices", Action: &sdui.Action{Type: core.ActNavigate, To: "/page/" + ui.PageHost + "?" + q.Encode()}})
		}
	case ui.SourceFound:
		if !a.tryAttempt(u) {
			return sdui.ItemsAnswer{Items: []sdui.Item{{ID: "later", Kind: "view", Title: "That's a lot of connection attempts", Subtitle: "Try again in a few minutes.", Icon: "warning"}}}, nil
		}
		list, err := a.find(ctx, u)
		if err != nil {
			return sdui.ItemsAnswer{}, nil // already looking, or nothing to look on
		}
		for _, f := range list {
			addr := f.Address
			if f.Port != 0 && f.Port != gamestream.DefaultPort {
				addr = f.Address + ":" + strconv.Itoa(f.Port)
			}
			it := sdui.Item{ID: "found-" + addr, Kind: "view", Title: f.Name, Subtitle: f.Address, Icon: "devices"}
			if f.Added {
				it.Trailing = "Added"
			} else {
				it.Trailing = "Add"
				it.Action = &sdui.Action{Type: core.ActCommand, Op: ui.CommandAdd, Item: addr}
			}
			rows = append(rows, it)
		}
	case ui.SourceHost, ui.SourceApps:
		id, _ := strconv.ParseInt(srcStr(req.Source, "host"), 10, 64)
		h, err := a.hostOf(ctx, u.ID, id)
		if err != nil {
			return sdui.ItemsAnswer{}, nil
		}
		if req.Source.Type() == ui.SourceHost {
			rows = a.hostRows(ctx, u, h)
		} else {
			rows = a.appRows(ctx, u, h, req.Viewer.Platform)
		}
	}
	return pageItems(rows, req.Offset, req.Limit), nil
}

func stateWord(v hostView) string {
	switch {
	case !v.Paired || v.State == "not-paired":
		return "Needs pairing"
	case v.State == "busy":
		return "Playing"
	case v.State == "online":
		return "Online"
	case v.State == "asleep":
		return "Asleep"
	case v.State == "cert-changed":
		return "Not trusted"
	}
	return "Not available"
}

// hostRows are one PC: how it is, then what can be done to it.
func (a *App) hostRows(ctx context.Context, u person, h *pcHost) []sdui.Item {
	v := a.viewOf(ctx, u, h)
	id := strconv.FormatInt(h.ID, 10)
	sub := v.Message
	if v.State == "busy" && v.CurrentGame != "" {
		sub = "Running: " + v.CurrentGame
	}
	rows := []sdui.Item{{ID: "state", Kind: "view", Title: v.Name, Subtitle: sub, Trailing: stateWord(v), Icon: "devices"}}
	if v.CanWake {
		rows = append(rows, sdui.Item{ID: "wake", Kind: "view", Title: "Wake it up", Subtitle: "Sends a wake-up signal. A PC usually takes under a minute, if Wake-on-LAN is on.", Icon: "plug",
			Action: &sdui.Action{Type: core.ActCommand, Op: ui.CommandWake, Item: id}})
	}
	if v.State == "busy" {
		rows = append(rows, sdui.Item{ID: "quit", Kind: "view", Title: "End what's running", Subtitle: v.CurrentGame, Icon: "warning",
			Action: &sdui.Action{Type: core.ActCommand, Op: ui.CommandQuit, Item: id, Confirm: "End what's running on " + v.Name + "?"}})
	}
	p := a.pairingOf(h.ID)
	switch {
	case p.State == "waiting":
		rows = append(rows,
			sdui.Item{ID: "pin", Kind: "view", Title: "PIN " + p.PIN, Subtitle: "On that PC, open Sunshine (https://localhost:47990), go to PIN, and type it. Don't pair over a network you don't trust.", Icon: "info"},
			sdui.Item{ID: "pair-cancel", Kind: "view", Title: "Stop pairing", Icon: "history", Action: &sdui.Action{Type: core.ActCommand, Op: ui.CommandPairCancel, Item: id}})
	case !v.Paired || v.State == "not-paired" || v.State == "cert-changed":
		title := "Pair this PC"
		if v.Paired {
			title = "Pair again"
		}
		sub := "Shows a PIN to type into Sunshine on that PC."
		if p.State == "failed" && p.Message != "" {
			sub = p.Message
		}
		rows = append(rows, sdui.Item{ID: "pair", Kind: "view", Title: title, Subtitle: sub, Icon: "plus",
			Action: &sdui.Action{Type: core.ActCommand, Op: ui.CommandPair, Item: id}})
	}
	rows = append(rows, sdui.Item{ID: "remove", Kind: "view", Title: "Remove " + v.Name, Subtitle: "To finish, remove this device from the paired devices in Sunshine on that PC.", Icon: "warning",
		Action: &sdui.Action{Type: core.ActCommand, Op: ui.CommandRemove, Item: id, Confirm: "Remove " + v.Name + " from your list?"}})
	return rows
}

// appRows are what a paired PC offers, its desktop last. Playing in the page is a browser's, so
// elsewhere each row says how to play it.
func (a *App) appRows(ctx context.Context, u person, h *pcHost, platform string) []sdui.Item {
	if h.Cert == "" {
		return nil
	}
	apps, err := a.appsOf(ctx, u, h, false)
	if err != nil {
		return []sdui.Item{{ID: "unavailable", Kind: "view", Title: "Its games can't be read now", Subtitle: err.Error(), Icon: "warning"}}
	}
	running := a.status(ctx, u, h)
	var games, desktops []sdui.Item
	for _, app := range apps {
		it := sdui.Item{ID: "app-" + strconv.Itoa(app.ID), Kind: "view", Title: app.Title, Icon: "game"}
		if running.State == "busy" && running.currentApp == app.ID {
			it.Trailing = "Running"
		}
		if platform == "web" || platform == "desktop" {
			it.Action = &sdui.Action{Type: core.ActNavigate, To: "/couch/" + strconv.FormatInt(h.ID, 10) + "/play/" + strconv.Itoa(app.ID) + "?t=" + url.QueryEscape(app.Title)}
		} else {
			it.Subtitle = "Play it in Moonlight on this device, once Moonlight is paired with this PC too."
		}
		if app.IsDesktop {
			desktops = append(desktops, it)
		} else {
			games = append(games, it)
		}
	}
	return append(games, desktops...)
}

func pageItems(all []sdui.Item, offset, limit int) sdui.ItemsAnswer {
	total := len(all)
	if offset >= total {
		return sdui.ItemsAnswer{Items: []sdui.Item{}, Total: total}
	}
	all = all[max(offset, 0):]
	more := limit > 0 && len(all) > limit
	if more {
		all = all[:limit]
	}
	return sdui.ItemsAnswer{Items: all, More: more, Total: total}
}

// srcStr reads a source's parameter, which the page may have written as a string or a number.
func srcStr(s sdui.Source, k string) string {
	switch v := s[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	}
	return ""
}
