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

// A span under construction in a transforming Trace.
type transformingSpan[T any, CP, SP, DP fmt.Stringer] struct {
	// The original Span.
	originalSpan trace.Span[T, CP, SP, DP]
	// The Span modification applied to this Span.
	msts []*modifySpanTransform[T, CP, SP, DP]
	// The temporally-ordered transforming ElementarySpans within this Span.
	ess []elementarySpanTransformer[T, CP, SP, DP]
}

func (ts *transformingSpan[T, CP, SP, DP]) original() trace.Span[T, CP, SP, DP] {
	return ts.originalSpan
}

func (ts *transformingSpan[T, CP, SP, DP]) elementarySpans() []elementarySpanTransformer[T, CP, SP, DP] {
	return ts.ess
}

func (ts *transformingSpan[T, CP, SP, DP]) spanModifications() []*modifySpanTransform[T, CP, SP, DP] {
	return ts.msts
}

func (ts *transformingSpan[T, CP, SP, DP]) pushElementarySpan(est elementarySpanTransformer[T, CP, SP, DP]) {
	ts.ess = append(ts.ess, est)
}

func newTransformingSpan[T any, CP, SP, DP fmt.Stringer](
	tt traceTransformer[T, CP, SP, DP],
	original trace.Span[T, CP, SP, DP],
) (spanTransformer[T, CP, SP, DP], error) {
	tsb, err := newTransformingSpanBuilder(tt, original)
	if err != nil {
		return nil, err
	}
	// Transform each original ElementarySpan in increasing temporal order.
	for _, originalES := range original.ElementarySpans() {
		if err := tsb.transformNextOriginalElementarySpan(originalES); err != nil {
			return nil, err
		}
	}
	// The last ElementarySpan in the builder has definitely seen its last
	// incoming dependency.
	if tsb.lastES != nil {
		tsb.lastES.allIncomingDependenciesAdded()
	}
	return tsb.span, nil
}

// A helper for properly assembling transformingSpans.
type transformingSpanBuilder[T any, CP, SP, DP fmt.Stringer] struct {
	tt traceTransformer[T, CP, SP, DP]
	// The new Span being transformed.
	span *transformingSpan[T, CP, SP, DP]
	// The duration of the original Span.  Used to satisfy 'percentage-through'
	// transformations.
	spanDuration float64
	// The start offset adjustment applied to this Span.
	startOffsetAdjustment float64
	// The time-ordered added Dependencies (incoming and outgoing) for this Span.
	dependencyAdditions []*addedDependency[T, CP, SP, DP]
	// The currently-last transforming ElementarySpan under this Span.
	lastES elementarySpanTransformer[T, CP, SP, DP]
	// If true, originally-nonblocking, unmodified incoming dependencies to this
	// Span's ElementarySpans may shrink.
	incomingDependenciesMayShrinkIfNotOriginallyBlocking bool
	// If nonblockingOriginalDependenciesMayShrink is true, the offset from the
	// non-blocking incoming dependencies origin times to which those
	// dependencies may shrink.
	incomingDependencyMayShrinkToOriginOffset float64
}

func newTransformingSpanBuilder[T any, CP, SP, DP fmt.Stringer](
	tt traceTransformer[T, CP, SP, DP],
	originalSpan trace.Span[T, CP, SP, DP],
) (*transformingSpanBuilder[T, CP, SP, DP], error) {
	ret := &transformingSpanBuilder[T, CP, SP, DP]{
		tt: tt,
		span: &transformingSpan[T, CP, SP, DP]{
			originalSpan: originalSpan,
			ess:          make([]elementarySpanTransformer[T, CP, SP, DP], 0, len(originalSpan.ElementarySpans())),
			msts:         tt.appliedTransformations().getSpanModifications(originalSpan),
		},
		spanDuration:        tt.comparator().Diff(originalSpan.End(), originalSpan.Start()),
		dependencyAdditions: tt.appliedTransformations().getAddedDependencies(originalSpan),
	}
	offsetAdjustmentChanged := false
	for _, mst := range ret.span.msts {
		if mst.hasNewStart {
			if offsetAdjustmentChanged {
				return nil, fmt.Errorf("multiple start offset changes applied to span %s", tt.namer().SpanName(originalSpan))
			}
			ret.startOffsetAdjustment = tt.comparator().Diff(
				mst.newStart,
				originalSpan.Start(),
			)
			offsetAdjustmentChanged = true
		}
	}
	incomingShrinkChanged := false
	for _, adm := range tt.appliedTransformations().getAppliedDependencyModificationsForDestinationSpan(originalSpan) {
		if adm.mdt.mayShrinkIfNotOriginallyBlocking {
			if incomingShrinkChanged {
				return nil, fmt.Errorf("multiple nonblocking original incoming dependencies' shrink factors applied to span %s", tt.namer().SpanName(originalSpan))
			}
			ret.incomingDependenciesMayShrinkIfNotOriginallyBlocking = true
			ret.incomingDependencyMayShrinkToOriginOffset = adm.mdt.mayShrinkToOriginOffset
			incomingShrinkChanged = true
		}
	}
	return ret, nil
}

// Pushes a new transforming ElementarySpan, with all its Dependencies added,
// into the transforming Span.
func (tsb *transformingSpanBuilder[T, CP, SP, DP]) pushTransformingElementarySpan(
	start, end T,
	initiallyBlockedByPredecessor bool,
) (elementarySpanTransformer[T, CP, SP, DP], error) {
	adjustedStart := tsb.tt.comparator().Add(
		start,
		tsb.startOffsetAdjustment,
	)
	duration := tsb.tt.comparator().Diff(end, start)
	tes := newTransformingElementarySpan[T, CP, SP, DP](
		tsb.tt, tsb.lastES, tsb.span,
		adjustedStart, duration,
		initiallyBlockedByPredecessor,
		tsb.incomingDependenciesMayShrinkIfNotOriginallyBlocking,
		tsb.incomingDependencyMayShrinkToOriginOffset,
	)
	if tsb.lastES != nil {
		tsb.lastES.allIncomingDependenciesAdded()
	}
	tsb.span.pushElementarySpan(tes)
	tsb.lastES = tes
	return tes, nil
}

// Push a new ElementarySpan corresponding (in extent and Dependencies) to the
// provided original ElementarySpan into the transforming Span.
func (tsb *transformingSpanBuilder[T, CP, SP, DP]) pushOriginalElementarySpan(
	original trace.ElementarySpan[T, CP, SP, DP],
) error {
	tes, err := tsb.pushTransformingElementarySpan(
		original.Start(), original.End(),
		original.Predecessor() != nil &&
			tsb.tt.comparator().Equal(original.Predecessor().End(), original.Start()),
	)
	if err != nil {
		return err
	}
	if err := tes.setOriginalOutgoingDependency(
		original.Outgoing(),
	); err != nil {
		return err
	}
	return tes.setOriginalIncomingDependency(
		original,
		original.Incoming(),
	)
}

// Finds the next added Dependency, if any, between the provided points.
// If an added Dependency is found, both it and the point where it should be
// placed are returned.
func (tsb *transformingSpanBuilder[T, CP, SP, DP]) findNextAddedDependency(start, end T) (at T, ad *addedDependency[T, CP, SP, DP]) {
	if len(tsb.dependencyAdditions) > 0 {
		nextOriginatingPoint := tsb.tt.comparator().Add(
			tsb.span.original().Start(),
			tsb.spanDuration*tsb.dependencyAdditions[0].percentageThrough(),
		)
		// Place the dependence at the first viable point at or after the requested
		// percentageThroughOrigin.
		if tsb.tt.comparator().LessOrEqual(nextOriginatingPoint, end) {
			// The dependency origin time is the later of the start time and the next
			// added dependency point.
			at = start
			if tsb.tt.comparator().Less(at, nextOriginatingPoint) {
				at = nextOriginatingPoint
			}
			ad, tsb.dependencyAdditions = tsb.dependencyAdditions[0], tsb.dependencyAdditions[1:]
			return at, ad
		}
	}
	return at, nil
}

// Pushes a new ElementarySpan (i.e., a fragment of an original one) onto the
// end of the receiver's ElementarySpans.
func (tsb *transformingSpanBuilder[T, CP, SP, DP]) pushNewElementarySpan(
	start, end T,
	lastAD, thisAD *addedDependency[T, CP, SP, DP],
) error {
	tes, err := tsb.pushTransformingElementarySpan(start, end, true)
	if err != nil {
		return err
	}
	if thisAD != nil && thisAD.outgoingHere {
		if err := tes.setNewOutgoingDependency(thisAD.dependencyAdditions); err != nil {
			return err
		}
	}
	if lastAD != nil && !lastAD.outgoingHere {
		if err := tes.setNewIncomingDependency(lastAD.dependencyAdditions); err != nil {
			return err
		}
		if _, ok := lastAD.dependencyAdditions.destinationsByOriginalSpan[tsb.span.original()]; ok {
			return fmt.Errorf("add dependency has multiple destination ElementarySpans within the same original span")
		}
		lastAD.dependencyAdditions.destinationsByOriginalSpan[tsb.span.original()] = tes
	}
	return nil
}

// Transforms an original ElementarySpan, turning it into one (if no added
// Dependencies impinge within the original Span) or more (if some added
// Dependencies do impinge within the original Span) new ElementarySpans.
// Original ElementarySpans must be processed in increasing temporal order.
func (tsb *transformingSpanBuilder[T, CP, SP, DP]) transformNextOriginalElementarySpan(
	original trace.ElementarySpan[T, CP, SP, DP],
) error {
	nextStart, nextAD := tsb.findNextAddedDependency(original.Start(), original.End())
	if nextAD == nil {
		// The original ElementarySpan can be added unmodified.
		tsb.pushOriginalElementarySpan(original)
		return nil
	}
	// An added dependency was found.  Emit an initial fractional ElementarySpan
	// representing the start of the original, and any incoming dependency it
	// might have.
	if err := tsb.pushNewElementarySpan(original.Start(), nextStart, nil, nextAD); err != nil {
		return err
	}
	if err := tsb.lastES.
		setOriginalIncomingDependency(original, original.Incoming()); err != nil {
		return err
	}
	lastStart, lastAD := nextStart, nextAD
	nextStart, nextAD = tsb.findNextAddedDependency(lastStart, original.End())
	// As long as there's another added Dependency within the original
	// ElementarySpan, create another fractional new ElementarySpan.
	for ; nextAD != nil; nextStart, nextAD = tsb.findNextAddedDependency(lastStart, original.End()) {
		if err := tsb.pushNewElementarySpan(lastStart, nextStart, lastAD, nextAD); err != nil {
			return err
		}
		lastStart, lastAD = nextStart, nextAD
	}
	// If we've got time left in the original Span, or a pending incoming edge,
	// produce one last ElementarySpan.
	if !tsb.tt.comparator().Equal(lastStart, original.End()) ||
		(lastAD != nil && !lastAD.outgoingHere) {
		tsb.pushNewElementarySpan(lastStart, original.End(), lastAD, nil)
	}
	// Add any outgoing dependency from the original ElementarySpan to the last
	// new fractional ElementarySpan.
	return tsb.lastES.setOriginalOutgoingDependency(original.Outgoing())
}
