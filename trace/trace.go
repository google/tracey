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

// A type containing, and supporting common operations on, a slice of
// elementary spans.
type elementarySpanSequence[T any, CP, SP, DP fmt.Stringer] struct {
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
}

// Simplifies the receiver by merging abutting elementary spans and suspend
// intervals where no dependencies intervene.
func (ess *elementarySpanSequence[T, CP, SP, DP]) simplify(comparator Comparator[T]) {
	var lastES *elementarySpan[T, CP, SP, DP]
	newESs := make([]ElementarySpan[T, CP, SP, DP], 0, len(ess.elementarySpans))
	for idx := 0; idx < len(ess.elementarySpans); idx++ {
		thisES := ess.elementarySpanAt(idx)
		// Remove instantaneous interior elementary spans having no dependencies.
		if idx > 0 && idx < len(ess.elementarySpans)-1 &&
			comparator.Equal(thisES.Start(), thisES.End()) &&
			thisES.incoming == nil && thisES.outgoing == nil {
			ess.elementarySpans = slices.Delete(ess.elementarySpans, idx, idx)
			continue
		}
		// Merge abutting elementary spans with no intervening dependencies.
		if lastES != nil &&
			comparator.Equal(lastES.End(), thisES.Start()) &&
			lastES.outgoing == nil && thisES.incoming == nil {
			lastES.outgoing = thisES.outgoing
			lastES.end = thisES.end
			ess.elementarySpans = slices.Delete(ess.elementarySpans, idx, idx)
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
	ess.elementarySpans = newESs
}

// Returns the index in the receiver's slice of elementary spans of the
// first elementary span ending at or after the specified point, and a boolean
// indicating whether the elementary span at that index contains the specified
// point (inclusively at both ends).  If the specified point lies before the
// first elementary span, returns (0, false); if it lies after the last
// elementary span, returns (n, false), where n is the number of elementary
// spans.
func (ess *elementarySpanSequence[T, CP, SP, DP]) findFirstElementarySpanIndexEndingAtOrAfter(
	comparator Comparator[T],
	at T,
) (idx int, contains bool) {
	if len(ess.elementarySpans) == 0 ||
		comparator.Less(at, ess.elementarySpans[0].Start()) {
		// The requested point lies before the first span, or there are no
		// elementary spans.
		return 0, false
	}
	// Find the position of the first elementary span whose endpoint is not less
	// than the requested point.
	ret := sort.Search(len(ess.elementarySpans), func(x int) bool {
		return comparator.LessOrEqual(at, ess.elementarySpanAt(x).End())
	})
	if ret == len(ess.elementarySpans) {
		// The requested point lies after the span.
		return ret, false
	}
	if comparator.Greater(ess.elementarySpanAt(ret).Start(), at) {
		// The only elementary span that could contain the requested point does
		// not.  That is, the requested point lies in a gap between elementary
		// spans.
		return ret, false
	}
	return ret, true
}

func assembleSuspendOptions(options ...SuspendOption) SuspendOption {
	ret := DefaultSuspendOptions
	for _, option := range options {
		ret = ret | option
	}
	return ret
}

func (ess *elementarySpanSequence[T, CP, SP, DP]) singleSuspend(
	comparator Comparator[T],
	start, end T,
) error {
	// If the specified suspend is zero-width, there's nothing to do.
	if comparator.Equal(start, end) {
		return nil
	}
	suspendSpanIdx, found := ess.findFirstElementarySpanIndexEndingAtOrAfter(comparator, end)
	if !found {
		return fmt.Errorf("no elementary span at %v", end)
	}
	suspendSpan := ess.elementarySpanAt(suspendSpanIdx)

	if !suspendSpan.contains(comparator, start) {
		return fmt.Errorf("requested suspend crosses elementary span boundaries")
	}
	preSuspendSpanIdx, postSuspendSpanIdx, ok := ess.fissionElementarySpanAt(comparator, end)
	if !ok {
		return fmt.Errorf("failed to fission an elementary span that should have existed")
	}
	preSuspendSpan := ess.elementarySpanAt(preSuspendSpanIdx)
	preSuspendSpan.end = start
	postSuspendSpan := ess.elementarySpanAt(postSuspendSpanIdx)
	postSuspendSpan.start = end
	return nil
}

func (ess *elementarySpanSequence[T, CP, SP, DP]) Suspend(
	comparator Comparator[T],
	start, end T,
	options ...SuspendOption,
) error {
	opts := assembleSuspendOptions(options...)
	if opts&SuspendFissionsAroundElementarySpanEndpoints == SuspendFissionsAroundElementarySpanEndpoints {
		for comparator.Greater(end, start) {
			endSpanIdx, found := ess.findFirstElementarySpanIndexEndingAtOrAfter(comparator, end)
			if !found {
				return fmt.Errorf("no elementary span at %v", end)
			}
			endSpan := ess.elementarySpanAt(endSpanIdx)
			chunkStart := endSpan.Start()
			if comparator.Greater(start, chunkStart) {
				chunkStart = start
			}
			if err := ess.singleSuspend(comparator, chunkStart, end); err != nil {
				return err
			}
			end = endSpan.Start()
		}
		return nil
	}
	return ess.singleSuspend(comparator, start, end)
}

// Returns the *elementarySpan in the receiver at the specified index.  The
// caller is responsible for ensuring that this is a valid index.
func (ess *elementarySpanSequence[T, CP, SP, DP]) elementarySpanAt(idx int) *elementarySpan[T, CP, SP, DP] {
	return ess.elementarySpans[idx].(*elementarySpan[T, CP, SP, DP])
}

// Fissions the receiver's elementary span at the specified point, returning
// the indexes of the elementary spans before and after that point after the
// fission, and true on success.  If there is no elementary span at the
// specified point, returns false.
// Fissioning an elementary span at one of its ends will result in a zero-width
// elementary span (at beforeIdx when fissioning at the start, and afterIdx
// when fissioning at the end).  When such zero-width spans exist, a point may
// lie 'within' several spans: suppose elementary spans A, B, and C, of which A
// and B are zero-width and both start (and end) at T1, whereas C starts at T1
// and ends at T2; T1 then lies 'within' spans A, B, and C.  In such case, the
// first span whose endpoint is not less than the requested span will be
// fissioned: for the above example, A will be fissioned.
func (ess *elementarySpanSequence[T, CP, SP, DP]) fissionElementarySpanAt(
	comparator Comparator[T],
	at T,
) (beforeIdx, afterIdx int, fissioned bool) {
	esIdx, found := ess.findFirstElementarySpanIndexEndingAtOrAfter(comparator, at)
	if !found {
		return 0, 0, false
	}
	// The span to fission.
	originalES := ess.elementarySpanAt(esIdx)
	// Fission the original span by moving its end point forward to the fission
	// point, and creating a new span from the fission point to the original
	// span's endpoint.
	newES := &elementarySpan[T, CP, SP, DP]{
		span:        originalES.Span(),
		start:       at,
		end:         originalES.End(),
		predecessor: originalES,
		successor:   originalES.successor,
		outgoing:    nil,
	}
	originalES.successor = newES
	if newES.successor != nil {
		newES.successor.(*elementarySpan[T, CP, SP, DP]).predecessor = newES
	}
	originalES.end = at
	if originalES.outgoing != nil {
		// If the original span had an outgoing dependency, this is moved to the
		// new span (and the dependency's origin updated.)
		originalES.outgoing, newES.outgoing = nil, originalES.outgoing
		newES.outgoing.WithOriginElementarySpan(newES)
	}
	// Insert the new span into this ess.
	ess.elementarySpans = slices.Insert(ess.elementarySpans, esIdx+1, ElementarySpan[T, CP, SP, DP](newES))
	return esIdx, esIdx + 1, true
}

func (ess *elementarySpanSequence[T, CP, SP, DP]) createInstantaneousElementarySpanAt(
	comparator Comparator[T],
	at T,
) (idx int, err error) {
	spanIdx, found := ess.findFirstElementarySpanIndexEndingAtOrAfter(comparator, at)
	if found {
		return 0, fmt.Errorf("can't create instantaneous elementary span: another elementary span would overlap it")
	}
	if spanIdx == 0 || spanIdx == len(ess.elementarySpans) {
		return 0, fmt.Errorf("can't create instantaneous elementary span: it would lie outside its span")
	}
	prevES := ess.elementarySpanAt(spanIdx - 1)
	nextES := ess.elementarySpanAt(spanIdx)
	newES := &elementarySpan[T, CP, SP, DP]{
		span:        nextES.Span(),
		start:       at,
		end:         at,
		predecessor: prevES,
		successor:   nextES,
	}
	nextES.predecessor = newES
	prevES.successor = newES
	ess.elementarySpans = slices.Insert(ess.elementarySpans, spanIdx, ElementarySpan[T, CP, SP, DP](newES))
	return spanIdx, nil
}

func assembleDependencyOptions(options ...DependencyOption) DependencyOption {
	ret := DefaultDependencyOptions
	for _, option := range options {
		ret = ret | option
	}
	return ret
}

// Adds the provided outgoing dependency in the receiver at the specified
// point.  If an existing elementary span ends at the specified time and has
// no outgoing dependences, the provided dependency will be added to that one,
// otherwise a new elementary span will be created (which may be zero-width.)
func (ess *elementarySpanSequence[T, CP, SP, DP]) addOutgoingDependency(
	comparator Comparator[T],
	dep *dependency[T, CP, SP, DP],
	at T,
	options ...DependencyOption,
) error {
	opts := assembleDependencyOptions(options...)
	if dep.Origin() != nil {
		return fmt.Errorf("multiple dependency origins")
	}
	spanIdx, found := ess.findFirstElementarySpanIndexEndingAtOrAfter(comparator, at)
	if !found {
		if opts&DependencyCanFissionSuspend != DependencyCanFissionSuspend {
			return fmt.Errorf("no elementary span at %v to which to add an outgoing dependency", at)
		}
		var err error
		spanIdx, err = ess.createInstantaneousElementarySpanAt(comparator, at)
		if err != nil {
			return err
		}
	}
	es := ess.elementarySpanAt(spanIdx)
	if !comparator.Equal(es.End(), at) || es.Outgoing() != nil {
		spanIdx, _, ok := ess.fissionElementarySpanAt(comparator, at)
		if !ok {
			return fmt.Errorf("failed to fission an elementary span that should have existed")
		}
		es = ess.elementarySpans[spanIdx].(*elementarySpan[T, CP, SP, DP])
	}
	es.outgoing = dep
	dep.setOrigin(es)
	return nil
}

// Adds the provided incoming dependency in the receiver at the specified
// point.  If an existing elementary span starts at the specified time and has
// no incoming dependences, the provided dependency will be added to that one,
// otherwise a new elementary span will be created (which may be zero-width.)
func (ess *elementarySpanSequence[T, CP, SP, DP]) addIncomingDependency(
	comparator Comparator[T],
	dep *dependency[T, CP, SP, DP],
	at T,
	options ...DependencyOption,
) error {
	opts := assembleDependencyOptions(options...)
	spanIdx, found := ess.findFirstElementarySpanIndexEndingAtOrAfter(comparator, at)
	if !found {
		if opts&DependencyCanFissionSuspend != DependencyCanFissionSuspend {
			return fmt.Errorf("no elementary span at %v to which to add an incoming dependency", at)
		}
		var err error
		spanIdx, err = ess.createInstantaneousElementarySpanAt(comparator, at)
		if err != nil {
			return err
		}
	}
	span := ess.elementarySpanAt(spanIdx)
	if !comparator.Equal(span.Start(), at) || span.Incoming() != nil {
		_, spanIdx, ok := ess.fissionElementarySpanAt(comparator, at)
		if !ok {
			return fmt.Errorf("failed to fission an elementary span that should have existed")
		}
		span = ess.elementarySpanAt(spanIdx)
	}
	span.incoming = dep
	dep.destinations = append(dep.destinations, span)
	return nil
}

// Adds the provided incoming dependency in the receiver at the specified
// dependency point, preceding it with a wait from the specified wait point.
func (ess *elementarySpanSequence[T, CP, SP, DP]) addIncomingDependencyWithWait(
	comparator Comparator[T],
	dep *dependency[T, CP, SP, DP],
	waitFrom, dependencyAt T,
	options ...DependencyOption,
) error {
	if err := ess.Suspend(comparator, waitFrom, dependencyAt); err != nil {
		return err
	}
	return ess.addIncomingDependency(comparator, dep, dependencyAt, options...)
}

// A common base type for rootSpan and nonRootSpan.
type commonSpan[T any, CP, SP, DP fmt.Stringer] struct {
	elementarySpanSequence[T, CP, SP, DP]
	start, end T
	payload    SP
	childSpans []Span[T, CP, SP, DP]
}

func (cs *commonSpan[T, CP, SP, DP]) simplify(comparator Comparator[T]) {
	cs.elementarySpanSequence.simplify(comparator)
	for _, child := range cs.childSpans {
		child.simplify(comparator)
	}
}

func newCommonSpan[T any, CP, SP, DP fmt.Stringer](start, end T, payload SP) *commonSpan[T, CP, SP, DP] {
	return &commonSpan[T, CP, SP, DP]{
		elementarySpanSequence: elementarySpanSequence[T, CP, SP, DP]{},
		start:                  start,
		end:                    end,
		payload:                payload,
	}
}

func newMutableCommonSpan[T any, CP, SP, DP fmt.Stringer](elementarySpans []MutableElementarySpan[T, CP, SP, DP], payload SP) (*commonSpan[T, CP, SP, DP], error) {
	if len(elementarySpans) == 0 {
		return nil, fmt.Errorf("at least one elementary span must be provided to newMutableCommonSpan")
	}
	ess := make([]ElementarySpan[T, CP, SP, DP], len(elementarySpans))
	for idx, es := range elementarySpans {
		ess[idx] = es
	}
	return &commonSpan[T, CP, SP, DP]{
		elementarySpanSequence: elementarySpanSequence[T, CP, SP, DP]{
			elementarySpans: ess,
		},
		start:   elementarySpans[0].Start(),
		end:     elementarySpans[len(elementarySpans)-1].End(),
		payload: payload,
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
	}
	if err := parent.addOutgoingDependency(comparator, call, start); err != nil {
		return nil, err
	}
	if err := child.addIncomingDependency(comparator, call, start); err != nil {
		return nil, err
	}
	ret := &dependency[T, CP, SP, DP]{
		dependencyType: Return,
	}
	if err := child.addOutgoingDependency(comparator, ret, end); err != nil {
		return nil, err
	}
	if err := parent.addIncomingDependencyWithWait(comparator, ret, start, end); err != nil {
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
	origin         ElementarySpan[T, CP, SP, DP]   // Non-nil for a complete dependency.
	destinations   []ElementarySpan[T, CP, SP, DP] // Non-empty for a complete dependency.
}

func (t *trace[T, CP, SP, DP]) NewDependency(
	dependencyType DependencyType,
	payload DP,
) Dependency[T, CP, SP, DP] {
	t.observedDependencyType(dependencyType)
	return &dependency[T, CP, SP, DP]{
		dependencyType: dependencyType,
		payload:        payload,
	}
}

func (t *trace[T, CP, SP, DP]) NewMutableDependency(
	dependencyType DependencyType,
) MutableDependency[T, CP, SP, DP] {
	t.observedDependencyType(dependencyType)
	return &dependency[T, CP, SP, DP]{
		dependencyType: dependencyType,
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

func (d *dependency[T, CP, SP, DP]) Origin() ElementarySpan[T, CP, SP, DP] {
	return d.origin
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
	options ...DependencyOption,
) error {
	return commonSpanFrom(from).
		addOutgoingDependency(comparator, d, start, options...)
}

func (d *dependency[T, CP, SP, DP]) AddDestinationSpan(
	comparator Comparator[T],
	to Span[T, CP, SP, DP], end T,
	options ...DependencyOption,
) error {
	return commonSpanFrom(to).
		addIncomingDependency(comparator, d, end, options...)
}

func (d *dependency[T, CP, SP, DP]) AddDestinationSpanAfterWait(
	comparator Comparator[T],
	to Span[T, CP, SP, DP],
	waitFrom, end T,
	options ...DependencyOption,
) error {
	return commonSpanFrom(to).
		addIncomingDependencyWithWait(
			comparator,
			d,
			waitFrom,
			end,
			options...,
		)
}

func (d *dependency[T, CP, SP, DP]) WithOriginElementarySpan(es MutableElementarySpan[T, CP, SP, DP]) MutableDependency[T, CP, SP, DP] {
	d.setOrigin(es.withOutgoing(d))
	return d
}

func (d *dependency[T, CP, SP, DP]) WithPayload(payload DP) MutableDependency[T, CP, SP, DP] {
	d.payload = payload
	return d
}

func (d *dependency[T, CP, SP, DP]) SetOriginElementarySpan(es MutableElementarySpan[T, CP, SP, DP]) error {
	if d.origin != nil {
		return fmt.Errorf("a dependency may only have one origin")
	}
	d.WithOriginElementarySpan(es)
	return nil
}

func (d *dependency[T, CP, SP, DP]) WithDestinationElementarySpan(es MutableElementarySpan[T, CP, SP, DP]) MutableDependency[T, CP, SP, DP] {
	d.destinations = append(d.destinations, es.withIncoming(d))
	return d
}

func (d *dependency[T, CP, SP, DP]) setOrigin(es ElementarySpan[T, CP, SP, DP]) {
	d.origin = es
}

// Implements MutableElementarySpan[T, CP, SP, DP]
type elementarySpan[T any, CP, SP, DP fmt.Stringer] struct {
	span                   Span[T, CP, SP, DP]
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

func (es *elementarySpan[T, CP, SP, DP]) withIncoming(incoming MutableDependency[T, CP, SP, DP]) MutableElementarySpan[T, CP, SP, DP] {
	es.incoming = incoming
	return es
}

func (es *elementarySpan[T, CP, SP, DP]) withOutgoing(outgoing MutableDependency[T, CP, SP, DP]) MutableElementarySpan[T, CP, SP, DP] {
	es.outgoing = outgoing
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
func NewMutableElementarySpan[T any, CP, SP, DP fmt.Stringer](
	predecessor ElementarySpan[T, CP, SP, DP],
) MutableElementarySpan[T, CP, SP, DP] {
	ret := &elementarySpan[T, CP, SP, DP]{
		predecessor: predecessor,
	}
	if predecessor != nil {
		predecessor.(*elementarySpan[T, CP, SP, DP]).successor = ret
	}
	return ret
}
