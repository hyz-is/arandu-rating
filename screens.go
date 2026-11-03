package rating

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/kyse/components"
)

// This file builds what the screens are handed. Like pages.go it reads nothing
// and decides nothing: the handler already asked the service, and these only
// ask the policy what to draw -- a button for an act the subject may do -- never
// whether to do it.

// recordOf is a rating as the policy is asked about it: the fields it decides
// on, and no connection.
func recordOf(v RatingView) Rating {
	return Rating{ID: v.ID, TenantID: v.Tenant, AuthorID: v.AuthorID, Status: v.Status, UnderReview: Flag(v.UnderReview)}
}

// allows says whether an author may do an act on a rating, to decide whether a
// button is drawn. It cannot authorize anything: it throws the Grant away, and
// the route behind the button asks the policy again.
func (m *Module) allows(ctx *fhttp.Context, who security.Subject, a security.Action, record Rating) bool {
	return auth.Allows(ctx.Ctx(), RatingPolicy{}, who, a, record)
}

// teamAllows is allows for a member of the moderation.
func (m *Module) teamAllows(ctx *fhttp.Context, who security.Subject, a security.Action) bool {
	return auth.Allows(ctx.Ctx(), ModerationPolicy{Realm: m.cfg.TeamTenant}, who, a, Report{})
}

// subjectRow is a subject as a screen shows it.
func (sc Screen) subjectRow(subject Subject, summary Summary, owned bool) SubjectRow {
	row := SubjectRow{
		Name: subject.Name, URL: subject.URL, Key: subject.Ref.String(),
		Count: summary.Count, Average: sc.Labels.Average(summary), Stars: summary.Stars(), StarsValue: stars(summary.Stars()), Rated: summary.Count > 0,
	}
	if owned {
		row.OwnURL = sc.URLs.OwnerSubject(subject.Ref)
	}
	return row
}

// ratingRow is a rating as a screen shows it. The text is plain: the view
// escapes it, and nothing here turns it into markup.
func (sc Screen) ratingRow(e RatingEntry, open, report string) RatingRow {
	v := e.Rating
	author := v.AuthorName
	if author == "" {
		author = sc.Labels.T("screen.anonymous")
	}
	row := RatingRow{
		ID: v.ID, Subject: e.Subject.Name, SubjectURL: e.Subject.URL, SubjectKey: v.Subject.String(),
		Author: author, Score: v.Score, ScoreText: strconv.Itoa(v.Score), Body: v.Body,
		Status: sc.Labels.Status(v.Status), StatusKey: string(v.Status), UnderReview: v.UnderReview,
		ModerationNote: v.ModerationNote, Created: sc.When(v.CreatedAt), Updated: sc.When(v.UpdatedAt),
		CreatedSort: v.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedSort: v.UpdatedAt.UTC().Format(time.RFC3339Nano),
		URL: open, ReportURL: report,
	}
	if v.ModeratedAt != nil {
		row.Moderated = sc.When(*v.ModeratedAt)
	}
	if v.EditedAt != nil {
		row.Edited = sc.When(*v.EditedAt)
	}
	return row
}

// reportRow is a report as a screen shows it.
func (sc Screen) reportRow(e ReportEntry, open string) ReportRow {
	v := e.Report
	reporter := v.ReporterName
	if reporter == "" {
		reporter = sc.Labels.T("screen.anonymous")
	}
	row := ReportRow{
		ID: v.ID, RatingID: v.RatingID, Subject: e.Subject.Name, SubjectURL: e.Subject.URL, SubjectKey: v.Subject.String(),
		Reporter: reporter, Reason: v.Reason,
		Status: sc.Labels.ReportStatus(v.Status), StatusKey: string(v.Status),
		Outcome: sc.Labels.Outcome(v.Outcome), OutcomeKey: string(v.Outcome),
		DecisionNote: v.DecisionNote, DecidedBy: v.DecidedByName,
		ScoreBefore: v.ScoreBefore, ScoreAfter: v.ScoreAfter, BodyBefore: v.BodyBefore, BodyAfter: v.BodyAfter,
		Created: sc.When(v.CreatedAt), Updated: sc.When(v.UpdatedAt), CreatedSort: v.CreatedAt.UTC().Format(time.RFC3339Nano),
		URL: open, Open: v.Status == ReportOpen,
	}
	if v.DecidedAt != nil {
		row.Decided = sc.When(*v.DecidedAt)
	}
	return row
}

// ScoreOptions are the numbers of stars as options of a filter.
func (sc Screen) ScoreOptions() []components.SelectOption {
	out := make([]components.SelectOption, 0, MaxScore)
	for n := MaxScore; n >= MinScore; n-- {
		out = append(out, components.SelectOption{Value: strconv.Itoa(n), Label: sc.Labels.ScoreLabel(n)})
	}
	return out
}

// statusOptions are the statuses an author can filter by.
func (sc Screen) statusOptions() []components.SelectOption {
	out := make([]components.SelectOption, 0, 2)
	for _, status := range []Status{StatusPublished, StatusRemoved} {
		out = append(out, components.SelectOption{Value: string(status), Label: sc.Labels.Status(status)})
	}
	return out
}

// reportStatusOptions are the statuses of a report as options of a filter.
func (sc Screen) reportStatusOptions() []components.SelectOption {
	out := make([]components.SelectOption, 0, len(ReportStatuses))
	for _, status := range ReportStatuses {
		out = append(out, components.SelectOption{Value: string(status), Label: sc.Labels.ReportStatus(status)})
	}
	return out
}

// defaulted is the parameters with the order the list is in when the address
// named none: the sort and the direction of the list itself.
func (p params) defaulted(sort, dir string) params {
	if p.sort == "" {
		p.sort, p.dir = sort, dir
	}
	if p.dir == "" {
		p.dir = dir
	}
	return p
}

// order is the order in force in a list, for the data table to build its
// addresses from.
func (p params) order(perPage, page int, facets map[string][]string) TableOrder {
	return TableOrder{SortKey: p.sort, SortDir: p.dir, Query: p.search, Facets: facets, CurrentPage: page, PerPage: perPage}
}

func (m *Module) indexPage(sc Screen, p params, page RatingPage) IndexPageData {
	u := m.URLs()
	rows := make([]RatingRow, 0, len(page.Rows))
	for _, e := range page.Rows {
		rows = append(rows, sc.ratingRow(e, u.Show(e.Rating.ID), ""))
	}
	return IndexPageData{
		Screen: sc,
		TableOrder: p.order(m.cfg.PageSize, page.Page, map[string][]string{
			"status": p.statuses, "score": scoreValues(p.scores),
		}),
		Rows: rows, URL: u.Index(),
		Pager: pager(len(rows), page.Total, m.cfg.PageSize, page.Page, u.Index(), listQueryValues(p)),
		Pages: page.Pages, Total: page.Total,
		StatusOptions: sc.statusOptions(), ScoreOptions: sc.ScoreOptions(),
	}
}

func (m *Module) formPage(sc Screen, card SubjectCard, ref Ref) FormPageData {
	u := m.URLs()
	return FormPageData{
		Screen: sc, Subject: sc.subjectRow(card.Subject, card.Summary, false), Action: u.Rate(ref), BackURL: u.Index(),
		MaxChars: m.cfg.MaxBodyChars, BodyRequire: m.cfg.RequireBody,
	}
}

func (m *Module) showPage(ctx *fhttp.Context, sc Screen, who security.Subject, d RatingDetail) ShowPageData {
	u := m.URLs()
	record := recordOf(d.Rating)
	return ShowPageData{
		Screen: sc, Rating: sc.ratingRow(RatingEntry{Rating: d.Rating, Subject: d.Subject}, u.Show(d.Rating.ID), ""),
		Subject: sc.subjectRow(d.Subject, d.Summary, false), BackURL: u.Index(),
		UpdateURL: u.Update(d.Rating.ID), WithdrawURL: u.Withdraw(d.Rating.ID),
		CanEdit:     d.CanEdit && m.allows(ctx, who, RatingUpdate, record),
		CanWithdraw: d.CanWithdraw && m.allows(ctx, who, RatingDelete, record),
		MaxChars:    m.cfg.MaxBodyChars, BodyRequire: m.cfg.RequireBody,
	}
}

func (m *Module) ownerIndexPage(sc Screen, p params, page OwnedPage) OwnerIndexPageData {
	u := m.URLs()
	rows := make([]SubjectRow, 0, len(page.Rows))
	for _, e := range page.Rows {
		rows = append(rows, sc.subjectRow(e.Subject, e.Summary, true))
	}
	return OwnerIndexPageData{
		Screen: sc, TableOrder: p.order(m.cfg.PageSize, page.Page, nil),
		Rows: rows, URL: u.OwnerIndex(),
		Pager: pager(len(rows), page.Total, m.cfg.PageSize, page.Page, u.OwnerIndex(), listQueryValues(p)),
		Pages: page.Pages, Total: page.Total,
	}
}

func (m *Module) ownerSubjectPage(sc Screen, ref Ref, p params, d SubjectRatings) OwnerSubjectPageData {
	u := m.URLs()
	rows := make([]RatingRow, 0, len(d.Page.Rows))
	for _, e := range d.Page.Rows {
		report := ""
		if !e.Rating.UnderReview {
			report = u.OwnerReport(e.Rating.ID)
		}
		rows = append(rows, sc.ratingRow(e, "", report))
	}
	here := u.OwnerSubject(ref)
	return OwnerSubjectPageData{
		Screen: sc, TableOrder: p.order(m.cfg.PageSize, d.Page.Page, map[string][]string{"score": scoreValues(p.scores)}),
		Subject: sc.subjectRow(d.Subject, d.Summary, true), Rows: rows, URL: here, BackURL: u.OwnerIndex(),
		Pager: pager(len(rows), d.Page.Total, m.cfg.PageSize, d.Page.Page, here, listQueryValues(p)),
		Pages: d.Page.Pages, Total: d.Page.Total, ScoreOptions: sc.ScoreOptions(),
	}
}

func (m *Module) ownerReportsPage(sc Screen, p params, page ReportPage) OwnerReportsPageData {
	u := m.URLs()
	rows := make([]ReportRow, 0, len(page.Rows))
	for _, e := range page.Rows {
		rows = append(rows, sc.reportRow(e, ""))
	}
	return OwnerReportsPageData{
		Screen: sc, TableOrder: p.order(m.cfg.PageSize, page.Page, map[string][]string{"status": p.statuses}),
		Rows: rows, URL: u.OwnerReports(), BackURL: u.OwnerIndex(),
		Pager: pager(len(rows), page.Total, m.cfg.PageSize, page.Page, u.OwnerReports(), listQueryValues(p)),
		Pages: page.Pages, Total: page.Total, StatusOptions: sc.reportStatusOptions(),
	}
}

func (m *Module) ownerReportPage(sc Screen, e RatingEntry) OwnerReportPageData {
	u := m.URLs()
	return OwnerReportPageData{
		Screen: sc, Rating: sc.ratingRow(e, "", ""), Subject: sc.subjectRow(e.Subject, Summary{}, true),
		Action: u.OwnerReport(e.Rating.ID), BackURL: u.OwnerSubject(e.Rating.Subject), MaxChars: m.cfg.MaxReasonChars,
	}
}

func (m *Module) teamIndexPage(sc Screen, p params, page ReportPage) TeamIndexPageData {
	u := m.URLs()
	rows := make([]ReportRow, 0, len(page.Rows))
	for _, e := range page.Rows {
		rows = append(rows, sc.reportRow(e, u.TeamShow(e.Report.ID)))
	}
	return TeamIndexPageData{
		Screen: sc, TableOrder: p.order(m.cfg.PageSize, page.Page, map[string][]string{"status": p.statuses}),
		Rows: rows, URL: u.TeamIndex(),
		Pager: pager(len(rows), page.Total, m.cfg.PageSize, page.Page, u.TeamIndex(), listQueryValues(p)),
		Pages: page.Pages, Total: page.Total, StatusOptions: sc.reportStatusOptions(),
	}
}

func (m *Module) teamShowPage(ctx *fhttp.Context, sc Screen, who security.Subject, d ReportDetail) TeamShowPageData {
	u := m.URLs()
	verdicts := make([]components.SelectOption, 0, len(Verdicts))
	for _, v := range Verdicts {
		verdicts = append(verdicts, components.SelectOption{Value: string(v), Label: sc.Labels.Verdict(v)})
	}
	report := sc.reportRow(ReportEntry{Report: d.Report, Subject: d.Subject}, u.TeamShow(d.Report.ID))
	return TeamShowPageData{
		Screen: sc, Report: report, Rating: sc.ratingRow(RatingEntry{Rating: d.Rating, Subject: d.Subject}, "", ""),
		Subject: sc.subjectRow(d.Subject, d.Summary, false), BackURL: u.TeamIndex(), DecideURL: u.TeamDecide(d.Report.ID),
		CanDecide:      d.Report.Status == ReportOpen && m.teamAllows(ctx, who, RatingModerate),
		ScoreEditable:  m.cfg.ModerationMayChangeScore,
		VerdictOptions: verdicts, ScoreOptions: append([]components.SelectOption{{Value: "", Label: sc.Labels.T("control.keep_score")}}, sc.ScoreOptions()...),
		MaxBody: m.cfg.MaxBodyChars, MaxNote: m.cfg.MaxNoteChars,
	}
}

// params are what a list's address says, read and cleaned: a status that is not
// one, and a sort the list does not offer, are dropped rather than refused,
// because an address somebody edited by hand is not a form to send back.
type params struct {
	search    string
	statuses  []string
	scores    []int
	sort, dir string
	page      int
}

// listParams reads the query string of a list. The sort is kept only when the
// list offers it, and a status only when it is one of the known.
func listParams(ctx *fhttp.Context, sortable func(string) bool, validStatus func(string) bool) params {
	query := ctx.Request.URL.Query()
	p := params{search: strings.TrimSpace(query.Get("q"))}
	for _, raw := range query["status"] {
		if s := strings.TrimSpace(raw); validStatus(s) {
			p.statuses = append(p.statuses, s)
		}
	}
	for _, raw := range query["score"] {
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && ValidScore(n) {
			p.scores = append(p.scores, n)
		}
	}
	if sort := query.Get("sort"); sort != "" && sortable(sort) {
		p.sort = sort
	}
	switch query.Get("dir") {
	case "asc":
		p.dir = "asc"
	case "desc":
		p.dir = "desc"
	}
	p.page, _ = strconv.Atoi(query.Get("page"))
	return p
}

func scoreValues(scores []int) []string {
	out := make([]string, 0, len(scores))
	for _, n := range scores {
		out = append(out, strconv.Itoa(n))
	}
	return out
}

// listQueryValues are the parameters a list carries onto its page links.
func listQueryValues(p params) url.Values {
	values := url.Values{}
	if p.search != "" {
		values.Set("q", p.search)
	}
	for _, s := range p.statuses {
		values.Add("status", s)
	}
	for _, n := range p.scores {
		values.Add("score", strconv.Itoa(n))
	}
	if p.sort != "" {
		values.Set("sort", p.sort)
	}
	if p.dir != "" {
		values.Set("dir", p.dir)
	}
	return values
}
