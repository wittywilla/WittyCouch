# Changelog

What changed in WittyCouch, newest first. What changed before it had a repository of its own is in the
[WittyMediaServer changelog](https://github.com/wittywilla/WittyMediaServer/blob/master/CHANGELOG.md).

## Unreleased

- A host program of protocol 10 or 12 is no longer taken for one of protocol 1: the version check reads the number exactly.
- The steps for pairing a device name the page as it's called now: Settings, Game PCs.
- WittyCouch is now a service of its own, with nothing compiled into a server: `wittycouch-module` talks to the computers games run on and holds the streams from them.
- WittyCouch is now a project of its own, developed and released from this repository.
