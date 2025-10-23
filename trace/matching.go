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
)

type matchingElementType int

const (
	name matchingElementType = iota
	id
	nothing
)

func (met matchingElementType) String() string {
	switch met {
	case name:
		return "name"
	case id:
		return "ID"
	case nothing:
		return "-"
	default:
		return "unknown"
	}
}

// PathElementMatcher describes types which can match
type PathElementMatcher interface {
	fmt.Stringer
	match(string) bool
	matchingElement() matchingElementType
	matchesAnything() bool
	isGlobstar() bool
}

// SpanFilter describes a filter applied to spans during matching.  It accepts
// a Span, and returns two bools: the first true if the span should be filtered
// in (true) or out (false); the second true if the traversal should be pruned
// at this span (true) or should continue to this span's children (false).  A
// nil SpanFilter filters in all spans and prunes none.
type SpanFilter[T any, CP, SP, DP fmt.Stringer] func(Span[T, CP, SP, DP]) (include, prune bool)

func matchesSpan[T any, CP, SP, DP fmt.Stringer](
	pem PathElementMatcher,
	namer Namer[T, CP, SP, DP],
	span Span[T, CP, SP, DP],
) bool {
	switch pem.matchingElement() {
	case name:
		return pem.match(namer.SpanName(span))
	case id:
		return pem.match(namer.SpanUniqueID(span))
	default:
		return pem.matchesAnything()
	}
}

func matchesCategory[T any, CP, SP, DP fmt.Stringer](
	pem PathElementMatcher,
	namer Namer[T, CP, SP, DP],
	category Category[T, CP, SP, DP],
) bool {
	switch pem.matchingElement() {
	case name:
		return pem.match(namer.CategoryName(category))
	case id:
		return pem.match(namer.CategoryUniqueID(category))
	default:
		return pem.matchesAnything()
	}
}

type literalMatcher struct {
	literal string
	met     matchingElementType
}

// NewLiteralNameMatcher returns a PathElementMatcher which matches path
// elements' names, as rendered by the provided namer, against the provided
// literal name.
func NewLiteralNameMatcher(
	literal string,
) PathElementMatcher {
	return &literalMatcher{
		literal: literal,
		met:     name,
	}
}

// NewLiteralIDMatcher returns a PathElementMatcher which matches path
// elements' unique IDs, as rendered by the provided namer, against the
// provided literal name.
func NewLiteralIDMatcher(
	literal string,
) PathElementMatcher {
	return &literalMatcher{
		literal: literal,
		met:     id,
	}
}

func (lm *literalMatcher) match(literal string) bool {
	return literal == lm.literal
}

func (lm *literalMatcher) matchingElement() matchingElementType {
	return lm.met
}

func (lm *literalMatcher) matchesAnything() bool {
	return false
}

func (lm *literalMatcher) isGlobstar() bool {
	return false
}

func (lm *literalMatcher) String() string {
	return fmt.Sprintf("<literal %s %s>", lm.met, lm.literal)
}

type regexpMatcher struct {
	regex *regexp.Regexp
	met   matchingElementType
}

// NewRegexpNameMatcher returns a PathElementMatcher which matches path
// elements' names, as rendered by the provided namer, against the provided
// regular expression.
func NewRegexpNameMatcher(
	regexStr string,
) (PathElementMatcher, error) {
	regex, err := regexp.Compile(regexStr)
	if err != nil {
		return nil, err
	}
	return &regexpMatcher{
		regex: regex,
		met:   name,
	}, nil
}

// NewRegexpIDMatcher returns a PathElementMatcher which matches path
// elements' unique IDs, as rendered by the provided namer, against the
// provided regular expression.
func NewRegexpIDMatcher[T any, CP, SP, DP fmt.Stringer](
	regexStr string,
) (PathElementMatcher, error) {

	regex, err := regexp.Compile(regexStr)
	if err != nil {
		return nil, err
	}
	return &regexpMatcher{
		regex: regex,
		met:   id,
	}, nil
}

func (rm *regexpMatcher) match(literal string) bool {
	return rm.regex.MatchString(literal)
}

func (rm *regexpMatcher) matchingElement() matchingElementType {
	return rm.met
}

func (rm *regexpMatcher) matchesAnything() bool {
	return false
}

func (rm *regexpMatcher) isGlobstar() bool {
	return false
}

func (rm *regexpMatcher) String() string {
	return fmt.Sprintf("<regexp %s %s>", rm.met, rm.regex.String())
}

type globstar struct{}

func (g globstar) match(literal string) bool {
	return true
}

func (g globstar) matchingElement() matchingElementType {
	return nothing
}

func (g globstar) matchesAnything() bool {
	return true
}

func (g globstar) isGlobstar() bool {
	return true
}

func (g globstar) String() string {
	return fmt.Sprintf("<globstar>")
}

// Globstar is a globstar matcher, matching any number of path elements.
var Globstar = globstar{}

type star struct{}

func (s star) match(literal string) bool {
	return true
}

func (s star) matchingElement() matchingElementType {
	return nothing
}

func (s star) matchesAnything() bool {
	return true
}

func (s star) isGlobstar() bool {
	return false
}

func (s star) String() string {
	return fmt.Sprintf("<star>")
}

// Star is a star matcher, matching any single path element.
var Star = star{}

// A generalization of the matcher-visitor pattern required for both Span and
// Category.
func visit[E any](
	els []E,
	matchers []PathElementMatcher,
	// Should return true if the provided matcher matches the provided element.
	matchFn func(el E, matcher PathElementMatcher) bool,
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
	case thisMatcher.isGlobstar():
		if noMoreMatchers {
			elsMatchFn(els...)
		}
		visit(els, remainingMatchers, matchFn, getChildrenFn, elsMatchFn)
		for _, el := range els {
			visit(getChildrenFn(el), matchers, matchFn, getChildrenFn, elsMatchFn)
		}
	case noMoreMatchers && thisMatcher.matchesAnything():
		elsMatchFn(els...)
	case noMoreMatchers:
		for _, el := range els {
			if matchFn(el, thisMatcher) {
				elsMatchFn(el)
			}
		}
	default:
		var matchingEls []E
		for _, el := range els {
			if matchFn(el, thisMatcher) {
				matchingEls = append(matchingEls, el)
			}
		}
		if remainingMatchers[0].isGlobstar() {
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
	matchers := make([]PathElementMatcher, len(path))
	for idx, pathEl := range path {
		matchers[idx] = NewLiteralIDMatcher(pathEl)
	}
	spans := findSpans(
		trace,
		namer,
		nil,
		matchers,
		SpanOnlyHierarchyType,
		nil,
	)
	if len(spans) != 1 {
		return nil, fmt.Errorf("encoded span path %s matched %d spans; expected 1", encodedIDPath, len(spans))
	}
	return spans[0], nil
}

func applySpanFilter[T any, CP, SP, DP fmt.Stringer](
	span Span[T, CP, SP, DP],
	spanFilter SpanFilter[T, CP, SP, DP],
) (include, prune bool) {
	if spanFilter == nil {
		return true, false
	}
	return spanFilter(span)
}

// findSpans finds and returns all spans from the provided trace whose stacks
// match the provided matcher slices.
func findSpans[T any, CP, SP, DP fmt.Stringer](
	trace Trace[T, CP, SP, DP],
	namer Namer[T, CP, SP, DP],
	spanFilter SpanFilter[T, CP, SP, DP],
	spanMatchers []PathElementMatcher,
	hierarchyType HierarchyType,
	categoryMatchers []PathElementMatcher,
) []Span[T, CP, SP, DP] {
	if len(spanMatchers) == 0 {
		return nil
	}
	includedSpans := map[Span[T, CP, SP, DP]]struct{}{}
	var ret []Span[T, CP, SP, DP]
	addSpans := func(spans ...Span[T, CP, SP, DP]) {
		for _, span := range spans {
			if include, _ := applySpanFilter(span, spanFilter); include {
				if _, ok := includedSpans[span]; !ok {
					includedSpans[span] = struct{}{}
					ret = append(ret, span)
				}
			}
		}
	}
	var rootSpans []Span[T, CP, SP, DP]
	if len(categoryMatchers) == 0 || hierarchyType == SpanOnlyHierarchyType {
		for _, rs := range trace.RootSpans() {
			rootSpans = append(rootSpans, rs)
		}
	} else {
		for _, cat := range findCategories(trace, namer, hierarchyType, categoryMatchers) {
			for _, span := range cat.RootSpans() {
				rootSpans = append(rootSpans, span)
			}
		}
	}
	visit(
		rootSpans,
		spanMatchers,
		func(span Span[T, CP, SP, DP], matcher PathElementMatcher) bool {
			return matchesSpan(matcher, namer, span)
		},
		func(span Span[T, CP, SP, DP]) []Span[T, CP, SP, DP] {
			if _, prune := applySpanFilter(span, spanFilter); prune {
				return nil
			}
			return span.ChildSpans()
		},
		addSpans,
	)
	return ret
}

// findCategories finds and returns all categories from the provided trace
// whose stacks match the provided matcher slice.
func findCategories[T any, CP, SP, DP fmt.Stringer](
	trace Trace[T, CP, SP, DP],
	namer Namer[T, CP, SP, DP],
	ht HierarchyType,
	matchers []PathElementMatcher,
) []Category[T, CP, SP, DP] {
	if len(matchers) == 0 {
		return nil
	}
	includedCategories := map[Category[T, CP, SP, DP]]struct{}{}
	var ret []Category[T, CP, SP, DP]
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
	visit(
		rootCategories,
		matchers,
		func(category Category[T, CP, SP, DP], matcher PathElementMatcher) bool {
			return matchesCategory(matcher, namer, category)
		},
		func(category Category[T, CP, SP, DP]) []Category[T, CP, SP, DP] {
			return category.ChildCategories()
		},
		addCategories,
	)
	return ret
}

type spanAndCategoryMatcher struct {
	ht               HierarchyType
	spanMatchers     []PathElementMatcher
	categoryMatchers []PathElementMatcher
}

// SpanPattern describes a pattern matching a set of spans, not yet
// specialized to a particular Trace or Namer.
type SpanPattern struct {
	matchers []*spanAndCategoryMatcher
}

// SpanPatternOption is an option applied to NewSpanPattern.
type SpanPatternOption func(
	sp *SpanPattern,
)

// SpanMatchers applies a set of span matcher patterns to NewSpanPattern.
func SpanMatchers(
	spanMatchersSets ...[]PathElementMatcher,
) SpanPatternOption {
	return func(sp *SpanPattern) {
		for _, spanMatchers := range spanMatchersSets {
			sp.matchers = append(sp.matchers, &spanAndCategoryMatcher{
				ht:           SpanOnlyHierarchyType,
				spanMatchers: spanMatchers,
			})
		}
	}
}

// SpanAndCategoryMatchers applies sets of span and category matcher patterns
// to NewSpanPattern.  If the category matcher patterns are nonempty, they will
// be used to find categories on invocations of SpanFinder.FindCategories;
// otherwise the span matcher patterns will also be used to find categories.
func SpanAndCategoryMatchers(
	hierarchyType HierarchyType,
	categoryMatchers []PathElementMatcher,
	spanMatchers []PathElementMatcher,
) SpanPatternOption {
	return func(sp *SpanPattern) {
		sp.matchers = append(sp.matchers, &spanAndCategoryMatcher{
			ht:               hierarchyType,
			categoryMatchers: categoryMatchers,
			spanMatchers:     spanMatchers,
		})
	}
}

// NewSpanPattern returns a new SpanPattern with the provided— options applied.
func NewSpanPattern(
	opts ...SpanPatternOption,
) *SpanPattern {
	ret := &SpanPattern{}
	for _, opt := range opts {
		opt(ret)
	}
	return ret
}

// FindCategoryOption defines an option to SpanFinder's FindCategories()
// method.
type FindCategoryOption func(*findCategoryOpts)

// SpanFinder instances can return slices of Spans from a particular Trace that
// match some selection criteria.
type SpanFinder[T any, CP, SP, DP fmt.Stringer] interface {
	Comparator() Comparator[T]
	FindSpans() []Span[T, CP, SP, DP]
	FindCategories(opts ...FindCategoryOption) []Category[T, CP, SP, DP]
	// Defaults to the trace's DefaultNamer.
	WithNamer(Namer[T, CP, SP, DP]) SpanFinder[T, CP, SP, DP]
	// If specified, only spans for which the provided function returns true are
	// traversed.
	WithSpanFilter(SpanFilter[T, CP, SP, DP]) SpanFinder[T, CP, SP, DP]
}

func allSpans[T any, CP, SP, DP fmt.Stringer](t Trace[T, CP, SP, DP]) SpanFinder[T, CP, SP, DP] {
	return NewSpanFinder(
		NewSpanPattern(SpanMatchers([]PathElementMatcher{Globstar})),
		t)
}

type spanFinder[T any, CP, SP, DP fmt.Stringer] struct {
	sp         *SpanPattern
	t          Trace[T, CP, SP, DP]
	namer      Namer[T, CP, SP, DP]
	spanFilter SpanFilter[T, CP, SP, DP]
}

func (sf *spanFinder[T, CP, SP, DP]) FindSpans() []Span[T, CP, SP, DP] {
	var ret []Span[T, CP, SP, DP]
	for _, matcher := range sf.sp.matchers {
		ret = append(
			ret,
			findSpans(sf.t, sf.namer, sf.spanFilter, matcher.spanMatchers, matcher.ht, matcher.categoryMatchers)...,
		)
	}
	return ret
}

type findCategoryOpts struct {
	useSpanIfNoCategory, useCategoryEvenIfSpan bool
}

// UseSpanIfNoCategory specifies that FindCategories should use a matcher's
// span pattern to match categories, if no category pattern is specified.  This
// option should be used when only a single pattern, which could identify
// either spans or categories, has been specified.
func UseSpanIfNoCategory(fco *findCategoryOpts) {
	fco.useSpanIfNoCategory = true
}

// UseCategoryEvenIfSpan specifies that FindCategories should use a matcher's
// category pattern to match categories, even if the span pattern is populated.
// This option should be used when the set of categories containing matched
// spans is desired.
func UseCategoryEvenIfSpan(fco *findCategoryOpts) {
	fco.useCategoryEvenIfSpan = true
}

func (sf *spanFinder[T, CP, SP, DP]) FindCategories(
	opts ...FindCategoryOption,
) []Category[T, CP, SP, DP] {
	fco := &findCategoryOpts{}
	for _, opt := range opts {
		opt(fco)
	}
	var ret []Category[T, CP, SP, DP]
	if sf.spanFilter != nil {
		// SpanFinders with applied span filters cannot find categories.
		return ret
	}
	for _, matcher := range sf.sp.matchers {
		if matcher.ht == SpanOnlyHierarchyType {
			continue
		}
		var pems []PathElementMatcher
		if fco.useSpanIfNoCategory && len(matcher.categoryMatchers) == 0 {
			pems = matcher.spanMatchers
		}
		if len(matcher.categoryMatchers) > 0 && (fco.useCategoryEvenIfSpan || len(matcher.spanMatchers) == 0) {
			pems = matcher.categoryMatchers
		}
		if len(pems) == 0 {
			continue
		}
		ret = append(
			ret,
			findCategories(sf.t, sf.namer, matcher.ht, pems)...,
		)
	}
	return ret
}

func (sf *spanFinder[T, CP, SP, DP]) Comparator() Comparator[T] {
	return sf.t.Comparator()
}

func (sf *spanFinder[T, CP, SP, DP]) WithNamer(
	namer Namer[T, CP, SP, DP],
) SpanFinder[T, CP, SP, DP] {
	sf.namer = namer
	return sf
}

func (sf *spanFinder[T, CP, SP, DP]) WithSpanFilter(spanFilter SpanFilter[T, CP, SP, DP]) SpanFinder[T, CP, SP, DP] {
	sf.spanFilter = spanFilter
	return sf
}

// NewSpanFinder returns a new SpanFinder applying the provided SpanFinder to
// the provided Trace.
func NewSpanFinder[T any, CP, SP, DP fmt.Stringer](
	sp *SpanPattern,
	t Trace[T, CP, SP, DP],
) SpanFinder[T, CP, SP, DP] {
	if sp == nil {
		return allSpans(t)
	}
	ret := &spanFinder[T, CP, SP, DP]{
		sp:    sp,
		t:     t,
		namer: t.DefaultNamer(),
	}
	return ret
}
