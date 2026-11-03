---
name: rating-policy
description: Decide who may do what in this Arandu package, and change an action. Use when the request is to "add an action", "add a permission", "let the owner do X", "let the moderation do X", "everything returns 403", "the tests say ErrForbidden", "add a Model-backed service method", "why does Find authorize twice", "filter the list by author", "a system grant", "a scoped grant", or when a change touches policy.go, service.go, owner.go, moderation.go or grants.go. Covers the three policies, the Grant, authorization before the Model, the custom block that survives regeneration, why the tenant is never read from the request, the one place a Grant is scoped to the tenant of a row, and the shapes that look like authorization and are not.
license: MIT
---

# The policies

There are three, and they answer for different people.

- `RatingPolicy` is the author's. It allows an action to a subject that was
  granted it (`s.Can(action)`, filled by arandu-permission), on a rating of the
  subject's own tenant, and for the actions about a stored rating only when the
  subject wrote it or holds `rating.manage_space`.
- `OwnerPolicy` is the owner of a subject's. Ownership is the one thing the
  package cannot know, so the application answers it (`Subjects`) and the policy
  compares: a `Target` whose owner is not the subject's tenant is refused before
  any rule.
- `ModerationPolicy` is the platform team's. It allows an action to a subject of
  `Config.TeamTenant` that was granted it, and refuses a guest and a subject
  somebody else is viewing as. It is the one policy that does not compare the
  record's tenant with the subject's: crossing tenants is what the moderation
  does.

None has a branch that allows an action by role. A subject that holds no action,
an administrator included, is refused everything; the default-deny test walks
every action with one.

## How a request actually gets through

1. The service validates the input. A rejected input never reaches a policy
   (the moderation's `reach` runs first because it also finds the report).
2. The service calls `security.Authorize(ctx, s.policy, actor, ACTION, record)`.
3. `Can` returns nil, or the reason it will not.
4. Only then does a `security.Grant` exist, and the service reaches
   `Ratings(s.db)` with it.

The Model requires a Grant and applies its tenant, but it does not decide which
action the Grant represents, so `Authorize` before the first `Ratings(s.db)` is
mandatory even though the terminal also takes `g`.

## The procedure for an action

**1. Declare it** in `policy.go` with a doc comment, and put it in `Actions()`
or `TeamActions()`, never both. Say in `reads` whether it only looks: an action
not listed there issues a Grant that writes.

**2. Write the rule inside the custom block.** Nothing regenerates the file in
this repository, so the markers cost nothing and buy one thing: the same policy
regenerated elsewhere keeps what is between them. What is not written there
stays closed.

**3. Leave the tenant check above the block alone.** It runs before every rule:
a rule that allows an author to read their own record would otherwise allow it
across customers. The test asserts the message contains `another tenant`.

**4. Add it to the tests.** `everyAction` in `tests/Unit/policy_test.go` is the
whole set the default-deny tests walk; a list holding ten of eleven passes while
the eleventh is open. Add the rule's cases to the table beside it, and name the
action in the changelog (`TestEveryActionIsNamedInAReleasedChangelogEntry`).

## Reads are authorized like writes

`Find` authorizes twice: the first call sees an empty rating, so every rule about
the record never ran; the second, on the row, is the one that keeps the policy
honest. A rating the second call refuses is answered `ErrNotFound`, as one of
another space is: telling an author that a rating exists and is not theirs tells
them what exists. `List` authorizes once and narrows the statement by a predicate
(`author_id`), because a policy call per row would still read the rows.

The owner's reads work the same way: `owns` and `reach` ask `OwnerPolicy` with
nothing in hand, ask the application who owns the subject, and ask again with the
owner it named. A subject the account does not own is `ErrNotFound`.

## The moderation crosses tenants in steps

`ModerationService.reach` asks `ModerationPolicy` for a realm Grant, finds the
report's tenant through `internal/platform` (the reports table with no tenant
filter), and then acts with a Grant scoped to that one tenant. A decision touches
two tenants -- the owner's, where the report is, and the author's, where the
rating is -- and each gets its own scoped Grant. Nothing a request names chooses a
tenant.

## The one place a Grant is scoped

`grants.go` is the only file that builds a Grant without a Policy answering for
it, and `TestAGrantIsScopedInGrantsGoAlone` holds that to the two calls of
`scopedGrant` -- the account's, and the one that names who really acts when the
subject is somebody an operator acts as -- each marked `//arandu:system-grant`
with the reason. `scopedGrant` checks the Grant it comes from was issued for the
same action, that the tenant can be one, and, when the act writes, that the
Grant is writable: a subject somebody is only viewing as cannot reach another
account through it.

It is used for the mark on a rating that says a report is open (the owner writes
it in the author's tenant), and for the moderation's reads and writes. A third
use is a decision to take in review, not a line to find in a diff. Do not reach
for `security.SystemGrant` anywhere else in this package.

## Shapes that look like authorization and are not

- **A tenant taken from the request.** The path, the body, the query string, a
  header: values the caller chose. `data.Tenant(g)` is the only source, and
  `TestNoTenantIsReadOutOfTheRequest` fails on the others. The subject of a
  rating is named by the address, and the service checks it with the
  application: it is never a scope.
- **A check in the handler.** Handlers read the input and who is asking, ask the
  service, and return what it returned. `TestHandlersLeaveTheSubjectAndTheStatusToTheFramework`
  fails on one that holds the database or constructs a query.
- **A refusal that explains itself to the client.** The router answers with the
  status and its standard sentence; the reason is in the log.
- **A button hidden by `auth.Allows`.** It decides what to draw, never whether to
  do it; the route behind the button asks the policy again.
- **Eligibility as authorization.** `Config.Eligibility` says whether an account
  may rate a subject; it runs after the policy allowed the act and never instead
  of it.

## Run the gates

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go') \
  && go build ./... && go vet ./... && go test -race -p 2 ./...
```
