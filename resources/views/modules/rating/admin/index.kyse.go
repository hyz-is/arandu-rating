//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	rating "github.com/hyz-is/arandu-rating"
)

@go
// IndexData is what the handler hands this page.
type IndexData = rating.TeamIndexPageData

// queueTable is the moderation's work: the reports of every space, paged,
// searched, filtered and ordered by the server, the oldest waiting first. Each
// row opens the report.
func queueTable(data IndexData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, r := range data.Rows {
		rows = append(rows, components.TableRow{Key: r.ID, Cells: []components.TableCell{
			{HTML: components.Link(components.LinkProps{Label: r.Subject, URL: r.URL, Variant: "hover"})},
			{Text: r.Reporter},
			{Text: r.Reason},
			{HTML: components.Badge(components.BadgeProps{Label: r.Status, Variant: rating.ReportVariant(r.StatusKey)})},
			{Text: r.Created},
		}})
	}
	return components.DataTableProps{
		ID: "rating-queue", Label: data.Labels.T("screen.team_index_title"), URL: data.URL,
		State: data.State(), Paginator: data.Pager, FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.team_index_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Searchable: true, SearchPlaceholder: data.Labels.T("screen.team_search"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.team_index_title")),
		Filters: []components.DataTableFilter{
			{Key: "status", Label: data.Labels.T("field.status"), Options: data.StatusOptions},
		},
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.subject"), Key: "subject", Sortable: true},
			{Label: data.Labels.T("field.reporter"), Key: "reporter", Sortable: true, Hideable: true},
			{Label: data.Labels.T("field.reason"), Key: "reason", Hideable: true},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
			{Label: data.Labels.T("field.reported_at"), Key: "created_at", Sortable: true},
		},
		Rows: rows,
		Empty: components.EmptyProps{
			Title: data.Labels.T("screen.team_index_empty"), Message: data.Labels.T("screen.team_index_empty_lead"),
		},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<header>
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.team_index_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.team_index_lead") }}</p>
	</header>

	<div class="mt-6 min-w-0">{!! components.DataTable(queueTable(.)) !!}</div>
</section>
@endsection
