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
	"slices"
	"sort"
)

// Implements MutableTrace[T, CP, SP, DP].
type trace[T any, CP, SP, DP fmt.Stringer] struct {
	comparator                        Comparator[T]
	defaultNamer                      Namer[T, CP, SP, DP]
	hierarchyTypesInDeclarationOrder  []HierarchyType
	observedHierarchyTypes            map[HierarchyType]struct{}
	dependencyTypesInDeclarationOrder []DependencyType
	observedDependencyTypes           map[DependencyType]struct{}
	rootCategories                    map[HierarchyType][]Category[T, CP, SP, DP]
	rootSpans                         []RootSpan[T, CP, SP, DP]
}

func (t *trace[T, CP, SP, DP]) observedHierarchyType(ht HierarchyType) {
	if _, ok := t.observedHierarchyTypes[ht]; !ok {
		t.observedHierarchyTypes[ht] = struct{}{}
		t.hierarchyTypesInDeclarationOrder = append(t.hierarchyTypesInDeclarationOrder, ht)
	}
}

func (t *trace[T, CP, SP, DP]) observedDependencyType(dt DependencyType) {
	if _, ok := t.observedDependencyTypes[dt]; !ok {
		t.observedDependencyTypes[dt] = struct{}{}
		t.dependencyTypesInDeclarationOrder = append(t.dependencyTypesInDeclarationOrder, dt)
	}
}

// NewTrace returns a new Trace with the provided Comparator and set of
// HierarchyTypes.
func NewTrace[T any, CP, SP, DP fmt.Stringer](
	comparator Comparator[T],
	defaultNamer Namer[T, CP, SP, DP],
) Trace[T, CP, SP, DP] {
	return NewMutableTrace[T, CP, SP, DP](comparator, defaultNamer)
}

// NewMutableTrace returns a new MutableTrace with the provided Comparator and
// set of HierarchyTypes.
func NewMutableTrace[T any, CP, SP, DP fmt.Stringer](
	comparator Comparator[T],
	defaultNamer Namer[T, CP, SP, DP],
) MutableTrace[T, CP, SP, DP] {
	ret := &trace[T, CP, SP, DP]{
		defaultNamer:            defaultNamer,
		comparator:              comparator,
		rootCategories:          map[HierarchyType][]Category[T, CP, SP, DP]{},
		observedHierarchyTypes:  map[HierarchyType]struct{}{},
		observedDependencyTypes: map[DependencyType]struct{}{},
	}
	ret.observedDependencyType(Call)
	ret.observedDependencyType(Return)
	return ret
}

// Traces can serve as their own wrappers.
func (t *trace[T, CP, SP, DP]) Trace() Trace[T, CP, SP, DP] {
	return t
}

func (t *trace[T, CP, SP, DP]) Comparator() Comparator[T] {
	return t.comparator
}

func (t *trace[T, CP, SP, DP]) DefaultNamer() Namer[T, CP, SP, DP] {
	return t.defaultNamer
}

func (t *trace[T, CP, SP, DP]) HierarchyTypes() []HierarchyType {
	return t.hierarchyTypesInDeclarationOrder
}

func (t *trace[T, CP, SP, DP]) DependencyTypes() []DependencyType {
	return t.dependencyTypesInDeclarationOrder
}

func (t *trace[T, CP, SP, DP]) RootCategories(ht HierarchyType) []Category[T, CP, SP, DP] {
	roots, ok := t.rootCategories[ht]
	if !ok {
		return nil
	}
	return roots
}

func (t *trace[T, CP, SP, DP]) RootSpans() []RootSpan[T, CP, SP, DP] {
	return t.rootSpans
}

func (t *trace[T, CP, SP, DP]) NewRootCategory(ht HierarchyType, payload CP) Category[T, CP, SP, DP] {
	t.observedHierarchyType(ht)
	ret := &category[T, CP, SP, DP]{
		ht:      ht,
		payload: payload,
	}
	t.rootCategories[ht] = append(t.rootCategories[ht], ret)
	return ret
}

func (t *trace[T, CP, SP, DP]) NewRootSpan(start, end T, payload SP) RootSpan[T, CP, SP, DP] {
	ret := &rootSpan[T, CP, SP, DP]{
		commonSpan:                      newCommonSpan[T, CP, SP, DP](start, end, payload),
		parentCategoriesByHierarchyType: map[HierarchyType]Category[T, CP, SP, DP]{},
	}
	t.rootSpans = append(t.rootSpans, ret)
	ret.elementarySpans = append(ret.elementarySpans, makeInitialElementarySpan[T, CP, SP, DP](ret))
	return ret
}

func (t *trace[T, CP, SP, DP]) NewMutableRootSpan(elementarySpans []MutableElementarySpan[T, CP, SP, DP], payload SP) (MutableRootSpan[T, CP, SP, DP], error) {
	cs, err := newMutableCommonSpan[T, CP, SP, DP](elementarySpans, payload)
	if err != nil {
		return nil, err
	}
	ret := &rootSpan[T, CP, SP, DP]{
		commonSpan:                      cs,
		parentCategoriesByHierarchyType: map[HierarchyType]Category[T, CP, SP, DP]{},
	}
	for _, es := range elementarySpans {
		es.withParentSpan(ret)
	}
	t.rootSpans = append(t.rootSpans, ret)
	return ret, nil
}

type category[T any, CP, SP, DP fmt.Stringer] struct {
	ht              HierarchyType
	parent          Category[T, CP, SP, DP]
	payload         CP
	childCategories []Category[T, CP, SP, DP]
	rootSpans       []RootSpan[T, CP, SP, DP]
}

func (c *category[T, CP, SP, DP]) NewChildCategory(payload CP) Category[T, CP, SP, DP] {
	ret := &category[T, CP, SP, DP]{
		ht:      c.ht,
		parent:  c,
		payload: payload,
	}
	c.childCategories = append(c.childCategories, ret)
	return ret
}

func (c *category[T, CP, SP, DP]) AddRootSpan(rs RootSpan[T, CP, SP, DP]) error {
	if rs.ParentCategory(c.HierarchyType()) != nil {
		return fmt.Errorf("can't add root span to category: the span already has a parent category under that hierarchy")
	}
	c.rootSpans = append(c.rootSpans, rs)
	rs.(*rootSpan[T, CP, SP, DP]).setParentCategory(c)
	return nil
}

func (c *category[T, CP, SP, DP]) HierarchyType() HierarchyType {
	return c.ht
}

func (c *category[T, CP, SP, DP]) Payload() CP {
	return c.payload
}

func (c *category[T, CP, SP, DP]) Parent() Category[T, CP, SP, DP] {
	return c.parent
}

func (c *category[T, CP, SP, DP]) ChildCategories() []Category[T, CP, SP, DP] {
	return c.childCategories
}

func (c *category[T, CP, SP, DP]) RootSpans() []RootSpan[T, CP, SP, DP] {
	return c.rootSpans
}

func (c *category[T, CP, SP, DP]) UpdatePayload(payload CP) {
	c.payload = payload
}

// A common base type for rootSpan and nonRootSpan.
type commonSpan[T any, CP, SP, DP fmt.Stringer] struct {
	// This span's ElementarySpans, in increasing temporal order.  This type
	// maintains several invariants on this slice and its ElementarySpans:
	// *  Each ElementarySpan begins at or after the end of its predecessor;
	//    ElementarySpans may not overlap.
	// *  The start point of the first ElementarySpan, and the end point of the
	//    last one, are never modified.
	// *  Each ElementarySpan has at most one incoming Dependency, which occurs
	//    at the ElementarySpan's start, and at most one outgoing Dependency,
	//    which occurs at the ElementarySpan's end.
	// *  Each ElementarySpan is causally dependent on all of its predecessors,
	//    as well as its and its predecessors' incoming Dependencies.
	// *  Zero-duration ElementarySpans are permitted.
	// *  Gaps between adjacent ElementarySpans represent intervals of suspended
	//    execution.
	elementarySpans []ElementarySpan[T, CP, SP, DP]
	start, end      T
	payload         SP
	childSpans      []Span[T, CP, SP, DP]
}

// Simplifies the receiver by merging abutting elementary spans and suspend
// intervals where no dependencies intervene.
func (cs *commonSpan[T, CP, SP, DP]) simplifyElementarySpans(comparator Comparator[T]) {
	var lastES *elementarySpan[T, CP, SP, DP]
	newESs := make([]ElementarySpan[T, CP, SP, DP], 0, len(cs.elementarySpans))
	for idx := 0; idx < len(cs.elementarySpans); idx++ {
		thisES := cs.elementarySpanAt(idx)
		// Remove instantaneous interior elementary spans having no dependencies.
		if idx > 0 && idx < len(cs.elementarySpans)-1 &&
			comparator.Equal(thisES.Start(), thisES.End()) &&
			thisES.incoming == nil && thisES.outgoing == nil {
			cs.elementarySpans = slices.Delete(cs.elementarySpans, idx, idx)
			continue
		}
		// Merge abutting elementary spans with no intervening dependencies.
		if lastES != nil &&
			comparator.Equal(lastES.End(), thisES.Start()) &&
			lastES.outgoing == nil && thisES.incoming == nil {
			if thisES.outgoing != nil {
				thisES.outgoing.replaceOriginElementarySpan(thisES, lastES)
			}
			lastES.end = thisES.end
			cs.elementarySpans = slices.Delete(cs.elementarySpans, idx, idx)
			continue
		}
		if lastES != nil {
			thisES.predecessor = lastES
			lastES.successor = thisES
			newESs = append(newESs, lastES)
		}
		lastES = thisES
	}
	if lastES != nil {
		newESs = append(newESs, lastES)
	}
	cs.elementarySpans = newESs
}

// Returns the index in the receiver's slice of elementary spans of the
// first elementary span ending at or after the specified point, and a boolean
// indicating whether the elementary span at that index contains the specified
// point (inclusively at both ends).  If the specified point lies before the
// first elementary span, returns (0, false); if it lies after the last
// elementary span, returns (n, false), where n is the number of elementary
// spans.
func (cs *commonSpan[T, CP, SP, DP]) findFirstElementarySpanIndexEndingAtOrAfter(
	comparator Comparator[T],
	at T,
) (idx int, contains bool) {
	if len(cs.elementarySpans) == 0 ||
		comparator.Less(at, cs.elementarySpans[0].Start()) {
		// The requested point lies before the first span, or there are no
		// elementary spans.
		return 0, false
	}
	// Find the position of the first elementary span whose endpoint is not less
	// than the requested point.
	ret := sort.Search(len(cs.elementarySpans), func(x int) bool {
		return comparator.LessOrEqual(at, cs.elementarySpanAt(x).End())
	})
	if ret == len(cs.elementarySpans) {
		// The requested point lies after the span.
		return ret, false
	}
	if comparator.Greater(cs.elementarySpanAt(ret).Start(), at) {
		// The only elementary span that could contain the requested point does
		// not.  That is, the requested point lies in a gap between elementary
		// spans.
		return ret, false
	}
	return ret, true
}

// Returns the index in the receiver's slice of elementary spans of the
// last elementary span ending at or after the specified point, and a boolean
// indicating whether the elementary span at that index contains the specified
// point (inclusively at both ends).  If the specified point lies before the
// first elementary span, returns (0, false); if it lies after the last
// elementary span, returns (n, false), where n is the number of elementary
// spans.
func (cs *commonSpan[T, CP, SP, DP]) findLastElementarySpanIndexEndingAtOrAfter(
	comparator Comparator[T],
	at T,
) (idx int, contains bool) {
	// Find the position of the first elementary span whose endpoint is greater
	// than the requested point.
	ret := sort.Search(len(cs.elementarySpans), func(x int) bool {
		return comparator.Less(at, cs.elementarySpanAt(x).End())
	})
	if ret < len(cs.elementarySpans) &&
		comparator.GreaterOrEqual(at, cs.elementarySpanAt(ret).Start()) {
		// The requested point lies within the elementary span at ret.
		return ret, true
	}
	if ret == 0 {
		// The point lies before the first elementary span.
		return ret, false
	}
	// The requested point either lies in the elementary span before ret, or
	// between that span and the one at ret.
	prev := ret - 1
	if comparator.Greater(at, cs.elementarySpanAt(prev).End()) {
		// The requested point lies before the elementary span at ret, but after
		// the one before ret.
		return ret, false
	}
	// The elementary span at prev is the last ending at or after the requested
	// point.
	return prev, true
}

func assembleOptions[T ~uint64](def T, options ...T) T {
	for _, option := range options {
		def = def | option
	}
	return def
}

func (cs *commonSpan[T, CP, SP, DP]) suspendWithinOneElementarySpan(
	comparator Comparator[T],
	start, end T,
	opts SuspendOption,
) error {
	// If the specified suspend is zero-width, there's nothing to do.
	if comparator.Equal(start, end) {
		return nil
	}
	suspendSpanIdx, contains := cs.findFirstElementarySpanIndexEndingAtOrAfter(comparator, end)
	if !contains {
		return fmt.Errorf("no elementary span at %v", end)
	}
	suspendSpan := cs.elementarySpanAt(suspendSpanIdx)
	if !suspendSpan.contains(comparator, start) {
		return fmt.Errorf("requested suspend crosses elementary span boundaries")
	}
	preSuspendSpanIdx, postSuspendSpanIdx, ok, err := cs.fissionElementarySpanAt(comparator, end, fissionEarliest)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("failed to fission an elementary span that should have existed")
	}
	preSuspendSpan := cs.elementarySpanAt(preSuspendSpanIdx)
	postSuspendSpan := cs.elementarySpanAt(postSuspendSpanIdx)
	preSuspendSpan.end = start
	preSuspendOrphanedMarkStartIndex := sort.Search(len(preSuspendSpan.marks), func(idx int) bool {
		return comparator.Greater(preSuspendSpan.marks[idx].Moment(), start)
	})
	if preSuspendOrphanedMarkStartIndex < len(preSuspendSpan.marks) {
		if opts&SuspendFissionsAroundMarks == 0 {
			return fmt.Errorf("failed to suspend: at least one mark lies within suspend interval")
		}
		for markIdx := preSuspendOrphanedMarkStartIndex; markIdx < len(preSuspendSpan.marks); markIdx++ {
			mark := preSuspendSpan.marks[markIdx]
			markedSpanIdx, err := cs.createInstantaneousElementarySpanWithinSuspendAt(comparator, mark.Moment())
			if err != nil {
				return err
			}
			markedSpan := cs.elementarySpanAt(markedSpanIdx)
			markedSpan.marks = []MutableMark[T]{mark}
		}
		preSuspendSpan.marks = preSuspendSpan.marks[:preSuspendOrphanedMarkStartIndex]
	}
	postSuspendSpan.start = end
	postSuspendOrphanedMarkEndIndex := sort.Search(len(postSuspendSpan.marks), func(idx int) bool {
		return comparator.GreaterOrEqual(end, postSuspendSpan.marks[idx].Moment())
	})
	if postSuspendOrphanedMarkEndIndex < len(postSuspendSpan.marks) {
		if opts&SuspendFissionsAroundMarks == 0 {
			return fmt.Errorf("failed to suspend: at least one mark lies within suspend interval")
		}
		for markIdx := 0; markIdx <= postSuspendOrphanedMarkEndIndex; markIdx++ {
			mark := postSuspendSpan.marks[markIdx]
			markedSpanIdx, err := cs.createInstantaneousElementarySpanWithinSuspendAt(comparator, mark.Moment())
			if err != nil {
				return err
			}
			markedSpan := cs.elementarySpanAt(markedSpanIdx)
			markedSpan.marks = []MutableMark[T]{mark}
		}
		postSuspendSpan.marks = postSuspendSpan.marks[postSuspendOrphanedMarkEndIndex:]
	}
	return nil
}

func (cs *commonSpan[T, CP, SP, DP]) Mark(
	comparator Comparator[T],
	label string,
	moment T,
	options ...MarkOption,
) error {
	opts := assembleOptions(DefaultMarkOptions, options...)
	markedSpanIdx, _ := cs.findFirstElementarySpanIndexEndingAtOrAfter(comparator, moment)
	if markedSpanIdx == len(cs.elementarySpans) {
		return fmt.Errorf("cannot add mark at %v: moment lies after the span", moment)
	}
	markedSpan := cs.elementarySpanAt(markedSpanIdx)
	if !markedSpan.contains(comparator, moment) {
		if opts&MarkCanFissionSuspend == 0 {
			return fmt.Errorf("cannot add mark at %v: no elementary span at moment", moment)
		}
		var err error
		markedSpanIdx, err = cs.createInstantaneousElementarySpanWithinSuspendAt(comparator, moment)
		if err != nil {
			return err
		}
		markedSpan = cs.elementarySpanAt(markedSpanIdx)
	}
	insertPoint := sort.Search(len(markedSpan.marks), func(idx int) bool {
		return comparator.Diff(moment, markedSpan.marks[idx].Moment()) <= 0
	})
	var m MutableMark[T] = &mark[T]{
		label:  label,
		moment: moment,
	}
	markedSpan.marks = slices.Insert(markedSpan.marks, insertPoint, m)
	return nil
}

func (cs *commonSpan[T, CP, SP, DP]) Suspend(
	comparator Comparator[T],
	start, end T,
	options ...SuspendOption,
) error {
	opts := assembleOptions(DefaultSuspendOptions, options...)
	if opts&SuspendFissionsAroundElementarySpanEndpoints == SuspendFissionsAroundElementarySpanEndpoints {
		// Suspends were requested to fission around elementary span endpoints.
		// Work `end` backwards from the original endpoint until it is earlier than
		// `start`, and at each iteration:
		//   * find the elementary span E containing `end`;
		//   * suspend E from its start to `end` (which fissions E);
		//   * If (before the fission) E was not the first elementary span and it
		//     had the predecessor P, set `end` to P.End()
		//   * Otherwise, there's no more to be done.
		for comparator.Greater(end, start) {
			endSpanIdx, contains := cs.findFirstElementarySpanIndexEndingAtOrAfter(comparator, end)
			if !contains {
				return fmt.Errorf("no elementary span at %v", end)
			}
			endSpan := cs.elementarySpanAt(endSpanIdx)
			chunkStart := endSpan.Start()
			if comparator.Greater(start, chunkStart) {
				chunkStart = start
			}
			if err := cs.suspendWithinOneElementarySpan(comparator, chunkStart, end, opts); err != nil {
				return err
			}
			if endSpanIdx > 0 {
				previousSpan := cs.elementarySpanAt(endSpanIdx - 1)
				end = previousSpan.End()
			} else {
				break
			}
		}
		return nil
	}
	// Suspends should not fission around elementary span endpoints.  Expect to
	// suspend within a single elementary span, and fail if that isn't possible.
	return cs.suspendWithinOneElementarySpan(comparator, start, end, opts)
}

// Returns the *elementarySpan in the receiver at the specified index.  The
// caller is responsible for ensuring that this is a valid index.
func (cs *commonSpan[T, CP, SP, DP]) elementarySpanAt(idx int) *elementarySpan[T, CP, SP, DP] {
	return cs.elementarySpans[idx].(*elementarySpan[T, CP, SP, DP])
}

type fissionPolicy int

const (
	fissionEarliest fissionPolicy = iota
	fissionLatest
)

// Fissions the receiver's elementary span at the specified point, returning
// the indexes of the elementary spans before and after that point after the
// fission, and true on succcs.  If there is no elementary span at the
// specified point, returns false.
// Fissioning an elementary span at one of its ends will result in a zero-width
// elementary span (at beforeIdx when fissioning at the start, and afterIdx
// when fissioning at the end).  When such zero-width spans exist, a point may
// lie 'within' several spans: suppose elementary spans A, B, and C, of which A
// and B are zero-width and both start (and end) at T1, whereas C starts at T1
// and ends at T2; T1 then lies 'within' spans A, B, and C.  In such case, the
// specified fission policy determines whether the first or the last span
// containing the requested point is the one fissioned.
// If fissioning is successful, any incoming dependency the original elementary
// span had will lead into the elementary span at beforeIdx, and any outgoing
// dependency the original elementary span had will lead from the elementary
// span at afterIdx.
func (cs *commonSpan[T, CP, SP, DP]) fissionElementarySpanAt(
	comparator Comparator[T],
	at T,
	policy fissionPolicy,
) (beforeIdx, afterIdx int, fissioned bool, err error) {
	var esIdx int
	var contains bool
	switch policy {
	case fissionEarliest:
		esIdx, contains = cs.findFirstElementarySpanIndexEndingAtOrAfter(comparator, at)
	case fissionLatest:
		esIdx, contains = cs.findLastElementarySpanIndexEndingAtOrAfter(comparator, at)
	default:
		return 0, 0, false, fmt.Errorf("unknown fission policy %d", policy)
	}
	if !contains {
		return 0, 0, false, nil
	}
	// The span to fission.
	originalES := cs.elementarySpanAt(esIdx)
	// Fission the original span by moving its end point forward to the fission
	// point, and creating a new span from the fission point to the original
	// span's endpoint.
	markSplitIndex := sort.Search(len(originalES.marks), func(idx int) bool {
		return comparator.GreaterOrEqual(originalES.marks[idx].Moment(), at)
	})
	newES := &elementarySpan[T, CP, SP, DP]{
		span:        originalES.Span(),
		start:       at,
		end:         originalES.End(),
		predecessor: originalES,
		successor:   originalES.successor,
		outgoing:    nil,
		marks:       originalES.marks[markSplitIndex:],
	}
	originalES.marks = originalES.marks[:markSplitIndex]
	originalES.successor = newES
	if newES.successor != nil {
		newES.successor.(*elementarySpan[T, CP, SP, DP]).predecessor = newES
	}
	originalES.end = at
	if originalES.outgoing != nil {
		originalES.outgoing.replaceOriginElementarySpan(originalES, newES)
	}
	// Insert the new span into this cs.
	cs.elementarySpans = slices.Insert(cs.elementarySpans, esIdx+1, ElementarySpan[T, CP, SP, DP](newES))
	return esIdx, esIdx + 1, true, nil
}

func (cs *commonSpan[T, CP, SP, DP]) createInstantaneousElementarySpanWithinSuspendAt(
	comparator Comparator[T],
	at T,
) (idx int, err error) {
	spanIdx, contains := cs.findFirstElementarySpanIndexEndingAtOrAfter(comparator, at)
	if contains {
		return 0, fmt.Errorf("can't create instantaneous elementary span: another elementary span would overlap it")
	}
	if spanIdx == 0 || spanIdx == len(cs.elementarySpans) {
		return 0, fmt.Errorf("can't create instantaneous elementary span at %v: it would lie outside its span (%v-%v)",
			at, cs.Start(), cs.End(),
		)
	}
	nextES := cs.elementarySpanAt(spanIdx)
	if comparator.Less(nextES.Start(), at) {
		return 0, fmt.Errorf("can't create instantaneous elementary span at %v: span is not suspended then", at)
	}
	prevES := cs.elementarySpanAt(spanIdx - 1)
	newES := &elementarySpan[T, CP, SP, DP]{
		span:        nextES.Span(),
		start:       at,
		end:         at,
		predecessor: prevES,
		successor:   nextES,
	}
	nextES.predecessor = newES
	if prevES != nil {
		prevES.successor = newES
	}
	cs.elementarySpans = slices.Insert(cs.elementarySpans, spanIdx, ElementarySpan[T, CP, SP, DP](newES))
	return spanIdx, nil
}

func checkDependencyEndpointOptions(opts DependencyEndpointOption) error {
	if opts&(PlaceDependencyEndpointAsEarlyAsPossible|PlaceDependencyEndpointAsLateAsPossible) ==
		(PlaceDependencyEndpointAsEarlyAsPossible | PlaceDependencyEndpointAsLateAsPossible) {
		return fmt.Errorf("invalid options: cannot place a dependency edge both as early and as late as possible")
	}
	return nil
}

// Adds the provided outgoing dependency in the receiver at the specified
// point.  If an existing elementary span ends at the specified time and has
// no outgoing dependences, the provided dependency will be added to that one,
// otherwise a new elementary span will be created (which may be zero-width.)
// By default, outgoing dependencies are placed in the last elementary span at
// the specified point.
func (cs *commonSpan[T, CP, SP, DP]) addOutgoingDependency(
	comparator Comparator[T],
	dep *dependency[T, CP, SP, DP],
	at T,
	options ...DependencyEndpointOption,
) error {
	opts := assembleOptions(DefaultDependencyEndpointOptions, options...)
	if err := checkDependencyEndpointOptions(opts); err != nil {
		return nil
	}
	spanIdx, contains := cs.findFirstElementarySpanIndexEndingAtOrAfter(comparator, at)
	if !contains {
		if opts&DependencyEndpointCanFissionSuspend != DependencyEndpointCanFissionSuspend {
			return fmt.Errorf("no elementary span at %v to which to add an outgoing dependency", at)
		}
		var err error
		spanIdx, err = cs.createInstantaneousElementarySpanWithinSuspendAt(comparator, at)
		if err != nil {
			return err
		}
	}
	es := cs.elementarySpanAt(spanIdx)
	if !comparator.Equal(es.End(), at) || es.Outgoing() != nil {
		fissionPolicy := fissionLatest
		if opts&PlaceDependencyEndpointAsEarlyAsPossible == PlaceDependencyEndpointAsEarlyAsPossible {
			fissionPolicy = fissionEarliest
		}
		spanIdx, _, ok, err := cs.fissionElementarySpanAt(comparator, at, fissionPolicy)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("failed to fission an elementary span that should have existed")
		}
		es = cs.elementarySpans[spanIdx].(*elementarySpan[T, CP, SP, DP])
	}
	if dep.setOrigin(comparator, es) {
		return fmt.Errorf("adding an outgoing dependency invalidated a previous origin for that dependency")
	}
	return nil
}

// Adds the provided incoming dependency in the receiver at the specified
// point.  If an existing elementary span starts at the specified time and has
// no incoming dependences, the provided dependency will be added to that one,
// otherwise a new elementary span will be created (which may be zero-width.)
// By default, incoming dependencies are placed in the first elementary span at
// the specified point.
func (cs *commonSpan[T, CP, SP, DP]) addIncomingDependency(
	comparator Comparator[T],
	dep *dependency[T, CP, SP, DP],
	at T,
	options ...DependencyEndpointOption,
) error {
	opts := assembleOptions(DefaultDependencyEndpointOptions, options...)
	if err := checkDependencyEndpointOptions(opts); err != nil {
		return nil
	}
	spanIdx, contains := cs.findFirstElementarySpanIndexEndingAtOrAfter(comparator, at)
	if !contains {
		if opts&DependencyEndpointCanFissionSuspend != DependencyEndpointCanFissionSuspend {
			return fmt.Errorf("no elementary span at %v to which to add an incoming dependency", at)
		}
		var err error
		spanIdx, err = cs.createInstantaneousElementarySpanWithinSuspendAt(comparator, at)
		if err != nil {
			return err
		}
	}
	span := cs.elementarySpanAt(spanIdx)
	if !comparator.Equal(span.Start(), at) || span.Incoming() != nil {
		fissionPolicy := fissionEarliest
		if opts&PlaceDependencyEndpointAsLateAsPossible == PlaceDependencyEndpointAsLateAsPossible {
			fissionPolicy = fissionLatest
		}
		_, spanIdx, ok, err := cs.fissionElementarySpanAt(comparator, at, fissionPolicy)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("failed to fission an elementary span that should have existed")
		}
		span = cs.elementarySpanAt(spanIdx)
	}
	span.incoming = dep
	dep.destinations = append(dep.destinations, span)
	return nil
}

// Adds the provided incoming dependency in the receiver at the specified
// dependency point, preceding it with a wait from the specified wait point.
func (cs *commonSpan[T, CP, SP, DP]) addIncomingDependencyWithWait(
	comparator Comparator[T],
	dep *dependency[T, CP, SP, DP],
	waitFrom, dependencyAt T,
	options ...DependencyEndpointOption,
) error {
	if err := cs.Suspend(comparator, waitFrom, dependencyAt); err != nil {
		return err
	}
	return cs.addIncomingDependency(comparator, dep, dependencyAt, options...)
}

func (cs *commonSpan[T, CP, SP, DP]) simplify(comparator Comparator[T]) {
	cs.simplifyElementarySpans(comparator)
	for _, child := range cs.childSpans {
		child.simplify(comparator)
	}
}

func newCommonSpan[T any, CP, SP, DP fmt.Stringer](start, end T, payload SP) *commonSpan[T, CP, SP, DP] {
	return &commonSpan[T, CP, SP, DP]{
		start:   start,
		end:     end,
		payload: payload,
	}
}

func newMutableCommonSpan[T any, CP, SP, DP fmt.Stringer](elementarySpans []MutableElementarySpan[T, CP, SP, DP], payload SP) (*commonSpan[T, CP, SP, DP], error) {
	if len(elementarySpans) == 0 {
		return nil, fmt.Errorf("at least one elementary span must be provided to newMutableCommonSpan")
	}
	ess := make([]ElementarySpan[T, CP, SP, DP], len(elementarySpans))
	var lastES *elementarySpan[T, CP, SP, DP]
	for idx, es := range elementarySpans {
		thisES := es.(*elementarySpan[T, CP, SP, DP])
		if lastES != nil {
			lastES.successor, thisES.predecessor = thisES, lastES
		}
		lastES = thisES
		ess[idx] = es
	}
	return &commonSpan[T, CP, SP, DP]{
		elementarySpans: ess,
		start:           elementarySpans[0].Start(),
		end:             elementarySpans[len(elementarySpans)-1].End(),
		payload:         payload,
	}, nil
}

func (cs *commonSpan[T, CP, SP, DP]) Payload() SP {
	return cs.payload
}

func (cs *commonSpan[T, CP, SP, DP]) UpdatePayload(payload SP) {
	cs.payload = payload
}

func (cs *commonSpan[T, CP, SP, DP]) Start() T {
	return cs.start
}

func (cs *commonSpan[T, CP, SP, DP]) End() T {
	return cs.end
}

func (cs *commonSpan[T, CP, SP, DP]) ChildSpans() []Span[T, CP, SP, DP] {
	return cs.childSpans
}

func (cs *commonSpan[T, CP, SP, DP]) ElementarySpans() []ElementarySpan[T, CP, SP, DP] {
	return cs.elementarySpans
}

// Returns a new child span under the receiver with the provided start and end
// points and payload.  The receiver is suspended between start and end, and
// Call and Return dependencies are set up between the receiver and the child
// span.
func (cs *commonSpan[T, CP, SP, DP]) newChildSpan(
	comparator Comparator[T],
	start, end T,
	payload SP,
) (*nonRootSpan[T, CP, SP, DP], error) {
	parent := cs
	child := &nonRootSpan[T, CP, SP, DP]{
		commonSpan: newCommonSpan[T, CP, SP, DP](start, end, payload),
	}
	child.elementarySpans = append(child.elementarySpans, makeInitialElementarySpan[T, CP, SP, DP](child))
	call := &dependency[T, CP, SP, DP]{
		dependencyType: Call,
		options:        DefaultDependencyOptions,
	}
	if err := parent.addOutgoingDependency(comparator, call, start); err != nil {
		return nil, err
	}
	if err := child.addIncomingDependency(comparator, call, start); err != nil {
		return nil, err
	}
	ret := &dependency[T, CP, SP, DP]{
		dependencyType: Return,
		options:        DefaultDependencyOptions,
	}
	if err := child.addOutgoingDependency(comparator, ret, end); err != nil {
		return nil, err
	}
	// Generally, if multiple dependencies impinge upon a span at the same
	// moment, all incoming dependencies should be resolved before all outgoing
	// dependencies.  However, in this case, we know that the incoming return
	// dependency in the parent is causally dependent on the outgoing call in the
	// parent, and so we force that incoming return to be placed last.
	if err := parent.addIncomingDependencyWithWait(comparator, ret, start, end, PlaceDependencyEndpointAsLateAsPossible); err != nil {
		return nil, err
	}
	return child, nil
}

// Implements MutableSpan[T, CP, SP, DP]
type nonRootSpan[T any, CP, SP, DP fmt.Stringer] struct {
	*commonSpan[T, CP, SP, DP]
	parent Span[T, CP, SP, DP]
}

func (nrs *nonRootSpan[T, CP, SP, DP]) ParentSpan() Span[T, CP, SP, DP] {
	return nrs.parent
}

func (nrs *nonRootSpan[T, CP, SP, DP]) RootSpan() RootSpan[T, CP, SP, DP] {
	return nrs.parent.RootSpan()
}

func (nrs *nonRootSpan[T, CP, SP, DP]) NewChildSpan(
	comparator Comparator[T],
	start, end T, payload SP,
) (Span[T, CP, SP, DP], error) {
	child, err := nrs.newChildSpan(comparator, start, end, payload)
	if err != nil {
		return nil, err
	}
	nrs.childSpans = append(nrs.childSpans, child)
	child.parent = nrs
	return child, nil
}

func (nrs *nonRootSpan[T, CP, SP, DP]) NewMutableChildSpan(elementarySpans []MutableElementarySpan[T, CP, SP, DP], payload SP) (MutableSpan[T, CP, SP, DP], error) {
	cs, err := newMutableCommonSpan(elementarySpans, payload)
	if err != nil {
		return nil, err
	}
	ret := &nonRootSpan[T, CP, SP, DP]{
		commonSpan: cs,
	}
	for _, es := range elementarySpans {
		es.withParentSpan(ret)
	}
	nrs.childSpans = append(nrs.childSpans, ret)
	ret.parent = nrs
	return ret, nil
}

// Implements MutableRootSpan[T, CP, SP, DP]
type rootSpan[T any, CP, SP, DP fmt.Stringer] struct {
	*commonSpan[T, CP, SP, DP]
	parentCategoriesByHierarchyType map[HierarchyType]Category[T, CP, SP, DP]
}

func (rs *rootSpan[T, CP, SP, DP]) ParentSpan() Span[T, CP, SP, DP] {
	return nil
}

func (rs *rootSpan[T, CP, SP, DP]) RootSpan() RootSpan[T, CP, SP, DP] {
	return rs
}

func (rs *rootSpan[T, CP, SP, DP]) ParentCategory(ht HierarchyType) Category[T, CP, SP, DP] {
	return rs.parentCategoriesByHierarchyType[ht]
}

func (rs *rootSpan[T, CP, SP, DP]) NewChildSpan(
	comparator Comparator[T],
	start, end T,
	payload SP,
) (Span[T, CP, SP, DP], error) {
	child, err := rs.newChildSpan(comparator, start, end, payload)
	if err != nil {
		return nil, err
	}
	rs.childSpans = append(rs.childSpans, child)
	child.parent = rs
	return child, nil
}

func (rs *rootSpan[T, CP, SP, DP]) NewMutableChildSpan(elementarySpans []MutableElementarySpan[T, CP, SP, DP], payload SP) (MutableSpan[T, CP, SP, DP], error) {
	cs, err := newMutableCommonSpan(elementarySpans, payload)
	if err != nil {
		return nil, err
	}
	ret := &nonRootSpan[T, CP, SP, DP]{
		commonSpan: cs,
	}
	for _, es := range elementarySpans {
		es.withParentSpan(ret)
	}
	rs.childSpans = append(rs.childSpans, ret)
	ret.parent = rs
	return ret, nil
}

func (rs *rootSpan[T, CP, SP, DP]) setParentCategory(parentCategory Category[T, CP, SP, DP]) error {
	if _, hasThis := rs.parentCategoriesByHierarchyType[parentCategory.HierarchyType()]; hasThis {
		return fmt.Errorf("a root span may only have one parent category for each hierarchy type")
	}
	rs.parentCategoriesByHierarchyType[parentCategory.HierarchyType()] = parentCategory
	return nil
}

func (rs *rootSpan[T, CP, SP, DP]) setParentSpan(child MutableSpan[T, CP, SP, DP]) {
	panic("cannot set parent span of a root span")
}

// Implements MutableDependency[T, CP, SP, DP]
type dependency[T any, CP, SP, DP fmt.Stringer] struct {
	dependencyType DependencyType
	payload        DP
	options        DependencyOption
	origins        []ElementarySpan[T, CP, SP, DP] // Non-empty for a complete dependency.
	destinations   []ElementarySpan[T, CP, SP, DP] // Non-empty for a complete dependency.
}

func (t *trace[T, CP, SP, DP]) NewDependency(
	dependencyType DependencyType,
	payload DP,
	dependencyOptions ...DependencyOption,
) Dependency[T, CP, SP, DP] {
	options := DefaultDependencyOptions
	for _, do := range dependencyOptions {
		options |= do
	}
	t.observedDependencyType(dependencyType)
	return &dependency[T, CP, SP, DP]{
		dependencyType: dependencyType,
		payload:        payload,
		options:        options,
	}
}

func (t *trace[T, CP, SP, DP]) NewMutableDependency(
	dependencyType DependencyType,
	dependencyOptions ...DependencyOption,
) MutableDependency[T, CP, SP, DP] {
	options := DefaultDependencyOptions
	for _, do := range dependencyOptions {
		options |= do
	}
	t.observedDependencyType(dependencyType)
	return &dependency[T, CP, SP, DP]{
		dependencyType: dependencyType,
		options:        options,
	}
}

func (t *trace[T, CP, SP, DP]) Simplify() {
	for _, rootSpan := range t.rootSpans {
		rootSpan.simplify(t.comparator)
	}
}

func (d *dependency[T, CP, SP, DP]) DependencyType() DependencyType {
	return d.dependencyType
}

func (d *dependency[T, CP, SP, DP]) TriggeringOrigin() ElementarySpan[T, CP, SP, DP] {
	if len(d.origins) == 0 {
		return nil
	}
	return d.origins[0]
}

func (d *dependency[T, CP, SP, DP]) Origins() []ElementarySpan[T, CP, SP, DP] {
	return d.origins
}

func (d *dependency[T, CP, SP, DP]) Options() DependencyOption {
	return d.options
}

func (d *dependency[T, CP, SP, DP]) Destinations() []ElementarySpan[T, CP, SP, DP] {
	return d.destinations
}

func (d *dependency[T, CP, SP, DP]) Payload() DP {
	return d.payload
}

func commonSpanFrom[T any, CP, SP, DP fmt.Stringer](spanIf Span[T, CP, SP, DP]) *commonSpan[T, CP, SP, DP] {
	switch v := spanIf.(type) {
	case *rootSpan[T, CP, SP, DP]:
		return v.commonSpan
	case *nonRootSpan[T, CP, SP, DP]:
		return v.commonSpan
	default:
		panic("unsupported span type")
	}
}

func (d *dependency[T, CP, SP, DP]) SetOriginSpan(
	comparator Comparator[T],
	from Span[T, CP, SP, DP], start T,
	options ...DependencyEndpointOption,
) error {
	if err := commonSpanFrom(from).
		addOutgoingDependency(comparator, d, start, options...); err != nil {
		return fmt.Errorf("failed to set dependency origin span at %v: %w",
			start, err)
	}
	return nil
}

func (d *dependency[T, CP, SP, DP]) AddDestinationSpan(
	comparator Comparator[T],
	to Span[T, CP, SP, DP], end T,
	options ...DependencyEndpointOption,
) error {
	if err := commonSpanFrom(to).
		addIncomingDependency(comparator, d, end, options...); err != nil {
		return fmt.Errorf("failed to add dependency destination span at %v: %w",
			end, err)
	}
	return nil
}

func (d *dependency[T, CP, SP, DP]) AddDestinationSpanAfterWait(
	comparator Comparator[T],
	to Span[T, CP, SP, DP],
	waitFrom, end T,
	options ...DependencyEndpointOption,
) error {
	if err := commonSpanFrom(to).
		addIncomingDependencyWithWait(
			comparator,
			d,
			waitFrom,
			end,
			options...,
		); err != nil {
		return fmt.Errorf("failed to set dependency destination span at %v, waiting from %v: %w",
			end, waitFrom, err)
	}
	return nil
}

func (d *dependency[T, CP, SP, DP]) WithOriginElementarySpan(comparator Comparator[T], es MutableElementarySpan[T, CP, SP, DP]) MutableDependency[T, CP, SP, DP] {
	// Ignore the return value; WithOriginElementarySpan explicitly has replace semantics.
	d.setOrigin(comparator, es)
	return d
}

func (d *dependency[T, CP, SP, DP]) WithPayload(payload DP) MutableDependency[T, CP, SP, DP] {
	d.payload = payload
	return d
}

func (d *dependency[T, CP, SP, DP]) SetOriginElementarySpan(
	comparator Comparator[T],
	es MutableElementarySpan[T, CP, SP, DP],
) error {
	if d.options.Includes(MultipleOriginsWithAndSemantics) && d.options.Includes(MultipleOriginsWithOrSemantics) {
		return fmt.Errorf("cannot set dependency origin: dependency has both AND and OR semantics")
	}
	if len(d.origins) > 0 && !d.options.Includes(multipleOrigins) {
		return fmt.Errorf("cannot set dependency origin: it already has an origin, and it does not support multiple origins")
	}
	if d.setOrigin(comparator, es) {
		return fmt.Errorf("adding an outgoing dependency invalidated a previous origin for that dependency")
	}
	return nil
}

func (d *dependency[T, CP, SP, DP]) WithDestinationElementarySpan(es MutableElementarySpan[T, CP, SP, DP]) MutableDependency[T, CP, SP, DP] {
	es.(*elementarySpan[T, CP, SP, DP]).incoming = d
	d.destinations = append(d.destinations, es)
	return d
}

func (d *dependency[T, CP, SP, DP]) replaceOriginElementarySpan(original, new MutableElementarySpan[T, CP, SP, DP]) {
	for idx, origin := range d.origins {
		if origin == original {
			d.origins[idx] = new
			original.(*elementarySpan[T, CP, SP, DP]).outgoing = nil
			new.(*elementarySpan[T, CP, SP, DP]).outgoing = d
			break
		}
	}
}

// setOrigin sets the provided ElementarySpan as an origin of the receiver,
// returning a boolean indicating whether any preexisting origins were removed,
// and their connection to the receiver deleted, as a result of this operation.
//
// If the receiver can support multiple origins, then the provided origin is
// added to the receiver's set of origins, updating its TriggeringOrigin if
// it is earlier (OR semantics) or later (AND semantics) than the existing
// TriggeringOrigin.  In this case, this method always returns false.
//
// If the receiver cannot support multiple origins, then the provided origin
// replaces the previous origin, if there is one.  If this occurs, the
// previous origin's Outgoing dependency is nulled out, and this method returns
// true.
func (d *dependency[T, CP, SP, DP]) setOrigin(
	comparator Comparator[T],
	es ElementarySpan[T, CP, SP, DP],
) (existingOriginRemoved bool) {
	es.(*elementarySpan[T, CP, SP, DP]).outgoing = d
	if d.options.Includes(MultipleOriginsWithAndSemantics) {
		if len(d.origins) > 0 && comparator.Greater(es.End(), d.origins[0].End()) {
			es, d.origins[0] = d.origins[0], es
		}
		d.origins = append(d.origins, es)
	} else if d.options.Includes(MultipleOriginsWithOrSemantics) {
		if len(d.origins) > 0 && comparator.Less(es.End(), d.origins[0].End()) {
			es, d.origins[0] = d.origins[0], es
		}
		d.origins = append(d.origins, es)
	} else {
		if len(d.origins) > 0 {
			d.origins[0].(*elementarySpan[T, CP, SP, DP]).outgoing = nil
			existingOriginRemoved = true
		}
		d.origins = []ElementarySpan[T, CP, SP, DP]{es}
	}
	return existingOriginRemoved
}

// Implements MutableMark[T]
type mark[T any] struct {
	label  string
	moment T
}

// NewMutableMark returns a new, empty MutableMark.
func NewMutableMark[T any]() MutableMark[T] {
	return &mark[T]{}
}

func (m *mark[T]) Label() string {
	return m.label
}

func (m *mark[T]) Moment() T {
	return m.moment
}

func (m *mark[T]) WithLabel(label string) MutableMark[T] {
	m.label = label
	return m
}

func (m *mark[T]) WithMoment(moment T) MutableMark[T] {
	m.moment = moment
	return m
}

// Implements MutableElementarySpan[T, CP, SP, DP]
type elementarySpan[T any, CP, SP, DP fmt.Stringer] struct {
	span                   Span[T, CP, SP, DP]
	marks                  []MutableMark[T]
	start, end             T
	predecessor, successor ElementarySpan[T, CP, SP, DP]
	// The dependency, if any, at the start of this elementary span.
	// If non-nil, this elementary span will be among Incoming.Destinations.
	incoming MutableDependency[T, CP, SP, DP] // nil if none.
	// The dependency, if any, at the end of this elementary span.
	// If non-nil, this elementary span will be Outgoing.Origin.
	outgoing MutableDependency[T, CP, SP, DP] // nil if none.
}

// Returns true if the receiver contains the provided point, inclusive at both
// ends.
func (es *elementarySpan[T, CP, SP, DP]) contains(comparator Comparator[T], at T) bool {
	return comparator.GreaterOrEqual(at, es.start) && comparator.LessOrEqual(at, es.end)
}

// Returns true if the receiver ends after the provided point
func (es *elementarySpan[T, CP, SP, DP]) endsAfter(comparator Comparator[T], at T) bool {
	return comparator.Less(at, es.end)
}

func (es *elementarySpan[T, CP, SP, DP]) Span() Span[T, CP, SP, DP] {
	return es.span
}

func (es *elementarySpan[T, CP, SP, DP]) Marks() []Mark[T] {
	ret := make([]Mark[T], len(es.marks))
	for idx, m := range es.marks {
		ret[idx] = m
	}
	return ret
}

func (es *elementarySpan[T, CP, SP, DP]) Start() T {
	return es.start
}

func (es *elementarySpan[T, CP, SP, DP]) End() T {
	return es.end
}

func (es *elementarySpan[T, CP, SP, DP]) Predecessor() ElementarySpan[T, CP, SP, DP] {
	return es.predecessor
}

func (es *elementarySpan[T, CP, SP, DP]) Successor() ElementarySpan[T, CP, SP, DP] {
	return es.successor
}

func (es *elementarySpan[T, CP, SP, DP]) Incoming() Dependency[T, CP, SP, DP] {
	if es.incoming == nil {
		return nil
	}
	return es.incoming
}

func (es *elementarySpan[T, CP, SP, DP]) Outgoing() Dependency[T, CP, SP, DP] {
	if es.outgoing == nil {
		return nil
	}
	return es.outgoing
}

func (es *elementarySpan[T, CP, SP, DP]) withParentSpan(span MutableSpan[T, CP, SP, DP]) MutableElementarySpan[T, CP, SP, DP] {
	es.span = span
	return es
}

func (es *elementarySpan[T, CP, SP, DP]) WithMarks(marks []MutableMark[T]) MutableElementarySpan[T, CP, SP, DP] {
	es.marks = marks
	return es
}

func (es *elementarySpan[T, CP, SP, DP]) WithStart(start T) MutableElementarySpan[T, CP, SP, DP] {
	es.start = start
	return es
}

func (es *elementarySpan[T, CP, SP, DP]) WithEnd(end T) MutableElementarySpan[T, CP, SP, DP] {
	es.end = end
	return es
}

func makeInitialElementarySpan[T any, CP, SP, DP fmt.Stringer](s Span[T, CP, SP, DP]) *elementarySpan[T, CP, SP, DP] {
	es := &elementarySpan[T, CP, SP, DP]{
		span:  s,
		start: s.Start(),
		end:   s.End(),
	}
	return es
}

// NewMutableElementarySpan creates and returns a new MutableElementarySpan.
func NewMutableElementarySpan[T any, CP, SP, DP fmt.Stringer]() MutableElementarySpan[T, CP, SP, DP] {
	return &elementarySpan[T, CP, SP, DP]{}
}
