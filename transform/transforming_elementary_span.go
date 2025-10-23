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

// An ElementarySpan in a transforming Trace.
type transformingElementarySpan[T any, CP, SP, DP fmt.Stringer] struct {
	spanTransformer spanTransformer[T, CP, SP, DP]
	// The transformed ElementarySpan under construction.
	newElementarySpan trace.MutableElementarySpan[T, CP, SP, DP]
	// The start point of the ElementarySpan, in the original trace domain.
	originalStart T
	// The original outgoing dependency of the ElementarySpan, if there is one.
	originalOutgoing trace.Dependency[T, CP, SP, DP]
	// The offset adjustment to apply to the initial start.
	startOffsetAdjustment float64
	originalMarks         []trace.Mark[T]
	// If non-nil, the transforming outgoing dependency from this ElementarySpan.
	newOutgoingDependency dependencyTransformer[T, CP, SP, DP]
	// If non-nil, the newly-added outgoing dependency from this ElementarySpan.
	// Used to resolve dependent transforming ElementarySpans when this one is
	// scheduled.
	addedOutgoingDependency *appliedDependencyAdditions[T, CP, SP, DP]
	// The new ElementarySpan's parent Span.
	newParent spanTransformer[T, CP, SP, DP]
	// The transforming ElementarySpan that will succeed this new ElementarySpan.
	// Used to resolve temporal dependencies between adjacent ElementarySpans
	// within a parent Span.
	newSuccessor elementarySpanTransformer[T, CP, SP, DP]

	// The number of predecessor elementary spans still pending (i.e., not yet
	// scheduled.)
	pendingPredecessorCount int
	// The number of incoming origin elementary spans still pending (i.e., not
	// yet scheduled.)
	pendingIncomingOriginCount int

	// If nonblockingOriginalDependenciesMayShrink is true, the offset from the
	// non-blocking incoming dependency's origin time to which it may shrink.
	nonblockingOriginalDependenciesShrinkStartOffset float64
	// The initial duration (as from trace.Comparator.Diff(start, end)) of the
	// ElementarySpan.
	initialDuration float64
	// The duration of the transformed elementary span.
	newDuration float64

	// The elementary span's new start time from non-dependency sources (i.e.,
	// the original span's start time, its scheduled predecessor's end time.)
	newNonDependencyStart T
	// Whether newNonDependencyStart has been set.
	hasNewNonDependencyStart bool
	// The elementary span's incoming-dependencies-resolved time of record.
	// If the incoming dependency has no multiple origin policy, this will be set
	// to the end time of the dependency origin once it's scheduled.  Otherwise,
	// it is the earliest-resolving origin (OR semantics) or the latest-resolving
	// origin (AND semantics).
	newIncomingDependenciesResolved T
	// Whether newIncomingDependenciesResolved has been set.
	hasNewIncomingDependenciesResolved bool

	// If true, this ElementarySpan has a predecessor within its Span.
	hasPredecessor bool
	// If true, and if this ElementarySpan's incoming dependency is originally
	// nonblocking and is unmodified, its incoming dependency may shrink.
	nonblockingOriginalDependenciesMayShrink bool
	// If true, this elementary span was initially blocked by its predecessor:
	// its start time equaled its predecessor's end time.
	initiallyBlockedByPredecessor bool
}

// Creates and returns a new transforming ElementarySpan
func newTransformingElementarySpan[T any, CP, SP, DP fmt.Stringer](
	st spanTransformer[T, CP, SP, DP],
	predecessor elementarySpanTransformer[T, CP, SP, DP],
	parent spanTransformer[T, CP, SP, DP],
	originalStart T,
	startOffsetAdjustment float64,
	initialDuration float64,
	initiallyBlockedByPredecessor bool,
	nonblockingOriginalDependenciesMayShrink bool,
	nonblockingOriginalDependenciesShrinkStartOffset float64,
	originalMarks []trace.Mark[T],
) elementarySpanTransformer[T, CP, SP, DP] {
	ret := &transformingElementarySpan[T, CP, SP, DP]{
		spanTransformer:                          st,
		newElementarySpan:                        trace.NewMutableElementarySpan[T, CP, SP, DP](),
		originalStart:                            originalStart,
		startOffsetAdjustment:                    startOffsetAdjustment,
		initialDuration:                          initialDuration,
		initiallyBlockedByPredecessor:            initiallyBlockedByPredecessor,
		newParent:                                parent,
		hasPredecessor:                           predecessor != nil,
		nonblockingOriginalDependenciesMayShrink: nonblockingOriginalDependenciesMayShrink,
		nonblockingOriginalDependenciesShrinkStartOffset: nonblockingOriginalDependenciesShrinkStartOffset,
		originalMarks: originalMarks,
	}
	if predecessor != nil {
		ret.pendingPredecessorCount = 1
		predecessor.(*transformingElementarySpan[T, CP, SP, DP]).setSuccessor(ret)
	}
	return ret
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) hasOutgoingDep() bool {
	return tes.newElementarySpan.Outgoing() != nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) getTransformed() trace.MutableElementarySpan[T, CP, SP, DP] {
	return tes.newElementarySpan
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) setSuccessor(successor elementarySpanTransformer[T, CP, SP, DP]) error {
	if tes.newSuccessor != nil {
		return fmt.Errorf("can't set ElementarySpan successor: it already has one")
	}
	tes.newSuccessor = successor
	return nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) isStartOfSpan() bool {
	return tes.newElementarySpan.Predecessor() == nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) isEndOfSpan() bool {
	return tes.newSuccessor == nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) originalParent() trace.Span[T, CP, SP, DP] {
	return tes.newParent.original()
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) comparator() trace.Comparator[T] {
	return tes.spanTransformer.traceTransformer().comparator()
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) updateNonDependencyStart(startAt T) {
	if !tes.hasNewNonDependencyStart || tes.comparator().Diff(
		startAt, tes.newNonDependencyStart) > 0 {
		tes.hasNewNonDependencyStart = true
		tes.newNonDependencyStart = startAt
	}
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) updateIncomingDependencyResolved(at T, chooseEarliest bool) {
	if !tes.hasNewIncomingDependenciesResolved {
		tes.hasNewIncomingDependenciesResolved = true
		tes.newIncomingDependenciesResolved = at
		return
	}
	if chooseEarliest {
		if tes.comparator().Less(at, tes.newIncomingDependenciesResolved) {
			tes.newIncomingDependenciesResolved = at
		}
	} else {
		if tes.comparator().Greater(at, tes.newIncomingDependenciesResolved) {
			tes.newIncomingDependenciesResolved = at
		}
	}
}

// Resolves one of the receiver's incoming Dependencies at the provided time.
// If, after this, no pending Dependencies remain, schedule.
func (tes *transformingElementarySpan[T, CP, SP, DP]) resolveIncomingDependencyAt(
	dependencyOptions trace.DependencyOption,
	resolvedAt T,
) {
	chooseEarliest := dependencyOptions.Includes(trace.MultipleOriginsWithOrSemantics)
	tes.updateIncomingDependencyResolved(resolvedAt, chooseEarliest)
	tes.pendingIncomingOriginCount--
	if tes.pendingIncomingOriginCount == 0 && tes.pendingPredecessorCount == 0 {
		tes.spanTransformer.traceTransformer().schedule(tes)
	}
}

// Resolves the receiver's predecessor at the provided time.
// If, after this, no pending Dependencies remain, schedule.
func (tes *transformingElementarySpan[T, CP, SP, DP]) resolvePredecessorAt(resolvedAt T) {
	tes.updateNonDependencyStart(resolvedAt)
	tes.pendingPredecessorCount--
	if tes.pendingIncomingOriginCount == 0 && tes.pendingPredecessorCount == 0 {
		tes.spanTransformer.traceTransformer().schedule(tes)
	}
}

// Sets the new ElementarySpan's endpoint and any of its marks to their proper
// position in the new span's extent.
func (tes *transformingElementarySpan[T, CP, SP, DP]) finalizeMoments() error {
	// The new ES will start at its non-dependency start time, or its resolving
	// incoming dependency time, whichever is later.
	newStart := tes.newNonDependencyStart
	if tes.hasNewIncomingDependenciesResolved && tes.comparator().Greater(tes.newIncomingDependenciesResolved, newStart) {
		newStart = tes.newIncomingDependenciesResolved
	}
	tes.newElementarySpan.WithStart(newStart)
	newEnd := tes.comparator().Add(tes.newElementarySpan.Start(), tes.newDuration)
	tes.newElementarySpan.WithEnd(newEnd)
	if tes.originalOutgoing != nil {
		transformingOutgoing := tes.spanTransformer.traceTransformer().dependencyFromOriginal(tes.originalOutgoing)
		if transformingOutgoing != nil {
			tes.newOutgoingDependency = transformingOutgoing
			if err := transformingOutgoing.setOrigin(tes.comparator(), tes); err != nil {
				return err
			}
		}
	}
	var scalingFactor float64 = 1.0
	if tes.initialDuration != 0 {
		scalingFactor = tes.newDuration / tes.initialDuration
	}
	scaleOriginalMoment := func(originalMoment T) (transformedMoment T) {
		originalOffset := tes.comparator().Diff(originalMoment, tes.originalStart)
		return tes.comparator().Add(tes.newElementarySpan.Start(), originalOffset*scalingFactor)
	}
	newMarks := make([]trace.MutableMark[T], len(tes.originalMarks))
	for idx, mark := range tes.originalMarks {
		newMoment := scaleOriginalMoment(mark.Moment())
		if tes.comparator().Less(newMoment, tes.newElementarySpan.Start()) ||
			tes.comparator().Greater(newMoment, tes.newElementarySpan.End()) {
			return fmt.Errorf(
				"mark '%s' in span '%s' mapped outside its elementary span: %v not in [%v,%v]",
				mark.Label(),
				tes.spanTransformer.traceTransformer().namer().SpanName(
					tes.spanTransformer.original(),
				),
				newMoment,
				tes.newElementarySpan.Start(),
				tes.newElementarySpan.End(),
			)
		}
		newMarks[idx] = trace.NewMutableMark[T]().
			WithLabel(mark.Label()).
			WithMoment(newMoment)
	}
	tes.newElementarySpan.WithMarks(newMarks)
	return nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) schedule() error {
	// Set the new ElementarySpan's endpoint.
	if err := tes.finalizeMoments(); err != nil {
		return err
	}
	outgoing := tes.newOutgoingDependency
	if outgoing != nil {
		// If there's an outgoing dependency from the original trace, then resolve
		// each of its destinations.  Each destination is resolved at this
		// ElementarySpan's endpoint, plus the original dependency edge's duration
		// scaled by any applicable dependencyModification.
		for _, dest := range outgoing.destinations() {
			// Force the destination's spanTransformer (and therefore the destination
			// elementarySpanTransformer, and therefore dest.new) to be created, if
			// it hasn't already.
			if _, err := tes.spanTransformer.traceTransformer().spanFromOriginal(dest.original.Span()); err != nil {
				return err
			}
			if dest.new == nil {
				return fmt.Errorf("outgoing dependency has no new destination ElementarySpan")
			}
			newDuration := tes.comparator().Diff(
				dest.original.Start(),
				outgoing.original().TriggeringOrigin().End(),
			)
			modified := false
			for _, dependencyModification := range outgoing.appliedDependencyModifications() {
				if dependencyModification.dependencySelection.IncludesDestinationSpan(
					dest.original.Span(),
				) {
					if modified {
						return fmt.Errorf("a single dependency may be affected by no more than one scaling factor")
					}
					modified = true
					newDuration = newDuration * dependencyModification.mdt.durationScalingFactor
				}
			}

			dest.new.(*transformingElementarySpan[T, CP, SP, DP]).resolveIncomingDependencyAt(
				outgoing.original().Options(),
				tes.comparator().Add(tes.newElementarySpan.End(), newDuration),
			)
		}
	} else if tes.addedOutgoingDependency != nil {
		// If this ElementarySpan has an outgoing newly-added dependency, resolve
		// that.  Note that added dependencies are not subject to other dependency-
		// modifying transforms.
		for _, newDest := range tes.addedOutgoingDependency.destinationsByOriginalSpan {
			newDest.(*transformingElementarySpan[T, CP, SP, DP]).resolveIncomingDependencyAt(
				trace.DefaultDependencyOptions,
				tes.comparator().Add(tes.newElementarySpan.End(), tes.addedOutgoingDependency.adt.schedulingDelay),
			)
		}
	}
	// If this ElementarySpan has a successor, resolve that dependency at this
	// ElementarySpan's end point.  There's no scheduling delay between sibling
	// ElementarySpans.
	if tes.newSuccessor != nil {
		tes.newSuccessor.(*transformingElementarySpan[T, CP, SP, DP]).resolvePredecessorAt(tes.newElementarySpan.End())
	}
	return nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) originalDuration() float64 {
	return tes.initialDuration
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) setNewDuration(newDuration float64) {
	tes.newDuration = newDuration
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) setOriginalOutgoingDependency(
	originalOutgoing trace.Dependency[T, CP, SP, DP],
) {
	tes.originalOutgoing = originalOutgoing
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) setOriginalIncomingDependency(
	original trace.ElementarySpan[T, CP, SP, DP],
	originalIncoming trace.Dependency[T, CP, SP, DP],
) error {
	transformingIncoming := tes.spanTransformer.traceTransformer().dependencyFromOriginal(originalIncoming)
	// The entire incoming dependency may have been deleted, or if it still
	// exists, this destination may have been deleted.
	if transformingIncoming != nil &&
		!transformingIncoming.isOriginalDestinationDeleted(original) {
		if tes.newElementarySpan.Incoming() != nil {
			return fmt.Errorf("can't set original incoming dependency for ElementarySpan: it already has one")
		}
		if err := transformingIncoming.addDestination(original, tes); err != nil {
			return err
		}
		tes.pendingIncomingOriginCount += len(originalIncoming.Origins())
	}
	return nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) setNewOutgoingDependency(
	addedOutgoingDependency *appliedDependencyAdditions[T, CP, SP, DP],
) error {
	if tes.newElementarySpan.Outgoing() != nil {
		return fmt.Errorf("can't set new outgoing dependency for ElementarySpan: it already has one")
	}
	tes.addedOutgoingDependency = addedOutgoingDependency
	addedOutgoingDependency.dep.SetOriginElementarySpan(tes.comparator(), tes.newElementarySpan)
	return nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) setNewIncomingDependency(
	addedIncomingDependency *appliedDependencyAdditions[T, CP, SP, DP],
) error {
	if tes.newElementarySpan.Incoming() != nil {
		return fmt.Errorf("can't set new incoming dependency for ElementarySpan: it already has one")
	}
	if addedIncomingDependency != nil {
		addedIncomingDependency.dep.WithDestinationElementarySpan(tes.newElementarySpan)
		tes.pendingIncomingOriginCount++
	}
	return nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) allIncomingDependenciesAdded() {
	// If the receiver has no causal dependencies at its creation, then it is
	// ready to schedule at the same point as its original counterpart started.
	if tes.pendingIncomingOriginCount == 0 && tes.pendingPredecessorCount == 0 {
		adjustedStart := tes.comparator().Add(
			tes.originalStart,
			tes.startOffsetAdjustment,
		)
		tes.updateNonDependencyStart(adjustedStart)
		tes.spanTransformer.traceTransformer().schedule(tes)
	}
}
