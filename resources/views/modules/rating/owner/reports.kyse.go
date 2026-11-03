//go:build kyse

package owner

import (
	"github.com/arandu-io/kyse/components"

	rating "github.com/hyz-is/arandu-rating"
)

@go
// ReportsData is what the handler hands this page.
type ReportsData = rating.OwnerReportsPageData

// reportsTable is the reports the account filed, with what the moderation
// decided about each: paged, searched, filtered and ordered by the server.
func reportsTable(data ReportsData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, r := range data.Rows {
		outcome := r.Outcome
		if outcome == "" {
			outcome = "—"
		}
		rows = append(rows, components.TableRow{Key: r.ID, Cells: []components.TableCell{
			{Text: r.Subject},
			{Text: r.Reason},
			{HTML: components.Badge(components.BadgeProps{Label: r.Status, Variant: rating.ReportVariant(r.StatusKey)})},
			{Text: outcome},
			{Text: r.DecisionNote},
			{Text: r.Created},
		}})
	}
	return components.DataTableProps{
		ID: "rating-reports", Label: data.Labels.T("screen.owner_reports_title"), URL: data.URL,
		State: data.State(), Paginator: data.Pager, FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.owner_reports_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Searchable: true, SearchPlaceholder: data.Labels.T("control.search_report"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.owner_reports_title")),
		Filters: []components.DataTableFilter{
			{Key: "status", Label: data.Labels.T("field.status"), Options: data.StatusOptions},
		},
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.subject"), Key: "subject", Sortable: true},
			{Label: data.Labels.T("field.reason"), Key: "reason", Hideable: true},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
			{Label: data.Labels.T("field.outcome"), Key: "outcome", Hideable: true},
			{Label: data.Labels.T("field.decision_note"), Key: "decision_note", Hideable: true},
			{Label: data.Labels.T("field.created_at"), Key: "created_at", Sortable: true},
		},
		Rows: rows,
		Empty: components.EmptyProps{
			Title: data.Labels.T("screen.owner_reports_empty"), Message: data.Labels.T("screen.owner_reports_empty_lead"),
		},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-6xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>

	<header class="mt-3">
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.owner_reports_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.owner_reports_lead") }}</p>
	</header>

	<div class="mt-6 min-w-0">{!! components.DataTable(reportsTable(.)) !!}</div>
</section>
@endsection
