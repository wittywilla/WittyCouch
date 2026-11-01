// Command wittycouch-module is WittyCouch's service: the only way a server reaches it. It talks to
// the computers games run on, and holds the streams from them. It runs while it's in use, and
// listens on this machine only. See package service in WittyModule.
package main

import (
	"github.com/wittywilla/wittycouch"
	"github.com/wittywilla/wittycouch/rpc"
	"github.com/wittywilla/wittymodule/service"
)

func main() {
	service.Main(service.Config{
		Manifest:  wittycouch.ManifestJSON,
		Handlers:  rpc.Handlers,
		ErrorCode: rpc.ErrorCode,
		Busy:      rpc.Busy,
	})
}
