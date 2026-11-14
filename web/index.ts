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

// WittyCouch's web half: its screens in the web app (register, module API 5), and the player for
// game streams from any PC running Sunshine, which a page provides the API to through CouchHost
// (WittyRigs' shared PCs play through it too). The server half is the Go service of the same name.
export { default } from './Player'
export type { CouchHost, StreamInfo, StreamRequest } from './Player'
export { register } from './app/register'
