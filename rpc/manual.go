// Package rpc answers a server's calls to WittyCouch's service: gen.go is generated, by kit/facade in
// WittyModule, and this is what can't be.
package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"sync"

	"github.com/wittywilla/wittycouch/gamestream"
	"github.com/wittywilla/wittycouch/relay"
	"github.com/wittywilla/wittymodule/service"
)

var (
	mu       sync.Mutex
	sessions = map[string]*running{}
	log      = slog.New(slog.NewTextHandler(os.Stderr, nil))
)

// running is a stream, and how it ended once it has.
type running struct {
	s      *relay.Session
	reason string
	over   chan struct{}
}

func resolveSession(raw json.RawMessage) (*relay.Session, error) {
	var r struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	if x := sessions[r.ID]; x != nil {
		return x.s, nil
	}
	return nil, errors.New("that stream has ended")
}

func init() {
	Handlers["relay.New"] = func(ctx context.Context, _ json.RawMessage, args []json.RawMessage) ([]any, any, error) {
		var o relay.Options
		if err := service.Arg(args, 0, &o); err != nil {
			return nil, nil, err
		}
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		id := hex.EncodeToString(b)
		x := &running{over: make(chan struct{})}
		o.Log, o.Prepare = log, prepare
		o.OnEnd = func(reason string) {
			mu.Lock()
			delete(sessions, id)
			mu.Unlock()
			x.reason = reason
			close(x.over)
		}
		mu.Lock()
		sessions[id] = x
		mu.Unlock()
		s, err := relay.New(ctx, o)
		if err != nil {
			mu.Lock()
			delete(sessions, id)
			mu.Unlock()
			return nil, nil, err
		}
		mu.Lock()
		x.s = s
		mu.Unlock()
		return []any{struct {
			ID string `json:"id"`
		}{id}}, nil, nil
	}
	Handlers["relay.Session.Wait"] = func(ctx context.Context, _ json.RawMessage, args []json.RawMessage) ([]any, any, error) {
		var id string
		if err := service.Arg(args, 0, &id); err != nil {
			return nil, nil, err
		}
		mu.Lock()
		x := sessions[id]
		mu.Unlock()
		type answer struct {
			Over   bool   `json:"over"`
			Reason string `json:"reason"`
		}
		if x == nil {
			return []any{answer{Over: true}}, nil, nil
		}
		select {
		case <-x.over:
			return []any{answer{Over: true, Reason: x.reason}}, nil, nil
		case <-ctx.Done():
			return []any{answer{}}, nil, nil
		}
	}
	Handlers["gamestream.CheckAddress"] = func(ctx context.Context, _ json.RawMessage, args []json.RawMessage) ([]any, any, error) {
		var host string
		if err := service.Arg(args, 0, &host); err != nil {
			return nil, nil, err
		}
		r, err := gamestream.CheckAddress(ctx, host, nil)
		return []any{r}, nil, err
	}
	Handlers["relay.CheckProgram"] = func(ctx context.Context, _ json.RawMessage, args []json.RawMessage) ([]any, any, error) {
		var program string
		if err := service.Arg(args, 0, &program); err != nil {
			return nil, nil, err
		}
		return nil, nil, relay.CheckProgram(ctx, program, prepare)
	}
}

// Busy says a stream is running.
func Busy() bool {
	mu.Lock()
	defer mu.Unlock()
	return len(sessions) > 0
}
