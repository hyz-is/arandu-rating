//go:build kyse

package rating

import (
	"github.com/arandu-io/kyse/components"

	rating "github.com/hyz-is/arandu-rating"
)

@go
// FormData is what the handler hands this page.
type FormData = rating.FormPageData

// averageStars is the read-only group of stars a subject's average is drawn
// with, or nothing when nobody rated it yet.
func averageStars(data FormData) components.RatingProps {
	return components.RatingProps{
		ComponentProps: components.ComponentProps{Parts: components.Parts{"label": {Class: "sr-only"}}},
		Name:           "average", Label: data.Subject.Name, Value: data.Subject.StarsValue, ReadOnly: true,
		OptionLabels: data.Labels.StarNames(),
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-2xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>

	<header class="mt-3">
		<h1 class="text-2xl font-semibold tracking-tight break-words">{{ .Labels.T("screen.form_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.form_lead") }}</p>
	</header>

	<div class="card mt-6 grid gap-2 p-5">
		<p class="text-muted-foreground text-sm">{{ .Labels.T("field.subject") }}</p>
		@if(.Subject.URL != "")
			<a class="text-lg font-semibold break-words hover:underline" href="{{ .Subject.URL }}">{{ .Subject.Name }}</a>
		@else
			<p class="text-lg font-semibold break-words">{{ .Subject.Name }}</p>
		@endif
		@if(.Subject.Rated)
			<div class="flex flex-wrap items-center gap-3">
				{!! components.Rating(averageStars(.)) !!}
				<p class="text-muted-foreground text-sm">{{ .Reading(.Subject) }}</p>
			</div>
		@endif
	</div>

	<form class="card mt-6 grid gap-4 p-5" method="post" action="{{ .Action }}">
		@csrf
		@if(.HasErrors())
			<ul class="text-destructive grid gap-1 text-sm" role="alert">
				@foreach(.All() as line)
					<li>{{ line }}</li>
				@endforeach
			</ul>
		@endif
		{!! components.Rating(components.RatingProps{Name: "score", Label: .Labels.T("field.score"), Required: true, OptionLabels: .Labels.StarNames(), Hint: .Labels.T("screen.score_hint"), Page: .Form()}) !!}
		{!! components.Textarea(components.TextareaProps{Name: "body", Label: .Labels.T("field.body"), Hint: .CharsHint(.MaxChars, .BodyRequire), Rows: 6, Required: .BodyRequire, Page: .Form()}) !!}
		<div class="flex flex-wrap items-center gap-2">
			{!! components.Button(components.ButtonProps{Label: .Labels.T("control.send"), Type: "submit"}) !!}
			<a class="btn" data-variant="outline" href="{{ .BackURL }}">{{ .Labels.T("control.cancel") }}</a>
		</div>
	</form>
</section>
@endsection
