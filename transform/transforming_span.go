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
	"math"
	"sort"

	"github.com/google/tracey/trace"
)

// A span under construction in a transforming Trace.
type transformingSpan[T any, CP, SP, DP fmt.Stringer] struct {
	tt traceTransformer[T, CP, SP, DP]
	// The original Span.
	originalSpan trace.Span[T, CP, SP, DP]
	// The Span modification applied to this Span.
	msts []*modifySpanTransform[T, CP, SP, DP]
	// The temporally-ordered transforming ElementarySpans within this Span.
	ess []elementarySpanTransformer[T, CP, SP, DP]
}

func (ts *transformingSpan[T, CP, SP, DP]) traceTransformer() traceTransformer[T, CP, SP, DP] {
	return ts.tt
}

func (ts *transformingSpan[T, CP, SP, DP]) original() trace.Span[T, CP, SP, DP] {
	return ts.originalSpan
}

func (ts *transformingSpan[T, CP, SP, DP]) elementarySpans() []elementarySpanTransformer[T, CP, SP, DP] {
	return ts.ess
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
	newESCount := len(tsb.span.elementarySpans())
	var esMinDuration float64
	if tsb.originalUnsuspendedDuration == 0 {
		// Special case: if the original span had zero unsuspended duration, but
		// it now has >0 unsuspended duration, then each new elementary span gets
		// an equal share of the new unsuspended duration.
		esMinDuration = tsb.newUnsuspendedDuration / float64(newESCount)
	}
	var scalingFactor float64 = 1.0
	if tsb.originalUnsuspendedDuration != 0 {
		scalingFactor = tsb.newUnsuspendedDuration / tsb.originalUnsuspendedDuration
	}
	scale := func(originalDuration float64) float64 {
		return esMinDuration + originalDuration*scalingFactor
	}
	for _, es := range tsb.span.elementarySpans() {
		es.setNewDuration(scale(es.originalDuration()))
	}
	return tsb.span, nil
}

// A helper for properly assembling transformingSpans.
type transformingSpanBuilder[T any, CP, SP, DP fmt.Stringer] struct {
	// The new Span being transformed.
	span *transformingSpan[T, CP, SP, DP]
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
	// The unsuspended duration of the original span.
	originalUnsuspendedDuration float64
	// The unsuspended duration of the transformed span.
	newUnsuspendedDuration float64
}

func newTransformingSpanBuilder[T any, CP, SP, DP fmt.Stringer](
	tt traceTransformer[T, CP, SP, DP],
	originalSpan trace.Span[T, CP, SP, DP],
) (*transformingSpanBuilder[T, CP, SP, DP], error) {
	ret := &transformingSpanBuilder[T, CP, SP, DP]{
		span: &transformingSpan[T, CP, SP, DP]{
			tt:           tt,
			originalSpan: originalSpan,
			ess:          make([]elementarySpanTransformer[T, CP, SP, DP], 0, len(originalSpan.ElementarySpans())),
			msts:         tt.appliedTransformations().getSpanModifications(originalSpan),
		},
		dependencyAdditions: tt.appliedTransformations().getAddedDependencies(tt.comparator(), originalSpan),
	}
	scalingFactor := 1.0
	unsuspendedDurationDelta := 0.0
	upperDurationCap := math.MaxFloat64
	lowerDurationCap := 0.0
	seenOffsetAdjustment := false
	seenDurationDelta := false
	seenScalingFactor := false
	for _, mst := range ret.span.msts {
		if mst.startsAsEarlyAsPossible {
			if seenOffsetAdjustment {
				return nil, fmt.Errorf("multiple start offset changes applied to span %s", tt.namer().SpanName(originalSpan))
			}
			ret.startOffsetAdjustment = tt.comparator().Diff(
				tt.start(),
				originalSpan.Start(),
			)
			seenOffsetAdjustment = true
		}
		var err error
		if mst.hasUpperDurationCap {
			upperDurationCap, err = tt.comparator().DurationFromString(mst.upperDurationCapString)
			if err != nil {
				return nil, fmt.Errorf("failed to parse upper duration cap string: %w", err)
			}
		}
		if mst.hasLowerDurationCap {
			lowerDurationCap, err = tt.comparator().DurationFromString(mst.lowerDurationCapString)
			if err != nil {
				return nil, fmt.Errorf("failed to parse lower duration cap string: %w", err)
			}
		}
		if mst.hasDurationDelta {
			if seenDurationDelta {
				return nil, fmt.Errorf("multiple duration deltas applied to span %s", tt.namer().SpanName(originalSpan))
			}
			unsuspendedDurationDelta, err = tt.comparator().DurationFromString(mst.durationDeltaString)
			if err != nil {
				return nil, fmt.Errorf("failed to parse duration delta string: %w", err)
			}
		}
		if mst.hasDurationScalingFactor {
			if seenScalingFactor {
				return nil, fmt.Errorf("multiple scaling factors applied to span %s", tt.namer().SpanName(originalSpan))
			}
			scalingFactor = mst.durationScalingFactor
		}
	}
	if upperDurationCap < lowerDurationCap {
		return nil, fmt.Errorf("both an upper cap and lower cap are applied to span %s, and the lower cap is greater than the upper", tt.namer().SpanName(originalSpan))
	}
	var originalUnsuspendedDuration float64
	for _, es := range originalSpan.ElementarySpans() {
		originalUnsuspendedDuration += tt.comparator().Diff(es.End(), es.Start())
	}
	newUnsuspendedDuration := originalUnsuspendedDuration*scalingFactor + unsuspendedDurationDelta
	// Truncate the new unsuspended duration to the upper and lower caps.
	if newUnsuspendedDuration < lowerDurationCap {
		newUnsuspendedDuration = lowerDurationCap
	} else if newUnsuspendedDuration > upperDurationCap {
		newUnsuspendedDuration = upperDurationCap
	}
	ret.originalUnsuspendedDuration = originalUnsuspendedDuration
	ret.newUnsuspendedDuration = newUnsuspendedDuration
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

// Pushes a new transforming ElementarySpan into the transforming Span.
func (tsb *transformingSpanBuilder[T, CP, SP, DP]) pushTransformingElementarySpan(
	start, end T,
	marks []trace.Mark[T],
	initiallyBlockedByPredecessor bool,
) (elementarySpanTransformer[T, CP, SP, DP], error) {
	duration := tsb.span.traceTransformer().comparator().Diff(end, start)
	tes := newTransformingElementarySpan(
		tsb.span, tsb.lastES, tsb.span,
		start, tsb.startOffsetAdjustment, duration,
		initiallyBlockedByPredecessor,
		tsb.incomingDependenciesMayShrinkIfNotOriginallyBlocking,
		tsb.incomingDependencyMayShrinkToOriginOffset,
		marks,
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
	comparator := tsb.span.traceTransformer().comparator()
	tes, err := tsb.pushTransformingElementarySpan(
		original.Start(), original.End(),
		original.Marks(),
		original.Predecessor() != nil &&
			comparator.Equal(original.Predecessor().End(), original.Start()),
	)
	if err != nil {
		return err
	}
	tes.setOriginalOutgoingDependency(original.Outgoing())
	return tes.setOriginalIncomingDependency(
		original,
		original.Incoming(),
	)
}

// Finds the next added Dependency, if any, between the provided original
// moments.  If an added Dependency is found, both it and the point where it
// should be placed are returned.
func (tsb *transformingSpanBuilder[T, CP, SP, DP]) findNextAddedDependency(
	originalStart, originalEnd T,
) (at T, ad *addedDependency[T, CP, SP, DP]) {
	for len(tsb.dependencyAdditions) > 0 {
		nextOriginatingPoint, found := tsb.dependencyAdditions[0].originalMoment(
			tsb.span.tt.comparator(), tsb.span.original(),
		)
		if !found {
			tsb.dependencyAdditions = tsb.dependencyAdditions[1:]
			continue
		}
		// Place the dependence at the first viable point at or after the requested
		// percentageThroughOrigin.
		if found && tsb.span.traceTransformer().comparator().LessOrEqual(nextOriginatingPoint, originalEnd) {
			// The dependency origin time is the later of the start time and the next
			// added dependency point.
			at = originalStart
			if tsb.span.traceTransformer().comparator().Less(at, nextOriginatingPoint) {
				at = nextOriginatingPoint
			}
			ad, tsb.dependencyAdditions = tsb.dependencyAdditions[0], tsb.dependencyAdditions[1:]
			return at, ad
		}
		break
	}
	return at, nil
}

// Pushes a new ElementarySpan (i.e., a fragment of an original one) onto the
// end of the receiver's ElementarySpans.
func (tsb *transformingSpanBuilder[T, CP, SP, DP]) pushNewElementarySpan(
	start, end T,
	originalMarks []trace.Mark[T],
	lastAD, thisAD *addedDependency[T, CP, SP, DP],
) error {
	tes, err := tsb.pushTransformingElementarySpan(start, end, originalMarks, true)
	if err != nil {
		return err
	}
	if thisAD != nil && thisAD.outgoingHere {
		if err := tes.setNewOutgoingDependency(
			thisAD.dependencyAdditions,
		); err != nil {
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
	comparator := tsb.span.traceTransformer().comparator()
	nextStart, nextAD := tsb.findNextAddedDependency(original.Start(), original.End())
	if nextAD == nil {
		// The original ElementarySpan can be added unmodified.
		tsb.pushOriginalElementarySpan(original)
		return nil
	}
	originalMarks := original.Marks()
	// Snip originalMarks into two parts.  The first, with all marks with moments
	// less than or equal to the provided timestamp, is returned; originalMarks
	// is reset to the remainder.
	snipMarksAt := func(at T) []trace.Mark[T] {
		snipIndex := sort.Search(len(originalMarks), func(idx int) bool {
			return comparator.GreaterOrEqual(originalMarks[idx].Moment(), at)
		})
		ret := originalMarks[:snipIndex]
		originalMarks = originalMarks[snipIndex:]
		return ret
	}
	// At least one dependency was added.  If there's an original incoming
	// dependency, emit an initial fractional ElementarySpan to hold it.
	if comparator.Less(original.Start(), nextStart) ||
		original.Incoming() != nil {
		if err := tsb.pushNewElementarySpan(
			original.Start(), nextStart,
			snipMarksAt(nextStart),
			nil, nextAD); err != nil {
			return err
		}
		if err := tsb.lastES.
			setOriginalIncomingDependency(original, original.Incoming()); err != nil {
			return err
		}
	}
	lastStart, lastAD := nextStart, nextAD
	nextStart, nextAD = tsb.findNextAddedDependency(lastStart, original.End())
	// As long as there's another added Dependency within the original
	// ElementarySpan, create another fractional new ElementarySpan.
	for ; nextAD != nil; nextStart, nextAD = tsb.findNextAddedDependency(lastStart, original.End()) {
		if err := tsb.pushNewElementarySpan(
			lastStart, nextStart,
			snipMarksAt(nextStart),
			lastAD, nextAD); err != nil {
			return err
		}
		lastStart, lastAD = nextStart, nextAD
	}
	// If we've got time left in the original Span, or a pending incoming edge,
	// produce one last ElementarySpan.
	if !comparator.Equal(lastStart, original.End()) ||
		(lastAD != nil && !lastAD.outgoingHere) {
		tsb.pushNewElementarySpan(
			lastStart, original.End(),
			snipMarksAt(nextStart),
			lastAD, nil)
	}
	// If the original ElementarySpan had an outgoing dependency, and there's now
	// a new outgoing dependency at the end of the transformed ElementarySpan,
	// emit a new fractional ElementarySpan to hold the original outgoing dep.
	if original.Outgoing() != nil {
		if tsb.lastES.hasOutgoingDep() {
			if err := tsb.pushNewElementarySpan(
				original.End(), original.End(),
				snipMarksAt(nextStart),
				nil, nil); err != nil {
				return err
			}
		}
		tsb.lastES.setOriginalOutgoingDependency(original.Outgoing())
	}
	return nil
}
