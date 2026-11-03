//go:build kyse

package owner

import (
	"github.com/arandu-io/kyse/components"

	rating "github.com/hyz-is/arandu-rating"
)

@go
// SubjectData is what the handler hands this page.
type SubjectData = rating.OwnerSubjectPageData

// averageStars is the read-only group of stars the subject's average is drawn
// with.
func averageStars(data SubjectData) components.RatingProps {
	return components.RatingProps{
		ComponentProps: components.ComponentProps{Parts: components.Parts{"label": {Class: "sr-only"}}},
		Name:           "average", Label: data.StarsLabel(data.Subject.Name, data.Subject.Stars), Value: data.Subject.StarsValue, ReadOnly: true,
		OptionLabels: data.Labels.StarNames(),
	}
}

// ratingsTable is the ratings the subject received, paged, searched, filtered by
// stars and ordered by the server. A rating already reported shows that, in
// place of the way to report it.
func ratingsTable(data SubjectData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, r := range data.Rows {
		var action components.TableCell
		switch {
		case r.UnderReview:
			action = components.TableCell{HTML: components.Badge(components.BadgeProps{Label: data.Labels.T("screen.under_review"), Variant: "outline"})}
		case r.ReportURL != "":
			action = components.TableCell{HTML: components.Link(components.LinkProps{Label: data.Labels.T("control.report"), URL: r.ReportURL, Variant: "hover"})}
		default:
			action = components.TableCell{Text: "—"}
		}
		rows = append(rows, components.TableRow{Key: r.ID, Cells: []components.TableCell{
			{Text: r.Author},
			{HTML: components.Rating(components.RatingProps{
				ComponentProps: components.ComponentProps{Parts: components.Parts{"label": {Class: "sr-only"}}},
				Name:           "score-" + r.ID, Label: data.StarsLabel(r.Author, r.Score), Value: r.ScoreText, ReadOnly: true,
				OptionLabels: data.Labels.StarNames(),
			})},
			{Text: r.Body},
			{Text: r.Created},
			action,
		}})
	}
	return components.DataTableProps{
		ID: "rating-received", Label: data.Labels.T("screen.owner_subject_title"), URL: data.URL,
		State: data.State(), Paginator: data.Pager, FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.owner_subject_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Searchable: true, SearchPlaceholder: data.Labels.T("control.search_text"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.owner_subject_title")),
		Filters: []components.DataTableFilter{
			{Key: "score", Label: data.Labels.T("field.score"), Options: data.ScoreOptions},
		},
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.author"), Key: "author", Sortable: true, Hideable: true},
			{Label: data.Labels.T("field.score"), Key: "score", Sortable: true},
			{Label: data.Labels.T("field.body"), Key: "body"},
			{Label: data.Labels.T("field.created_at"), Key: "created_at", Sortable: true},
			{Label: data.Labels.T("field.report"), Key: "report"},
		},
		Rows: rows,
		Empty: components.EmptyProps{
			Title: data.Labels.T("screen.owner_subject_empty"), Message: data.Labels.T("screen.owner_subject_empty_lead"),
		},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-6xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>

	<header class="mt-3 flex flex-wrap items-start justify-between gap-4">
		<div class="min-w-0">
			<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.owner_subject_title") }}</p>
			<h1 class="text-2xl font-semibold tracking-tight break-words">{{ .Subject.Name }}</h1>
		</div>
		<div class="flex flex-wrap items-center gap-3">
			@if(.Subject.Rated)
				{!! components.Rating(averageStars(.)) !!}
			@endif
			<p class="text-muted-foreground text-sm">{{ .Reading(.Subject) }}</p>
		</div>
	</header>

	<div class="mt-6 min-w-0">{!! components.DataTable(ratingsTable(.)) !!}</div>
</section>
@endsection
