/*
	Copyright 2024 Google Inc.

	Licensed under the Apache License, Version 2.0 (the "License");
	you may not use this file except in compliance with the License.
	You may obtain a copy of the License at

			http://www.apache.org/licenses/LICENSE-2.0

	Unless required by applicable law or agreed to in writing, software
	distributed under the License is distributed on an "AS IS" BASIS,
	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
	See the License for the specific language governing permissions and
	limitations under the License.
*/

package trace

import (
	"fmt"
	"regexp"
	"sort"
)

// PathElementMatcher describes types which can match
type PathElementMatcher[T any, CP, SP, DP fmt.Stringer] interface {
	fmt.Stringer
	// Returns true iff the provided span matches this matcher element.
	MatchesSpan(namer Namer[T, CP, SP, DP], span Span[T, CP, SP, DP]) bool
	MatchesCategory(namer Namer[T, CP, SP, DP], category Category[T, CP, SP, DP]) bool
	MatchesAnything() bool
	IsGlobstar() bool
}

type literalNameMatcher[T any, CP, SP, DP fmt.Stringer] struct {
	name string
}

// NewLiteralNameMatcher returns a PathElementMatcher which matches path
// element's names, as rendered by the provided namer, against the provided
// literal name.
func NewLiteralNameMatcher[T any, CP, SP, DP fmt.Stringer](
	name string,
) PathElementMatcher[T, CP, SP, DP] {
	return &literalNameMatcher[T, CP, SP, DP]{
		name: name,
	}
}

func (lnm *literalNameMatcher[T, CP, SP, DP]) MatchesSpan(namer Namer[T, CP, SP, DP], span Span[T, CP, SP, DP]) bool {
	return namer.SpanName(span) == lnm.name
}

func (lnm *literalNameMatcher[T, CP, SP, DP]) MatchesCategory(namer Namer[T, CP, SP, DP], category Category[T, CP, SP, DP]) bool {
	return namer.CategoryName(category) == lnm.name
}

func (lnm *literalNameMatcher[T, CP, SP, DP]) MatchesAnything() bool {
	return false
}

func (lnm *literalNameMatcher[T, CP, SP, DP]) IsGlobstar() bool {
	return false
}

func (lnm *literalNameMatcher[T, CP, SP, DP]) String() string {
	return fmt.Sprintf("<literal name %s>", lnm.name)
}

type literalIDMatcher[T any, CP, SP, DP fmt.Stringer] struct {
	id string
}

// NewLiteralIDMatcher returns a PathElementMatcher which matches path
// element's unique IDs, as rendered by the provided namer, against the
// provided literal ID.
func NewLiteralIDMatcher[T any, CP, SP, DP fmt.Stringer](
	id string,
) PathElementMatcher[T, CP, SP, DP] {
	return &literalIDMatcher[T, CP, SP, DP]{
		id: id,
	}
}

func (lim *literalIDMatcher[T, CP, SP, DP]) MatchesSpan(namer Namer[T, CP, SP, DP], span Span[T, CP, SP, DP]) bool {
	return namer.SpanUniqueID(span) == lim.id
}

func (lim *literalIDMatcher[T, CP, SP, DP]) MatchesCategory(namer Namer[T, CP, SP, DP], category Category[T, CP, SP, DP]) bool {
	return namer.CategoryUniqueID(category) == lim.id
}

func (lim *literalIDMatcher[T, CP, SP, DP]) MatchesAnything() bool {
	return false
}

func (lim *literalIDMatcher[T, CP, SP, DP]) IsGlobstar() bool {
	return false
}

func (lim *literalIDMatcher[T, CP, SP, DP]) String() string {
	return fmt.Sprintf("<literal ID %s>", lim.id)
}

type regexpNameMatcher[T any, CP, SP, DP fmt.Stringer] struct {
	regex *regexp.Regexp
}

// NewRegexpNameMatcher returns a PathElementMatcher which matches path
// element's names, as rendered by the provided namer, against the provided
// regular expression.
func NewRegexpNameMatcher[T any, CP, SP, DP fmt.Stringer](
	regexStr string,
) (PathElementMatcher[T, CP, SP, DP], error) {
	regex, err := regexp.Compile(regexStr)
	if err != nil {
		return nil, err
	}
	return &regexpNameMatcher[T, CP, SP, DP]{
		regex: regex,
	}, nil
}

func (rnm *regexpNameMatcher[T, CP, SP, DP]) MatchesSpan(namer Namer[T, CP, SP, DP], span Span[T, CP, SP, DP]) bool {
	spanName := namer.SpanName(span)
	return rnm.regex.MatchString(spanName)
}

func (rnm *regexpNameMatcher[T, CP, SP, DP]) MatchesCategory(namer Namer[T, CP, SP, DP], category Category[T, CP, SP, DP]) bool {
	categoryName := namer.CategoryName(category)
	return rnm.regex.MatchString(categoryName)
}

func (rnm *regexpNameMatcher[T, CP, SP, DP]) MatchesAnything() bool {
	return false
}

func (rnm *regexpNameMatcher[T, CP, SP, DP]) IsGlobstar() bool {
	return false
}

func (rnm *regexpNameMatcher[T, CP, SP, DP]) String() string {
	return fmt.Sprintf("<regexp %s>", rnm.regex.String())
}

type globstar[T any, CP, SP, DP fmt.Stringer] struct{}

func (g globstar[T, CP, SP, DP]) MatchesSpan(namer Namer[T, CP, SP, DP], span Span[T, CP, SP, DP]) bool {
	return true
}

func (g globstar[T, CP, SP, DP]) MatchesCategory(namer Namer[T, CP, SP, DP], category Category[T, CP, SP, DP]) bool {
	return true
}

func (g globstar[T, CP, SP, DP]) MatchesAnything() bool {
	return true
}

func (g globstar[T, CP, SP, DP]) IsGlobstar() bool {
	return true
}

func (g globstar[T, CP, SP, DP]) String() string {
	return fmt.Sprintf("<globstar>")
}

// Globstar returns a new globstar matcher.  A globstar matcher matches any
// number of path elements.
func Globstar[T any, CP, SP, DP fmt.Stringer]() PathElementMatcher[T, CP, SP, DP] {
	return globstar[T, CP, SP, DP]{}
}

type star[T any, CP, SP, DP fmt.Stringer] struct{}

func (s star[T, CP, SP, DP]) MatchesSpan(namer Namer[T, CP, SP, DP], span Span[T, CP, SP, DP]) bool {
	return true
}

func (s star[T, CP, SP, DP]) MatchesCategory(namer Namer[T, CP, SP, DP], category Category[T, CP, SP, DP]) bool {
	return true
}

func (s star[T, CP, SP, DP]) MatchesAnything() bool {
	return true
}

func (s star[T, CP, SP, DP]) IsGlobstar() bool {
	return false
}

func (s star[T, CP, SP, DP]) String() string {
	return fmt.Sprintf("<star>")
}

// Star returns a new star matcher.  A star matcher matches any single path
// element.
func Star[T any, CP, SP, DP fmt.Stringer]() PathElementMatcher[T, CP, SP, DP] {
	return star[T, CP, SP, DP]{}
}

// A generalization of the matcher-visitor pattern required for both Span and
// Category.
func visit[E any, T any, CP, SP, DP fmt.Stringer](
	els []E,
	matchers []PathElementMatcher[T, CP, SP, DP],
	// Should return true if the provided matcher matches the provided element.
	matchFn func(el E, matcher PathElementMatcher[T, CP, SP, DP]) bool,
	// Should return all children of the provided element.
	getChildrenFn func(el E) []E,
	// Should record that the provided elements match.  Note that the same
	// element may be provided multiple times in a traversal, if that traversal's
	// matchers include globstars.
	elsMatchFn func(els ...E),
) {
	if len(els) == 0 || len(matchers) == 0 {
		return
	}
	thisMatcher, remainingMatchers := matchers[0], matchers[1:]
	noMoreMatchers := len(remainingMatchers) == 0
	switch {
	case thisMatcher.IsGlobstar():
		if noMoreMatchers {
			elsMatchFn(els...)
		}
		visit(els, remainingMatchers, matchFn, getChildrenFn, elsMatchFn)
		for _, el := range els {
			visit(getChildrenFn(el), matchers, matchFn, getChildrenFn, elsMatchFn)
		}
	case noMoreMatchers && thisMatcher.MatchesAnything():
		elsMatchFn(els...)
	case noMoreMatchers:
		for _, el := range els {
			if matchFn(el, thisMatcher) {
				elsMatchFn(el)
			}
		}
	default:
		matchingEls := []E{}
		for _, el := range els {
			if matchFn(el, thisMatcher) {
				matchingEls = append(matchingEls, el)
			}
		}
		if remainingMatchers[0].IsGlobstar() {
			// Since globstars, uniquely, can match no spans, if the next matcher
			// is a globstar, we have to apply all subsequent patterns *here* too.
			visit(matchingEls, remainingMatchers, matchFn, getChildrenFn, elsMatchFn)
		}
		for _, matchingEl := range matchingEls {
			visit(getChildrenFn(matchingEl), remainingMatchers, matchFn, getChildrenFn, elsMatchFn)
		}
	}
}

// FindSpanByEncodedIDPath returns the span in the provided trace identified by
// the provided encoded unique ID path.
func FindSpanByEncodedIDPath[T any, CP, SP, DP fmt.Stringer](
	trace Trace[T, CP, SP, DP],
	namer Namer[T, CP, SP, DP],
	encodedIDPath string,
) (Span[T, CP, SP, DP], error) {
	path, err := DecodePath(encodedIDPath)
	if err != nil {
		return nil, err
	}
	matchers := make([]PathElementMatcher[T, CP, SP, DP], len(path))
	for idx, pathEl := range path {
		matchers[idx] = NewLiteralIDMatcher[T, CP, SP, DP](pathEl)
	}
	spans := findSpans(
		trace,
		namer,
		[][]PathElementMatcher[T, CP, SP, DP]{matchers},
		SpanOnlyHierarchyType,
		nil,
	)
	if len(spans) != 1 {
		return nil, fmt.Errorf("encoded span path %s matched %d spans; expected 1", encodedIDPath, len(spans))
	}
	return spans[0], nil
}

// findSpans finds and returns all spans from the provided trace whose stacks
// match the provided matcher slice.
func findSpans[T any, CP, SP, DP fmt.Stringer](
	trace Trace[T, CP, SP, DP],
	namer Namer[T, CP, SP, DP],
	spanMatchers [][]PathElementMatcher[T, CP, SP, DP],
	hierarchyType HierarchyType,
	categoryMatchers [][]PathElementMatcher[T, CP, SP, DP],
) []Span[T, CP, SP, DP] {
	if len(spanMatchers) == 0 || (hierarchyType != SpanOnlyHierarchyType && len(categoryMatchers) == 0) {
		return nil
	}
	includedSpans := map[Span[T, CP, SP, DP]]struct{}{}
	ret := []Span[T, CP, SP, DP]{}
	addSpans := func(spans ...Span[T, CP, SP, DP]) {
		for _, span := range spans {
			_, ok := includedSpans[span]
			if !ok {
				includedSpans[span] = struct{}{}
				ret = append(ret, span)
			}
		}
	}
	var rootSpans []Span[T, CP, SP, DP]
	if hierarchyType == SpanOnlyHierarchyType {
		rootSpans = make([]Span[T, CP, SP, DP], len(trace.RootSpans()))
		for idx, rs := range trace.RootSpans() {
			rootSpans[idx] = rs
		}
	} else {
		for _, cat := range FindCategories(trace, namer, hierarchyType, categoryMatchers) {
			for _, span := range cat.RootSpans() {
				rootSpans = append(rootSpans, span)
			}
		}
	}
	for _, matcherSlice := range spanMatchers {
		visit[Span[T, CP, SP, DP], T, CP, SP, DP](
			rootSpans,
			matcherSlice,
			func(span Span[T, CP, SP, DP], matcher PathElementMatcher[T, CP, SP, DP]) bool {
				return matcher.MatchesSpan(namer, span)
			},
			func(span Span[T, CP, SP, DP]) []Span[T, CP, SP, DP] {
				return span.ChildSpans()
			},
			addSpans,
		)
	}
	return ret
}

// FindCategories finds and returns all categories from the provided trace
// whose stacks match the provided matcher slice.
func FindCategories[T any, CP, SP, DP fmt.Stringer](
	trace Trace[T, CP, SP, DP],
	namer Namer[T, CP, SP, DP],
	ht HierarchyType,
	matchers [][]PathElementMatcher[T, CP, SP, DP],
) []Category[T, CP, SP, DP] {
	if len(matchers) == 0 {
		return nil
	}
	includedCategories := map[Category[T, CP, SP, DP]]struct{}{}
	ret := []Category[T, CP, SP, DP]{}
	addCategories := func(categories ...Category[T, CP, SP, DP]) {
		for _, category := range categories {
			_, ok := includedCategories[category]
			if !ok {
				includedCategories[category] = struct{}{}
				ret = append(ret, category)
			}
		}
	}

	rootCategories := make([]Category[T, CP, SP, DP], len(trace.RootCategories(ht)))
	for idx, rs := range trace.RootCategories(ht) {
		rootCategories[idx] = rs
	}
	for _, matcherSlice := range matchers {
		visit[Category[T, CP, SP, DP], T, CP, SP, DP](
			rootCategories,
			matcherSlice,
			func(category Category[T, CP, SP, DP], matcher PathElementMatcher[T, CP, SP, DP]) bool {
				return matcher.MatchesCategory(namer, category)
			},
			func(category Category[T, CP, SP, DP]) []Category[T, CP, SP, DP] {
				return category.ChildCategories()
			},
			addCategories,
		)
	}
	return ret
}

// SpanFinder facilitates finding spans by pattern within a trace.
type SpanFinder[T any, CP, SP, DP fmt.Stringer] struct {
	namer            Namer[T, CP, SP, DP]
	spanMatchers     [][]PathElementMatcher[T, CP, SP, DP]
	categoryMatchers map[HierarchyType][][]PathElementMatcher[T, CP, SP, DP]
}

// NewSpanFinder returns a new SpanFinder, using the provided trace namer.
func NewSpanFinder[T any, CP, SP, DP fmt.Stringer](
	namer Namer[T, CP, SP, DP],
) *SpanFinder[T, CP, SP, DP] {
	return &SpanFinder[T, CP, SP, DP]{
		namer:            namer,
		categoryMatchers: map[HierarchyType][][]PathElementMatcher[T, CP, SP, DP]{},
	}
}

// WithSpanMatchers adds the specified span matchers.
func (sf *SpanFinder[T, CP, SP, DP]) WithSpanMatchers(
	spanMatchers ...[]PathElementMatcher[T, CP, SP, DP],
) *SpanFinder[T, CP, SP, DP] {
	sf.spanMatchers = append(sf.spanMatchers, spanMatchers...)
	return sf
}

// WithCategoryMatchers adds the specified span
func (sf *SpanFinder[T, CP, SP, DP]) WithCategoryMatchers(
	hierarchyType HierarchyType,
	categoryMatchers ...[]PathElementMatcher[T, CP, SP, DP],
) *SpanFinder[T, CP, SP, DP] {
	if hierarchyType != SpanOnlyHierarchyType && len(categoryMatchers) > 0 {
		sf.categoryMatchers[hierarchyType] = append(sf.categoryMatchers[hierarchyType], categoryMatchers...)
	}
	return sf
}

// Find returns all spans matching the receiver within the provided Trace.
func (sf *SpanFinder[T, CP, SP, DP]) Find(
	t Trace[T, CP, SP, DP],
) []Span[T, CP, SP, DP] {
	if len(sf.categoryMatchers) == 0 {
		return findSpans(t, sf.namer, sf.spanMatchers, SpanOnlyHierarchyType, nil)
	}
	hts := make([]HierarchyType, 0, len(sf.categoryMatchers))
	for ht := range sf.categoryMatchers {
		hts = append(hts, ht)
	}
	sort.Slice(hts, func(a, b int) bool {
		return hts[a] < hts[b]
	})
	ret := []Span[T, CP, SP, DP]{}
	for _, ht := range hts {
		ret = append(ret, findSpans(t, sf.namer, sf.spanMatchers, ht, sf.categoryMatchers[ht])...)
	}
	return ret
}
