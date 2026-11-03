//go:build kyse

package rating

import (
	"github.com/arandu-io/kyse/components"

	rating "github.com/hyz-is/arandu-rating"
)

@go
// IndexData is what the handler hands this page.
//
// It is an alias rather than a struct declared here, so the shape is written
// once, beside the code that fills it.
type IndexData = rating.IndexPageData

// ratingsTable is the account's list, paged, filtered, searched and ordered by
// the server. Each row opens the rating. The stars are the read-only group, one
// name per row: radios of one name are one choice, and two rows would otherwise
// undo each other.
func ratingsTable(data IndexData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, r := range data.Rows {
		status := components.BadgeProps{Label: r.Status, Variant: rating.StatusVariant(r.StatusKey)}
		if r.UnderReview {
			status = components.BadgeProps{Label: data.Labels.T("screen.under_review"), Variant: "outline"}
		}
		rows = append(rows, components.TableRow{Key: r.ID, Cells: []components.TableCell{
			{HTML: components.Link(components.LinkProps{Label: r.Subject, URL: r.URL, Variant: "hover"})},
			{HTML: components.Rating(components.RatingProps{
				ComponentProps: components.ComponentProps{Parts: components.Parts{"label": {Class: "sr-only"}}},
				Name:           "score-" + r.ID, Label: data.StarsLabel(r.Subject, r.Score), Value: r.ScoreText, ReadOnly: true,
				OptionLabels: data.Labels.StarNames(),
			})},
			{HTML: components.Badge(status)},
			{Text: r.Updated},
		}})
	}
	return components.DataTableProps{
		ID: "rating-list", Label: data.Labels.T("screen.index_title"), URL: data.URL,
		State: data.State(), Paginator: data.Pager, FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.index_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Searchable: true, SearchPlaceholder: data.Labels.T("control.search_placeholder"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.index_title")),
		Filters: []components.DataTableFilter{
			{Key: "status", Label: data.Labels.T("field.status"), Options: data.StatusOptions},
			{Key: "score", Label: data.Labels.T("field.score"), Options: data.ScoreOptions},
		},
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.subject"), Key: "subject", Sortable: true},
			{Label: data.Labels.T("field.score"), Key: "score", Sortable: true},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
			{Label: data.Labels.T("field.updated_at"), Key: "updated_at", Sortable: true},
		},
		Rows: rows,
		Empty: components.EmptyProps{
			Title: data.Labels.T("screen.index_empty"), Message: data.Labels.T("screen.index_empty_lead"),
		},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-5xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<header>
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.index_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.index_lead") }}</p>
	</header>

	<div class="mt-6 min-w-0">{!! components.DataTable(ratingsTable(.)) !!}</div>
</section>
@endsection
