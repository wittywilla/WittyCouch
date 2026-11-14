// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wittywilla/wittycouch"
	"github.com/wittywilla/wittycouch/gamestream"
	"github.com/wittywilla/wittycouch/ui"
)

// pcHost is a PC as it's stored, for one person.
type pcHost struct {
	ID, UserID int64
	Name       string
	Address    string
	Port       int
	UUID, MAC  string
	Cert       string
	Public     bool
}

const hostCols = `id, user_id, name, address, port, uuid, mac, cert, public`

func scanHost(row interface{ Scan(...any) error }) (*pcHost, error) {
	var h pcHost
	if err := row.Scan(&h.ID, &h.UserID, &h.Name, &h.Address, &h.Port, &h.UUID, &h.MAC, &h.Cert, &h.Public); err != nil {
		return nil, err
	}
	return &h, nil
}

func (a *App) hostsOf(ctx context.Context, user int64) ([]*pcHost, error) {
	rows, err := a.DB.QueryContext(ctx, `SELECT `+hostCols+` FROM hosts WHERE user_id = ? ORDER BY name COLLATE NOCASE`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pcHost
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (a *App) hostOf(ctx context.Context, user, id int64) (*pcHost, error) {
	return scanHost(a.DB.QueryRowContext(ctx, `SELECT `+hostCols+` FROM hosts WHERE id = ? AND user_id = ?`, id, user))
}

// hostFor loads a PC in the path. Someone else's answers "not found", as if it isn't there.
func (a *App) hostFor(w http.ResponseWriter, r *http.Request, u person) *pcHost {
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "bad id")
		return nil
	}
	h, err := a.hostOf(r.Context(), u.ID, id)
	if err != nil {
		writeErr(w, 404, "not found")
		return nil
	}
	return h
}

func (a *App) client(ctx context.Context, u person, h *pcHost) (*gamestream.Rig, error) {
	id, err := a.identity(u.ID)
	if err != nil {
		return nil, err
	}
	return &gamestream.Rig{Address: h.Address, Port: h.Port, Identity: id, Cert: []byte(h.Cert), Policy: a.policy(ctx, u)}, nil
}

// pcState is how a PC is doing, in words for people.
type pcState struct {
	State       string `json:"state"` // online, busy, asleep, no-sunshine, not-paired, cert-changed, error
	Message     string `json:"message"`
	CurrentGame string `json:"currentGame,omitempty"`
	currentApp  int
}

type cachedState struct {
	at   time.Time
	view pcState
}

func statusFor(paired bool, info *gamestream.Info, err error) pcState {
	switch {
	case err == nil && !paired:
		return pcState{State: "not-paired", Message: "This PC is on, and needs pairing."}
	case err == nil && info.CurrentGame > 0:
		return pcState{State: "busy", Message: "A game is running on this PC.", currentApp: info.CurrentGame}
	case err == nil:
		return pcState{State: "online", Message: "Ready to play."}
	case errors.Is(err, gamestream.ErrRefused):
		return pcState{State: "no-sunshine", Message: "This PC is on, but Sunshine isn't running on it. Start Sunshine on that PC."}
	case errors.Is(err, gamestream.ErrNoAnswer):
		return pcState{State: "asleep", Message: "This PC isn't answering. It's asleep, turned off, or not on this network."}
	case errors.Is(err, gamestream.ErrNotPaired):
		return pcState{State: "not-paired", Message: "This PC no longer trusts this server, so it has to be paired again."}
	case errors.Is(err, gamestream.ErrCertChanged):
		return pcState{State: "cert-changed", Message: "Something other than the PC you paired with answered at that address, so it wasn't trusted. If you reinstalled Sunshine, pair it again. If not, check what's at that address."}
	case errors.Is(err, gamestream.ErrBlocked):
		return pcState{State: "error", Message: "This PC's address isn't allowed: " + err.Error()}
	}
	return pcState{State: "error", Message: "This PC answered with something that isn't from Sunshine."}
}

func (a *App) status(ctx context.Context, u person, h *pcHost) pcState {
	a.mu.Lock()
	c, ok := a.statuses[h.ID]
	a.mu.Unlock()
	if ok && time.Since(c.at) < statusLife {
		return c.view
	}
	var view pcState
	client, err := a.client(ctx, u, h)
	if err != nil {
		view = pcState{State: "error", Message: "This account's certificate couldn't be read."}
	} else if h.Cert == "" {
		info, err := client.Hello(ctx)
		view = statusFor(false, info, err)
	} else {
		info, err := client.Status(ctx)
		view = statusFor(true, info, err)
		if err == nil && info.MAC != h.MAC {
			if _, merr := gamestream.ParseMAC(info.MAC); merr == nil {
				_, _ = a.DB.ExecContext(ctx, `UPDATE hosts SET mac = ? WHERE id = ?`, info.MAC, h.ID)
				h.MAC = info.MAC
			}
		}
	}
	if !u.Admin && (view.State == "asleep" || view.State == "no-sunshine") {
		// Whether something refused or never answered is how ports get scanned.
		view = pcState{State: "asleep", Message: "This PC isn't answering. It may be asleep or off, or Sunshine may not be running."}
	}
	if view.currentApp > 0 {
		a.mu.Lock()
		for _, app := range a.apps[h.ID].apps {
			if app.ID == view.currentApp {
				view.CurrentGame = app.Title
			}
		}
		a.mu.Unlock()
	}
	a.mu.Lock()
	a.statuses[h.ID] = cachedState{at: time.Now(), view: view}
	a.mu.Unlock()
	return view
}

func (a *App) forgetStatus(id int64) {
	a.mu.Lock()
	delete(a.statuses, id)
	a.mu.Unlock()
}

// hostView is a PC as its owner sees it.
type hostView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	Port        int    `json:"port,omitempty"`
	Paired      bool   `json:"paired"`
	State       string `json:"state"`
	Message     string `json:"message"`
	CurrentGame string `json:"currentGame,omitempty"`
	CanWake     bool   `json:"canWake"`
	Public      bool   `json:"public,omitempty"`
}

func (a *App) viewOf(ctx context.Context, u person, h *pcHost) hostView {
	st := a.status(ctx, u, h)
	return hostView{ID: h.ID, Name: h.Name, Address: h.Address, Port: h.Port, Paired: h.Cert != "", State: st.State, Message: st.Message,
		CurrentGame: st.CurrentGame, CanWake: h.MAC != "" && st.State == "asleep", Public: h.Public}
}

func (a *App) viewsOf(ctx context.Context, u person, hosts []*pcHost) []hostView {
	out := make([]hostView, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer recoverLog(a, "status")
			out[i] = a.viewOf(ctx, u, h)
		}()
	}
	wg.Wait()
	return out
}

func recoverLog(a *App, what string) {
	if v := recover(); v != nil {
		a.Log.Error("wittycouch: "+what+" failed", "cat", "games", "panic", fmt.Sprint(v))
	}
}

// GET /status: whether this account may use this, whether this server can, and the settings a
// person needs to know.
func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := personOf(r)
	program, args := a.program(ctx)
	body := map[string]any{"allowed": a.allowed(ctx, u), "ready": a.programWorks(ctx, program, args), "credits": wittycouch.Credits()}
	if u.Admin {
		body["program"] = program != ""
		body["access"] = a.settings(ctx).str(ui.SettingAccess, "adults")
	}
	writeJSON(w, 200, body)
}

// found is a PC that answered on the network.
type found struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Version string `json:"version"`
	Added   bool   `json:"added"`
}

var errFinding = errors.New("Already looking. Try again in a moment.")

// find is who on this server's network runs Sunshine, as this person may reach it.
func (a *App) find(ctx context.Context, u person) ([]found, error) {
	policy := a.policy(ctx, u)
	if policy.DenyPrivate {
		return []found{}, nil
	}
	a.mu.Lock()
	busy := a.finding[u.ID]
	if !busy {
		a.finding[u.ID] = true
	}
	a.mu.Unlock()
	if busy {
		return nil, errFinding
	}
	defer func() {
		a.mu.Lock()
		delete(a.finding, u.ID)
		a.mu.Unlock()
	}()
	list, err := browse(ctx, 3*time.Second)
	if err != nil {
		return nil, err
	}
	mine, _ := a.hostsOf(ctx, u.ID)
	out := []found{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, f := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer recoverLog(a, "asking a PC")
			info, err := (&gamestream.Rig{Address: f.Address, Port: f.Port, Policy: policy}).Hello(ctx)
			if err != nil {
				return
			}
			item := found{Name: info.Name, Address: f.Address, Port: f.Port, Version: info.Version}
			for _, h := range mine {
				if strings.EqualFold(h.UUID, info.UUID) {
					item.Added = true
				}
			}
			mu.Lock()
			out = append(out, item)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, nil
}

// browse looks for PCs on the network; tests stand in for it.
var browse = gamestream.Browse

// GET /find: who on this server's network runs Sunshine.
func (a *App) handleFind(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	if a.policy(r.Context(), u).DenyPrivate {
		writeJSON(w, 200, []found{})
		return
	}
	a.mu.Lock()
	busy := a.finding[u.ID]
	a.mu.Unlock()
	if busy {
		writeErr(w, 409, errFinding.Error())
		return
	}
	if !a.attempt(w, u) {
		return
	}
	out, err := a.find(r.Context(), u)
	if errors.Is(err, errFinding) {
		writeErr(w, 409, err.Error())
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, out)
}

// GET /hosts: this person's PCs, and how they are.
func (a *App) handleHosts(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	hosts, err := a.hostsOf(r.Context(), u.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, a.viewsOf(r.Context(), u, hosts))
}

// checkAddress applies this person's rules to an address they typed.
func (a *App) checkAddress(ctx context.Context, u person, address string) (host string, port int, reach gamestream.Reach, msg string) {
	host, port, err := gamestream.CleanAddress(address)
	if err != nil {
		return "", 0, 0, err.Error()
	}
	reach, err = gamestream.CheckAddress(ctx, host, nil)
	if err != nil {
		if !u.Admin { // whether a name resolves here is a question about this network
			padFailure(ctx, u, time.Now())
			return "", 0, 0, connectMessage(u, err)
		}
		return "", 0, 0, err.Error()
	}
	if port != 0 && port != gamestream.DefaultPort && !u.Admin && !a.settings(ctx).on(ui.SettingCustomPorts, false) {
		return "", 0, 0, "This server only connects to Sunshine's usual port. Ask whoever runs it to allow others."
	}
	policy := a.policy(ctx, u)
	switch {
	case reach == gamestream.Loopback && policy.DenyLoopback:
		return "", 0, 0, "That address is this server itself, which this can't connect to."
	case reach == gamestream.Private && policy.DenyPrivate:
		return "", 0, 0, "This server has been set not to connect to PCs on its own network."
	case reach == gamestream.Public && !policy.AllowPublic:
		return "", 0, 0, "This server has been set not to connect to PCs over the internet."
	}
	return host, port, reach, ""
}

// answerWait is the longest a PC is given to answer.
var answerWait = 5 * time.Second

// padFailure: "refused" comes back at once, and "nobody there" only after the wait, which would
// tell anyone but an admin what's on the network as surely as saying so.
func padFailure(ctx context.Context, u person, asked time.Time) {
	if u.Admin {
		return
	}
	select {
	case <-time.After(time.Until(asked.Add(answerWait))):
	case <-ctx.Done():
	}
}

// connectMessage: an admin gets the reason, and anyone else gets one sentence.
func connectMessage(u person, err error) string {
	if errors.Is(err, gamestream.ErrBlocked) {
		return "That address isn't allowed here."
	}
	if !u.Admin {
		return "That PC didn't answer as a Sunshine server. Check the address, that it's on, and that Sunshine is running."
	}
	switch {
	case errors.Is(err, gamestream.ErrRefused):
		return "That PC is on, but Sunshine isn't running on it."
	case errors.Is(err, gamestream.ErrBadAnswer):
		return "Something answered at that address, but it isn't Sunshine."
	}
	return "That PC didn't answer. Check the address, that it's on, and that Sunshine is running."
}

func reachName(r gamestream.Reach) string {
	switch r {
	case gamestream.Public:
		return "internet"
	case gamestream.Private:
		return "private"
	case gamestream.Loopback:
		return "this machine"
	}
	return "unknown"
}

// httpError is a refusal with the status it's answered with.
type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }

// add adds a PC to this person's list. It has to answer as Sunshine.
func (a *App) add(ctx context.Context, u person, address, name string) (*pcHost, error) {
	host, port, reach, msg := a.checkAddress(ctx, u, address)
	if msg != "" {
		return nil, &httpError{400, msg}
	}
	var n int
	if err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM hosts WHERE user_id = ?`, u.ID).Scan(&n); err == nil && n >= hostLimit {
		return nil, &httpError{409, "That's as many PCs as one account can have. Remove one first."}
	}
	client := &gamestream.Rig{Address: host, Port: port, Policy: a.policy(ctx, u)}
	asked := time.Now()
	info, err := client.Hello(ctx)
	if err != nil {
		a.Log.Info("wittycouch: a PC didn't answer", "cat", "games", "user", u.idString())
		padFailure(ctx, u, asked)
		return nil, &httpError{502, connectMessage(u, err)}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = info.Name
	}
	if name == "" {
		name = "Game PC"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	_, err = a.DB.ExecContext(ctx, `INSERT INTO hosts(user_id, name, address, port, uuid, public, created_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(user_id, uuid) DO UPDATE SET address = excluded.address, port = excluded.port, public = excluded.public`,
		u.ID, name, host, port, info.UUID, reach == gamestream.Public, now())
	if err != nil {
		return nil, err
	}
	h, err := scanHost(a.DB.QueryRowContext(ctx, `SELECT `+hostCols+` FROM hosts WHERE user_id = ? AND uuid = ?`, u.ID, info.UUID))
	if err != nil {
		return nil, err
	}
	a.Log.Info("wittycouch: a PC was added", "cat", "games", "user", u.idString(), "where", reachName(reach))
	return h, nil
}

func writeFailure(w http.ResponseWriter, err error) {
	var he *httpError
	if errors.As(err, &he) {
		writeErr(w, he.code, he.msg)
		return
	}
	writeErr(w, 500, err.Error())
}

// POST /hosts {address, name}: add a PC.
func (a *App) handleAdd(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	var b struct {
		Address string `json:"address"`
		Name    string `json:"name"`
	}
	if err := readJSON(r, &b); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// The address is checked before an attempt is counted, as before.
	if _, _, _, msg := a.checkAddress(r.Context(), u, b.Address); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	var n int
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM hosts WHERE user_id = ?`, u.ID).Scan(&n); err == nil && n >= hostLimit {
		writeErr(w, 409, "That's as many PCs as one account can have. Remove one first.")
		return
	}
	if !a.attempt(w, u) {
		return
	}
	h, err := a.add(r.Context(), u, b.Address, b.Name)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, 201, a.viewOf(r.Context(), u, h))
}

// PATCH /hosts/{id} {name}
func (a *App) handleRename(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	h := a.hostFor(w, r, u)
	if h == nil {
		return
	}
	var b struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &b); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	name := strings.TrimSpace(b.Name)
	if name == "" || len(name) > 100 {
		writeErr(w, 400, "A name is required, up to 100 characters.")
		return
	}
	if _, err := a.DB.ExecContext(r.Context(), `UPDATE hosts SET name = ? WHERE id = ?`, name, h.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	h.Name = name
	writeJSON(w, 200, a.viewOf(r.Context(), u, h))
}

// remove forgets a PC. That PC keeps this account on its own list.
func (a *App) remove(ctx context.Context, u person, h *pcHost) error {
	a.mu.Lock()
	if p := a.pairs[h.ID]; p != nil && p.cancel != nil {
		p.cancel()
	}
	delete(a.pairs, h.ID)
	delete(a.statuses, h.ID)
	delete(a.apps, h.ID)
	delete(a.wakes, h.ID)
	a.mu.Unlock()
	if _, err := a.DB.ExecContext(ctx, `DELETE FROM hosts WHERE id = ?`, h.ID); err != nil {
		return err
	}
	a.removeArt(u.ID, h.ID)
	return nil
}

// DELETE /hosts/{id}
func (a *App) handleRemove(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	h := a.hostFor(w, r, u)
	if h == nil {
		return
	}
	if err := a.remove(r.Context(), u, h); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "note": "To finish, remove this device from the paired devices in Sunshine on that PC."})
}

// ---------------------------------------------------------------- pairing

type pairing struct {
	PIN     string    `json:"pin,omitempty"`
	State   string    `json:"state"` // waiting, paired, failed
	Message string    `json:"message,omitempty"`
	Started time.Time `json:"-"`
	Kind    string    `json:"kind"`
	cancel  context.CancelFunc
}

func pairMessage(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "Pairing was cancelled."
	case errors.Is(err, gamestream.ErrWrongPIN), errors.Is(err, gamestream.ErrPairTimedOut), errors.Is(err, gamestream.ErrAlreadyPairing),
		errors.Is(err, gamestream.ErrImposter), errors.Is(err, gamestream.ErrTooOld):
		return err.Error() + "."
	}
	return "Pairing didn't finish: " + err.Error() + "."
}

// pairingOf is how pairing a PC is going; State "none" when nothing has been started.
func (a *App) pairingOf(id int64) pairing {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p := a.pairs[id]; p != nil {
		return *p
	}
	return pairing{State: "none"}
}

// startPair starts pairing, or answers with the pairing already waiting. ok is false when the
// person has used up their attempts (and w has been answered).
func (a *App) startPair(w http.ResponseWriter, u person, h *pcHost) (pairing, bool) {
	a.mu.Lock()
	if p := a.pairs[h.ID]; p != nil && p.State == "waiting" {
		answer := *p
		a.mu.Unlock()
		return answer, true
	}
	a.mu.Unlock()
	if !a.attempt(w, u) {
		return pairing{}, false
	}
	pin, err := gamestream.NewPIN()
	if err != nil {
		writeErr(w, 500, err.Error())
		return pairing{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), gamestream.PairTimeout)
	p := &pairing{PIN: pin, State: "waiting", Started: time.Now(), Kind: "couch", cancel: cancel}
	a.mu.Lock()
	if other := a.pairs[h.ID]; other != nil && other.State == "waiting" { // while that attempt was counted
		answer := *other
		a.mu.Unlock()
		cancel()
		return answer, true
	}
	a.pairs[h.ID] = p
	answer := *p // the pairing changes this from here on
	a.mu.Unlock()
	go func() {
		defer recoverLog(a, "pairing")
		defer cancel()
		finish := func(state, msg string) {
			a.mu.Lock()
			p.State, p.Message, p.PIN = state, msg, ""
			a.mu.Unlock()
		}
		client, err := a.client(ctx, u, h)
		if err != nil {
			finish("failed", err.Error())
			return
		}
		client.Cert = nil
		cert, err := client.Pair(ctx, pin)
		if err != nil {
			finish("failed", pairMessage(err))
			return
		}
		if _, err := a.DB.ExecContext(context.Background(), `UPDATE hosts SET cert = ?, paired_at = ? WHERE id = ?`, string(cert), now(), h.ID); err != nil {
			finish("failed", err.Error())
			return
		}
		a.forgetStatus(h.ID)
		finish("paired", "Paired.")
		a.Log.Info("wittycouch: a PC was paired", "cat", "games", "user", u.idString())
	}()
	return answer, true
}

// POST /hosts/{id}/pair: start pairing. The answer has the PIN to type into Sunshine.
func (a *App) handlePair(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	h := a.hostFor(w, r, u)
	if h == nil {
		return
	}
	if p, ok := a.startPair(w, u, h); ok {
		writeJSON(w, 200, &p)
	}
}

func (a *App) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	if h := a.hostFor(w, r, u); h != nil {
		p := a.pairingOf(h.ID)
		writeJSON(w, 200, &p)
	}
}

func (a *App) cancelPair(id int64) {
	a.mu.Lock()
	if p := a.pairs[id]; p != nil && p.cancel != nil {
		p.cancel()
	}
	a.mu.Unlock()
}

func (a *App) handlePairCancel(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	if h := a.hostFor(w, r, u); h != nil {
		a.cancelPair(h.ID)
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

// ---------------------------------------------------------------- what a PC offers

// desktopLike are apps that hand over the whole computer rather than a game.
var desktopLike = regexp.MustCompile(`(?i)\b(desktop|terminal|powershell|command prompt|cmd|file explorer|task manager)\b`)

// pcApp is something a PC offers. Sunshine answers with one default picture for any app without
// its own, so a picture that several apps share counts as none.
type pcApp struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	HasArt    bool   `json:"hasArt"`
	IsDesktop bool   `json:"isDesktop,omitempty"`
}

type appsCache struct {
	at   time.Time
	apps []pcApp
}

func (a *App) artPath(user, host int64, app int) string {
	return filepath.Join(a.Dir, "art", fmt.Sprintf("u%d-h%d-%d.img", user, host, app))
}

// dropOldArt removes pictures of what a PC no longer lists, as those numbers are the PC's to
// choose: one that keeps changing them could otherwise fill the disk.
func (a *App) dropOldArt(user, host int64, list []gamestream.App) {
	keep := map[string]bool{}
	for _, app := range list {
		keep[a.artPath(user, host, app.ID)] = true
	}
	files, _ := filepath.Glob(filepath.Join(a.Dir, "art", fmt.Sprintf("u%d-h%d-*.img", user, host)))
	for _, f := range files {
		if !keep[f] {
			_ = os.Remove(f)
		}
	}
}

func (a *App) removeArt(user, host int64) {
	matches, _ := filepath.Glob(filepath.Join(a.Dir, "art", fmt.Sprintf("u%d-h%d-*", user, host)))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// appsOf is what a paired PC offers, kept for a while; fresh reads it again.
func (a *App) appsOf(ctx context.Context, u person, h *pcHost, fresh bool) ([]pcApp, error) {
	a.mu.Lock()
	c, ok := a.apps[h.ID]
	a.mu.Unlock()
	if ok && time.Since(c.at) < appsLife && !fresh {
		return c.apps, nil
	}
	client, err := a.client(ctx, u, h)
	if err != nil {
		return nil, err
	}
	list, err := client.Apps(ctx)
	if err != nil {
		return nil, &httpError{502, statusFor(h.Cert != "", nil, err).Message}
	}
	if len(list) > maxApps {
		list = list[:maxApps]
	}
	out := make([]pcApp, len(list))
	sums := make([][32]byte, len(list))
	seen := map[[32]byte]int{}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	if err := os.MkdirAll(filepath.Join(a.Dir, "art"), 0o700); err != nil {
		return nil, err
	}
	for i, app := range list {
		out[i] = pcApp{ID: app.ID, Title: app.Title, IsDesktop: desktopLike.MatchString(app.Title)}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer recoverLog(a, "game picture")
			defer func() { <-sem }()
			data, _, err := client.AppImage(ctx, app.ID)
			if err != nil || len(data) > maxAppArt {
				return
			}
			sums[i] = sha256.Sum256(data)
			_ = os.WriteFile(a.artPath(u.ID, h.ID, app.ID), data, 0o600)
		}()
	}
	wg.Wait()
	for _, sum := range sums {
		seen[sum]++
	}
	for i := range out {
		out[i].HasArt = sums[i] != [32]byte{} && seen[sums[i]] == 1
	}
	a.dropOldArt(u.ID, h.ID, list)
	a.mu.Lock()
	a.apps[h.ID] = appsCache{at: time.Now(), apps: out}
	a.mu.Unlock()
	return out, nil
}

// GET /hosts/{id}/apps: what a paired PC offers.
func (a *App) handleApps(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	h := a.hostFor(w, r, u)
	if h == nil {
		return
	}
	fresh := r.URL.Query().Get("fresh") != ""
	if fresh && !a.attempt(w, u) {
		return
	}
	out, err := a.appsOf(r.Context(), u, h, fresh)
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, 200, out)
}

// GET /hosts/{id}/apps/{app}/art: a picture kept from the last read of the list, and only ever
// served as a picture, as the bytes came from somebody else's computer.
func (a *App) handleArt(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	h := a.hostFor(w, r, u)
	if h == nil {
		return
	}
	app, err := strconv.Atoi(r.PathValue("app"))
	if err != nil {
		writeErr(w, 400, "bad app")
		return
	}
	data, err := os.ReadFile(a.artPath(u.ID, h.ID, app))
	if err != nil {
		writeErr(w, 404, "no artwork")
		return
	}
	ct := "image/png"
	if len(data) > 3 && data[0] == 0xff && data[1] == 0xd8 {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}

// ---------------------------------------------------------------- waking and ending

// wake sends the wake-up signal to one of this person's PCs.
func (a *App) wake(u person, h *pcHost) (string, error) {
	if h.MAC == "" {
		return "", &httpError{409, "This PC hasn't told this account how to wake it yet. It says so when it's paired and on."}
	}
	if h.Public {
		return "", &httpError{409, "A PC over the internet can't be woken from here."}
	}
	a.mu.Lock()
	if time.Since(a.wakes[h.ID]) < wakeEvery {
		a.mu.Unlock()
		return "", nil
	}
	a.wakes[h.ID] = time.Now()
	delete(a.statuses, h.ID)
	a.mu.Unlock()
	if err := gamestream.Wake(h.MAC, h.Address); err != nil {
		return "", &httpError{500, "The wake-up signal couldn't be sent."}
	}
	return "A wake-up signal was sent. A PC usually takes under a minute, if Wake-on-LAN is on in its settings.", nil
}

// POST /hosts/{id}/wake
func (a *App) handleWake(w http.ResponseWriter, r *http.Request) {
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	h := a.hostFor(w, r, u)
	if h == nil {
		return
	}
	note, err := a.wake(u, h)
	if err != nil {
		writeFailure(w, err)
		return
	}
	body := map[string]any{"ok": true}
	if note != "" {
		body["note"] = note
	}
	writeJSON(w, 200, body)
}

// POST /hosts/{id}/quit: end what's running on this person's PC.
func (a *App) handleQuit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u, ok := a.user(w, r)
	if !ok {
		return
	}
	h := a.hostFor(w, r, u)
	if h == nil {
		return
	}
	client, err := a.client(ctx, u, h)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := client.Quit(ctx); err != nil {
		writeStartError(w, u, err)
		return
	}
	a.forgetStatus(h.ID)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
