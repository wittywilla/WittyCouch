// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/wittywilla/wittymodule"
)

// Lifecycle answers the lifecycle calls integration.json says WittyCouch answers: taken off
// without keeping its data, its folder (database, keys, pictures) goes.
func Lifecycle(ctx context.Context, call string, args wittymodule.LifecycleArgs) error {
	if call == wittymodule.CallUninstall && !args.KeepData {
		Stop()
		if a, err := Get(); err == nil {
			a.DB.Close()
			return os.RemoveAll(a.Dir)
		}
	}
	return nil
}

var watchOnce sync.Once

// Watch follows the server's events while the service runs: an account that goes takes its
// streams, PCs, key and pictures with it.
func Watch() {
	watchOnce.Do(func() {
		go func() {
			var after int64
			for {
				a, err := Get()
				if err != nil {
					time.Sleep(30 * time.Second)
					continue
				}
				evs, next, err := a.Host.Events(context.Background(), after, 50*time.Second)
				if err != nil {
					time.Sleep(10 * time.Second)
					continue
				}
				after = next
				for _, e := range evs {
					if e.Type != "user.deleted" {
						continue
					}
					var d struct {
						User int64 `json:"user"`
					}
					if json.Unmarshal(e.Data, &d) == nil && d.User > 0 {
						a.Forget(d.User)
					}
				}
			}
		}()
	})
}

// Forget ends an account's streams and removes what it kept here.
func (a *App) Forget(u int64) {
	a.mu.Lock()
	var streams []*stream
	for _, st := range a.sessions {
		if st.User == u {
			streams = append(streams, st)
		}
	}
	delete(a.identities, u)
	delete(a.attempts, u)
	a.mu.Unlock()
	for _, st := range streams {
		st.Session.Close()
	}
	ctx := context.Background()
	if hosts, err := a.hostsOf(ctx, u); err == nil {
		for _, h := range hosts {
			a.cancelPair(h.ID)
			a.mu.Lock()
			delete(a.pairs, h.ID)
			delete(a.statuses, h.ID)
			delete(a.apps, h.ID)
			delete(a.wakes, h.ID)
			a.mu.Unlock()
		}
	}
	_, _ = a.DB.ExecContext(ctx, `DELETE FROM hosts WHERE user_id = ?`, u)
	_ = os.RemoveAll(filepath.Join(a.Dir, "identities", "u"+strconv.FormatInt(u, 10)))
	files, _ := filepath.Glob(filepath.Join(a.Dir, "art", fmt.Sprintf("u%d-h*.img", u)))
	for _, f := range files {
		_ = os.Remove(f)
	}
	a.Log.Info("wittycouch: an account's PCs went with it", "cat", "games", "user", strconv.FormatInt(u, 10))
}
