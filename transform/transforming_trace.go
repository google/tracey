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

package transform

import (
	"fmt"

	"github.com/google/tracey/trace"
)

// A Span-initial ElementarySpan which is gated by some SpanGate.
type gatedElementarySpan[T any, CP, SP, DP fmt.Stringer] struct {
	est   elementarySpanTransformer[T, CP, SP, DP]
	gates []*appliedSpanGates[T, CP, SP, DP]
}

// A Trace in the process of transforming.
type transformingTrace[T any, CP, SP, DP fmt.Stringer] struct {
	original    trace.Trace[T, CP, SP, DP]
	new         trace.MutableTrace[T, CP, SP, DP]
	n           trace.Namer[T, CP, SP, DP]
	ats         *appliedTransforms[T, CP, SP, DP]
	firstMoment T

	// A queue of ElementarySpans whose prior dependencies are all satisfied,
	// ready to schedule.
	schedulableElementarySpans []elementarySpanTransformer[T, CP, SP, DP]
	// The set of Span-initial ElementarySpans which are currently gated.
	gatedElementarySpans []*gatedElementarySpan[T, CP, SP, DP]
	// The total number of ElementarySpans created so far in the new Trace, and
	// the number of ElementarySpans in the new Trace which have been scheduled.
	// If the former is greater than the latter after all schedulable
	// ElementarySpans have been finalized, then some ElementarySpans in the new
	// Trace are not schedulable, generally because transformations introduced
	// causal loops or unresolvable gates.
	createdElementarySpanCount, scheduledElementarySpanCount int
	// Mappings from original to new Spans (or RootSpans).
	transformingSpansByOriginal     map[trace.Span[T, CP, SP, DP]]spanTransformer[T, CP, SP, DP]
	transformingRootSpansByOriginal map[trace.RootSpan[T, CP, SP, DP]]spanTransformer[T, CP, SP, DP]
	// A mapping from original to new Dependencies.
	transformingDependenciesByOriginal map[trace.Dependency[T, CP, SP, DP]]dependencyTransformer[T, CP, SP, DP]
}

func (tt *transformingTrace[T, CP, SP, DP]) comparator() trace.Comparator[T] {
	return tt.original.Comparator()
}

func (tt *transformingTrace[T, CP, SP, DP]) start() T {
	return tt.firstMoment
}

func (tt *transformingTrace[T, CP, SP, DP]) namer() trace.Namer[T, CP, SP, DP] {
	return tt.n
}

func (tt *transformingTrace[T, CP, SP, DP]) appliedTransformations() *appliedTransforms[T, CP, SP, DP] {
	return tt.ats
}

func (tt *transformingTrace[T, CP, SP, DP]) newMutableDependency(original trace.Dependency[T, CP, SP, DP]) trace.MutableDependency[T, CP, SP, DP] {
	return tt.new.NewMutableDependency(original.DependencyType()).WithPayload(original.Payload())
}

// Returns the new Span corresponding to the provided original one.
func (tt *transformingTrace[T, CP, SP, DP]) spanFromOriginal(
	original trace.Span[T, CP, SP, DP],
) (spanTransformer[T, CP, SP, DP], error) {
	ret, ok := tt.transformingSpansByOriginal[original]
	if !ok {
		var err error
		ret, err = newTransformingSpan(tt, original)
		if err != nil {
			return nil, err
		}
		tt.transformingSpansByOriginal[original] = ret
		tt.createdElementarySpanCount += len(ret.elementarySpans())
	}
	return ret, nil
}

// Returns the new Dependency corresponding to the provided original one.
// Returns nil if the provided original is nil, or if the original Dependency
// was fully removed (i.e., its source and all its destinations matched
// Dependency deletion selectors),
func (tt *transformingTrace[T, CP, SP, DP]) dependencyFromOriginal(
	original trace.Dependency[T, CP, SP, DP],
) dependencyTransformer[T, CP, SP, DP] {
	if original == nil {
		return nil
	}
	ret, ok := tt.transformingDependenciesByOriginal[original]
	if !ok {
		ret = newTransformingDependency[T, CP, SP, DP](tt, original)
		tt.transformingDependenciesByOriginal[original] = ret
	}
	return ret
}

// Enqueues the provided ElementarySpan for scheduling.  This ElementarySpan
// must be schedulable:
//   - All of its causal antecedents (Incoming().Origin() and Predecessor())
//     must be scheduled; and
//   - Its new start point (tes.New.Start()) must be set, and should be the
//     earliest point this ElementarySpan could run (e.g., its Predecessor's
//     end point, or its Incoming dependency's point).
//
// Note that if the provided ElementarySpan is the first in its Span, and that
// Span is affected by a gating transformation, the provided ElementarySpan
// may not immediately become schedulable, but may have to wait until it is no
// longer gated (which may push its start time further back.)
func (tt *transformingTrace[T, CP, SP, DP]) schedule(est elementarySpanTransformer[T, CP, SP, DP]) {
	if est.isStartOfSpan() {
		gated := false
		asgs := []*appliedSpanGates[T, CP, SP, DP]{}
		if len(tt.ats.appliedSpanGates) > 0 {
			// Determine which Span gates will apply to this new Span.
			for _, asg := range tt.ats.appliedSpanGates {
				if asg.spanSelection.Includes(est.originalParent()) {
					asgs = append(asgs, asg)
					gated = gated || !asg.gater.SpanCanStart(est.originalParent())
				}
			}
		}
		if gated {
			// If this Span is gated, enqueue it among the currently-gated Spans.
			tt.gatedElementarySpans = append(tt.gatedElementarySpans, &gatedElementarySpan[T, CP, SP, DP]{
				est:   est,
				gates: asgs,
			})
		} else {
			// This Span isn't gated, so we can schedule it immediately.
			tt.scheduleUngatedInitialElementarySpan(est, est.getTransformed().Start())
		}
	} else {
		tt.schedulableElementarySpans = append(tt.schedulableElementarySpans, est)
	}
}

// Schedules the provided ungated Span-initial ElementarySpan, and marks its
// Span as 'starting' in all Span gaters.
func (tt *transformingTrace[T, CP, SP, DP]) scheduleUngatedInitialElementarySpan(
	est elementarySpanTransformer[T, CP, SP, DP],
	at T,
) {
	est.updateStart(at)
	tt.schedulableElementarySpans = append(tt.schedulableElementarySpans, est)
	for _, asg := range tt.ats.appliedSpanGates {
		span := est.originalParent()
		selected := asg.spanSelection.Includes(span)
		asg.gater.SpanStarting(span, selected)
	}
}

// Checks all currently-gated Span-initial ElementarySpans to see whether they
// remain gated.  Newly-ungated ElementarySpans are scheduled.
func (tt *transformingTrace[T, CP, SP, DP]) scheduleNewlyUngatedElementarySpans(at T) {
	newGatedElementarySpans := []*gatedElementarySpan[T, CP, SP, DP]{}
	for _, ges := range tt.gatedElementarySpans {
		gated := false
		for _, asg := range ges.gates {
			gated = gated || !asg.gater.SpanCanStart(ges.est.originalParent())
		}
		if gated {
			newGatedElementarySpans = append(newGatedElementarySpans, ges)
		} else {
			ges.est.updateStart(at)
			tt.schedulableElementarySpans = append(tt.schedulableElementarySpans, ges.est)
			for _, asg := range tt.ats.appliedSpanGates {
				span := ges.est.originalParent()
				selected := asg.spanSelection.Includes(span)
				asg.gater.SpanStarting(span, selected)
			}
		}
	}
	tt.gatedElementarySpans = newGatedElementarySpans
}

// Given a slice of scheduled elementarySpanTransformers, creates and returns a
// slice of corresponding MutableElementarySpans.
func (tt *transformingTrace[T, CP, SP, DP]) transformElementarySpans(
	ests []elementarySpanTransformer[T, CP, SP, DP],
) []trace.MutableElementarySpan[T, CP, SP, DP] {
	newESs := make([]trace.MutableElementarySpan[T, CP, SP, DP], 0, len(ests))
	var lastES trace.MutableElementarySpan[T, CP, SP, DP]
	for _, est := range ests {
		thisES := est.getTransformed()
		if lastES != nil &&
			lastES.Outgoing() == nil &&
			thisES.Incoming() == nil &&
			tt.original.Comparator().Equal(lastES.End(), thisES.Start()) {
			// If the transformations have left contiguous ElementarySpans with no
			// incoming or outgoing dependences between them, merge those by updating
			// the earlier one's endpoint, adding any outgoing Dependency from the
			// later one to the earlier one, and then dropping the later one.
			lastES.WithEnd(thisES.End())
			if thisES.Outgoing() != nil {
				thisES.Outgoing().(trace.MutableDependency[T, CP, SP, DP]).
					WithOriginElementarySpan(lastES)
			}
		} else {
			newESs = append(newESs, thisES)
			lastES = thisES
		}
	}
	return newESs
}

// Creates all RootSpans in the new Trace.  Since any entry Span (that is,
// a span with no incoming Dependencies within the Trace) must also be a
// RootSpan, this also places all initially-schedulable ElementarySpans on the
// schedule queue.
func (tt *transformingTrace[T, CP, SP, DP]) createRootSpans(
	original trace.Trace[T, CP, SP, DP],
) error {
	firstMomentSet := false
	updateFirstMoment := func(span trace.Span[T, CP, SP, DP]) {
		if !firstMomentSet || tt.comparator().Less(span.Start(), tt.firstMoment) {
			tt.firstMoment = span.Start()
			firstMomentSet = true
		}
	}
	var visit func(originalSpan trace.Span[T, CP, SP, DP]) error
	visit = func(originalSpan trace.Span[T, CP, SP, DP]) error {
		updateFirstMoment(originalSpan)
		if _, err := tt.spanFromOriginal(originalSpan); err != nil {
			return err
		}
		for _, child := range originalSpan.ChildSpans() {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, originalRootSpan := range original.RootSpans() {
		updateFirstMoment(originalRootSpan)
		rs, err := tt.spanFromOriginal(originalRootSpan)
		if err != nil {
			return err
		}
		for _, child := range originalRootSpan.ChildSpans() {
			if err := visit(child); err != nil {
				return err
			}
		}
		tt.transformingRootSpansByOriginal[originalRootSpan] = rs
	}
	return nil
}

// Begins the trace transform specified by the receiver, updating the new Trace
// with copies of the original Trace's Category hierarchies, and returning a
// mapping from original to new Trace Categories, which can be used to attach
// RootSpans to Category hierarchies in the new Trace.
func (tt *transformingTrace[T, CP, SP, DP]) copyCategories(
	original, new trace.Trace[T, CP, SP, DP],
) (
	originalToNewCategories map[trace.Category[T, CP, SP, DP]]trace.Category[T, CP, SP, DP],
) {
	originalToNewCategories = map[trace.Category[T, CP, SP, DP]]trace.Category[T, CP, SP, DP]{}
	// Recursively copy the original trace's category tree, maintaining mappings
	// from old to new categories.
	var copyCategory func(originalCategory, newCategory trace.Category[T, CP, SP, DP])
	copyCategory = func(originalCategory, newCategory trace.Category[T, CP, SP, DP]) {
		originalToNewCategories[originalCategory] = newCategory
		for _, originalChildCategory := range originalCategory.ChildCategories() {
			newChildCategory := newCategory.NewChildCategory(originalChildCategory.Payload())
			if tt.ats.categoryPayloadMapping != nil {
				newChildCategory.UpdatePayload(tt.ats.categoryPayloadMapping(originalChildCategory, newChildCategory))
			}
			copyCategory(originalChildCategory, newChildCategory)
		}
	}
	for _, ht := range original.HierarchyTypes() {
		for _, originalRootCat := range original.RootCategories(ht) {
			newRootCat := new.NewRootCategory(ht, originalRootCat.Payload())
			if tt.ats.categoryPayloadMapping != nil {
				newRootCat.UpdatePayload(tt.ats.categoryPayloadMapping(originalRootCat, newRootCat))
			}
			copyCategory(originalRootCat, newRootCat)
		}
	}
	return originalToNewCategories
}

// Given a new Span and its corresponding MutableSpan, reconstruct the original
// Trace's Span hierarchy under the new Span.
func (tt *transformingTrace[T, CP, SP, DP]) buildChildSpans(
	st spanTransformer[T, CP, SP, DP],
	newMutableSpan trace.MutableSpan[T, CP, SP, DP],
) error {
	for _, originalChildSpan := range st.original().ChildSpans() {
		childST, err := tt.spanFromOriginal(originalChildSpan)
		if err != nil {
			return err
		}
		newMutableChildSpan, err := newMutableSpan.NewMutableChildSpan(
			tt.transformElementarySpans(childST.elementarySpans()),
			originalChildSpan.Payload(),
		)
		if tt.ats.spanPayloadMapping != nil {
			newMutableChildSpan.UpdatePayload(
				tt.ats.spanPayloadMapping(originalChildSpan, newMutableChildSpan),
			)
		}
		if err != nil {
			return err
		}
		if err := tt.buildChildSpans(childST, newMutableChildSpan); err != nil {
			return err
		}
	}
	return nil
}

func transformTrace[T any, CP, SP, DP fmt.Stringer](
	original trace.Trace[T, CP, SP, DP],
	ats *appliedTransforms[T, CP, SP, DP],
) (trace.Trace[T, CP, SP, DP], error) {
	// Build a transformingTrace to manage the transformation.
	tt := &transformingTrace[T, CP, SP, DP]{
		original: original,
		new: trace.NewMutableTrace[T, CP, SP, DP](
			original.Comparator(),
			original.DefaultNamer(),
		),
		n:                                  original.DefaultNamer(),
		ats:                                ats,
		transformingSpansByOriginal:        map[trace.Span[T, CP, SP, DP]]spanTransformer[T, CP, SP, DP]{},
		transformingRootSpansByOriginal:    map[trace.RootSpan[T, CP, SP, DP]]spanTransformer[T, CP, SP, DP]{},
		transformingDependenciesByOriginal: map[trace.Dependency[T, CP, SP, DP]]dependencyTransformer[T, CP, SP, DP]{},
	}
	// Create a new Dependency for each added-Dependency transform.
	for _, aad := range tt.ats.appliedDependencyAdditions {
		if aad != nil {
			aad.dep = tt.new.NewMutableDependency(aad.adt.dependencyType)
		}
	}
	// Create all RootSpans in the new trace, which also places all initially-
	// schedulable ElementarySpans onto the scheduling queue.
	if err := tt.createRootSpans(tt.original); err != nil {
		return nil, err
	}
	originalToNewCategories := tt.copyCategories(tt.original, tt.new)
	// While the scheduling queue has anything in it,
	//   * Remove and schedule the first schedulable ElementarySpan.  This may
	//     result in some ElementarySpans which depend on it to become
	//     schedulable and to enter the scheduling queue.
	//   * If the scheduled ElementarySpan was the last in its Span, update any
	//     Span gaters.
	for len(tt.schedulableElementarySpans) > 0 {
		est := tt.schedulableElementarySpans[0]
		tt.schedulableElementarySpans = tt.schedulableElementarySpans[1:]
		if err := est.schedule(); err != nil {
			return nil, err
		}
		tt.scheduledElementarySpanCount++
		if est.isEndOfSpan() {
			for _, asg := range tt.ats.appliedSpanGates {
				span := est.originalParent()
				selected := asg.spanSelection.Includes(span)
				asg.gater.SpanEnding(span, selected)
			}
			tt.scheduleNewlyUngatedElementarySpans(est.getTransformed().End())
		}
	}
	// At this point, no ElementarySpans should be gated, and all created
	// ElementarySpans should have been scheduled.
	if len(tt.gatedElementarySpans) != 0 {
		return nil, fmt.Errorf("after the transformation, %d Spans remain gated", len(tt.gatedElementarySpans))
	}
	if tt.scheduledElementarySpanCount < tt.createdElementarySpanCount {
		return nil, fmt.Errorf("could not transform at least %d ElementarySpans; this may indicate a loop in the transformed Trace's dependence graph",
			tt.createdElementarySpanCount-tt.scheduledElementarySpanCount)
	}
	// Finally, assemble the Span hierarchy of the new Trace.
	for originalRootSpan, newRootST := range tt.transformingRootSpansByOriginal {
		newMutableRootSpan, err := tt.new.NewMutableRootSpan(
			tt.transformElementarySpans(newRootST.elementarySpans()),
			originalRootSpan.Payload(),
		)
		if ats.spanPayloadMapping != nil {
			newMutableRootSpan.UpdatePayload(ats.spanPayloadMapping(originalRootSpan, newMutableRootSpan))
		}
		if err != nil {
			return nil, err
		}

		// Add the new RootSpan into categories corresponding to the original's
		// parents.
		for _, ht := range tt.new.HierarchyTypes() {
			originalCat := originalRootSpan.ParentCategory(ht)
			if originalCat != nil {
				newCat := originalToNewCategories[originalCat]
				newCat.AddRootSpan(newMutableRootSpan)
			}
		}
		if err := tt.buildChildSpans(newRootST, newMutableRootSpan); err != nil {
			return nil, err
		}
	}
	return tt.new, nil
}
