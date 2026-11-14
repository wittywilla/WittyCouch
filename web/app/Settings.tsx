// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '@wms/app/api'
import { LoadError, Spinner } from '@wms/app/components/common'
import LegalNote from '@wms/app/components/LegalNote'
import { toast } from '@wms/app/state'
import { PageHead, Row, Section, Switch } from '@wms/app/pages/settings/ui'
import { WebLink } from '@wms/app/components/WebLink'
import { COUCH } from './paths'
import './couch.css'

// Settings, Game PCs: who may connect to PCs running Sunshine from this server, and where. These
// are WittyCouch's own settings (sdui.json), which every app's Settings also draws; here they keep
// the page they had. Sharing a PC with everyone on this server is WittyRigs' (Settings, Shared PCs).

type Credit = { name: string; role: string; license: string; source: string }
type Status = { allowed: boolean; ready: boolean; program?: boolean; access?: string; credits: Credit[] }
type View = { values: Record<string, unknown> }

const SETTINGS = '/sdui/modules/wittycouch/settings'

export function Credits({ list }: { list: Credit[] }) {
  return (
    <ul className="rig-apps">
      {list.map((c) => (
        <li key={c.name}><b>{c.name}</b>: {c.role}. License: {c.license}. <WebLink href={c.source}>Source</WebLink></li>
      ))}
    </ul>
  )
}

const DEFAULTS: Record<string, unknown> = { access: 'adults', private: true, internet: true, customPorts: false, maxStreams: 2, udpPort: 0, publicIp: '', stun: '' }

export default function CouchSettings() {
  const [st, setSt] = useState<Status | null>(null)
  const [values, setValues] = useState<Record<string, unknown> | null>(null)
  const [failed, setFailed] = useState(false)
  const loadAll = () => {
    setFailed(false)
    api.get<Status>(`${COUCH}/status`).then(setSt).catch(() => setFailed(true))
    api.get<View>(SETTINGS).then((v) => setValues({ ...DEFAULTS, ...v.values })).catch(() => setFailed(true))
  }
  useEffect(loadAll, [])
  const put = async (key: string, value: unknown) => {
    try {
      const v = await api.put<View>(SETTINGS, { values: { [key]: value } })
      setValues({ ...DEFAULTS, ...v.values })
      toast('Saved')
    } catch (e: any) { toast(e.message) }
  }
  if (failed && (!st || !values)) return <LoadError what="This page" onRetry={loadAll} />
  if (!st || !values) return <Spinner />
  const on = (k: string) => values[k] === true || values[k] === '1' || values[k] === 'true'
  const text = (k: string) => (values[k] == null || values[k] === 0 ? '' : String(values[k]))
  const num = (s: string) => { const n = parseInt(s.trim(), 10); return Number.isFinite(n) ? n : 0 }
  return (
    <>
      <PageHead title="Game PCs" desc="People can play the games on any PC running Sunshine, right here in their browser, through this server. This feature is called WittyCouch." />
      <Section title="This server" desc="Playing in the page needs a separate program, WittyCouch Host.">
        {st.ready
          ? <p>WittyCouch Host is installed and working.</p>
          : <div className="rig-note" role="status">
              <p><b>WittyCouch Host isn’t installed on this server.</b> People can add and pair PCs, and the Moonlight app still works, but they can’t play in the page until it is.</p>
              <p>It’s a module: <Link to="/settings/modules/store/wittycouchhost">add it from the module store</Link>. It’s AGPL-3.0-or-later software with Moonlight-derived parts under the GPL-3.0, with its source alongside, and it runs as a program of its own beside this one, which is AGPL-3.0-or-later too. See <a href="https://github.com/wittywilla/WittyCouchHost" target="_blank" rel="noreferrer noopener">its project</a>.</p>
            </div>}
      </Section>
      <Section title="Who may connect" desc="This has the server connect wherever a person says, so it’s yours to decide. It only ever speaks this one protocol, and nothing else is reachable through it.">
        <div className="set-form">
          <label>Who can use it
            <select className="select" value={String(values.access || 'adults')} onChange={(e) => put('access', e.target.value)}>
              <option value="adults">Adult profiles with no limits set (recommended)</option>
              <option value="everyone">Everyone, Kids and Guest profiles too</option>
              <option value="admins">Admins only</option>
            </select>
            <small className="muted">A PC’s desktop is outside anything age limits can reach, so Kids and Guest profiles and limited accounts start without this. Give a Kids profile single games through a shared PC (WittyRigs).</small>
          </label>
        </div>
        <Row label="PCs on this server’s own network"><Switch label="PCs on this server’s own network" checked={on('private')} onChange={(v: boolean) => put('private', v)} /></Row>
        <Row label="PCs over the internet, such as a friend’s"><Switch label="PCs over the internet, such as a friend’s" checked={on('internet')} onChange={(v: boolean) => put('internet', v)} /></Row>
        <Row label="Ports other than Sunshine’s usual one" desc="Admins can always. Off makes it harder to use this to look for what’s open on a network."><Switch label="Ports other than Sunshine’s usual one" checked={on('customPorts')} onChange={(v: boolean) => put('customPorts', v)} /></Row>
        <div className="set-form">
          <label>Streams at once
            <input className="input" type="number" min={1} max={16} defaultValue={text('maxStreams') || '2'} onBlur={(e) => put('maxStreams', Math.min(16, Math.max(1, num(e.target.value) || 2)))} />
            <small className="muted">Each stream passes through this server, at up to its full bitrate. The server doesn’t re-encode it.</small>
          </label>
        </div>
      </Section>
      <Section title="Reaching browsers away from home" desc="Nothing needed at home. A stream goes from this server to a browser over UDP.">
        <div className="set-form">
          <label>UDP port
            <input className="input" inputMode="numeric" defaultValue={text('udpPort')} onBlur={(e) => put('udpPort', num(e.target.value))} placeholder="Any free port" />
            <small className="muted">Choose one to forward on your router, or to publish from a container.</small>
          </label>
          <label>This server’s public address
            <input className="input" defaultValue={text('publicIp')} onBlur={(e) => put('publicIp', e.target.value.trim())} autoCapitalize="off" spellCheck={false} />
          </label>
          <label>STUN server
            <input className="input" defaultValue={text('stun')} onBlur={(e) => put('stun', e.target.value.trim())} placeholder="None: nothing leaves your network" autoCapitalize="off" spellCheck={false} />
          </label>
        </div>
        <p className="muted">The PC being played is never opened to the internet by this: only this server is. Don’t forward Sunshine’s own ports, and <b>never</b> port 47990, its web interface, which can run commands on that PC.</p>
      </Section>
      <Section title="Built on">
        <Credits list={st.credits} />
      </Section>
      <LegalNote kind="thirdPartyServer" className="set-legal" />
    </>
  )
}
