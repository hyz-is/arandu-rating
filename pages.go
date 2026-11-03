package rating

import (
	"log/slog"
	"net/url"
	"strconv"
	"time"

	fview "github.com/arandu-io/framework/view"
	"github.com/arandu-io/hesape/pagination"
	hview "github.com/arandu-io/hesape/view"
	"github.com/arandu-io/kyse/components"
)

// What the screens show, built from what the services answer. Nothing here
// reads the database or asks the policy: the handler already did both, and
// these only turn statuses, instants and texts into what a page draws.

// The screens this package renders, by the name their path gives them.
const (
	ViewIndex        = "modules.rating.index"
	ViewForm         = "modules.rating.form"
	ViewShow         = "modules.rating.show"
	ViewOwnerIndex   = "modules.rating.owner.index"
	ViewOwnerSubject = "modules.rating.owner.subject"
	ViewOwnerReports = "modules.rating.owner.reports"
	ViewOwnerReport  = "modules.rating.owner.report"
	ViewTeamIndex    = "modules.rating.admin.index"
	ViewTeamShow     = "modules.rating.admin.show"
)

// URLs are the addresses of the routes under the module's configuration. A link
// is built from here and never written out a second time: under another
// Prefix the paths move and the names do not.
type URLs struct{ cfg Config }

func (u URLs) child(base, id string) string { return base + "/" + url.PathEscape(id) }

func (u URLs) subject(base string, ref Ref) string {
	return base + u.cfg.Paths.Subject + "/" + url.PathEscape(ref.Type) + "/" + url.PathEscape(ref.ID)
}

// Index is the account's ratings.
func (u URLs) Index() string { return u.cfg.Prefix }

// Rate is the form that rates a subject, or the rating's own page when the
// account already rated it. It is the address an application links to from the
// page of the subject.
func (u URLs) Rate(ref Ref) string { return u.subject(u.cfg.Prefix, ref) }

// Show is one rating, for its author.
func (u URLs) Show(id string) string { return u.child(u.cfg.Prefix, id) }

// Update is where the author posts a change.
func (u URLs) Update(id string) string { return u.Show(id) + u.cfg.Paths.Edit }

// Withdraw is where the author posts a deletion.
func (u URLs) Withdraw(id string) string { return u.Show(id) + u.cfg.Paths.Withdrawal }

// OwnerIndex is the subjects the account owns.
func (u URLs) OwnerIndex() string { return u.cfg.OwnerPrefix }

// OwnerSubject is the ratings one subject received, for its owner.
func (u URLs) OwnerSubject(ref Ref) string { return u.subject(u.cfg.OwnerPrefix, ref) }

// OwnerReports is the reports the account filed.
func (u URLs) OwnerReports() string { return u.cfg.OwnerPrefix + u.cfg.Paths.Reports }

// OwnerReport is the form that reports a rating, and where it posts.
func (u URLs) OwnerReport(ratingID string) string { return u.child(u.OwnerReports(), ratingID) }

// TeamIndex is the moderation's queue.
func (u URLs) TeamIndex() string { return u.cfg.TeamPrefix }

// TeamShow is one report, for the moderation.
func (u URLs) TeamShow(id string) string { return u.child(u.cfg.TeamPrefix, id) }

// TeamDecide is where the moderation posts a decision.
func (u URLs) TeamDecide(id string) string { return u.TeamShow(id) + u.cfg.Paths.Decision }

// Screen is what every page of this package carries: the page hesape's view.New
// builds, the application's chrome, and the sentences.
//
// It embeds hesape's view.Page, so every page of this package is a
// hesape/view.Layout -- what the layouts of an application on Arandu framework
// v0.62 and newer assert -- and it answers HasErrors and ErrorSummary as well,
// so it is also the framework/view.Layout an older layout asks for.
type Screen struct {
	hview.Page
	Labels Labels
	// URLs are the addresses of the package's routes.
	URLs URLs
	// Location is the timezone instants are written in.
	Location *time.Location

	chrome any
}

// Compile-time proof that a page of this package fits both layout contracts.
var (
	_ hview.Layout   = Screen{}
	_ fview.Layout   = Screen{}
	_ slog.LogValuer = Screen{}
)

// Chrome is what Config.Chrome answered for this screen: the application's
// sidebar, navigation and account menu, in whatever type its layout draws from.
// A layout asserts that type on it; nil when Config.Chrome is nil.
func (s Screen) Chrome() any { return s.chrome }

// HasErrors says whether the attempt that landed here was rejected:
// framework/view.Layout's name for hesape's Any.
func (s Screen) HasErrors() bool { return s.Page.Any() }

// All is one line per rejected message, for the banner above a form, in the
// screen's language: the field is named by its label in the catalogue
// ("Assunto: Preencha este campo.").
func (s Screen) All() []string { return s.Labels.Summary(s.Page.Errors) }

// ErrorSummary is one line per rejected field: framework/view.Layout's name for
// All.
func (s Screen) ErrorSummary() []string { return s.All() }

// FieldErrors is every message for one field.
func (s Screen) FieldErrors(name string) []string { return s.Page.Get(name) }

// MarshalJSON writes the page with the CSRF token redacted, as
// framework/view.Page does: a page dumped for debugging never publishes the
// token. It answers for every page data that embeds Screen.
func (s Screen) MarshalJSON() ([]byte, error) { return framePage(s.Page).MarshalJSON() }

// LogValue is the page as a log line carries it, the token redacted.
func (s Screen) LogValue() slog.Value { return framePage(s.Page).LogValue() }

// Form is the form state of the page.
func (s Screen) Form() FormState { return FormState{Page: s.Page} }

// FormState is what a form field reads from the page it is drawn on: what was
// typed before a refusal, and the message for each field.
type FormState struct{ hview.Page }

// FieldError is the first message for one field, empty when it was accepted.
func (f FormState) FieldError(name string) string { return f.Page.FieldError(name) }

// HasErrors says whether the attempt that landed here was rejected.
func (f FormState) HasErrors() bool { return f.Page.Any() }

// ErrorSummary is one line per rejected field.
func (f FormState) ErrorSummary() []string { return f.Page.All() }

// FieldErrors is every message for one field.
func (f FormState) FieldErrors(name string) []string { return f.Page.Get(name) }

// MarshalJSON writes the form state with the CSRF token redacted.
func (f FormState) MarshalJSON() ([]byte, error) { return framePage(f.Page).MarshalJSON() }

// LogValue is the form state as a log line carries it, the token redacted.
func (f FormState) LogValue() slog.Value { return framePage(f.Page).LogValue() }

// framePage copies a hesape page into framework/view.Page, by name, for the two
// methods that redact the token there.
func framePage(p hview.Page) fview.Page {
	return fview.Page{
		Title: p.Title, Description: p.Description, Canonical: p.Canonical, AppName: p.AppName, Token: p.Token,
		Authenticated: p.Authenticated, UserName: p.UserName,
		HomeURL: p.HomeURL, LoginURL: p.LoginURL, LogoutURL: p.LogoutURL, RegisterURL: p.RegisterURL,
		PanelURL: p.PanelURL, AdminURL: p.AdminURL,
		Impersonating: p.Impersonating, ImpersonationActs: p.ImpersonationActs, Impersonator: p.Impersonator,
		LeaveImpersonationURL: p.LeaveImpersonationURL,
		Path:                  p.Path, Errors: p.Errors, Old: p.Old, Messages: p.Messages,
	}
}

// TableLines are the sentences a data table on these screens says, every one
// from the package's catalogue (the keys under "table"), so the table speaks the
// screen's language and not the English underneath. The placeholders ({label},
// {total}, {n}, {column}) are the table's own and are filled when it draws them.
func (s Screen) TableLines() components.DataTableLines {
	l := s.Labels
	return components.DataTableLines{
		Filter: l.T("table.filter"), NoResult: l.T("table.no_result"),
		RowsOne: l.T("table.rows_one"), RowsMany: l.T("table.rows_many"),
		SelectedCount: l.T("table.selected_count"), RowsPerPage: l.T("table.rows_per_page"), PageOf: l.T("table.page_of"),
		First: l.T("table.first"), Previous: l.T("table.previous"), Next: l.T("table.next"), Last: l.T("table.last"),
		Back: l.T("table.back"), Forward: l.T("table.forward"),
		SelectAll: l.T("table.select_all"), SelectRow: l.T("table.select_row"), Bulk: l.T("table.bulk"), Apply: l.T("table.apply"),
		Ascending: l.T("table.ascending"), Descending: l.T("table.descending"), Unsorted: l.T("table.unsorted"),
		Reset: l.T("table.reset"), ClearFilters: l.T("table.clear_filters"),
		NoFacetResults: l.T("table.no_facet_results"), FacetSelected: l.T("table.facet_selected"),
		OpenMenu: l.T("table.open_menu"), Actions: l.T("table.actions"), ToggleColumns: l.T("table.toggle_columns"),
		Asc: l.T("table.asc"), Desc: l.T("table.desc"), Hide: l.T("table.hide"),
	}
}

// SearchLabel is the name a screen reader gives a table's search box:
// "Buscar em Avaliações".
func (s Screen) SearchLabel(table string) string {
	return s.Labels.line("control.search_label", map[string]any{"label": table})
}

// When is an instant as Brazilians write it, in the screen's timezone.
func (s Screen) When(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	loc := s.Location
	if loc == nil {
		loc = time.UTC
	}
	return t.In(loc).Format("02/01/2006 15:04")
}

// SubjectRow is a subject as a screen shows it, with what its ratings add up
// to.
type SubjectRow struct {
	Name, URL, Key string
	// Count is how many published ratings there are, Average how they average
	// in the screen's decimal notation, and Stars the average rounded to a whole
	// star for the read-only group. Rated says there is at least one.
	Count   int
	Average string
	// Stars is the average rounded to a whole star, and StarsValue the same as
	// the value of a rating group: empty when nobody rated.
	Stars      int
	StarsValue string
	Rated      bool
	// OwnURL is the subject's ratings, for its owner.
	OwnURL string
}

// RatingRow is one rating as a list or a page shows it.
type RatingRow struct {
	ID, Subject, SubjectURL, SubjectKey string
	Author                              string
	Score                               int
	ScoreText                           string
	Body                                string
	Status, StatusKey                   string
	UnderReview                         bool
	ModerationNote                      string
	Moderated, Created, Updated, Edited string
	// CreatedSort and UpdatedSort order like the instants they stand for.
	CreatedSort, UpdatedSort string
	// URL opens the rating, for its author; ReportURL reports it, for the owner
	// of the subject, and is empty when it cannot be.
	URL, ReportURL string
}

// ReportRow is one report as a list or a page shows it.
type ReportRow struct {
	ID, RatingID, Subject, SubjectURL, SubjectKey string
	Reporter, Reason                              string
	Status, StatusKey                             string
	Outcome, OutcomeKey                           string
	DecisionNote, DecidedBy, Decided              string
	ScoreBefore, ScoreAfter                       int
	BodyBefore, BodyAfter                         string
	Created, Updated                              string
	CreatedSort                                   string
	URL                                           string
	Open                                          bool
}

// TableOrder is the order in force in a list, and the page it is on, for the
// data table to build its addresses from.
type TableOrder struct {
	SortKey, SortDir string
	Query            string
	Facets           map[string][]string
	CurrentPage      int
	PerPage          int
}

// State is the order as the data table draws its addresses from: what is
// searched, the order in force, the facets chosen, the page and its size. Every
// control of the table builds its address from it, so a step keeps the others.
func (o TableOrder) State() components.DataTableState {
	return components.DataTableValues{
		Q: o.Query, Sort: o.SortKey, Dir: o.SortDir, Facets: o.Facets,
		CurrentPage: o.CurrentPage, Size: o.PerPage, DefaultSize: o.PerPage,
	}
}

// IndexPageData is the account's ratings, a page at a time, filtered, searched
// and ordered on the server.
type IndexPageData struct {
	Screen
	TableOrder
	Rows          []RatingRow
	URL           string
	Pager         components.Paginator
	Pages, Total  int
	StatusOptions []components.SelectOption
	ScoreOptions  []components.SelectOption
}

// FormPageData is the form that rates a subject.
type FormPageData struct {
	Screen
	Subject         SubjectRow
	Action, BackURL string
	// Hint is the sentence under the text box, saying how long it may be.
	MaxChars    int
	BodyRequire bool
}

// ShowPageData is one rating, for its author.
type ShowPageData struct {
	Screen
	Rating  RatingRow
	Subject SubjectRow
	BackURL string
	// UpdateURL and WithdrawURL are where the forms post; CanEdit and
	// CanWithdraw say whether to draw them.
	UpdateURL, WithdrawURL string
	CanEdit, CanWithdraw   bool
	MaxChars               int
	BodyRequire            bool
}

// OwnerIndexPageData is the subjects the account owns, with their averages.
type OwnerIndexPageData struct {
	Screen
	TableOrder
	Rows         []SubjectRow
	URL          string
	Pager        components.Paginator
	Pages, Total int
}

// OwnerSubjectPageData is the ratings one subject received.
type OwnerSubjectPageData struct {
	Screen
	TableOrder
	Subject      SubjectRow
	Rows         []RatingRow
	URL, BackURL string
	Pager        components.Paginator
	Pages, Total int
	ScoreOptions []components.SelectOption
}

// OwnerReportsPageData is the reports the account filed.
type OwnerReportsPageData struct {
	Screen
	TableOrder
	Rows          []ReportRow
	URL, BackURL  string
	Pager         components.Paginator
	Pages, Total  int
	StatusOptions []components.SelectOption
}

// OwnerReportPageData is the form that reports one rating.
type OwnerReportPageData struct {
	Screen
	Rating          RatingRow
	Subject         SubjectRow
	Action, BackURL string
	MaxChars        int
}

// TeamIndexPageData is the moderation's queue, a page at a time, filtered,
// searched and ordered on the server.
type TeamIndexPageData struct {
	Screen
	TableOrder
	Rows          []ReportRow
	URL           string
	Pager         components.Paginator
	Pages, Total  int
	StatusOptions []components.SelectOption
}

// TeamShowPageData is one report, with the rating and the form that decides it.
type TeamShowPageData struct {
	Screen
	Report  ReportRow
	Rating  RatingRow
	Subject SubjectRow
	BackURL string
	// DecideURL is where the decision posts; CanDecide says whether to draw the
	// form, and ScoreEditable whether it offers the stars.
	DecideURL      string
	CanDecide      bool
	ScoreEditable  bool
	VerdictOptions []components.SelectOption
	ScoreOptions   []components.SelectOption
	MaxBody        int
	MaxNote        int
}

// pager is the page of a list the data table reads its position from.
func pager(rows, total, perPage, page int, path string, query url.Values) components.Paginator {
	return pagination.NewLengthAwarePage(rows, total, perPage, page, pagination.Options{Path: path, Query: query})
}

// StatusVariant is the badge variant a status wears.
func StatusVariant(key string) string {
	switch Status(key) {
	case StatusRemoved:
		return "destructive"
	case StatusWithdrawn:
		return "outline"
	}
	return "secondary"
}

// ReportVariant is the badge variant a report's status wears: the open one
// stands out, the ones that are over fade.
func ReportVariant(key string) string {
	switch ReportStatus(key) {
	case ReportOpen:
		return "destructive"
	case ReportDismissed:
		return "outline"
	}
	return "secondary"
}

// NewURLs are the addresses of the routes under three prefixes and the
// segments, for a screen built outside a request: a test that draws a page, an
// e-mail that links to one. Empty values are the defaults.
func NewURLs(prefix, ownerPrefix, teamPrefix string, paths Paths) URLs {
	return URLs{cfg: Config{Prefix: prefix, OwnerPrefix: ownerPrefix, TeamPrefix: teamPrefix, Paths: paths}.withDefaults()}
}

// stars is a whole number of stars written as the value of a rating group.
func stars(n int) string {
	if n < MinScore || n > MaxScore {
		return ""
	}
	return strconv.Itoa(n)
}

// CharsHint is the sentence under a text box: how long the text may be.
func (s Screen) CharsHint(max int, required bool) string {
	if required {
		return s.Labels.line("screen.chars_hint_required", map[string]any{"max": max})
	}
	return s.Labels.line("screen.chars_hint", map[string]any{"max": max})
}

// StarsLabel is the name a screen reader gives a read-only group of stars: the
// subject it is about and the score, "Câmera Norte: 4 de 5 estrelas".
func (s Screen) StarsLabel(name string, score int) string {
	return s.Labels.line("screen.stars_label", map[string]any{"name": name, "score": score, "max": MaxScore})
}

// Reading is how a read-only group of stars is named when there are no ratings
// yet: it draws none, and the sentence says so.
func (s Screen) Reading(r SubjectRow) string {
	if !r.Rated {
		return s.Labels.T("screen.no_ratings")
	}
	return s.Labels.line("screen.average_line", map[string]any{"average": r.Average, "count": s.Labels.Count(r.Count)})
}
