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
)

// SelectSpans uses the provided SpanFinder to generate the returned
// SpanSelection from the provided Trace.
func SelectSpans[T any, CP, SP, DP fmt.Stringer](
	spanFinder SpanFinder[T, CP, SP, DP],
) *SpanSelection[T, CP, SP, DP] {
	ret := &SpanSelection[T, CP, SP, DP]{}
	if spanFinder != nil {
		spans := spanFinder.FindSpans()
		ret.selectedSpans = make(map[Span[T, CP, SP, DP]]struct{}, len(spans))
		if len(spans) > 0 {
			for _, span := range spans {
				ret.selectedSpans[span] = struct{}{}
			}
		}
	}
	return ret
}

// SpanSelection manages a set of Spans selected from a Trace.
type SpanSelection[T any, CP, SP, DP fmt.Stringer] struct {
	selectedSpans map[Span[T, CP, SP, DP]]struct{} // If nil, selects all.
}

// Includes returns true if the receiver includes (selects) the provided Span.
func (ss *SpanSelection[T, CP, SP, DP]) Includes(span Span[T, CP, SP, DP]) bool {
	if ss.selectedSpans == nil {
		return true
	}
	_, found := ss.selectedSpans[span]
	return found
}

// Spans returns a slice of all selected Spans.  The order of the returned
// slice is not deterministic.
func (ss *SpanSelection[T, CP, SP, DP]) Spans() []Span[T, CP, SP, DP] {
	ret := make([]Span[T, CP, SP, DP], 0, len(ss.selectedSpans))
	for selectedSpan := range ss.selectedSpans {
		ret = append(ret, selectedSpan)
	}
	return ret
}

// Returns a slice containing all of the Categories within the provided Trace.
func allCategories[T any, CP, SP, DP fmt.Stringer](
	t Trace[T, CP, SP, DP],
	ht HierarchyType,
) []Category[T, CP, SP, DP] {
	var ret []Category[T, CP, SP, DP]
	var visit func(categories []Category[T, CP, SP, DP])
	visit = func(categories []Category[T, CP, SP, DP]) {
		ret = append(ret, categories...)
		for _, category := range categories {
			visit(category.ChildCategories())
		}
	}
	rootCategories := make([]Category[T, CP, SP, DP], len(t.RootCategories(ht)))
	for idx, rootCategory := range t.RootCategories(ht) {
		rootCategories[idx] = rootCategory
	}
	visit(rootCategories)
	return ret
}

// SelectCategories uses the provided set of matchers, and the provided Namer,
// to generate the returned CategorySelection from the provided Trace.
func SelectCategories[T any, CP, SP, DP fmt.Stringer](
	spanFinder SpanFinder[T, CP, SP, DP],
	opts ...FindCategoryOption,
) *CategorySelection[T, CP, SP, DP] {
	ret := &CategorySelection[T, CP, SP, DP]{}
	if spanFinder != nil {
		categories := spanFinder.FindCategories(opts...)
		ret.selectedCategories = make(map[Category[T, CP, SP, DP]]struct{}, len(categories))
		if len(categories) > 0 {
			for _, category := range categories {
				ret.selectedCategories[category] = struct{}{}
			}
		}
	}
	return ret
}

// CategorySelection manages a set of Categories selected from a Trace.
type CategorySelection[T any, CP, SP, DP fmt.Stringer] struct {
	t                  Trace[T, CP, SP, DP]
	selectedCategories map[Category[T, CP, SP, DP]]struct{} // If nil, selects all.
}

// Includes returns true if the receiver includes (selects) the provided
// Category.
func (cs *CategorySelection[T, CP, SP, DP]) Includes(category Category[T, CP, SP, DP]) bool {
	if cs.selectedCategories == nil {
		return true
	}
	_, found := cs.selectedCategories[category]
	return found
}

// Categories returns the slice of selected Categories.
func (cs *CategorySelection[T, CP, SP, DP]) Categories(ht HierarchyType) []Category[T, CP, SP, DP] {
	if cs.selectedCategories == nil {
		return allCategories(cs.t, ht)
	}
	ret := make([]Category[T, CP, SP, DP], 0, len(cs.selectedCategories))
	for selectedCategory := range cs.selectedCategories {
		ret = append(ret, selectedCategory)
	}
	return ret
}

// SelectDependencies uses the provided set of origin and destination Span
// matchers, the provided set of matching DependencyTypes, and the provided
// Trace's default Namer to generate the returned DependencySelection from the
// provided Trace.
func SelectDependencies[T any, CP, SP, DP fmt.Stringer](
	t Trace[T, CP, SP, DP],
	originSpanFinder, destinationSpanFinder SpanFinder[T, CP, SP, DP],
	matchingDependencyTypes ...DependencyType,
) *DependencySelection[T, CP, SP, DP] {
	ret := &DependencySelection[T, CP, SP, DP]{
		OriginSelection:      SelectSpans(originSpanFinder),
		DestinationSelection: SelectSpans(destinationSpanFinder),
	}
	if len(matchingDependencyTypes) == 0 {
		matchingDependencyTypes = t.DependencyTypes()
	}
	ret.selectedDependencyTypes = make(map[DependencyType]struct{}, len(matchingDependencyTypes))
	for _, dt := range matchingDependencyTypes {
		ret.selectedDependencyTypes[dt] = struct{}{}
	}
	return ret
}

// DependencySelection selects a set of dependences for participation in a
// transformation.
type DependencySelection[T any, CP, SP, DP fmt.Stringer] struct {
	OriginSelection         *SpanSelection[T, CP, SP, DP]
	DestinationSelection    *SpanSelection[T, CP, SP, DP]
	selectedDependencyTypes map[DependencyType]struct{} // if nil, selects all.
}

// Includes returns true if the receiver includes (selects) the dependency
// relationship defined by the provided origin and destination Spans and
// DependencyType.
func (ds *DependencySelection[T, CP, SP, DP]) Includes(
	originSpan, destinationSpan Span[T, CP, SP, DP],
	dependencyType DependencyType,
) bool {
	return ds.IncludesDependencyType(dependencyType) &&
		ds.IncludesOriginSpan(originSpan) &&
		ds.IncludesDestinationSpan(destinationSpan)
}

// SelectedDependencyTypes returns a slice of selected
// DependencyTypes.
func (ds *DependencySelection[T, CP, SP, DP]) SelectedDependencyTypes(
	t Trace[T, CP, SP, DP],
) []DependencyType {
	ret := make([]DependencyType, 0, len(ds.selectedDependencyTypes))
	for dt := range ds.selectedDependencyTypes {
		ret = append(ret, dt)
	}
	return ret
}

// IncludesDependencyType returns true if the receiver includes (selects) the
// provided DependencyType.
func (ds *DependencySelection[T, CP, SP, DP]) IncludesDependencyType(
	dependencyType DependencyType,
) bool {
	_, ok := ds.selectedDependencyTypes[dependencyType]
	return ok
}

// IncludesOriginSpan returns true if the receiver includes (selects) the
// provided origin Span.
func (ds *DependencySelection[T, CP, SP, DP]) IncludesOriginSpan(
	originSpan Span[T, CP, SP, DP],
) bool {
	return ds.OriginSelection.Includes(originSpan)
}

// IncludesDestinationSpan returns true if the receiver includes (selects) the
// provided destination Span.
func (ds *DependencySelection[T, CP, SP, DP]) IncludesDestinationSpan(
	destinationSpan Span[T, CP, SP, DP],
) bool {
	return ds.DestinationSelection.Includes(destinationSpan)
}
