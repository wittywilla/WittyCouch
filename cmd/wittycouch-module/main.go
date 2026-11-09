// SPDX-License-Identifier: AGPL-3.0-or-later
//
// WittyCouch - Play the games on any PC running Sunshine, in your browser.
// Copyright (C) 2026 WittyWilla and contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

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
