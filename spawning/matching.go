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

package spawning

import (
	"fmt"

	"github.com/google/tracey/trace"
)

type spanPatternPathElement struct {
	spanPattern *trace.SpanPattern
	directChild bool
}

// SpanPattern supports programmatically assembling spawning span
// patterns.
type SpanPattern struct {
	// A chain of path elements, each of which after the first is spawned
	// (directly or indirectly) by its predecessor.
	pathElements []*spanPatternPathElement
}

// DirectlySpawning specifies a trace.SpanPattern as a descendant, through
// exactly one spawn, of the previous pattern element.
func (spb *SpanPattern) DirectlySpawning(
	spanPattern *trace.SpanPattern,
) *SpanPattern {
	spb.pathElements = append(
		spb.pathElements,
		&spanPatternPathElement{
			spanPattern: spanPattern,
			directChild: true,
		},
	)
	return spb
}

// EventuallySpawning specifies a trace.SpanPattern as a descendant, through
// one or more spawns, of the previous pattern element.
func (spb *SpanPattern) EventuallySpawning(
	spanPattern *trace.SpanPattern,
) *SpanPattern {
	spb.pathElements = append(
		spb.pathElements,
		&spanPatternPathElement{
			spanPattern: spanPattern,
			directChild: false,
		},
	)
	return spb
}

// SpecifiesSpawning returns true iff the receiving pattern specifies spawning
// behavior.
func (spb *SpanPattern) SpecifiesSpawning() bool {
	return len(spb.pathElements) > 1
}

// RootPattern returns the receiver's first, or root, SpanPattern.  For
// SpanPatterns that do not specify any spawning, this is the entirety of the
// pattern.
func (spb *SpanPattern) RootPattern() *trace.SpanPattern {
	return spb.pathElements[0].spanPattern
}

// NewSpanPatternBuilder returns a new SpanPatternBuilder with the provided options, evaluated
// in order.  Each option after the first specifies either a direct spawning
// child (if SpanFinder()) or a descendant via spawning of the previous option.
func NewSpanPatternBuilder(
	rootSpanPattern *trace.SpanPattern,
) *SpanPattern {
	ret := &SpanPattern{
		pathElements: []*spanPatternPathElement{
			&spanPatternPathElement{
				spanPattern: rootSpanPattern,
				directChild: true,
			},
		},
	}
	return ret
}

// Returns a root span filter function that accepts spawn descendants, at any
// spawn-edge depth greater than zero, of the provided set of spans.
func (f *Forest[T, CP, SP, DP]) descendantSpanFilter(
	spans ...trace.Span[T, CP, SP, DP],
) trace.SpanFilter[T, CP, SP, DP] {
	spawnedChildren := map[trace.RootSpan[T, CP, SP, DP]]struct{}{}
	originalSpans := map[trace.Span[T, CP, SP, DP]]struct{}{}
	for _, s := range spans {
		originalSpans[s] = struct{}{}
		for _, spawnedChild := range f.SpawnedRootSpansBySpawner[s] {
			spawnedChildren[spawnedChild] = struct{}{}
		}
	}
	var check func(rs trace.RootSpan[T, CP, SP, DP]) (include, prune bool)
	check = func(rs trace.RootSpan[T, CP, SP, DP]) (include, prune bool) {
		// We made it to the root without observing a spawned child; prune this
		// branch.
		if rs == nil {
			return false, true
		}
		if _, ok := spawnedChildren[rs]; ok {
			// This is a directly spawned child; it's good.
			return true, false
		}
		if spawner, ok := f.SpawnersBySpawnedRootSpan[rs]; ok {
			// This isn't a directly spawned child, but it might be indirectly
			// spawned.
			return check(spawner.RootSpan())
		}
		// This span is off the spawning network, prune this branch.
		return false, true
	}
	return func(s trace.Span[T, CP, SP, DP]) (include, prune bool) {
		if s.ParentSpan() != nil {
			_, inOriginalSpans := originalSpans[s]
			return !inOriginalSpans, false
		}
		return check(s.RootSpan())
	}
}

type spanFinder[T any, CP, SP, DP fmt.Stringer] struct {
	sp             *SpanPattern
	t              trace.Trace[T, CP, SP, DP]
	namer          trace.Namer[T, CP, SP, DP]
	f              *Forest[T, CP, SP, DP]
	leafSpanFilter trace.SpanFilter[T, CP, SP, DP]
}

func (sf *spanFinder[T, CP, SP, DP]) Comparator() trace.Comparator[T] {
	return sf.t.Comparator()
}

func (sf *spanFinder[T, CP, SP, DP]) WithNamer(namer trace.Namer[T, CP, SP, DP]) trace.SpanFinder[T, CP, SP, DP] {
	sf.namer = namer
	return sf
}

func (sf *spanFinder[T, CP, SP, DP]) WithSpanFilter(
	spanFilter trace.SpanFilter[T, CP, SP, DP],
) trace.SpanFinder[T, CP, SP, DP] {
	sf.leafSpanFilter = spanFilter
	return sf
}

func (sf *spanFinder[T, CP, SP, DP]) FindSpans() []trace.Span[T, CP, SP, DP] {
	if len(sf.sp.pathElements) == 0 {
		return nil
	}
	// Start with all spans identified by the first path element (with the
	// external leaf root span filter applied, if it is non-nil).
	spans := trace.NewSpanFinder(
		sf.sp.pathElements[0].spanPattern,
		sf.t,
	).FindSpans()
	nonRootPathElements := sf.sp.pathElements[1:]
	// Then, for each path element after the first, traverse one or more spawn
	// edges from the last identified set of spans to a set of descendant
	// 'parent' root spans.  Then, match that path element's SpanFinder,
	// filtering down to the 'parent' root spans (and the external leaf root span
	// filter applied, if it is non-nil).
	for idx, pathElement := range nonRootPathElements {
		var internalSpanFilter trace.SpanFilter[T, CP, SP, DP]
		if pathElement.directChild {
			// Direct-child path elements are filtered in if their root spans are
			// spawned by one of the spans identified by the last path element.
			parentRootSpanMap := map[trace.RootSpan[T, CP, SP, DP]]struct{}{}
			for _, span := range spans {
				for _, spawnedChild := range sf.f.SpawnedRootSpansBySpawner[span] {
					parentRootSpanMap[spawnedChild] = struct{}{}
				}
			}
			internalSpanFilter = func(s trace.Span[T, CP, SP, DP]) (include, prune bool) {
				_, include = parentRootSpanMap[s.RootSpan()]
				return include, !include
			}
		} else {
			// For a descendant-child (direct or deeper) path element, the 'parent'
			// root spans are the spawn-descendants, at depth at least one, of the
			// previous set of spans.
			internalSpanFilter = sf.f.descendantSpanFilter(spans...)
		}
		spanFilter := internalSpanFilter
		if idx == len(nonRootPathElements)-1 {
			spanFilter = func(s trace.Span[T, CP, SP, DP],
			) (include, prune bool) {
				externalInclude, externalPrune := true, false
				if sf.leafSpanFilter != nil {
					externalInclude, externalPrune = sf.leafSpanFilter(s)
				}
				internalInclude, internalPrune := internalSpanFilter(s)
				return externalInclude && internalInclude, internalPrune && externalPrune
			}
		}
		spans = trace.NewSpanFinder(
			pathElement.spanPattern,
			sf.t,
		).WithSpanFilter(spanFilter).FindSpans()
	}
	// After the last path element, return the most recent set of spans.
	return spans
}

func (sf *spanFinder[T, CP, SP, DP]) FindCategories(...trace.FindCategoryOption) []trace.Category[T, CP, SP, DP] {
	return nil
}

// NewSpanFinder returns a new trace.SpanFinder applying the provided
// SpanFinder to
// the provided Trace using the provided Namer.
func NewSpanFinder[T any, CP, SP, DP fmt.Stringer](
	sp *SpanPattern,
	t trace.Trace[T, CP, SP, DP],
	f *Forest[T, CP, SP, DP],
) trace.SpanFinder[T, CP, SP, DP] {
	return &spanFinder[T, CP, SP, DP]{
		sp:    sp,
		t:     t,
		f:     f,
		namer: t.DefaultNamer(),
	}
}
