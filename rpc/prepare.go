// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

package rpc

import "os/exec"

// prepare is how a stream's program starts. It has nothing to do yet: a helper's children
// already have what the server gave it.
func prepare(*exec.Cmd) {}
