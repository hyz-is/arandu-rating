---
name: rating-package
description: Install, wire and use the Arandu Rating package (Go, Arandu) in an application. Use when the request is to "install Arandu Rating", "add ratings to the app", "go get github.com/hyz-is/arandu-rating", "wire it into bootstrap/app.go", "register the module", "rate an application of the catalogue", "write the Subjects", "write the Eligibility", "write the Notifier", "report a rating", "moderate ratings", "everything under /ratings returns 403", "it redirects to /auth/login", "the table does not exist", "no such table", or when a project's go.mod already requires github.com/hyz-is/arandu-rating. Covers the wiring and where each line goes, the Config fields, the three sides and their prefixes, the actions to declare in arandu-permission, the Subjects, the Eligibility, the Notifier and the Directory the application writes, the migration step that is not optional, and the views to publish.
license: MIT
metadata:
  audience: app
---

# Using Arandu Rating

Ratings with stars for a multi-tenant application. An account rates a *subject*
-- an application of the catalogue, a plan, a partner; the package is generic
over a kind and an identifier -- with one to five stars and an optional text,
published at once. The owner of the subject reads what it received and reports
what it disagrees with; the platform team's moderation removes, corrects or
archives. Three separate sides, each with its own routes and screens. It is
registered by hand: there is no service provider, no container and no
discovery, so if a line is not written in the application, it does not happen.

## Install

```bash
go get github.com/hyz-is/arandu-rating
```

## The wiring, and where each line goes

All of it is in `bootstrap/app.go`. Nothing else in the application changes,
and `aru make:module` does not edit this file for you.

The import:

```go
import (
	rating "github.com/hyz-is/arandu-rating"
)
```

The construction, in `Build`, after the session store and the activity log exist
and before `k.Register`:

```go
	ratingModule, err := rating.New(rating.Config{
		TeamTenant:  cfg.Auth.Tenant,             // the tenant the platform team signs in to
		Subjects:    catalogueSubjects{apps: apps}, // what a subject is, and who owns it
		Eligibility: connectedApps{apps: apps},     // who may rate what
		Notifier:    ratingMail{mailer: mailer},
		Activity:    activityModule.Logger(),
	}, db, sessions)
	if err != nil {
		return App{}, err
	}
```

The registration, inside the `k.Register(...)` call already there:

```go
		ratingModule,
```

`New` refuses a configuration that cannot work, a nil database handle and a nil
session store, so a wiring mistake costs one restart. `TeamTenant`, `Subjects`
and `Eligibility` are the required settings.

An application that keeps the customer's screens on its panel and the
moderation's on its operator console mounts the sides itself, each with its
guards:

```go
ratingModule.CustomerRoutes(panel)  // the author's and the owner's
ratingModule.TeamRoutes(console)    // the moderation's
```

## Then run the migrations and publish the views

```bash
aru migrate
aru vendor:publish --tag=view --apply
aru view:build
```

and import the three directories `aru view:build` wrote
(`storage/framework/views/modules/rating`, `.../rating/owner` and
`.../rating/admin`). Without them the module refuses to boot and names what is
missing.

`aru migrate` is not optional and never runs at boot: with N replicas, N
migrations race each other. **Symptom to recognise:** `no such table`, or
`relation does not exist`, from a route that authorized correctly.

## Declare the actions

Permissions are arandu-permission's, and the policies decide by the actions it
fills on the subject. A role opens nothing. Add both lists to the catalogue, the
moderation's to the platform team's groups only:

```go
rating.Actions()     // customers: create, view_own, view_space, update, delete, manage_space, view_summary, view_subject, report
rating.TeamActions() // the moderation: view_all, moderate
```

Mount arandu-permission's `Roles()` in `Config.Middleware`, so the actions are
resolved on each request. Put the operator console's own guard in
`Config.TeamMiddleware`.

**Symptom to recognise:** every route answers `403` to someone who should be let
in. The subject holds no action: the group was not given the action, or `Roles()`
is not mounted. A team action carried by a subject outside `TeamTenant` is
refused on purpose.

## The things the application writes

- `Subjects`: `Subject(ctx, ref) (rating.Subject, error)` -- name, link and the
  tenant that owns it; `rating.ErrNoSubject` when there is none -- and
  `Owned(ctx, tenant)`, the subjects a tenant owns. The owner is the
  application's answer and nothing a request names.
- `Eligibility`: `CanRate(ctx, tenant, userID, ref) (bool, error)`. In an
  application where only the people who use a thing may judge it, this is where
  that is said. A `false` is a `403`.
- `Notifier` (optional): `Notify(ctx, rating.Event) error`, told when a rating is
  reported (`Kind.ForTeam()`) and when a report is decided. The package sends no
  e-mail; a failure of the notifier is logged and never undoes the change.
- `Directory` (optional): `Person(ctx, tenant, id)`. Without it a rating keeps no
  author name.

## Link to the screens

Build addresses from `ratingModule.URLs()`: `Rate(ref)` is the form that rates a
subject, or the account's rating of it when there is one. Draw stars beside the
catalogue with `ratingModule.Ratings().Summaries(ctx, who, refs)`: count and
average of the published ratings, no text and no name.

## Routes

Every route sits behind `RequireAuth`, mounted by the package itself. A request
with no session is sent to `/auth/login`: 303, or `HX-Redirect` for an htmx
request. **Symptom to recognise:** every route redirects to `/auth/login`.
Nobody is signed in on that request: the session cookie did not arrive, or the
session store passed to `New` is not the one the application signs people in
with.

Defaults: the author's under `/ratings` (`rating.index`, `rating.create`,
`rating.store`, `rating.show`, `rating.update`, `rating.withdraw`), the owner's
under `/ratings/received` (`rating.owner.index`, `.subject`, `.reports`,
`.report_form`, `.report`), the moderation's under `/admin/ratings`
(`rating.team.index`, `.show`, `.decide`). `Config.Prefix`, `Config.OwnerPrefix`,
`Config.TeamPrefix` and `Config.Paths` (`PortuguesePaths`) move them; the names
never change.

| status | what happened |
| --- | --- |
| `303` to `/auth/login` | there is no session on the request |
| `403` | the subject was not granted the action, is not eligible, or is not on the team |
| `404` | no such rating, subject or report for this subject: it does not exist, is in another space, or is somebody else's |
| `303` back | the input was rejected; the messages and what was typed are in the flash |
| `422` `application/problem+json` | the same, for a client that asked for JSON |

## Reporting a problem

Issues and pull requests go to the repository the module path names,
`github.com/hyz-is/arandu-rating`, which belongs to `hyz-is`. A vulnerability
goes to the private advisory form named in that repository's `SECURITY.md`, and
never into an issue.

MIT licensed. Copyright HYZIS.
