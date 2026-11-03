package rating

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"

	"github.com/hyz-is/arandu-rating/internal/platform"
)

// core is what the three services share: the database handle, the
// configuration, the sentences and the clock. It holds no policy, because each
// service has its own, and nothing here authorizes anything: a method of core
// is reached only after the service that called it asked its policy and holds
// the Grant.
type core struct {
	db     *data.DB
	cfg    Config
	labels Labels
	now    clock
}

func newCore(db *data.DB, cfg Config) *core {
	read := cfg.Now
	return &core{db: db, cfg: cfg, labels: labelsOf(cfg), now: func() time.Time { return read().UTC() }}
}

// RatingService orchestrates what an account does with the ratings it gives: it
// validates the request, asks the policy for a Grant, applies the rules the
// entity holds for itself, and persists with that Grant.
//
// It receives its collaborators through the constructor. There is no container
// and no resolution by reflection: what this service is made of is written at
// the one place that builds it, and reading that place is how somebody learns
// what the package touches.
//
// Everything a handler is allowed to do for an author goes through here. The
// service is the only owner of the database handle on this side, so the request
// layer cannot reach a Model before the policy has answered.
type RatingService struct {
	*core
	policy RatingPolicy
}

// NewRatingService wires the service over the application's database handle.
func NewRatingService(db *data.DB, cfg Config) *RatingService {
	return &RatingService{core: newCore(db, cfg.withDefaults()), policy: RatingPolicy{}}
}

// viewer is how much of the space an author reads.
type viewer int

const (
	// viewsOwn reads the ratings the subject wrote.
	viewsOwn viewer = iota
	// viewsSpace reads every rating of the space.
	viewsSpace
)

// action is the action the viewer was authorized with.
func (v viewer) action() security.Action {
	if v == viewsSpace {
		return RatingViewSpace
	}
	return RatingViewOwn
}

// authorizeView asks the policy for the widest reading the subject holds: the
// whole space's, else their own. Both questions go to the policy; the answer
// says which one it allowed, and the statement is narrowed to it.
func (s *RatingService) authorizeView(ctx context.Context, actor security.Subject) (security.Grant, viewer, error) {
	g, err := security.Authorize(ctx, s.policy, actor, RatingViewSpace, Rating{})
	if err == nil {
		return g, viewsSpace, nil
	}
	g, ownErr := security.Authorize(ctx, s.policy, actor, RatingViewOwn, Rating{})
	if ownErr != nil {
		return security.Grant{}, viewsOwn, ownErr
	}
	return g, viewsOwn, nil
}

// visible narrows a statement to what the viewer reads: every rating of the
// space, or the subject's own. The tenant is the Grant's, applied by the table;
// this is the second predicate, and the one a rule about individual records
// belongs in. A rating its author withdrew is read by nobody.
func (v viewer) visible(q *RatingQuery, actor security.Subject) *RatingQuery {
	q = q.Where("status", "!=", string(StatusWithdrawn))
	if v == viewsOwn {
		return q.Where("author_id", "=", actor.ID)
	}
	return q
}

// sortableRating is the ordering allowlist of the author's list. A column name
// taken directly from a request would turn ordering into an injection surface.
var sortableRating = map[string]string{
	"": "updated_at", "updated_at": "updated_at", "created_at": "created_at", "score": "score", "status": "status",
	"subject": "subject_id",
}

// List returns a page of the account's ratings: their own, or the whole
// space's when the policy allows it, searched, filtered by stars and status and
// ordered by the server.
func (s *RatingService) List(ctx context.Context, actor security.Subject, q ListQuery) (RatingPage, error) {
	g, scope, err := s.authorizeView(ctx, actor)
	if err != nil {
		return RatingPage{}, err
	}
	column, ok := sortableRating[q.Sort]
	if !ok {
		return RatingPage{}, s.labels.invalid("sort", "sort")
	}
	scores, err := s.scores(q.Scores)
	if err != nil {
		return RatingPage{}, err
	}
	statuses := make([]any, 0, len(q.Statuses))
	for _, status := range q.Statuses {
		if !status.Valid() || status == StatusWithdrawn {
			return RatingPage{}, s.labels.invalid("status", "status")
		}
		statuses = append(statuses, string(status))
	}
	perPage, page := s.paging(q.PerPage, q.Page)

	filtered := func() *RatingQuery {
		b := scope.visible(Ratings(s.db), actor)
		if len(scores) > 0 {
			b = b.WhereIn("score", scores)
		}
		if len(statuses) > 0 {
			b = b.WhereIn("status", statuses)
		}
		matching(b.Base(), q.Search, "body", "subject_id")
		return b
	}
	total, err := filtered().Count(ctx, g)
	if err != nil {
		return RatingPage{}, err
	}
	rows := filtered()
	if q.Dir == "asc" {
		rows = rows.OrderBy(column)
	} else {
		rows = rows.OrderByDesc(column)
	}
	found, err := rows.OrderByDesc("id").Offset((page-1)*perPage).Limit(perPage).Get(ctx, g)
	if err != nil {
		return RatingPage{}, err
	}
	out := RatingPage{Rows: make([]RatingEntry, 0, len(found)), Total: int(total), Page: page, Pages: pagesOf(int(total), perPage)}
	seen := map[Ref]Subject{}
	for _, r := range found {
		subject, err := s.describe(ctx, r.Subject(), seen)
		if err != nil {
			return RatingPage{}, err
		}
		out.Rows = append(out.Rows, RatingEntry{Rating: ratingViewOf(r), Subject: subject})
	}
	return out, nil
}

// Find returns one rating as its author reads it: the stars and the text, where
// it stands, why the moderation took it down if it did, and what the subject's
// ratings add up to. A rating of another space, one the subject may not read
// and one that was withdrawn are not found.
func (s *RatingService) Find(ctx context.Context, actor security.Subject, id string) (RatingDetail, error) {
	g, scope, err := s.authorizeView(ctx, actor)
	if err != nil {
		return RatingDetail{}, err
	}
	r, err := s.seen(ctx, g, scope, actor, id)
	if err != nil {
		return RatingDetail{}, err
	}
	subject, err := s.describe(ctx, r.Subject(), map[Ref]Subject{})
	if err != nil {
		return RatingDetail{}, err
	}
	summary, err := s.summaryIfAllowed(ctx, actor, r.Subject())
	if err != nil {
		return RatingDetail{}, err
	}
	return RatingDetail{
		Rating: ratingViewOf(r), Subject: subject, Summary: summary,
		CanEdit: r.CanEdit(s.now(), s.cfg.EditWindow), CanWithdraw: r.CanWithdraw(),
	}, nil
}

// seen loads a rating the viewer reads, and asks the policy a second time about
// the row itself.
//
// The first Authorize saw an empty rating, so every rule about the record --
// whose it is -- never ran. A rating the policy refuses on the second call is
// answered as not found, as a rating of another space is: telling an author
// that a rating exists and is not theirs tells them what exists.
func (s *RatingService) seen(ctx context.Context, g security.Grant, scope viewer, actor security.Subject, id string) (*Rating, error) {
	r, err := scope.visible(Ratings(s.db).WhereKey(id), actor).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, scope.action(), *r); err != nil {
		return nil, ErrNotFound
	}
	return r, nil
}

// Card returns the subject the form rates: what it is, what its ratings add up
// to when the subject may read that, and whether the account already rated it.
// It asks exactly what Create asks, so a form is drawn only for an account that
// can submit it.
func (s *RatingService) Card(ctx context.Context, actor security.Subject, ref Ref) (SubjectCard, error) {
	g, err := security.Authorize(ctx, s.policy, actor, RatingCreate, Rating{})
	if err != nil {
		return SubjectCard{}, err
	}
	if !ref.Valid() {
		return SubjectCard{}, ErrNotFound
	}
	subject, err := s.rateable(ctx, g, actor, ref)
	if err != nil {
		return SubjectCard{}, err
	}
	card := SubjectCard{Subject: subject}
	existing, err := Ratings(s.db).Where("subject_type", "=", ref.Type).Where("subject_id", "=", ref.ID).First(ctx, g)
	if err != nil {
		return SubjectCard{}, err
	}
	if existing != nil && !existing.CanRenew() {
		card.Rated, card.RatingID = true, existing.ID
	}
	if card.Summary, err = s.summaryIfAllowed(ctx, actor, ref); err != nil {
		return SubjectCard{}, err
	}
	return card, nil
}

// rateable is the subject, once the application agrees the account may rate it:
// it exists, it is not the account's own, and Eligibility says yes. The refusals
// are the policy's kind -- forbidden -- and a subject that does not exist is not
// found.
func (c *core) rateable(ctx context.Context, g security.Grant, actor security.Subject, ref Ref) (Subject, error) {
	subject, err := c.subject(ctx, ref)
	if err != nil {
		return Subject{}, err
	}
	tenant := data.Tenant(g)
	if subject.OwnerTenant == tenant {
		return Subject{}, fmt.Errorf("%w: an account does not rate what it owns", security.ErrForbidden)
	}
	ok, err := c.cfg.Eligibility.CanRate(ctx, tenant, actor.ID, ref)
	if err != nil {
		return Subject{}, err
	}
	if !ok {
		return Subject{}, fmt.Errorf("%w: the application says this account may not rate %s", security.ErrForbidden, ref)
	}
	return subject, nil
}

// Create writes the account's rating of a subject, in the account's own space,
// published at once. An account rates a subject once: a second rating is
// refused, unless the first was withdrawn, in which case it is written over.
func (s *RatingService) Create(ctx context.Context, actor security.Subject, ref Ref, in RateRequest) (*Rating, error) {
	if !ref.Valid() {
		return nil, ErrNotFound
	}
	if errs := s.validateRate(in); errs.Any() {
		return nil, errs
	}
	proposed := Rating{SubjectType: ref.Type, SubjectID: ref.ID, Score: in.Score}
	g, err := security.Authorize(ctx, s.policy, actor, RatingCreate, proposed)
	if err != nil {
		return nil, err
	}
	if _, err := s.rateable(ctx, g, actor, ref); err != nil {
		return nil, err
	}
	who := s.person(ctx, data.Tenant(g), actor.ID)
	now := s.now()
	var written *Rating
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		existing, err := Ratings(s.db).Where("subject_type", "=", ref.Type).Where("subject_id", "=", ref.ID).First(ctx, g)
		if err != nil {
			return err
		}
		switch {
		case existing != nil && existing.CanRenew():
			if err := existing.Renew(in.Score, body(in.Body), actor.ID, who.Name, now); err != nil {
				return err
			}
			if _, err := existing.Save(ctx, g); err != nil {
				return err
			}
			written = existing
			return nil
		case existing != nil && existing.Status == StatusRemoved:
			return s.labels.invalid("score", "rating_removed")
		case existing != nil:
			return s.labels.invalid("score", "already_rated")
		}
		row, err := Ratings(s.db).New()
		if err != nil {
			return err
		}
		if row.ID, err = data.NewID(); err != nil {
			return err
		}
		row.TenantID = data.Tenant(g)
		row.SubjectType, row.SubjectID = ref.Type, ref.ID
		row.AuthorID, row.AuthorName = actor.ID, who.Name
		row.Score, row.Body = in.Score, body(in.Body)
		row.Status = StatusPublished
		row.CreatedAt, row.UpdatedAt = now, now
		if _, err := row.Save(ctx, g); err != nil {
			return err
		}
		written = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, ActivityRatingCreated, written.ID, ref, map[string]any{"score": written.Score}); err != nil {
		return nil, err
	}
	return written, nil
}

// Update changes the stars and the text of a rating the account wrote. A rating
// under review, one the moderation took down and one past Config.EditWindow
// take no change.
func (s *RatingService) Update(ctx context.Context, actor security.Subject, id string, in RateRequest) (*Rating, error) {
	if errs := s.validateRate(in); errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, RatingUpdate, Rating{})
	if err != nil {
		return nil, err
	}
	r, err := s.owned(ctx, g, actor, RatingUpdate, id)
	if err != nil {
		return nil, err
	}
	from := r.Score
	if err := r.Revise(in.Score, body(in.Body), s.now(), s.cfg.EditWindow); err != nil {
		return nil, s.notEditable(r)
	}
	if _, err := r.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, ActivityRatingUpdated, r.ID, r.Subject(), map[string]any{"from": from, "to": r.Score}); err != nil {
		return nil, err
	}
	return r, nil
}

// Withdraw deletes what the account wrote. The text is gone, the stars stop
// counting, and the account can rate the subject again. A rating under review
// is not deleted: the report on it is decided first.
func (s *RatingService) Withdraw(ctx context.Context, actor security.Subject, id string) error {
	g, err := security.Authorize(ctx, s.policy, actor, RatingDelete, Rating{})
	if err != nil {
		return err
	}
	r, err := s.owned(ctx, g, actor, RatingDelete, id)
	if err != nil {
		return err
	}
	if err := r.Withdraw(s.now()); err != nil {
		return s.notEditable(r)
	}
	if _, err := r.Save(ctx, g); err != nil {
		return err
	}
	return s.record(ctx, g, actor, ActivityRatingWithdrawn, r.ID, r.Subject(), nil)
}

// notEditable is the refusal of a change to a rating that cannot take one, with
// the reason that applies.
func (s *RatingService) notEditable(r *Rating) error {
	switch {
	case bool(r.UnderReview):
		return s.labels.invalid("score", "under_review")
	case r.Status != StatusPublished:
		return s.labels.invalid("score", "not_published")
	}
	return s.labels.invalid("score", "edit_window")
}

// owned loads a rating the account acts on and asks the policy about the row.
// The read is narrowed to the ratings the subject can see, so a rating of
// another author is not found for one who is not allowed to manage the space's.
func (s *RatingService) owned(ctx context.Context, g security.Grant, actor security.Subject, a security.Action, id string) (*Rating, error) {
	scope := viewsOwn
	if actor.Can(RatingManageSpace) {
		scope = viewsSpace
	}
	r, err := scope.visible(Ratings(s.db).WhereKey(id), actor).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, a, *r); err != nil {
		return nil, ErrNotFound
	}
	return r, nil
}

// Summaries returns what the ratings of each subject add up to: how many are
// published and their average. It opens no text and no name, and it is for an
// application that draws stars beside its own catalogue.
func (s *RatingService) Summaries(ctx context.Context, actor security.Subject, refs []Ref) (map[Ref]Summary, error) {
	g, err := security.Authorize(ctx, s.policy, actor, RatingViewSummary, Rating{})
	if err != nil {
		return nil, err
	}
	if len(refs) > MaxPageSize {
		return nil, s.labels.invalid("subject", "too_many")
	}
	for _, ref := range refs {
		if !ref.Valid() {
			return nil, s.labels.invalid("subject", "subject")
		}
	}
	return s.summaries(ctx, g, refs)
}

// summaryIfAllowed is the summary of one subject for a screen that shows it
// when the subject may read it, and nothing when the policy says they may not:
// the screen then draws no average.
func (s *RatingService) summaryIfAllowed(ctx context.Context, actor security.Subject, ref Ref) (Summary, error) {
	g, err := security.Authorize(ctx, s.policy, actor, RatingViewSummary, Rating{})
	if err != nil {
		return Summary{}, nil
	}
	return s.summary(ctx, g, ref)
}

// CanRate answers whether the subject may rate, for the screen that draws a
// link to the form. It asks exactly what Create does about the account, and the
// refusal it returns is the one Create would.
func (s *RatingService) CanRate(ctx context.Context, actor security.Subject, ref Ref) error {
	g, err := security.Authorize(ctx, s.policy, actor, RatingCreate, Rating{})
	if err != nil {
		return err
	}
	if !ref.Valid() {
		return ErrNotFound
	}
	_, err = s.rateable(ctx, g, actor, ref)
	return err
}

// subject asks the application for a subject. A reference it does not know is
// not found, and an owner it cannot name is a wiring mistake, reported as one.
func (c *core) subject(ctx context.Context, ref Ref) (Subject, error) {
	subject, err := c.cfg.Subjects.Subject(ctx, ref)
	if errors.Is(err, ErrNoSubject) {
		return Subject{}, ErrNotFound
	}
	if err != nil {
		return Subject{}, err
	}
	if !security.ValidTenant(subject.OwnerTenant) {
		return Subject{}, fmt.Errorf("rating: Subjects answered the owner %q for %s, which cannot be a tenant", subject.OwnerTenant, ref)
	}
	subject.Ref = ref
	subject.Name = clip(trimmed(subject.Name), maxNameLen)
	if subject.Name == "" {
		subject.Name = ref.String()
	}
	subject.URL = safeURL(subject.URL)
	return subject, nil
}

// describe is a subject as a list names it. One that is gone is called by its
// reference, so a list does not fail because the catalogue moved on; the
// answers are kept in seen, so a page that repeats a subject asks once.
func (c *core) describe(ctx context.Context, ref Ref, seen map[Ref]Subject) (Subject, error) {
	if known, ok := seen[ref]; ok {
		return known, nil
	}
	subject, err := c.subject(ctx, ref)
	if errors.Is(err, ErrNotFound) {
		subject = Subject{Ref: ref, Name: ref.String()}
	} else if err != nil {
		return Subject{}, err
	}
	seen[ref] = subject
	return subject, nil
}

// safeURL keeps an address the screens may link to: the application's own path
// or an https address. Anything else -- a javascript: address, a protocol-
// relative one -- is dropped, because the link is drawn from text the
// application's catalogue holds and that is no reason to trust it.
func safeURL(raw string) string {
	raw = trimmed(raw)
	switch {
	case strings.HasPrefix(raw, "//"):
		return ""
	case strings.HasPrefix(raw, "/"), strings.HasPrefix(raw, "https://"):
		return raw
	}
	return ""
}

// summary adds up the published ratings of one subject, across every account.
// It is reached only after a policy allowed the act, and it reads counts and
// sums: no text and no name.
func (c *core) summary(ctx context.Context, g security.Grant, ref Ref) (Summary, error) {
	published := func() *platform.RatingQuery {
		return platform.Ratings(c.db).Where("subject_type", "=", ref.Type).Where("subject_id", "=", ref.ID).
			Where("status", "=", string(StatusPublished))
	}
	count, err := published().Count(ctx, g)
	if err != nil || count == 0 {
		return Summary{}, err
	}
	sum, err := published().Sum(ctx, g, "score")
	if err != nil {
		return Summary{}, err
	}
	return summaryOf(count, number(sum)), nil
}

// summaries is summary for each of several subjects.
func (c *core) summaries(ctx context.Context, g security.Grant, refs []Ref) (map[Ref]Summary, error) {
	out := make(map[Ref]Summary, len(refs))
	for _, ref := range refs {
		summary, err := c.summary(ctx, g, ref)
		if err != nil {
			return nil, err
		}
		out[ref] = summary
	}
	return out, nil
}

// number reads whatever an engine answers a sum with.
func number(value any) float64 {
	switch v := value.(type) {
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case float64:
		return v
	case []byte:
		return number(string(v))
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}

// scores turns the stars a request named into the values a statement compares
// with, refusing one that is not a number of stars.
func (c *core) scores(in []int) ([]any, error) {
	out := make([]any, 0, len(in))
	for _, n := range in {
		if !ValidScore(n) {
			return nil, c.labels.invalid("score", "score")
		}
		out = append(out, n)
	}
	return out, nil
}

// paging bounds a page: from one, and at most PageSize rows unless the caller
// asked for fewer.
func (c *core) paging(perPage, page int) (int, int) {
	if perPage <= 0 || perPage > MaxPageSize {
		perPage = c.cfg.PageSize
	}
	if page < 1 {
		page = 1
	}
	return perPage, page
}

// matching adds one group, column LIKE %term% OR ..., case insensitive on every
// engine, to a statement. The term is bound, never written into the statement;
// a % or _ in it widens the match and nothing more. An empty term adds nothing.
func matching(b *model.Builder, term string, columns ...string) {
	term = strings.TrimSpace(term)
	if term == "" {
		return
	}
	pattern := "%" + term + "%"
	b.WhereGroup("and", func(nested *model.Builder) {
		for i, column := range columns {
			boolean := "or"
			if i == 0 {
				boolean = "and"
			}
			nested.GetQuery().WhereLike(column, pattern, false, boolean, false)
		}
	})
}

// body is the text of a rating as it is stored: the space around it taken off
// and the line breaks of every system written as one.
func body(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(trimmed(text), "\r\n", "\n"), "\r", "\n")
}

// validateRate checks the stars and the text of a rating.
func (c *core) validateRate(in RateRequest) validation.Errors {
	errs := validation.Errors{}
	if !ValidScore(in.Score) {
		errs.Add("score", c.labels.message("score"))
	}
	text := body(in.Body)
	switch {
	case text == "" && c.cfg.RequireBody:
		errs.Add("body", c.labels.message("required"))
	case utf8.RuneCountInString(text) > c.cfg.MaxBodyChars:
		errs.Add("body", c.labels.message("max", map[string]any{"max": c.cfg.MaxBodyChars}))
	}
	return errs
}

// person is who a user is, as the application's Directory knows them; just the
// identifier when it has none or does not know them.
func (c *core) person(ctx context.Context, tenant, id string) Person {
	who := Person{ID: id}
	if c.cfg.Directory == nil || id == "" {
		return who
	}
	known, err := c.cfg.Directory.Person(ctx, tenant, id)
	if err != nil {
		return who
	}
	who.Name = clip(trimmed(known.Name), maxNameLen)
	who.Email = clip(trimmed(known.Email), maxNameLen)
	return who
}
