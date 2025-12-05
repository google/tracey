/*
	Copyright 2025 Google Inc.

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

package stitch

import (
	"fmt"

	"github.com/google/tracey/trace"
)

type singleTraceStitcher[T any, CP, SP, DP fmt.Stringer] struct {
	prefix                    string
	s                         *stitcher[T, CP, SP, DP]
	originalToNewDependencies map[trace.Dependency[T, CP, SP, DP]]trace.MutableDependency[T, CP, SP, DP]
	originalToNewSpans        map[trace.Span[T, CP, SP, DP]]trace.MutableSpan[T, CP, SP, DP]
	originalToNewCategories   map[trace.Category[T, CP, SP, DP]]trace.Category[T, CP, SP, DP]
}

func (sts *singleTraceStitcher[T, CP, SP, DP]) newDependencyFromOriginal(
	originalDependency trace.Dependency[T, CP, SP, DP],
) trace.MutableDependency[T, CP, SP, DP] {
	if ret, ok := sts.originalToNewDependencies[originalDependency]; ok {
		return ret
	}
	ret := sts.s.newTrace.NewMutableDependency(
		originalDependency.DependencyType(),
		originalDependency.Options(),
	).
		WithPayload(originalDependency.Payload())
	sts.originalToNewDependencies[originalDependency] = ret
	return ret
}

func (sts *singleTraceStitcher[T, CP, SP, DP]) newSpanFromOriginal(
	originalSpan trace.Span[T, CP, SP, DP],
	newParentSpan trace.MutableSpan[T, CP, SP, DP],
) error {
	// Clone the original span's elementary spans.
	newESs := make([]trace.MutableElementarySpan[T, CP, SP, DP], 0, len(originalSpan.ElementarySpans()))
	for _, originalES := range originalSpan.ElementarySpans() {
		newES := trace.NewMutableElementarySpan[T, CP, SP, DP]().
			WithStart(originalES.Start()).
			WithEnd(originalES.End())
		newESs = append(newESs, newES)
		if originalES.Incoming() != nil {
			newIncoming := sts.newDependencyFromOriginal(originalES.Incoming())
			newIncoming.WithDestinationElementarySpan(newES)
		}
		if originalES.Outgoing() != nil {
			newOutgoing := sts.newDependencyFromOriginal(originalES.Outgoing())
			if err := newOutgoing.SetOriginElementarySpan(sts.s.newTrace.Comparator(), newES); err != nil {
				return err
			}
		}
	}
	var newSpan trace.MutableSpan[T, CP, SP, DP]
	if newParentSpan != nil {
		var err error
		newSpan, err = newParentSpan.NewMutableChildSpan(newESs, originalSpan.Payload())
		if err != nil {
			return err
		}
	} else {
		// This is a root span; create it under the new trace.
		var err error
		newSpan, err = sts.s.newTrace.NewMutableRootSpan(newESs, originalSpan.Payload())
		if err != nil {
			return err
		}
	}
	if sts.s.updateSpanUniqueID != nil {
		sts.s.updateSpanUniqueID(newSpan.Payload(), sts.prefix)
	}
	sts.originalToNewSpans[originalSpan] = newSpan
	// Populate this span's descendants.
	for _, originalChildSpan := range originalSpan.ChildSpans() {
		if err := sts.newSpanFromOriginal(originalChildSpan, newSpan); err != nil {
			return err
		}
	}
	return nil
}

func (sts *singleTraceStitcher[T, CP, SP, DP]) newCategoryFromOriginal(
	originalCat trace.Category[T, CP, SP, DP],
	newParentCat trace.Category[T, CP, SP, DP],
) (trace.Category[T, CP, SP, DP], error) {
	var newCat trace.Category[T, CP, SP, DP]
	if newParentCat != nil {
		// This is a non-root category; fetch its parent and create it under that.
		newCat = newParentCat.NewChildCategory(originalCat.Payload())
	} else {
		// This is a root category; create it under the new trace.
		newCat = sts.s.newTrace.NewRootCategory(originalCat.HierarchyType(), originalCat.Payload())
	}
	sts.originalToNewCategories[originalCat] = newCat
	// Populate this category's spans.
	for _, originalRootSpan := range originalCat.RootSpans() {
		rootSpan, ok := sts.originalToNewSpans[originalRootSpan]
		if !ok {
			return nil, fmt.Errorf("failed to find root span '%s'", sts.s.newTrace.DefaultNamer().SpanName(originalRootSpan))
		}
		if err := newCat.AddRootSpan(rootSpan.RootSpan()); err != nil {
			return nil, err
		}
	}
	// Populate this category's descendants.
	for _, originalChildCategory := range originalCat.ChildCategories() {
		if _, err := sts.newCategoryFromOriginal(originalChildCategory, newCat); err != nil {
			return nil, err
		}
	}
	if sts.s.updateCategoryUniqueID != nil {
		sts.s.updateCategoryUniqueID(newCat.Payload(), sts.prefix)
	}
	return newCat, nil
}

func (s *stitcher[T, CP, SP, DP]) stitchInNextTrace() (bool, error) {
	if len(s.originalTraces) == 0 {
		return false, nil
	}
	var originalTrace trace.Trace[T, CP, SP, DP]
	originalTrace, s.originalTraces = s.originalTraces[0], s.originalTraces[1:]
	sts := &singleTraceStitcher[T, CP, SP, DP]{
		prefix:                    fmt.Sprintf("%d:", len(s.originalTraces)),
		s:                         s,
		originalToNewDependencies: map[trace.Dependency[T, CP, SP, DP]]trace.MutableDependency[T, CP, SP, DP]{},
		originalToNewSpans:        map[trace.Span[T, CP, SP, DP]]trace.MutableSpan[T, CP, SP, DP]{},
		originalToNewCategories:   map[trace.Category[T, CP, SP, DP]]trace.Category[T, CP, SP, DP]{},
	}
	for _, originalRootSpan := range originalTrace.RootSpans() {
		if err := sts.newSpanFromOriginal(originalRootSpan, nil); err != nil {
			return true, err
		}
	}
	// Copy over all original root categories.  This'll also recursively copy
	// over all descendant categories.
	for _, hierarchyType := range originalTrace.HierarchyTypes() {
		for _, originalRootCategory := range originalTrace.RootCategories(hierarchyType) {
			if _, err := sts.newCategoryFromOriginal(originalRootCategory, nil); err != nil {
				return true, err
			}
		}
	}
	s.newTrace.Simplify()
	return true, nil
}
