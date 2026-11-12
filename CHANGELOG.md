# Changelog

What changed in WittyCouch, newest first. What changed before it had a repository of its own is in the
[WittyMediaServer changelog](https://github.com/wittywilla/WittyMediaServer/blob/master/CHANGELOG.md).

## Unreleased

- **Module API 4:** carries `sdui.json`, its theme scope (the games accent), and needs WittyMediaServer 0.8.0-alpha or later. It adds no pages of its own: its screens are the server's.
- What a PC reports about itself (its versions, its stream address) can no longer add settings of its own to what WittyCouch Host is told: line breaks and NULs in those values are dropped.
- Fits servers up to 0.8.x, so WittyMediaServer 0.8.0-alpha accepts it; it was limited to 0.6.x.
- A host program of protocol 10 or 12 is no longer taken for one of protocol 1: the version check reads the number exactly.
- The steps for pairing a device name the page as it's called now: Settings, Game PCs.
- WittyCouch is now a service of its own, with nothing compiled into a server: `wittycouch-module` talks to the computers games run on and holds the streams from them.
- WittyCouch is now a project of its own, developed and released from this repository.
