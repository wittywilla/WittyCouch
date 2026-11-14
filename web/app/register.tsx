// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors
//
// WittyCouch's screens in WittyMediaServer's web app (module API 5; integration.json says what it
// claims). The app calls register(host) once, when one of these is first needed: Game PCs, one PC's
// page, the player around a stream, and Settings, Game PCs. Every other app draws the SDUI pages
// in sdui.json (wittycouch, wittycouch-host) instead.
import { lazy, Suspense, type ReactNode } from 'react'
import { Spinner } from '@wms/app/components/common'

const CouchPage = lazy(() => import('./Couch'))
const CouchHostPage = lazy(() => import('./Couch').then((m) => ({ default: m.CouchHostPage })))
const CouchPlayPage = lazy(() => import('./Couch').then((m) => ({ default: m.CouchPlayPage })))
const CouchSettings = lazy(() => import('./Settings'))

const wait = (el: ReactNode) => () => <Suspense fallback={<Spinner />}>{el}</Suspense>

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function register(_host: unknown): any {
  return {
    routes: {
      '/couch': wait(<CouchPage />),
      '/couch/:id': wait(<CouchHostPage />),
      '/couch/:id/play/:app': wait(<CouchPlayPage />),
    },
    settings: {
      couch: () => <Suspense fallback={null}><CouchSettings /></Suspense>,
    },
  }
}
