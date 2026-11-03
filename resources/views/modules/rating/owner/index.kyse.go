//go:build kyse

package owner

import (
	"github.com/arandu-io/kyse/components"

	rating "github.com/hyz-is/arandu-rating"
)

@go
// IndexData is what the handler hands this page.
type IndexData = rating.OwnerIndexPageData

// subjectsTable is the subjects the account owns, with what their ratings add
// up to: paged, searched and ordered by the server. Each row opens the ratings
// the subject received.
func subjectsTable(data IndexData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, s := range data.Rows {
		average := data.Labels.T("screen.no_ratings_short")
		if s.Rated {
			average = s.Average
		}
		rows = append(rows, components.TableRow{Key: s.Key, Cells: []components.TableCell{
			{HTML: components.Link(components.LinkProps{Label: s.Name, URL: s.OwnURL, Variant: "hover"})},
			{HTML: components.Rating(components.RatingProps{
				ComponentProps: components.ComponentProps{Parts: components.Parts{"label": {Class: "sr-only"}}},
				Name:           "average-" + s.Key, Label: data.StarsLabel(s.Name, s.Stars), Value: s.StarsValue, ReadOnly: true,
				OptionLabels: data.Labels.StarNames(),
			})},
			{Text: average},
			{Text: data.Labels.Count(s.Count)},
		}})
	}
	return components.DataTableProps{
		ID: "rating-owned", Label: data.Labels.T("screen.owner_index_title"), URL: data.URL,
		State: data.State(), Paginator: data.Pager, FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.owner_index_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Searchable: true, SearchPlaceholder: data.Labels.T("control.search_subject"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.owner_index_title")),
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.subject"), Key: "name", Sortable: true},
			{Label: data.Labels.T("field.stars"), Key: "average", Sortable: true},
			{Label: data.Labels.T("field.average"), Key: "average_value"},
			{Label: data.Labels.T("field.count"), Key: "count", Sortable: true},
		},
		Rows: rows,
		Empty: components.EmptyProps{
			Title: data.Labels.T("screen.owner_index_empty"), Message: data.Labels.T("screen.owner_index_empty_lead"),
		},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-5xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<div class="min-w-0">
			<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.owner_index_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.owner_index_lead") }}</p>
		</div>
		<a class="btn" data-variant="outline" href="{{ .URLs.OwnerReports() }}">{{ .Labels.T("control.my_reports") }}</a>
	</header>

	<div class="mt-6 min-w-0">{!! components.DataTable(subjectsTable(.)) !!}</div>
</section>
@endsection
