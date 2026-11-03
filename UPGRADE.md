# Upgrade Guide

## Unreleased

## v0.1.0

First release: nothing to upgrade from. A new installation reads the README, in
this order: `go get`, the wiring (`TeamTenant`, `Subjects` and `Eligibility` are
the required settings), the actions in the arandu-permission catalogue,
`aru migrate`, and `aru vendor:publish` with `aru view:build` for the screens.
The package requires Framework v0.67.0, Hesape v0.88.0, Kyse v0.51.0 (the
`Rating` group of stars first appears there) and arandu-activitylog v0.7.1, and
Go 1.26.10.
