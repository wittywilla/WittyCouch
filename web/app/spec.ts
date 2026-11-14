// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors
import { Cc, R, mk, secN, secP, secR } from '@wms/app/spec/model'

// The spec pages behind the Game PCs screens (canvas page "gamepcs"), drawn by the app's SpecPage.
// A person's own PCs, from GET /hosts: each row opens that PC.
export interface HostRow { id: number; name: string; paired: boolean; state: string; message: string; currentGame?: string }

export function couchHostsSpec(hosts: HostRow[]) {
  const rows = hosts.map((h) => {
    const up = h.state === 'online' || h.state === 'busy'
    const running = h.state === 'busy' && !!h.currentGame
    const value = running ? `Running: ${h.currentGame}` : !h.paired ? 'Needs pairing' : up ? 'Online' : ''
    return { ...R(h.name, value, up, h.paired && !running ? h.message : ''), id: `host.${h.id}`, to: `/couch/${h.id}` }
  })
  return mk('gamepcs', 'Game PCs', 'Your game PCs', 'Stream from a PC you own.', [secR('PCs', rows)])
}

// One PC's status line, then what can be done to it: wake it, end what's running. Ids: pc, wake, quit.
export interface PcState { name: string; state: string; message: string; currentGame?: string; paired: boolean; canWake: boolean; canQuit: boolean }

const WORDS: Record<string, string> = { online: 'Online', busy: 'Online', asleep: 'Asleep', offline: 'Not reachable', unpaired: 'Not paired' }
const word = (state: string) => WORDS[state] || (state ? state.charAt(0).toUpperCase() + state.slice(1) : 'Unknown')

export function pcSpec(pc: PcState, o: { busy?: boolean; wakeLabel?: string }) {
  const running = pc.state === 'busy' && pc.currentGame ? `Running: ${pc.currentGame}` : ''
  const chips = []
  if (pc.canWake) chips.push({ ...Cc(o.wakeLabel || 'Wake it up'), id: 'wake', disabled: o.busy })
  if (pc.canQuit) chips.push({ ...Cc('End what’s running'), id: 'quit', disabled: o.busy })
  const sections = [secR('', [{ ...R(pc.name, [word(pc.state), running].filter(Boolean).join(' · '), pc.state === 'online' || pc.state === 'busy', running ? '' : pc.message), id: 'pc' }])]
  if (chips.length) sections.push(secP('', chips))
  if (pc.state === 'asleep' && !pc.canWake) sections.push(secN('', 'Waking it from here needs it to have been paired and on once.'))
  return mk('gamepcs', 'Game PCs', 'Your game PCs', 'Stream from a PC you own.', sections)
}
