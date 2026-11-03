package rating

import (
	stdhttp "net/http"
	"strconv"
	"strings"

	fhttp "github.com/arandu-io/framework/http"
)

// The handlers are thin on purpose: read the input, ask the service, answer. No
// rule, database handle or Model construction lives here. A handler that
// reached data directly would skip the service's policy boundary, and the
// layout makes that visible rather than relying on review.
//
// Who is asking comes from ctx.User, which reads the subject RequireAuth put on
// the request. No handler loads the session again or takes an identity from the
// input, and none takes a tenant from anywhere: the tenant is the Grant's.
//
// An error from the service is returned as it is, and the router answers it:
// validation.Errors goes back where the request came from with the messages in
// the flash, ErrNotFound is 404 because it wraps model.ErrModelNotFound, and a
// policy refusal is 403. The answer is the status and its standard sentence,
// never the error's own text -- telling a client why a policy said no tells it
// what exists, one request at a time, and the reason belongs in the log. Any
// other error is one nobody expected, and it reaches the error page rather than
// a 200 with an empty body.
//
// Every GET answers the screen to a browser and JSON to a client that asks for
// it; every form posts to the same routes the JSON clients use.

// handlers are the handlers of the route table, by route name.
func (m *Module) handlers() map[string]func(*fhttp.Context) error {
	return map[string]func(*fhttp.Context) error{
		"rating.index":    m.index,
		"rating.create":   m.create,
		"rating.store":    m.store,
		"rating.show":     m.show,
		"rating.update":   m.update,
		"rating.withdraw": m.withdraw,

		"rating.owner.index":       m.ownerIndex,
		"rating.owner.subject":     m.ownerSubject,
		"rating.owner.reports":     m.ownerReports,
		"rating.owner.report_form": m.ownerReportForm,
		"rating.owner.report":      m.ownerReport,

		"rating.team.index":  m.teamIndex,
		"rating.team.show":   m.teamShow,
		"rating.team.decide": m.teamDecide,
	}
}

// subjectRef is the subject the address names. It is a value the caller chose,
// and the service refuses one that is not a reference or is not found.
func subjectRef(ctx *fhttp.Context) Ref { return Ref{Type: ctx.Param("type"), ID: ctx.Param("id")} }

// rateRequest reads the stars and the text of a rating form.
func rateRequest(ctx *fhttp.Context) RateRequest {
	score, _ := strconv.Atoi(strings.TrimSpace(ctx.Input("score")))
	return RateRequest{Score: score, Body: ctx.Input("body")}
}

// pageResource is a page of a list as one response.
func pageResource[T any](rows []T, fields func(T) map[string]any, total, page, pages int) fieldsResource {
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, fields(row))
	}
	return fieldsResource{"items": items, "total": total, "page": page, "pages": pages}
}

// subjectFields are the fields a subject answers with: what it is and what its
// ratings add up to, and not the tenant that owns it.
func subjectFields(s Subject, sum Summary) map[string]any {
	out := summaryFields(sum)
	out["type"], out["id"], out["name"], out["url"] = s.Ref.Type, s.Ref.ID, s.Name, s.URL
	return out
}

// index answers the account's ratings.
func (m *Module) index(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	p := listParams(ctx, func(s string) bool { _, ok := sortableRating[s]; return ok }, func(s string) bool { return Status(s).Valid() }).
		defaulted("updated_at", "desc")
	statuses := make([]Status, 0, len(p.statuses))
	for _, s := range p.statuses {
		statuses = append(statuses, Status(s))
	}
	page, err := m.ratings.List(ctx.Ctx(), who, ListQuery{
		Search: p.search, Scores: p.scores, Statuses: statuses, Sort: p.sort, Dir: p.dir, Page: p.page, PerPage: m.cfg.PageSize,
	})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, pageResource(page.Rows, func(e RatingEntry) map[string]any {
			fields := authorFields(e.Rating)
			fields["subject_name"] = e.Subject.Name
			return fields
		}, page.Total, page.Page, page.Pages))
	}
	sc, err := m.screen(ctx, who, "screen.index_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewIndex, m.indexPage(sc, p, page))
}

// create answers the form that rates a subject, or the rating the account
// already gave it.
func (m *Module) create(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	ref := subjectRef(ctx)
	card, err := m.ratings.Card(ctx.Ctx(), who, ref)
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		out := fieldsResource{"subject": subjectFields(card.Subject, card.Summary), "rated": card.Rated}
		if card.Rated {
			out["rating_id"] = card.RatingID
		}
		return ctx.JSON(stdhttp.StatusOK, out)
	}
	if card.Rated {
		return ctx.Redirect(m.URLs().Show(card.RatingID))
	}
	sc, err := m.screen(ctx, who, "screen.form_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewForm, m.formPage(sc, card, ref))
}

// store writes the account's rating of a subject.
func (m *Module) store(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	record, err := m.ratings.Create(ctx.Ctx(), who, subjectRef(ctx), rateRequest(ctx))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusCreated, fieldsResource(authorFields(ratingViewOf(record))))
	}
	return ctx.Redirect(m.URLs().Show(record.ID))
}

// show answers one rating, for its author.
func (m *Module) show(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	detail, err := m.ratings.Find(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		out := fieldsResource(authorFields(detail.Rating))
		out["subject"] = subjectFields(detail.Subject, detail.Summary)
		return ctx.JSON(stdhttp.StatusOK, out)
	}
	sc, err := m.screen(ctx, who, "screen.show_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewShow, m.showPage(ctx, sc, who, detail))
}

// update changes the stars and the text of the author's rating.
func (m *Module) update(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	record, err := m.ratings.Update(ctx.Ctx(), who, ctx.Param("id"), rateRequest(ctx))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource(authorFields(ratingViewOf(record))))
	}
	return ctx.Redirect(m.URLs().Show(record.ID))
}

// withdraw deletes the author's rating.
func (m *Module) withdraw(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	if err := m.ratings.Withdraw(ctx.Ctx(), who, ctx.Param("id")); err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.Status(stdhttp.StatusNoContent)
	}
	return ctx.Redirect(m.URLs().Index())
}

// ownerIndex answers the subjects the account owns.
func (m *Module) ownerIndex(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	p := listParams(ctx, func(s string) bool { return sortableOwned[s] }, func(string) bool { return false }).defaulted("name", "asc")
	page, err := m.owner.Subjects(ctx.Ctx(), who, OwnedQuery{Search: p.search, Sort: p.sort, Dir: p.dir, Page: p.page, PerPage: m.cfg.PageSize})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, pageResource(page.Rows, func(e OwnedEntry) map[string]any {
			return subjectFields(e.Subject, e.Summary)
		}, page.Total, page.Page, page.Pages))
	}
	sc, err := m.screen(ctx, who, "screen.owner_index_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewOwnerIndex, m.ownerIndexPage(sc, p, page))
}

// ownerSubject answers the ratings one subject received.
func (m *Module) ownerSubject(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	ref := subjectRef(ctx)
	p := listParams(ctx, func(s string) bool { _, ok := sortableSubjectRatings[s]; return ok }, func(string) bool { return false }).
		defaulted("created_at", "desc")
	d, err := m.owner.Received(ctx.Ctx(), who, ref, SubjectQuery{
		Search: p.search, Scores: p.scores, Sort: p.sort, Dir: p.dir, Page: p.page, PerPage: m.cfg.PageSize,
	})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		out := pageResource(d.Page.Rows, func(e RatingEntry) map[string]any { return ownerFields(e.Rating) }, d.Page.Total, d.Page.Page, d.Page.Pages)
		out["subject"] = subjectFields(d.Subject, d.Summary)
		return ctx.JSON(stdhttp.StatusOK, out)
	}
	sc, err := m.screen(ctx, who, "screen.owner_subject_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewOwnerSubject, m.ownerSubjectPage(sc, ref, p, d))
}

// ownerReports answers the reports the account filed.
func (m *Module) ownerReports(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	p := listParams(ctx, func(s string) bool { _, ok := sortableOwnerReports[s]; return ok }, func(s string) bool { return ReportStatus(s).Valid() }).
		defaulted("created_at", "desc")
	page, err := m.owner.Filed(ctx.Ctx(), who, ReportsQuery{
		Search: p.search, Statuses: reportStatuses(p.statuses), Sort: p.sort, Dir: p.dir, Page: p.page, PerPage: m.cfg.PageSize,
	})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, pageResource(page.Rows, func(e ReportEntry) map[string]any {
			return ownerReportFields(e.Report)
		}, page.Total, page.Page, page.Pages))
	}
	sc, err := m.screen(ctx, who, "screen.owner_reports_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewOwnerReports, m.ownerReportsPage(sc, p, page))
}

// ownerReportForm answers the form that reports a rating.
func (m *Module) ownerReportForm(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	entry, err := m.owner.Rating(ctx.Ctx(), who, ctx.Param("rating"))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource(ownerFields(entry.Rating)))
	}
	sc, err := m.screen(ctx, who, "screen.owner_report_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewOwnerReport, m.ownerReportPage(sc, entry))
}

// ownerReport files a report on a rating.
func (m *Module) ownerReport(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	report, err := m.owner.Report(ctx.Ctx(), who, ctx.Param("rating"), ReportRequest{Reason: ctx.Input("reason")})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusCreated, fieldsResource(ownerReportFields(reportViewOf(report))))
	}
	return ctx.Redirect(m.URLs().OwnerReports())
}

// teamIndex answers the queue of reports of every space.
func (m *Module) teamIndex(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	p := listParams(ctx, func(s string) bool { _, ok := sortableQueue[s]; return ok }, func(s string) bool { return ReportStatus(s).Valid() })
	// The queue a moderator opens is the work waiting, oldest first; asking for
	// every status, or for the decided ones, is a choice written in the address.
	if len(p.statuses) == 0 && ctx.Request.URL.Query().Get("status") == "" && ctx.Request.URL.Query().Get("all") == "" {
		p.statuses = []string{string(ReportOpen)}
	}
	p = p.defaulted("created_at", "asc")
	page, err := m.moderation.List(ctx.Ctx(), who, ReportsQuery{
		Search: p.search, Statuses: reportStatuses(p.statuses), Sort: p.sort, Dir: p.dir, Page: p.page, PerPage: m.cfg.PageSize,
	})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, pageResource(page.Rows, func(e ReportEntry) map[string]any {
			return teamReportFields(e.Report)
		}, page.Total, page.Page, page.Pages))
	}
	sc, err := m.screen(ctx, who, "screen.team_index_title", true)
	if err != nil {
		return err
	}
	return ctx.View(ViewTeamIndex, m.teamIndexPage(sc, p, page))
}

// teamShow answers one report with the rating it is about.
func (m *Module) teamShow(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	d, err := m.moderation.Find(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		rating := authorFields(d.Rating)
		rating["tenant"] = d.Rating.Tenant
		return ctx.JSON(stdhttp.StatusOK, fieldsResource{
			"report": teamReportFields(d.Report), "rating": rating, "subject": subjectFields(d.Subject, d.Summary),
		})
	}
	sc, err := m.screen(ctx, who, "screen.team_show_title", true)
	if err != nil {
		return err
	}
	return ctx.View(ViewTeamShow, m.teamShowPage(ctx, sc, who, d))
}

// teamDecide decides a report.
func (m *Module) teamDecide(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	score, _ := strconv.Atoi(strings.TrimSpace(ctx.Input("score")))
	report, err := m.moderation.Decide(ctx.Ctx(), who, ctx.Param("id"), DecisionRequest{
		Verdict: Verdict(ctx.Input("verdict")), Note: ctx.Input("note"), Body: ctx.Input("body"), Score: score,
	})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource(teamReportFields(reportViewOf(report))))
	}
	return ctx.Redirect(m.URLs().TeamShow(report.ID))
}

// reportStatuses turns the statuses an address named into the values the
// service takes.
func reportStatuses(in []string) []ReportStatus {
	out := make([]ReportStatus, 0, len(in))
	for _, s := range in {
		out = append(out, ReportStatus(s))
	}
	return out
}
