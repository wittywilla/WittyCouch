// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wittywilla/wittycouch/gamestream"
	"github.com/wittywilla/wittycouch/gamestream/fake"
	"github.com/wittywilla/wittycouch/relay/fakehost"
	"github.com/wittywilla/wittycouch/store"
	"github.com/wittywilla/wittycouch/ui"
	"github.com/wittywilla/wittymodule/host"
	"github.com/wittywilla/wittymodule/sdui"
)

// TestMain lets this test program stand in for the Moonlight host program, when a test starts it
// as one.
func TestMain(m *testing.M) {
	if os.Getenv(fakehost.Env) != "" {
		fakehost.Run()
		return
	}
	os.Exit(m.Run())
}

// fakeServer stands in for the server's host API: the module's settings, and where its program is.
type fakeServer struct {
	mu       sync.Mutex
	settings map[string]any
	program  string // "" answers 404
	args     []string
}

func (f *fakeServer) set(k string, v any) {
	f.mu.Lock()
	f.settings[k] = v
	f.mu.Unlock()
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("X-Witty-Host", "1")
	switch {
	case r.URL.Path == host.Base+"/settings":
		_ = json.NewEncoder(w).Encode(f.settings)
	case r.URL.Path == host.Base+"/programs/wittycouchhost":
		if f.program == "" {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"not installed"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"path": f.program, "args": f.args, "version": "1.0.0"})
	case r.URL.Path == host.Base+"/events":
		_, _ = w.Write([]byte(`{"events":[],"next":0}`))
	default:
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"no such call"}`))
	}
}

type testApp struct {
	*App
	srv *fakeServer
	mux *http.ServeMux
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := &fakeServer{settings: map[string]any{"access": "adults", "private": true, "internet": true, "customPorts": true, "maxStreams": 2}}
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	a := NewApp(db, dir, host.NewAt(hs.URL, "token", nil))
	Use(a)
	t.Cleanup(func() { Stop(); Use(nil) })
	mux := http.NewServeMux()
	Routes(mux)
	return &testApp{App: a, srv: srv, mux: mux}
}

// who is a person, as the server would tell the module.
type who struct {
	id      int64
	admin   bool
	kind    string
	limited bool
}

var (
	admin   = who{id: 1, admin: true, kind: "adult"}
	adult   = who{id: 2, kind: "adult"}
	adult2  = who{id: 3, kind: "adult"}
	kid     = who{id: 4, kind: "kids"}
	guest   = who{id: 5, kind: "guest"}
	limited = who{id: 6, kind: "adult", limited: true}
)

func (ta *testApp) do(t *testing.T, method, path string, u who, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, path, rd)
	if u.id > 0 {
		r.Header.Set("X-Witty-User", strconv.FormatInt(u.id, 10))
		r.Header.Set("X-Witty-Kind", u.kind)
		if u.admin {
			r.Header.Set("X-Witty-Admin", "1")
		}
		if u.limited {
			r.Header.Set(HeaderLimited, "1")
		}
	}
	w := httptest.NewRecorder()
	ta.mux.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body, err)
	}
	return v
}

func sunshine(t *testing.T) *fake.Sunshine {
	t.Helper()
	f, err := fake.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f
}

// testAllowLoopback lets ordinary accounts reach the fake, for tests of everything else.
func testAllowLoopback(t *testing.T) {
	t.Helper()
	allowLoopback = true
	t.Cleanup(func() { allowLoopback = false })
}

// quickFailures shortens the wait a failed connection is padded to.
func quickFailures(t *testing.T) {
	t.Helper()
	old := answerWait
	answerWait = time.Millisecond
	t.Cleanup(func() { answerWait = old })
}

func (ta *testApp) add(t *testing.T, f *fake.Sunshine, u who) hostView {
	t.Helper()
	w := ta.do(t, "POST", "/hosts", u, map[string]any{"address": fmt.Sprintf("127.0.0.1:%d", f.Port())})
	if w.Code != 201 {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	return decode[hostView](t, w)
}

func (ta *testApp) pair(t *testing.T, f *fake.Sunshine, u who, id int64) {
	t.Helper()
	url := fmt.Sprintf("/hosts/%d/pair", id)
	w := ta.do(t, "POST", url, u, nil)
	if w.Code != 200 {
		t.Fatalf("pair: %d %s", w.Code, w.Body)
	}
	p := decode[pairing](t, w)
	if !f.WaitForPairing(5 * time.Second) {
		t.Fatal("nothing started pairing")
	}
	f.TypePIN(p.PIN)
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := decode[pairing](t, ta.do(t, "GET", url, u, nil))
		if st.State == "paired" {
			return
		}
		if st.State == "failed" || time.Now().After(deadline) {
			t.Fatalf("pairing: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWhoMayUseIt(t *testing.T) {
	ta := newTestApp(t)
	allowed := func(u who) bool {
		return decode[struct {
			Allowed bool `json:"allowed"`
		}](t, ta.do(t, "GET", "/status", u, nil)).Allowed
	}
	if !allowed(admin) || !allowed(adult) {
		t.Error("admins and adults start with it on")
	}
	if allowed(kid) || allowed(guest) || allowed(limited) {
		t.Error("kids, guests and limited accounts start without it")
	}
	if w := ta.do(t, "GET", "/status", who{}, nil); w.Code != 401 {
		t.Errorf("nobody: %d", w.Code)
	}
	for _, u := range []who{kid, guest, limited} {
		for _, c := range [][2]string{{"GET", "/hosts"}, {"POST", "/hosts"}, {"GET", "/find"}, {"POST", "/hosts/1/pair"}, {"POST", "/hosts/1/apps/1/play"}} {
			if w := ta.do(t, c[0], c[1], u, map[string]any{"address": "192.168.1.2"}); w.Code != 403 {
				t.Errorf("%s %s: %d", c[0], c[1], w.Code)
			}
		}
	}
	ta.srv.set("access", "admins")
	if allowed(adult) || !allowed(admin) {
		t.Error("admins only")
	}
	ta.srv.set("access", "everyone")
	if !allowed(kid) {
		t.Error("everyone")
	}
	// The admin's own status says whether the program is there.
	st := decode[map[string]any](t, ta.do(t, "GET", "/status", admin, nil))
	if st["program"] != false || st["ready"] != false || st["access"] != "everyone" {
		t.Errorf("%v", st)
	}
	if _, ok := decode[map[string]any](t, ta.do(t, "GET", "/status", adult, nil))["program"]; ok {
		t.Error("an adult was told about the program")
	}
}

func TestAddressRules(t *testing.T) {
	quickFailures(t)
	ta := newTestApp(t)
	ta.srv.set("customPorts", false)
	add := func(u who, addr string) (int, string) {
		w := ta.do(t, "POST", "/hosts", u, map[string]any{"address": addr})
		return w.Code, w.Body.String()
	}
	for _, a := range []string{"169.254.169.254", "224.0.0.251", "0.0.0.0", "fe80::1", "http://192.168.1.2", "192.168.1.2/admin", "user@pc", "pc?x=1", "::ffff:169.254.169.254"} {
		if code, body := add(admin, a); code != 400 {
			t.Errorf("admin %q: %d %s", a, code, body)
		}
		if code, body := add(adult, a); code != 400 {
			t.Errorf("adult %q: %d %s", a, code, body)
		}
	}
	for _, a := range []string{"127.0.0.1", "::1", "localhost", "127.0.0.2"} {
		if code, body := add(adult, a); code != 400 || !strings.Contains(body, "this server") {
			t.Errorf("%q: %d %s", a, code, body)
		}
	}
	if code, body := add(adult, "192.168.1.2:22"); code != 400 || !strings.Contains(body, "usual port") {
		t.Errorf("another port: %d %s", code, body)
	}
	ta.srv.set("private", false)
	if code, body := add(adult, "192.168.1.2"); code != 400 || !strings.Contains(body, "own network") {
		t.Errorf("private off: %d %s", code, body)
	}
	ta.srv.set("private", true)
	ta.srv.set("internet", false)
	if code, body := add(adult, "8.8.8.8"); code != 400 || !strings.Contains(body, "internet") {
		t.Errorf("internet off: %d %s", code, body)
	}
	var n int
	_ = ta.DB.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&n)
	if n != 0 {
		t.Errorf("%d stored", n)
	}
}

func TestAnswersGiveNothingAway(t *testing.T) {
	testAllowLoopback(t)
	quickFailures(t)
	ta := newTestApp(t)
	f := sunshine(t)
	f.Garbage = true
	wa := ta.do(t, "POST", "/hosts", adult, map[string]any{"address": fmt.Sprintf("127.0.0.1:%d", f.Port())})
	f.Garbage = false
	f.Close()
	wb := ta.do(t, "POST", "/hosts", adult, map[string]any{"address": fmt.Sprintf("127.0.0.1:%d", f.Port())})
	if wa.Code != 502 || wb.Code != 502 || wa.Body.String() != wb.Body.String() {
		t.Errorf("a printer: %d %s; nothing: %d %s", wa.Code, wa.Body, wb.Code, wb.Body)
	}
}

func TestFailureTakesAsLongWhateverHappened(t *testing.T) {
	old := answerWait
	answerWait = 300 * time.Millisecond
	defer func() { answerWait = old }()
	start := time.Now()
	padFailure(context.Background(), person{ID: 2, Kind: "adult"}, start)
	if d := time.Since(start); d < 250*time.Millisecond {
		t.Errorf("an adult's answer came after %v", d)
	}
	start = time.Now()
	padFailure(context.Background(), person{ID: 1, Admin: true}, start)
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("an administrator waited %v", d)
	}
}

func TestRateLimit(t *testing.T) {
	testAllowLoopback(t)
	quickFailures(t)
	ta := newTestApp(t)
	f := sunshine(t)
	f.Garbage = true
	hit := false
	for range attemptsLimit + 3 {
		w := ta.do(t, "POST", "/hosts", adult, map[string]any{"address": fmt.Sprintf("127.0.0.1:%d", f.Port())})
		if w.Code == 429 {
			hit = true
			if w.Header().Get("Retry-After") == "" {
				t.Error("no Retry-After")
			}
			break
		}
	}
	if !hit {
		t.Error("attempts were never limited")
	}
	if w := ta.do(t, "POST", "/hosts", adult2, map[string]any{"address": fmt.Sprintf("127.0.0.1:%d", f.Port())}); w.Code == 429 {
		t.Error("another account was limited too")
	}
}

func TestEachPersonsOwn(t *testing.T) {
	testAllowLoopback(t)
	ta := newTestApp(t)
	f := sunshine(t)
	h := ta.add(t, f, adult)
	ta.pair(t, f, adult, h.ID)

	w := ta.do(t, "GET", "/hosts", adult, nil)
	list := decode[[]hostView](t, w)
	if len(list) != 1 || !list[0].Paired || list[0].State != "online" {
		t.Fatalf("list %+v", list)
	}
	if strings.Contains(w.Body.String(), "BEGIN") || strings.Contains(w.Body.String(), f.MAC) {
		t.Errorf("a secret: %s", w.Body)
	}
	base := fmt.Sprintf("/hosts/%d", h.ID)
	apps := decode[[]pcApp](t, ta.do(t, "GET", base+"/apps", adult, nil))
	if len(apps) != 3 {
		t.Fatalf("apps %+v", apps)
	}
	for _, app := range apps {
		switch app.Title {
		case "Desktop":
			if !app.IsDesktop || app.HasArt {
				t.Errorf("%+v", app)
			}
		case "Space Game":
			if app.IsDesktop || !app.HasArt {
				t.Errorf("%+v", app)
			}
		}
	}
	w = ta.do(t, "GET", base+"/apps/7/art", adult, nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("art: %d %v", w.Code, w.Header())
	}
	if got := decode[[]hostView](t, ta.do(t, "GET", "/hosts", adult2, nil)); len(got) != 0 {
		t.Errorf("another account sees %+v", got)
	}
	for _, c := range [][2]string{{"GET", base + "/apps"}, {"GET", base + "/apps/7/art"}, {"POST", base + "/apps/7/play"}, {"POST", base + "/pair"}, {"GET", base + "/pair"}, {"DELETE", base}, {"PATCH", base}, {"POST", base + "/wake"}, {"POST", base + "/quit"}} {
		if w := ta.do(t, c[0], c[1], adult2, map[string]any{"name": "mine"}); w.Code != 404 {
			t.Errorf("%s %s by another account: %d", c[0], c[1], w.Code)
		}
	}
	hb := ta.add(t, f, adult2)
	if hb.Paired || hb.State != "not-paired" {
		t.Errorf("the same PC for another account: %+v", hb)
	}
	if w := ta.do(t, "GET", fmt.Sprintf("/hosts/%d/apps", hb.ID), adult2, nil); w.Code != 502 {
		t.Errorf("apps with no pairing: %d", w.Code)
	}
	ia, _ := ta.identity(adult.id)
	ib, _ := ta.identity(adult2.id)
	if ia.ID == ib.ID || string(ia.CertPEM) == string(ib.CertPEM) {
		t.Error("two accounts share a certificate")
	}
	if w := ta.do(t, "PATCH", base, adult, map[string]any{"name": "Den"}); w.Code != 200 || decode[hostView](t, w).Name != "Den" {
		t.Errorf("rename: %d %s", w.Code, w.Body)
	}

	// The TV pages' sources: the person's PCs, one PC's rows, its games.
	items := func(u who, src sdui.Source, platform string) []sdui.Item {
		ans, err := ta.items(context.Background(), sdui.ItemsRequest{Source: src, Viewer: sdui.Viewer{User: u.id, ProfileKind: u.kind, Platform: platform}})
		if err != nil {
			t.Fatal(err)
		}
		return ans.Items
	}
	if rows := items(adult, sdui.Source{"type": ui.SourceHosts}, "tvos"); len(rows) != 1 || rows[0].Action == nil || !strings.Contains(rows[0].Action.To, ui.PageHost) {
		t.Errorf("hosts rows %+v", rows)
	}
	if rows := items(adult2, sdui.Source{"type": ui.SourceHost, "host": h.ID}, "tvos"); len(rows) != 0 {
		t.Errorf("another account's PC: %+v", rows)
	}
	rows := items(adult, sdui.Source{"type": ui.SourceHost, "host": strconv.FormatInt(h.ID, 10)}, "tvos")
	if len(rows) < 2 || rows[len(rows)-1].Action == nil || rows[len(rows)-1].Action.Op != ui.CommandRemove {
		t.Errorf("host rows %+v", rows)
	}
	if rows := items(adult, sdui.Source{"type": ui.SourceApps, "host": float64(h.ID)}, "tvos"); len(rows) != 3 || rows[2].Title != "Desktop" || rows[0].Action != nil {
		t.Errorf("app rows %+v", rows)
	}
	if rows := items(kid, sdui.Source{"type": ui.SourceHosts}, "tvos"); len(rows) != 0 {
		t.Errorf("a kid's rows: %+v", rows)
	}

	// An account that goes takes its PCs with it.
	ta.Forget(adult2.id)
	if got := decode[[]hostView](t, ta.do(t, "GET", "/hosts", adult2, nil)); len(got) != 0 {
		t.Errorf("after the account went: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(ta.Dir, "identities", "u3")); !os.IsNotExist(err) {
		t.Errorf("its key stayed: %v", err)
	}
	if w := ta.do(t, "DELETE", base, adult, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "paired devices") {
		t.Errorf("remove: %d %s", w.Code, w.Body)
	}
}

func TestNoProgramSaysSo(t *testing.T) {
	testAllowLoopback(t)
	ta := newTestApp(t)
	t.Setenv("WMS_WITTYCOUCH_HOST", "")
	f := sunshine(t)
	h := ta.add(t, f, adult)
	ta.pair(t, f, adult, h.ID)
	w := ta.do(t, "POST", fmt.Sprintf("/hosts/%d/apps/7/play", h.ID), adult, map[string]any{})
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"needProgram":true`) {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	if launches, _ := f.Counts(); launches != 0 {
		t.Error("a game was started with nothing to show it")
	}
}

// fakeLauncher is a "sandbox launcher" that runs this test program as the stand-in host, with the
// program it's given as its last arguments, as the server's answer for a sandboxed program does.
func fakeLauncher(t *testing.T) (string, []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "launcher")
	script := "#!/bin/sh\n[ \"$1\" = --sandboxed ] || exit 9\nshift\n" + fakehost.Env + "=ok exec '" + self + "' \"$@\"\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p, []string{"--sandboxed"}
}

func TestStreamsThroughTheProgram(t *testing.T) {
	testAllowLoopback(t)
	ta := newTestApp(t)
	t.Setenv("WMS_WITTYCOUCH_HOST", "")
	ta.srv.program, ta.srv.args = fakeLauncher(t)
	f := sunshine(t)
	h := ta.add(t, f, adult)
	ta.pair(t, f, adult, h.ID)
	if st := decode[map[string]any](t, ta.do(t, "GET", "/status", admin, nil)); st["ready"] != true || st["program"] != true {
		t.Errorf("status %v", st)
	}
	play := fmt.Sprintf("/hosts/%d/apps/7/play", h.ID)
	w := ta.do(t, "POST", play, adult, map[string]any{"width": 99999, "height": 1, "fps": 100000, "bitrate": 1})
	if w.Code != 200 {
		t.Fatalf("play: %d %s", w.Code, w.Body)
	}
	st := decode[sessionView](t, w)
	if st.ID == "" || st.Title != "Space Game" {
		t.Errorf("%+v", st)
	}
	if f.Running() != 7 {
		t.Errorf("running %d", f.Running())
	}
	if !strings.Contains(f.LastLaunch(), "mode=3840x360x120") {
		t.Errorf("sent %s", f.LastLaunch())
	}
	if !Busy() {
		t.Error("not busy while streaming")
	}
	url := "/sessions/" + st.ID
	for _, c := range [][2]string{{"GET", url}, {"POST", url + "/webrtc"}, {"DELETE", url}} {
		if w := ta.do(t, c[0], c[1], adult2, map[string]any{"sdp": "x"}); w.Code != 404 {
			t.Errorf("%s by another account: %d", c[0], w.Code)
		}
	}
	if w := ta.do(t, "GET", url, adult, nil); w.Code != 200 {
		t.Errorf("own stream: %d", w.Code)
	}
	if w := ta.do(t, "POST", url+"/webrtc", adult, map[string]any{"sdp": "not an offer"}); w.Code != 502 {
		t.Errorf("a bad offer: %d %s", w.Code, w.Body)
	}
	if w := ta.do(t, "POST", play, adult, map[string]any{}); w.Code != 200 {
		t.Errorf("again: %d %s", w.Code, w.Body)
	}
	if w := ta.do(t, "POST", fmt.Sprintf("/hosts/%d/apps/%d/play", h.ID, f.Apps[1].ID), adult, map[string]any{}); w.Code != 409 {
		t.Errorf("another game: %d %s", w.Code, w.Body)
	}
	if w := ta.do(t, "POST", fmt.Sprintf("/hosts/%d/apps/12345/play", h.ID), adult, map[string]any{}); w.Code != 404 {
		t.Errorf("no such app: %d", w.Code)
	}
	ta.mu.Lock()
	var sid string
	for id := range ta.sessions {
		sid = id
	}
	ta.mu.Unlock()
	if w := ta.do(t, "DELETE", "/sessions/"+sid+"?quit=1", adult, nil); w.Code != 200 {
		t.Errorf("end: %d %s", w.Code, w.Body)
	}
	if f.Running() != 0 {
		t.Error("the game is still running")
	}
}

func TestSessionAddressIsRebuilt(t *testing.T) {
	good, err := cleanSessionURL("rtspenc://10.0.0.9:48010", "192.168.1.2")
	if err != nil || good != "rtspenc://192.168.1.2:48010" {
		t.Errorf("%q %v", good, err)
	}
	if v, err := cleanSessionURL("rtsp://[fd00::1]:48010", "fd00::1"); err != nil || v != "rtsp://[fd00::1]:48010" {
		t.Errorf("%q %v", v, err)
	}
	for _, bad := range []string{"", "http://x:48010", "rtsp://x", "rtsp://x:22", "rtsp://x:99999", "rtsp://x:48010/../..", "rtsp://x:48010?y=1", "file:///etc/passwd", "rtsp://x:48010\nHost: evil"} {
		if v, err := cleanSessionURL(bad, "192.168.1.2"); err == nil {
			t.Errorf("%q became %q", bad, v)
		}
	}
	ctx := context.Background()
	if _, err := resolveAllowed(ctx, "169.254.169.254", gamestream.Policy{AllowPublic: true}); err == nil {
		t.Error("the metadata address")
	}
	if _, err := resolveAllowed(ctx, "8.8.8.8", gamestream.Policy{}); err == nil {
		t.Error("the internet, when it isn't allowed")
	}
	if ip, err := resolveAllowed(ctx, "192.168.1.2", gamestream.Policy{}); err != nil || ip != "192.168.1.2" {
		t.Errorf("%q %v", ip, err)
	}
}
