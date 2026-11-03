---
name: rating-module
description: Change what this Arandu package registers — its routes, handlers, configuration, response shape, screens, sentences or schema. Use when the request is to "add a route", "add a handler", "add a config option", "change the prefix", "add a field to the response", "add a column", "write a migration", "add a screen", "add a sentence", "add a rule to the entity", "regenerate the query", "return 404", or when a change touches module.go, config.go, handlers.go, model.go, migrations.go, screens.go or a *Query.go file. Covers foundation.Module and its optional interfaces, the route table of the three sides, thin handlers, ctx.User, returning errors for the router to answer, the generated queries and aru model:build, where the rules of an entity live, the migration name that carries the order, what a response may carry, and the catalogue.
license: MIT
---

# The module contract

`foundation.Module` is `Name()` and `Routes()` and nothing else. That pair is the
whole public contract between a package and the framework. This package also
takes on `Migratable`, `Bootable` (it refuses to boot until its views are
linked) and `Publishable`; the compile-time proofs sit at the top of `module.go`
and a new contract adds a line there.

## Adding a route

**1. Add a line to `routeTable` in `config.go`**, with a name, a method, a path
built from `c.Prefix`, `c.OwnerPrefix` or `c.TeamPrefix` and a `Paths` segment,
never a literal, and the side it belongs to. The author's and the owner's sides
are the customer's panel and share a session; the moderation's is the console's.
The same table is registered on a throwaway mux by `Validate`, so a collision
fails in `New`. Add the segment to `Paths`, `DefaultPaths`, `PortuguesePaths` and
`fields()`.

**2. Add the handler** to `handlers()` in `handlers.go`, thin:

```go
func (m *Module) withdraw(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	if err := m.ratings.Withdraw(ctx.Ctx(), who, ctx.Param("id")); err != nil {
		return err
	}
	return ctx.Status(stdhttp.StatusNoContent)
}
```

`register` mounts `RequireAuth` before the first route, so a route added to the
table needs a session like every other. `ctx.User()` reads the subject the guard
put on the request; no handler loads the session again or takes an identity from
the input. Behind the guard there is always a subject, and the zero one is
refused by `security.Authorize` before any policy runs.

**3. Return the error; the router answers it.** `validation.Errors` goes back
with the messages in the flash (422 for JSON), `ErrNotFound` is 404 because it
wraps `model.ErrModelNotFound`, a policy refusal is 403.
`TestHandlersLeaveTheSubjectAndTheStatusToTheFramework` fails on a handler that
calls `errors.Is`, `fhttp.Refuse` or `http.Error`, that does not read
`ctx.User()`, or that constructs a query.

**4. Add the case to the tests.** `everyRoute` in `tests/Feature/routes_test.go`
is a table of every route; the no-session, wrong-side and registered-routes tests
read it, and so does the count of routes per side.

## Changing a Model

Each entity embeds `model.Model`; its table is declared once beside it
(`ratingTable`, `reportTable`), with `ManualKey: true` and `NoTimestamps: true`
because the key is text the service writes and the timestamps come from
`Config.Now`. After changing a struct or a table run

```sh
aru model:build
```

and commit what it rewrote; `aru model:build --check` exits 1 when a file is
missing, stale or edited by hand. Keep rows as pointers after `New`, `First` or
`Get`: a copy keeps the model of the row it was taken from and refuses to write.
Every query starts at `Ratings(s.db)` or `Reports(s.db)`; the constructor returns
one mutable query.

The tables that read across tenants -- the owner's reading of the ratings its
subject received, the averages, the moderation's queue -- are in
`internal/platform`, `Global`, with their own generated queries. Keep it that
way: they are reached only from `owner.go`, `moderation.go` and the average in
`service.go`, after a policy answered, and their rows are copied into snapshots
that hold no connection.

The rules of an entity go in the custom block of `model.go` as pure methods (no
database, no clock read inside: the time arrives as a parameter). The services
orchestrate and hold none.

## What may leave in a response

`authorFields`, `ownerFields`, `ownerReportFields` and `teamReportFields` in
`model.go` are the declared lists. The author's carries the state and the
moderation's reason; the owner's carries the author by name and no identifier of
their account; the team's adds the tenants. A new column that should be seen is
added in the struct and in the right list; one that should not is added in the
struct only. `TenantID` is in no customer response. A text a person typed reaches
a page only escaped by the view: nothing in the package converts it to
`template.HTML`.

## Adding a configuration field

A doc comment on the field; a rule in `Validate` if there is a value it cannot be
(a value out of range is refused, not clamped); a default in `withDefaults` as a
named constant, which runs after `Validate`; and a case in
`TestNewRefusesAWiringThatCannotWork` and `TestTheConfigurationRefusesWhatCannotWork`.
A limit of the business -- a length, a window, whether the moderation may change
the stars -- is a field, never a literal in a service.

## Sentences and screens

Every sentence is a line of `resources/lang/<locale>/rating.json`, in every
shipped locale with the same placeholders; `tests/Unit/translation_test.go`
fails on a missing, extra or unused line. A screen is a `.kyse.go` file under
`resources/views/modules/rating/`, with its page data in `pages.go` and its
builder in `screens.go`; add it to the render probe in
`tests/Unit/published_views_compile_test.go`, which also fails on an inline
script, style or event attribute. Stars are Kyse's `Rating` group of radios:
`ReadOnly` to show a score, one `Name` per row in a list, because radios of one
name are one choice.

## Adding a migration

Append it to `Migrations()` with a name that sorts after the last one:
`20261103_0001_create_ratings` is fixed once published, so a change is a new
migration. `TestTheModuleDeclaresItsSchema` requires sorted names and a `Down`
with the right signature. Use types that spell the same in SQLite, PostgreSQL
and MySQL, give timestamps no database default, and index to match the `ORDER BY`
of the listing, tenant first. A new column is nullable or has a default, and
removing one takes two releases. Migrations never run at boot.
