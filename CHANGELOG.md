# Changelog

Everything worth knowing about a release of Arandu Rating is recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

A published module version is immutable: Go serves it from the proxy forever, so
a release is corrected by another release and never by moving a tag.

## [Unreleased]

## [0.1.0] - 2026-11-03

### Added

- Ratings with stars, reports and moderation, for the accounts of a multi-tenant
  application and for the platform team, as three separate sides: the author's
  and the owner's belong to the customer's panel and share its session, the
  moderation's to the operator console (`Module.CustomerRoutes`,
  `Module.TeamRoutes`, or `Module.Routes` for both). Each side has its own
  service, policy, controllers, screens and prefix.
- `Rating`: an account's evaluation of one subject -- a kind and an identifier
  the application chose, so the package is generic over what is rated -- with one
  to five stars and an optional text, published at once. An account rates a
  subject once, whoever in it wrote the text, and the database holds that line
  with a unique index. The author edits and deletes their own; a deleted rating
  loses its text, keeps its row, and the account can rate again over it. Its own
  rules are pure methods: `Revise`, `Withdraw`, `Renew`, `Remove`, `Alter`,
  `Release`.
- `Report`: the owner of a subject saying a rating should not stand, with a
  reason. A rating under a report keeps being published and counted until the
  moderation decides, and takes no change from its author in the meantime; it
  has one report open at a time, held by a conditional update so two reports at
  once file one. The decision keeps who decided, when, the justification, and
  the rating before and after.
- The author's side: `RatingService` with `List` (a data table paged, searched,
  ordered and filtered by stars and status on the server), `Find`, `Card`,
  `Create`, `Update`, `Withdraw`, `CanRate` and `Summaries`. An author reads what
  they wrote, or the whole space's with `rating.view_space`, and sees where each
  rating stands -- published, under review, removed by the moderation -- and the
  reason the moderation recorded.
- The owner's side: `OwnerService` with `Subjects` (the subjects the account owns
  with their average and count), `Received` (the published ratings of one, with
  the author by name and no identifier of their account), `Rating`, `Report` and
  `Filed` (the reports the account filed and what was decided about each).
- The moderation's side: `ModerationService` with `List` (the queue of reports of
  every space, oldest first), `Find` and `Decide`: remove the rating, correct its
  text, or archive the report. A removal and a correction close the report as
  upheld; the stars are the author's and stay, unless
  `Config.ModerationMayChangeScore` lets the moderation correct them. Nothing is
  deleted: a removed rating is a row that says `removed` and why.
- Averages and counts per subject count the published ratings only: a removed or
  a deleted rating does not count, and a reported one does until it is decided.
  `Summary` carries them, and `RatingService.Summaries` hands an application the
  numbers for the stars it draws beside its own catalogue.
- Three things the application provides, as interfaces in `Config`: `Subjects`
  (what a subject is, its name and link, and who owns it -- the tenant is the
  application's answer and nothing a request names), `Eligibility` (whether an
  account may rate a subject; a `false` is a 403) and, optional, `Directory` and
  `Notifier` (told when a rating is reported and when a report is decided).
- Settings with defaults, none of them a literal in a service: `MaxBodyChars`,
  `MaxReasonChars`, `MaxNoteChars`, `RequireBody`, `EditWindow`,
  `ModerationMayChangeScore` and `PageSize`.
- Fourteen routes, named `rating.*`: `rating.index`, `rating.create`,
  `rating.store`, `rating.show`, `rating.update`, `rating.withdraw`,
  `rating.owner.index`, `rating.owner.subject`, `rating.owner.reports`,
  `rating.owner.report_form`, `rating.owner.report`, `rating.team.index`,
  `rating.team.show` and `rating.team.decide`. Every GET answers the screen to a
  browser and JSON to a client that asks for it.
- Eleven actions, which arandu-permission fills on the subject and the policies
  decide on: `RatingCreate`, `RatingViewOwn`, `RatingViewSpace`, `RatingUpdate`,
  `RatingDelete`, `RatingManageSpace`, `RatingViewSummary`, `RatingViewSubject`,
  `RatingReport` (`Actions()`), `RatingViewAll` and `RatingModerate`
  (`TeamActions()`). A role opens nothing.
- Nine screens built from Kyse components -- the `Rating` group of stars, `DataTable`
  paged and filtered on the server, `Badge`, `Alert`, `FormDialog`, `Empty` --
  published to the application with `aru vendor:publish --tag=view`, in
  `pt-BR` and `en`, with no inline script or style.
- Activity recorded in arandu-activitylog under the log `rating` (`rating.created`,
  `rating.updated`, `rating.withdrawn`, `rating.reported`, `rating.removed`,
  `rating.altered`, `rating.report_dismissed`), under the Grant of the act and
  naming the operator when someone acts as the user. No entry carries a text.
- Migration `20261103_0001_create_ratings`, creating `ratings` and
  `rating_reports`.
