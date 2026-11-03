package rating

import (
	"context"
	"errors"
	"time"
)

// The things the application provides. This package knows no table of
// subjects, no table of users and sends no message of its own: each of those is
// an interface the application implements with what it already has, and passes
// in Config.

// ErrNoSubject is what Subjects answers when nothing has the reference.
var ErrNoSubject = errors.New("rating: no such subject")

// ErrNoPerson is what a Directory answers when it does not know the user.
var ErrNoPerson = errors.New("rating: no such person")

// Subject is a thing that can be rated, as the application knows it.
type Subject struct {
	// Ref is what the package stores to name it.
	Ref Ref
	// Name is how screens call it, and URL where it is, or empty. A name is
	// plain text, escaped when it is drawn; a URL is the application's own
	// address, and the screens draw a link only to one that starts with "/" or
	// with https://.
	Name string
	URL  string
	// OwnerTenant is the tenant that owns the subject: the space whose people
	// may read its ratings and report them. It is the application's answer and
	// nothing a request names, and it has to be a valid tenant.
	OwnerTenant string
}

// Subjects tells the package what a subject is and who owns it.
//
// It is the application's: it reads its own catalogue. The package asks it on
// every act that depends on ownership and trusts the answer, so it answers
// ErrNoSubject for a reference that is not one and never invents an owner.
type Subjects interface {
	// Subject returns the subject, or ErrNoSubject.
	Subject(ctx context.Context, ref Ref) (Subject, error)
	// Owned returns the subjects a tenant owns, in the order a list offers
	// them. The package pages and sorts what it gets, so it is a list of a size
	// a person reads, not a table.
	Owned(ctx context.Context, tenant string) ([]Subject, error)
}

// Eligibility says whether an account may rate a subject. In an application
// where only the people who use a thing may judge it, this is where that is
// said: the package asks it before it writes a rating, and a false answer is a
// refusal.
type Eligibility interface {
	// CanRate reports whether the user of the tenant may rate the subject.
	CanRate(ctx context.Context, tenant, userID string, ref Ref) (bool, error)
}

// Person is somebody the application knows, as the screens name them.
type Person struct {
	ID    string
	Name  string
	Email string
}

// Directory names people. It is optional: without one a rating keeps its
// author's name empty, and the screens call the author by what they have.
type Directory interface {
	// Person returns a user of a tenant, or ErrNoPerson when there is none.
	Person(ctx context.Context, tenant, id string) (Person, error)
}

// EventKind says what happened that somebody should be told about.
type EventKind string

// The events a Notifier is told about.
const (
	// EventReported is a rating the owner of a subject reported. The moderation
	// is the one to tell.
	EventReported EventKind = "rating.reported"
	// EventDecided is a report the moderation decided. The reporter is the one
	// to tell, and the author of the rating when the decision touched it.
	EventDecided EventKind = "rating.decided"
)

// ForTeam says whether the moderation is the one to tell.
func (k EventKind) ForTeam() bool { return k == EventReported }

// Event is what a Notifier is handed.
type Event struct {
	Kind    EventKind
	Subject Subject
	// Rating is the rating after the change.
	Rating RatingView
	// Report is the report after the change.
	Report ReportView
}

// Notifier tells people that something happened.
//
// The application implements it with its own mailer: this package sends no
// message. Notify runs after the change is stored, and its error does not undo
// the change: the report is filed and the decision taken, and a mail server
// that is down is no reason to ask anybody to do it twice. The error is logged
// and the request goes on. An application that must not lose a notification
// enqueues it here and delivers it from its queue.
type Notifier interface {
	Notify(ctx context.Context, e Event) error
}

// RateRequest is what an author gives when they rate a subject. There is no
// tenant, no author and no subject here and there must never be one in the
// body: the tenant and the author come from the Grant and the session, and the
// subject from the address.
type RateRequest struct {
	Score int
	Body  string
}

// ReportRequest is what the owner of a subject gives when they report a rating.
type ReportRequest struct {
	Reason string
}

// DecisionRequest is what the moderation gives when it decides a report.
type DecisionRequest struct {
	Verdict Verdict
	// Note is the justification, kept with the decision. For a removal or an
	// alteration it is also what the author reads.
	Note string
	// Body is the corrected text, for VerdictAlter.
	Body string
	// Score is the corrected stars, for VerdictAlter, and only when
	// Config.ModerationMayChangeScore allows it. Zero keeps the stars.
	Score int
}

// ListQuery narrows the author's list. Page counts from one.
type ListQuery struct {
	Search string
	// Scores narrows to ratings with these stars.
	Scores   []int
	Statuses []Status
	Sort     string
	Dir      string
	Page     int
	PerPage  int
}

// OwnedQuery narrows the list of subjects the owner owns. Page counts from one.
type OwnedQuery struct {
	Search  string
	Sort    string
	Dir     string
	Page    int
	PerPage int
}

// SubjectQuery narrows the ratings of one subject. Page counts from one.
type SubjectQuery struct {
	Search  string
	Scores  []int
	Sort    string
	Dir     string
	Page    int
	PerPage int
}

// ReportsQuery narrows a list of reports, the owner's own or the moderation's
// queue. Page counts from one.
type ReportsQuery struct {
	// Search matches the reason, the reporter's name and the subject's
	// identifier.
	Search   string
	Statuses []ReportStatus
	Sort     string
	Dir      string
	Page     int
	PerPage  int
}

// RatingEntry is a rating with the subject it is about.
type RatingEntry struct {
	Rating  RatingView
	Subject Subject
}

// RatingPage is one page of ratings, and how many rows the whole list has.
type RatingPage struct {
	Rows  []RatingEntry
	Total int
	Page  int
	Pages int
}

// ReportEntry is a report with the subject it is about.
type ReportEntry struct {
	Report  ReportView
	Subject Subject
}

// ReportPage is one page of reports.
type ReportPage struct {
	Rows  []ReportEntry
	Total int
	Page  int
	Pages int
}

// OwnedEntry is a subject the owner owns, and what its ratings add up to.
type OwnedEntry struct {
	Subject Subject
	Summary Summary
}

// OwnedPage is one page of the owner's subjects.
type OwnedPage struct {
	Rows  []OwnedEntry
	Total int
	Page  int
	Pages int
}

// SubjectCard is a subject as the form that rates it shows it.
type SubjectCard struct {
	Subject Subject
	Summary Summary
	// Rated says the account already has a rating of it that stands, and
	// RatingID is that rating: the form that rates the subject is then the
	// rating's own page. A rating the author withdrew does not count.
	Rated    bool
	RatingID string
}

// RatingDetail is one rating as its author reads it.
type RatingDetail struct {
	Rating  RatingView
	Subject Subject
	Summary Summary
	// CanEdit and CanWithdraw say whether the rating's own state allows it; the
	// policy still decides whether the subject may.
	CanEdit     bool
	CanWithdraw bool
}

// SubjectRatings is a subject's published ratings as its owner reads them.
type SubjectRatings struct {
	Subject Subject
	Summary Summary
	Page    RatingPage
}

// ReportDetail is one report as the moderation reads it: with the rating as it
// stands now, which can differ from the one reported only by a decision.
type ReportDetail struct {
	Report  ReportView
	Rating  RatingView
	Subject Subject
	Summary Summary
}

// pagesOf is how many pages of perPage rows hold total rows, at least one.
func pagesOf(total, perPage int) int {
	if perPage <= 0 || total <= 0 {
		return 1
	}
	return (total + perPage - 1) / perPage
}

// clock is what the services read the time from.
type clock func() time.Time
