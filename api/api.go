// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

// Package api is WittyCouch's own HTTP API (module API 5): the routes WittyMediaServer passes to it
// at /api/modules/wittycouch/... once it has checked that someone is signed in (integration.json),
// the sources of its SDUI pages, and its lifecycle. It keeps each person's PCs in WittyCouch's own
// database (package store) and their keys and the PCs' pictures in its own folder, and asks the
// server only what only the server knows, through the host API: its settings, where the WittyCouch
// Host program is, and when an account goes.
//
// This makes the server connect where a person says, which needs care:
//
//   - The connections only speak this one protocol, on its own ports, to fixed paths. Answers are
//     parsed into a few fields, and nothing a far end says is passed back raw, so this can't be
//     used to read other services or as a general network probe.
//   - What was refused and what timed out read the same to anyone but an admin, as the
//     difference is what a port scanner wants.
//   - Attempts are limited per person, and nobody but an admin reaches this machine itself.
//   - Each person's certificate and pairings are their own, kept apart, and never shown.
//   - Who may use it, and which networks it may reach, are an admin's settings.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wittywilla/wittycouch/gamestream"
	"github.com/wittywilla/wittycouch/store"
	"github.com/wittywilla/wittycouch/ui"
	"github.com/wittywilla/wittymodule/host"
	"github.com/wittywilla/wittymodule/service"
)

// DataEnv is where the server says WittyCouch's own folder is.
const DataEnv = "WITTYMODULE_DATA"

// HeaderLimited is "1" on a request from a profile with limits set (age, libraries, screen time).
const HeaderLimited = "X-Witty-Limited"

const (
	attemptsWindow = 10 * time.Minute
	attemptsLimit  = 20
	// maxAppArt: a game's picture from a PC, which has no use for more.
	maxAppArt = 2 << 20
	hostLimit = 32
	// statusLife is how long a PC's state is kept before it's asked again.
	statusLife = 5 * time.Second
	// appsLife is how long a PC's list of games is kept.
	appsLife  = 10 * time.Minute
	wakeEvery = 10 * time.Second
	maxApps   = 500
)

// App is what the routes need: WittyCouch's database and folder, the way back to the server, and
// what's in memory: identities, pairings, streams.
type App struct {
	DB   *store.DB
	Dir  string
	Host *host.Client
	Log  *slog.Logger

	mu         sync.Mutex
	identities map[int64]*gamestream.Identity
	attempts   map[int64][]time.Time
	statuses   map[int64]cachedState
	pairs      map[int64]*pairing
	sessions   map[string]*stream
	apps       map[int64]appsCache
	wakes      map[int64]time.Time
	finding    map[int64]bool
	starting   map[int64]bool // who has a stream on its way, as that takes a minute
	checked    string
	checkedAt  time.Time
	checkOK    bool
}

// NewApp is an App over a database, a folder and a host API client.
func NewApp(db *store.DB, dir string, h *host.Client) *App {
	return &App{DB: db, Dir: dir, Host: h, Log: slog.Default(),
		identities: map[int64]*gamestream.Identity{}, attempts: map[int64][]time.Time{}, statuses: map[int64]cachedState{},
		pairs: map[int64]*pairing{}, sessions: map[string]*stream{}, apps: map[int64]appsCache{}, wakes: map[int64]time.Time{},
		finding: map[int64]bool{}, starting: map[int64]bool{}}
}

var (
	appMu  sync.Mutex
	shared *App
)

// Get opens WittyCouch's database and reaches the server the first time something needs them.
func Get() (*App, error) {
	appMu.Lock()
	defer appMu.Unlock()
	if shared != nil {
		return shared, nil
	}
	dir := os.Getenv(DataEnv)
	db, err := store.Open(dir)
	if err != nil {
		return nil, err
	}
	h, err := host.New()
	if err != nil {
		db.Close()
		return nil, err
	}
	shared = NewApp(db, dir, h)
	return shared, nil
}

// Use makes a the App the routes use, for tests.
func Use(a *App) {
	appMu.Lock()
	shared = a
	appMu.Unlock()
}

// Routes adds the routes to a service's mux (service.Config.HTTP).
func Routes(mux *http.ServeMux) {
	with := func(f func(*App, http.ResponseWriter, *http.Request)) http.Handler {
		return service.People(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a, err := Get()
			if err != nil {
				slog.Error("wittycouch: its database or the server can't be reached", "cat", "games", "err", err)
				writeErr(w, 503, "WittyCouch can't reach its data right now")
				return
			}
			f(a, w, r)
		}))
	}
	mux.Handle("GET /status", with((*App).handleStatus))
	mux.Handle("GET /find", with((*App).handleFind))
	mux.Handle("GET /hosts", with((*App).handleHosts))
	mux.Handle("POST /hosts", with((*App).handleAdd))
	mux.Handle("PATCH /hosts/{id}", with((*App).handleRename))
	mux.Handle("DELETE /hosts/{id}", with((*App).handleRemove))
	mux.Handle("POST /hosts/{id}/pair", with((*App).handlePair))
	mux.Handle("GET /hosts/{id}/pair", with((*App).handlePairStatus))
	mux.Handle("DELETE /hosts/{id}/pair", with((*App).handlePairCancel))
	mux.Handle("GET /hosts/{id}/apps", with((*App).handleApps))
	mux.Handle("GET /hosts/{id}/apps/{app}/art", with((*App).handleArt))
	mux.Handle("POST /hosts/{id}/apps/{app}/play", with((*App).handlePlay))
	mux.Handle("POST /hosts/{id}/wake", with((*App).handleWake))
	mux.Handle("POST /hosts/{id}/quit", with((*App).handleQuit))
	mux.Handle("GET /sessions/{sid}", with((*App).handleSession))
	mux.Handle("POST /sessions/{sid}/webrtc", with((*App).handleWebRTC))
	mux.Handle("DELETE /sessions/{sid}", with((*App).handleSessionEnd))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	b, err := json.Marshal(v)
	if err != nil {
		code, b = 500, []byte(`{"error":"the answer could not be written"}`)
	}
	w.WriteHeader(code)
	_, _ = w.Write(append(b, '\n'))
}

// writeErr answers as the server's own routes do: {"error": "..."}.
func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func pathInt(r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n, err == nil && n > 0
}

func now() int64 { return time.Now().Unix() }

// person is who is asking, as the server said.
type person struct {
	ID      int64
	Admin   bool
	Kind    string // adult, kids, guest
	Limited bool   // the profile has limits set
}

func personOf(r *http.Request) person {
	id := service.Who(r)
	return person{ID: id.User, Admin: id.Admin, Kind: id.Kind, Limited: r.Header.Get(HeaderLimited) == "1"}
}

func (p person) idString() string { return strconv.FormatInt(p.ID, 10) }

// ---------------------------------------------------------------- settings

// settingsOf are the module's settings, as an administrator left them (the server's, with defaults).
type settingsOf map[string]json.RawMessage

func (a *App) settings(ctx context.Context) settingsOf {
	if a.Host == nil {
		return settingsOf{}
	}
	s, err := a.Host.Settings(ctx, host.On{})
	if err != nil {
		a.Log.Debug("wittycouch: settings", "err", err)
		return settingsOf{}
	}
	return s
}

func (s settingsOf) str(k, def string) string {
	var v any
	if raw, ok := s[k]; ok && json.Unmarshal(raw, &v) == nil {
		switch x := v.(type) {
		case string:
			return x
		case float64:
			return strconv.FormatInt(int64(x), 10)
		case bool:
			if x {
				return "1"
			}
			return "0"
		}
	}
	return def
}

func (s settingsOf) on(k string, def bool) bool {
	d := "0"
	if def {
		d = "1"
	}
	v := strings.ToLower(s.str(k, d))
	return v == "1" || v == "true"
}

func (s settingsOf) num(k string, def int) int {
	n, err := strconv.Atoi(s.str(k, strconv.Itoa(def)))
	if err != nil {
		return def
	}
	return n
}

// ---------------------------------------------------------------- who may

// allowLoopback is only ever set by tests, whose stand-in PC is on this machine.
var allowLoopback bool

// allowed: admins always. Otherwise it's an admin's setting, and an account with limits set on
// it doesn't get it, since a PC's desktop is outside anything those limits can reach.
func (a *App) allowed(ctx context.Context, u person) bool {
	if u.ID <= 0 {
		return false
	}
	if u.Admin {
		return true
	}
	switch a.settings(ctx).str(ui.SettingAccess, "adults") {
	case "everyone":
		return true
	case "adults":
		return (u.Kind == "adult" || u.Kind == "") && !u.Limited
	}
	return false
}

// user checks the request and answers for it if it isn't allowed.
func (a *App) user(w http.ResponseWriter, r *http.Request) (person, bool) {
	u := personOf(r)
	if !a.allowed(r.Context(), u) {
		writeErr(w, 403, "Connecting to game PCs isn't turned on for this account. Ask whoever runs this server.")
		return u, false
	}
	return u, true
}

// policy is where this person's connections may go.
func (a *App) policy(ctx context.Context, u person) gamestream.Policy {
	if u.Admin {
		return gamestream.Policy{AllowPublic: true}
	}
	s := a.settings(ctx)
	return gamestream.Policy{AllowPublic: s.on(ui.SettingInternet, true), DenyPrivate: !s.on(ui.SettingPrivate, true), DenyLoopback: !allowLoopback}
}

// attempt counts a new connection attempt against this person's limit.
func (a *App) attempt(w http.ResponseWriter, u person) bool {
	if a.tryAttempt(u) {
		return true
	}
	w.Header().Set("Retry-After", "600")
	writeErr(w, 429, "That's a lot of connection attempts. Try again in a few minutes.")
	return false
}

// tryAttempt counts an attempt, and says whether it's within the limit.
func (a *App) tryAttempt(u person) bool {
	if u.Admin {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	t0 := time.Now()
	kept := a.attempts[u.ID][:0]
	for _, t := range a.attempts[u.ID] {
		if t0.Sub(t) < attemptsWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= attemptsLimit {
		a.attempts[u.ID] = kept
		return false
	}
	a.attempts[u.ID] = append(kept, t0)
	return true
}

// identity is this person's own key and certificate, which their PCs pair with.
func (a *App) identity(u int64) (*gamestream.Identity, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if id := a.identities[u]; id != nil {
		return id, nil
	}
	id, err := gamestream.LoadOrCreateIdentity(filepath.Join(a.Dir, "identities", "u"+strconv.FormatInt(u, 10)))
	if err != nil {
		return nil, errors.New("this account's game PC certificate can't be read or made: " + err.Error())
	}
	a.identities[u] = id
	return id, nil
}

// ---------------------------------------------------------------- the host program

// program is where the WittyCouch Host program is, and what it's run with: named by the
// environment, or the server's answer for the module this one requires.
func (a *App) program(ctx context.Context) (string, []string) {
	if p := os.Getenv("WMS_WITTYCOUCH_HOST"); p != "" {
		return p, nil
	}
	if a.Host == nil {
		return "", nil
	}
	var out struct {
		Path string   `json:"path"`
		Args []string `json:"args"`
	}
	if err := a.Host.Do(ctx, host.On{}, "GET", "/programs/wittycouchhost", nil, &out); err != nil {
		return "", nil
	}
	return out.Path, out.Args
}

// programWorks runs the program to see that it can run here, at most once a minute.
func (a *App) programWorks(ctx context.Context, program string, args []string) bool {
	if program == "" {
		return false
	}
	key := program + "\x00" + strings.Join(args, "\x00")
	a.mu.Lock()
	cached := time.Since(a.checkedAt) < time.Minute && a.checked == key
	ok := a.checkOK
	a.mu.Unlock()
	if cached {
		return ok
	}
	ok = checkProgram(ctx, program, args) == nil
	a.mu.Lock()
	a.checked, a.checkedAt, a.checkOK = key, time.Now(), ok
	a.mu.Unlock()
	return ok
}
