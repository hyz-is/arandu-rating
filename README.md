<p align="center">
  <img src=".github/logo.svg" alt="Arandu" width="180">
</p>

# Arandu Rating

Ratings with stars, reports and moderation for a multi-tenant
[Arandu](https://github.com/arandu-io/framework) application: an account rates
something it uses, the owner of that thing disagrees with a rating and reports
it, and the platform team decides.

The package is generic over what is rated. A **subject** is a kind and an
identifier the application chose -- an application of its catalogue, a plan, a
partner -- and the package knows nothing else about it. There are three sides,
and they are separate:

- **The author** rates a subject with one to five stars and an optional text, and
  the rating is published at once. An account rates a subject once. The author
  edits and deletes their own, and sees where each one stands -- published, under
  review, removed by the moderation -- and the reason the moderation recorded.
- **The owner of the subject** reads the ratings it received, with their average
  and count, and reports one it disagrees with, giving the reason. It does not
  edit or delete anybody's rating, and it follows what was decided about each
  report.
- **The moderation** works a queue of reports: it removes the rating, corrects
  its text, or archives the report and changes nothing. Every decision keeps who
  decided, when, the justification, and the rating before and after.

The author's and the owner's screens belong to the customer's panel and share its
session; the moderation's belong to the operator console, and the team's session
is valid there only. The module sends no message, knows no table of subjects or
users and keeps no list of the team. Those are small interfaces the application
implements with what it already has.

## Install

```bash
go get github.com/hyz-is/arandu-rating
```

It requires [arandu-activitylog](https://github.com/hyz-is/arandu-activitylog),
and expects the application to administer permissions with
[arandu-permission](https://github.com/hyz-is/arandu-permission).

## Wire it

An Arandu application registers a module explicitly. There is no service
provider, no container and no discovery, so these are the lines to paste into
`bootstrap/app.go` and there are no others.

The import, with the other module imports:

```go
import (
	rating "github.com/hyz-is/arandu-rating"
)
```

The construction, in `Build`, after the session store and the activity log exist
and before `k.Register`:

```go
	ratingModule, err := rating.New(rating.Config{
		// The tenant the platform team signs in to: the operator console's
		// realm.
		TeamTenant: cfg.Auth.Tenant,

		// What the application knows and the package does not. Both required.
		Subjects:    catalogueSubjects{apps: apps},  // what a subject is, and who owns it
		Eligibility: connectedApps{apps: apps},      // who may rate what

		// Three prefixes, the application's own addresses, in the segments of
		// its language.
		Prefix:      "/painel/avaliacoes",
		OwnerPrefix: "/painel/avaliacoes-recebidas",
		TeamPrefix:  "/admin/avaliacoes",
		Paths:       rating.PortuguesePaths,

		Directory: peopleDirectory{users: users}, // names, optional
		Notifier:  ratingMail{mailer: mailer},    // the application's own mailer, optional

		Activity: activityModule.Logger(), // arandu-activitylog

		// The actions arandu-permission fills are resolved on each request, so
		// its Roles() runs on every route; the console's guard runs on the
		// moderation's alone.
		Middleware:     []func(http.Handler) http.Handler{permissionModule.Roles()},
		TeamMiddleware: []func(http.Handler) http.Handler{consoleGuard},
	}, db, sessions)
	if err != nil {
		return App{}, err
	}
```

And the registration, inside the `k.Register(...)` call already there:

```go
		ratingModule,
```

An application that keeps the sides on different routers mounts them itself,
each with its own guards, instead of registering the routes through
`k.Register`:

```go
ratingModule.CustomerRoutes(panel)  // the author's and the owner's, with the panel's guards
ratingModule.TeamRoutes(console)    // the moderation's, with the console's
```

Then, once, before the application serves:

```bash
aru migrate
```

The package owns two tables (`ratings`, `rating_reports`), which is why
`arandu.mod.toml` says `migrations = true`. arandu-activitylog's migration runs
in the same step. Nothing migrates at boot.

## Give people access

The policies are in `policy.go`, and the actions reach them through the subject,
which arandu-permission fills. **A role opens nothing**: a subject that holds no
action is refused everything, an administrator included.

Declare the actions in the permission catalogue beside the application's own.
The customers' and the team's are two lists, so a catalogue an organization
grants from never holds a team action:

```go
permission.Config{
	Actions: append(append(app.Actions(), permission.Actions()...), rating.Actions()...),
	// ...
}
// and, in the catalogue of the platform's own team only:
//   rating.TeamActions()
```

| action | side | what it opens |
| --- | --- | --- |
| `rating.create` | author | rating a subject, in the subject's own space |
| `rating.view_own` | author | reading the ratings the subject wrote |
| `rating.view_space` | author | reading every rating of the space, whoever wrote it |
| `rating.update` | author | changing a rating the subject wrote |
| `rating.delete` | author | deleting a rating the subject wrote |
| `rating.manage_space` | author | with `rating.update` or `rating.delete`, changing and deleting the space's ratings whoever wrote them |
| `rating.view_summary` | author | reading how many ratings a subject has and their average, and no text or name |
| `rating.view_subject` | owner | reading the ratings the space's subjects received, and the reports the space filed |
| `rating.report` | owner | reporting a rating of a subject the space owns |
| `rating.view_all` | team | reading every report of every space |
| `rating.moderate` | team | deciding a report: remove, correct, or archive |

Each action opens what it names and nothing else, and the Grants for reading
(`rating.view_*`) only read, so the database refuses a write made with one.

A member of the team is a subject of `Config.TeamTenant` who holds the team's
actions. `ModerationPolicy` refuses a subject of any other tenant whatever it
carries, and a subject somebody else is viewing as.

Two things the actions do not decide, because the package cannot know them, are
answered by the application:

- whether an account may rate a subject at all (`Eligibility`); and
- who owns a subject (`Subjects`). A subject another tenant owns is not found,
  whatever the subject was granted, and a subject cannot be rated by its owner.

## The things the application provides

### Subjects

What a subject is called, where it is, and who owns it. The package asks on every
act that depends on ownership and trusts the answer, so it answers
`rating.ErrNoSubject` for a reference that is not one and never invents an owner.

```go
type catalogueSubjects struct{ apps *repositories.Applications }

func (c catalogueSubjects) Subject(ctx context.Context, ref rating.Ref) (rating.Subject, error) {
	if ref.Type != "application" {
		return rating.Subject{}, rating.ErrNoSubject
	}
	app, err := c.apps.Find(ctx, ref.ID)
	if errors.Is(err, model.ErrModelNotFound) {
		return rating.Subject{}, rating.ErrNoSubject
	}
	if err != nil {
		return rating.Subject{}, err
	}
	return rating.Subject{
		Name: app.Name, URL: "/catalogo/" + app.ID, // an address of the application's own, or https
		OwnerTenant: app.OrganizationID,            // a valid tenant: the application's answer
	}, nil
}

// Owned lists the subjects a tenant owns, in a size a person reads.
func (c catalogueSubjects) Owned(ctx context.Context, tenant string) ([]rating.Subject, error) { /* ... */ }
```

The owner is the application's answer and nothing a request names, and it has to
be a valid tenant. A link that is neither a path of the application nor an
`https` address is dropped when the screens are drawn.

### Eligibility

Whether the user of an account may rate a subject. In an application where only
the people who use a thing may judge it, this is where that is said:

```go
type connectedApps struct{ apps *repositories.Applications }

func (c connectedApps) CanRate(ctx context.Context, tenant, userID string, ref rating.Ref) (bool, error) {
	return c.apps.IsConnected(ctx, tenant, ref.ID)
}
```

It runs after the policy allowed the act, never instead of it, and a `false` is a
`403`. The package does not rate for an account that owns the subject whatever
Eligibility says.

### Notifier (optional)

Told after the change is stored, when a rating is reported (`EventReported`,
`Kind.ForTeam()` is true: tell the moderation) and when a report is decided
(`EventDecided`: tell the reporter, and the author of the rating when the decision
touched it).

```go
type ratingMail struct{ mailer *mail.Mailer }

func (n ratingMail) Notify(ctx context.Context, e rating.Event) error {
	if e.Kind.ForTeam() {
		return n.mailer.Send(ctx, "moderacao@example.com", "Avaliação denunciada: "+e.Subject.Name, e.Report.Reason)
	}
	// the reporter's and the author's addresses are the application's to find
	return nil
}
```

Its failure does not undo the change: the report is filed and the decision taken,
and a mail server that is down is no reason to ask anybody to do it twice. The
error is logged (`Config.Logger`) with the event, the rating and the error, never
the text. An application that must not lose a notification enqueues it here and
delivers it from its queue.

### Directory (optional)

```go
type Directory interface {
	Person(ctx context.Context, tenant, id string) (rating.Person, error) // rating.ErrNoPerson if none
}
```

Without one a rating keeps no author name, and the screens call the author by what
they have.

## Link to the screens, and draw the stars

`Module.URLs()` builds every address, for an application that links to the
screens from its own pages: `URLs().Rate(ref)` is the form that rates a subject,
or the account's own rating of it when there is one.

To draw stars beside the catalogue, ask for the numbers:

```go
sums, err := ratingModule.Ratings().Summaries(ctx, who, []rating.Ref{{Type: "application", ID: app.ID}})
// sums[ref].Count and sums[ref].Average -- published ratings only
```

It asks the policy (`rating.view_summary`) and opens no text and no name.

## How a rating lives

1. The author gives one to five stars and, if they want, a text up to
   `MaxBodyChars`. It is **published at once**.
2. If the owner of the subject disagrees, it **reports** the rating with a
   reason. The rating stays published and **keeps counting** in the average until
   the moderation decides, and it is **under review**: its author can neither
   change nor delete it meanwhile. A rating has one open report at a time.
3. The moderation decides:
   - **remove**: the rating becomes `removed`. Nothing is deleted. It stops
     counting, the owner no longer reads its text, the author sees the state and
     the justification, and the account cannot rate that subject again.
   - **alter**: the text is corrected (to take out an offence, say) and the rating
     stays published. The stars are the author's: the moderation may change them
     only if `Config.ModerationMayChangeScore` says so.
   - **dismiss**: the report is archived, the rating is exactly as it was, and
     its author is not told of a moderation that did nothing to them.

   Every decision requires a justification, and keeps who decided, when, the
   justification and the rating before and after. A decision is final.
4. An author's own deletion keeps the row, without the text, so the account can
   rate again over it. It is refused while a report is open.

Averages and counts include **published ratings only**.

## Publish the views

This package carries the markup of its own nine pages and hands it over instead
of rendering it from the inside, because a page you cannot edit is a page that
says the wrong thing in your product.

Look at what would be written, then write it:

```bash
aru vendor:publish --tag=view
aru vendor:publish --tag=view --apply
```

Nothing is written without `--apply`. The preview lists every file as `create`,
`update`, `unchanged` or `conflict`, and running the command a second time
writes nothing. A file changed outside its `arandu:begin custom` markers is
reported as a conflict and left alone; `--force` publishes over one, and even
then what is inside the markers is carried forward.

The files land under `resources/views/modules/rating/`, and from that point they
are yours: the author's `index`, `form` and `show`, the owner's `owner/index`,
`owner/subject`, `owner/reports` and `owner/report`, and the moderation's
`admin/index` and `admin/show`. Nothing of this package is compiled beside them,
so no view name is registered twice and no rule has to decide which of two files
won, and a view that changes later does not reach a project that already
published it.

Two steps are left to you, and they are left to you because a command that
edited `bootstrap/app.go` behind your back is a command whose output nobody can
explain. Compile what was written:

```bash
aru view:build
```

and import the three directories it wrote into, with the other imports:

```go
	_ "your/module/path/storage/framework/views/modules/rating"
	_ "your/module/path/storage/framework/views/modules/rating/owner"
	_ "your/module/path/storage/framework/views/modules/rating/admin"
```

Without those imports the views are not in the binary, and the module refuses to
boot rather than answering the first request that reaches one of them with a 500.
The refusal names the views, the command and the imports.

The pages extend your `layouts.app` and use the Kyse components (`Rating` for the
stars -- a group of radios, so the keyboard, the required check and the submitted
value are the browser's -- `DataTable` paged, searched, ordered and filtered on
the server, `Badge`, `Alert`, `FormDialog`, `Empty`). They draw no inline style
and no inline script, so a content security policy of `'self'` holds. A text a
person wrote is plain: the view escapes it, and nothing in the package turns it
into markup.

## Configuration

| field | required | meaning |
| --- | --- | --- |
| `TeamTenant` | yes | the tenant the platform team signs in to. |
| `Subjects` | yes | the `Subjects`: what a subject is and who owns it. |
| `Eligibility` | yes | the `Eligibility`: whether an account may rate a subject. |
| `Prefix` | no | where the author's routes are mounted. Defaults to `/ratings`. |
| `OwnerPrefix` | no | where the owner's routes are mounted. Defaults to `/ratings/received`. |
| `TeamPrefix` | no | where the moderation's routes are mounted. Defaults to `/admin/ratings`. |
| `Paths` | no | the segments of every route. Empty fields keep `DefaultPaths`; `PortuguesePaths` is the set in pt-BR. |
| `PageSize` | no | rows per page. Defaults to 25, refused above 100. |
| `MaxBodyChars` | no | the longest text of a rating. Defaults to 2000, refused above 10000. |
| `MaxReasonChars` | no | the longest reason of a report. Defaults to 1000, refused above 5000. |
| `MaxNoteChars` | no | the longest justification of a decision. Defaults to 1000, refused above 5000. |
| `RequireBody` | no | makes the text mandatory. Defaults to false: the stars alone are a rating. |
| `EditWindow` | no | how long after it was written an author may still change a rating. Zero never closes. |
| `ModerationMayChangeScore` | no | lets the moderation correct the stars when it alters a rating. Defaults to false: it corrects the text. |
| `Middleware`, `TeamMiddleware` | no | run after `RequireAuth`, on every route and on the moderation's only. A guard says "this request is on the right side" and stops; the policy still decides. |
| `Directory` | no | names the people who rate and report. |
| `Notifier` | no | told about a report filed and a report decided. Nil tells nobody. |
| `Activity` | no | arandu-activitylog's logger. Nil records nothing. |
| `Locale` | no | the language of screens, messages and log descriptions. Defaults to `pt-BR`; `en` is shipped too. |
| `Translator` | no | the application's own catalogue, asked before the shipped one, key by key (`rating.screen.index_title`). |
| `TitleSeparator` | no | joins a screen's title to `APP_NAME` in the document title. |
| `Location` | no | the timezone instants are written in. Defaults to `America/Sao_Paulo`. |
| `Chrome` | no | what the application's layout draws around a screen, carried on the page as `Screen.Chrome()`. |
| `Now`, `Logger` | no | the clock, and where a notifier's failure is logged. |

`New` returns an error rather than starting half-wired: a prefix in the
framework's namespace, two routes that collide, a limit out of range, no
`Subjects`, no `Eligibility` or a team tenant that cannot be one fail where they
are written.

## Routes

Every route sits behind `RequireAuth`: a visitor with no session is sent to
`/auth/login`, and a handler reads who is asking with `ctx.User()`. The paths
below are the defaults; `Config.Prefix`, `Config.OwnerPrefix`, `Config.TeamPrefix`
and `Config.Paths` move them, and the route names never change.

| method | path | name | |
| --- | --- | --- | --- |
| `GET` | `/ratings` | `rating.index` | the account's ratings |
| `GET` | `/ratings/subject/{type}/{id}` | `rating.create` | the form that rates a subject, or the rating the account already gave it |
| `POST` | `/ratings/subject/{type}/{id}` | `rating.store` | rate it |
| `GET` | `/ratings/{id}` | `rating.show` | one rating, and where it stands |
| `POST` | `/ratings/{id}/edit` | `rating.update` | change it |
| `POST` | `/ratings/{id}/withdrawal` | `rating.withdraw` | delete it |
| `GET` | `/ratings/received` | `rating.owner.index` | the subjects the account owns |
| `GET` | `/ratings/received/subject/{type}/{id}` | `rating.owner.subject` | the ratings one received |
| `GET` | `/ratings/received/reports` | `rating.owner.reports` | the reports the account filed |
| `GET` | `/ratings/received/reports/{rating}` | `rating.owner.report_form` | the form that reports a rating |
| `POST` | `/ratings/received/reports/{rating}` | `rating.owner.report` | report it |
| `GET` | `/admin/ratings` | `rating.team.index` | the queue of reports |
| `GET` | `/admin/ratings/{id}` | `rating.team.show` | one report, and the rating |
| `POST` | `/admin/ratings/{id}/decision` | `rating.team.decide` | decide it |

Every `GET` answers the screen to a browser and JSON to a client that asks for it
(`Accept: application/json`); every form posts to the same routes the JSON clients
use, and a write answers `201`/`200` with the resource to a client that asks for
JSON, and redirects otherwise. The lists take `q`, `status` (repeatable), `score`
(repeatable, on the lists of ratings), `sort`, `dir` and `page`; the moderation's
queue opens on the reports that wait, oldest first, and `?all=1` or a `status`
shows the rest. A sort the list does not offer, or a status that is not one, is
dropped.

The decision takes `verdict` (`remove`, `alter` or `dismiss`), `note` (required),
and for an alteration `body` and, if the application allows it, `score`.

| status | what happened |
| --- | --- |
| `303` to `/auth/login` | there is no session on the request |
| `403` | the policy refused: the subject was not granted the action, is not on the team, or the application says the account may not rate the subject |
| `404` | no such rating, subject or report for this subject: it does not exist, it is in another space, it is somebody else's, or the subject is not the account's |
| `303` back to the page the request came from | the input was rejected; the messages (in Portuguese by default) and what was typed are in the flash |
| `422` `application/problem+json` | the input was rejected and the request asked for JSON; the messages are in `errors`, keyed by field |
| `409` | two writes raced for the same rating, and the database refused the second |

The package does not choose these: its handlers return what the service returned,
and the framework's router answers it. A refusal carries no detail beyond the
status and its standard sentence.

## What is recorded

With `Config.Activity` the log `rating` holds one entry per act that changes
something, about the rating, in the tenant of the act:

`rating.created`, `rating.updated`, `rating.withdrawn`, `rating.reported`,
`rating.removed`, `rating.altered` and `rating.report_dismissed`.

A moderator's act is written in the author's tenant with the moderator as its
causer, so it is in the log of the space it happened in and in the platform
administrator's read of every tenant's. An act made while somebody acts as the
user names the operator beside the account. No entry carries a text; the stars
are there, and the rating's identifier.

## The data path

`Rating` and `Report` embed `model.Model`, and each table is declared once,
beside the entity, in `model.go`. `aru model:build` generates `RatingQuery.go` and
`ReportQuery.go` from them: `Ratings(db)`, which starts a `*RatingQuery`, and
`RatingCollection`. The files are committed and never edited by hand; regenerate
them after changing an entity, and `aru model:build --check` fails when one is out
of date.

`RatingService`, `OwnerService` and `ModerationService` own `*data.DB` and follow
`validate -> security.Authorize -> Grant -> Model terminal`; handlers never hold
the database or start a query. The tenant of every write is `data.Tenant(g)`,
from the Grant, never from the path, the body, the query string or a header.

Whose is each row: a rating is in the tenant of the account that wrote it and a
report in the tenant of the owner of the subject. Reading across them goes through
`internal/platform`, which no application can import: the tables again, with no
tenant filter, read only after a policy answered, whose rows never leave the
service as anything but snapshots with no connection. The writes that reach into
another tenant -- the mark on a rating that says a report is open, and the
moderation's decision -- go through `scopedGrant` in `grants.go`, which narrows a
Grant that was issued for the same action to the one tenant of the row, and does
not let a subject somebody is only viewing as write through it.

## Layout

```
module.go        registration, routes, the contract with the framework
config.go        what the application passes in, and the route table
contracts.go     what the application implements: subjects, eligibility, directory, notifier
model.go         the entities, their own rules, and what they may answer with
policy.go        who may do what: the author, the owner, the moderation
service.go       the author's use cases, and what the three services share
owner.go         the owner's use cases
moderation.go    the moderation's use cases
grants.go        the one place a Grant is scoped to the tenant of a row
activity.go      the activity log and the notifier
handlers.go      the controllers of the three sides
pages.go         what the screens are handed
screens.go       building it from what a service answered
migrations.go    the schema
translation.go   the sentences, in resources/lang
views.go         the files the application takes ownership of
```

## What is not here

- A balance, credits or points: that is arandu-wallet's. A rating changes none.
- A second way to decide who may do what, a local role table or a list of the
  team: permissions are arandu-permission's, activity is arandu-activitylog's.
- Markdown, HTML or images in a rating. The text is plain.
- A table of the things that are rated, or of users: the application's.
- E-mail: the package sends none.

## Tests

```bash
export GOWORK=off
go test -race ./...
```

The denial suite constructs the services with a nil database, so even building
`Ratings(nil)` would panic. The structural twin reads every exported service
method and checks the same order on the allowed path, that a Grant is scoped in
`grants.go` alone, that the tables that read every tenant are reached only after
a policy answered, and that nothing converts a person's text to markup. The
feature suite runs the whole thing over a real SQLite database, and over the
router with sessions: an author rates, edits and deletes; an account rates once;
eligibility is the application's; the owner reads, reports and cannot report what
is not its own; another tenant's row is a 404; the moderation removes, corrects
and archives with the record of who and why; averages ignore what is not
published; two reports at once file one. The views are published into a scratch
project, compiled with the view compiler an application runs, and drawn once with
sample data.

## Licence

MIT. See [LICENSE.md](LICENSE.md). Copyright HYZIS.
