# Changelog

What changed in WittyCouch, newest first. What changed before it had a repository of its own is in the
[WittyMediaServer changelog](https://github.com/wittywilla/WittyMediaServer/blob/master/CHANGELOG.md).

## 0.4.2

- The README's pictures are retaken from WittyMediaServer 0.8.0-alpha.

## 0.4.1

- The first published release on module API 5, with the WittyModule 0.5.4 workflows. The tag v0.4.0 was made with workflows that couldn't build it and was never published.

## 0.4.0

- **Module API 5 (0.4.0):** WittyCouch owns the whole Game PCs feature, and the server has none of it. Its routes are its own, at `/api/modules/wittycouch/…` (`/status`, `/find`, `/hosts`, `/hosts/{id}` and its `pair`, `apps`, `wake`, `quit`, `/sessions/{sid}`), checked again in its service. Each person's PCs are in its own database, starting empty: nothing is brought over from a server's. Who may use it and where it may connect are its own settings (`access`, `private`, `internet`, `customPorts`, `maxStreams`, `udpPort`, `publicIp`, `stun`). The server says where WittyCouch Host is, and an account that goes takes its PCs, key and pictures with it.
- Game PCs, one PC's page, the player and Settings, Game PCs are its web part's, and every app draws its SDUI pages (`wittycouch`, `wittycouch-host`): a person's PCs, those found on the network to add with a press, waking, pairing with the PIN shown, ending what's running, removing, and each PC's games. On a TV a game is played in Moonlight.
- The server no longer calls it by facade: `rpc` and `facade.json` are gone. `relay` can run the host program with a launcher's arguments before its own (`Options.Args`, `StartHostWith`, `CheckProgramWith`).
- Built on WittyModule 0.5.4 and module API 5, with its web builder (its workflows use WittyModule 0.5.4); the module's own copy of `build-bundle.mjs` is gone.

- **Module API 4:** carries `sdui.json`, its theme scope (the games accent), and needs WittyMediaServer 0.8.0-alpha or later. It adds no pages of its own: its screens are the server's.
- What a PC reports about itself (its versions, its stream address) can no longer add settings of its own to what WittyCouch Host is told: line breaks and NULs in those values are dropped.
- Fits servers up to 0.8.x, so WittyMediaServer 0.8.0-alpha accepts it; it was limited to 0.6.x.
- A host program of protocol 10 or 12 is no longer taken for one of protocol 1: the version check reads the number exactly.
- The steps for pairing a device name the page as it's called now: Settings, Game PCs.
- WittyCouch is now a service of its own, with nothing compiled into a server: `wittycouch-module` talks to the computers games run on and holds the streams from them.
- WittyCouch is now a project of its own, developed and released from this repository.
