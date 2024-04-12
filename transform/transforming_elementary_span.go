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
	tt traceTransformer[T, CP, SP, DP]
	// The transformed ElementarySpan under construction.
	newElementarySpan trace.MutableElementarySpan[T, CP, SP, DP]
	// True if 'new' has its start time set.
	hasNewStart bool
	// The initial start point of the ElementarySpan.  The interval
	// between these is the ElementarySpan's base duration.
	initialStart T
	// The initial duration (as from trace.Comparator.Diff(start, end)) of the
	// ElementarySpan.
	initialDuration float64
	// If true, this elementary span was initially blocked by its predecessor:
	// its start time equaled its predecessor's end time.
	initiallyBlockedByPredecessor bool

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
	// The number of incoming dependencies (including a possible in-Span
	// predecessor) that must be resolved before this ElementarySpan is ready to
	// schedule.
	pendingIncomingDependencies int
	// If true, this ElementarySpan has a predecessor within its Span.
	hasPredecessor bool
	// If true, and if this ElementarySpan's incoming dependency is originally
	// nonblocking and is unmodified, its incoming dependency may shrink.
	nonblockingOriginalDependenciesMayShrink bool
	// If nonblockingOriginalDependenciesMayShrink is true, the offset from the
	// non-blocking incoming dependency's origin time to which it may shrink.
	nonblockingOriginalDependenciesShrinkStartOffset float64
}

// Creates and returns a new transforming ElementarySpan
func newTransformingElementarySpan[T any, CP, SP, DP fmt.Stringer](
	tt traceTransformer[T, CP, SP, DP],
	predecessor elementarySpanTransformer[T, CP, SP, DP],
	parent spanTransformer[T, CP, SP, DP],
	initialStart T,
	duration float64,
	initiallyBlockedByPredecessor bool,
	nonblockingOriginalDependenciesMayShrink bool,
	nonblockingOriginalDependenciesShrinkStartOffset float64,
) elementarySpanTransformer[T, CP, SP, DP] {
	var predES trace.ElementarySpan[T, CP, SP, DP]
	if predecessor != nil {
		predES = predecessor.getTransformed()
	}
	ret := &transformingElementarySpan[T, CP, SP, DP]{
		tt:                                       tt,
		newElementarySpan:                        trace.NewMutableElementarySpan(predES),
		initialStart:                             initialStart,
		initialDuration:                          duration,
		initiallyBlockedByPredecessor:            initiallyBlockedByPredecessor,
		newParent:                                parent,
		hasPredecessor:                           predecessor != nil,
		nonblockingOriginalDependenciesMayShrink: nonblockingOriginalDependenciesMayShrink,
		nonblockingOriginalDependenciesShrinkStartOffset: nonblockingOriginalDependenciesShrinkStartOffset,
	}
	if predecessor != nil {
		ret.pendingIncomingDependencies++
		predecessor.(*transformingElementarySpan[T, CP, SP, DP]).setSuccessor(ret)
	}
	return ret
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

func (tes *transformingElementarySpan[T, CP, SP, DP]) updateStart(startAt T) {
	if !tes.hasNewStart || tes.tt.comparator().Diff(startAt, tes.newElementarySpan.Start()) > 0 {
		tes.hasNewStart = true
		tes.newElementarySpan.WithStart(startAt)
	}
}

/*
Orig source (OS) time | Orig dest (OD) time | New source (NS) time | New dest (ND) time

If AllowUnchangedDependencesToChangedElementarySpans is true, then:
A dependency edge DE, into an elementary span ES, may scale down its duration
to as low as 0 if:
  1) any other dependency edge into the elementary span has changed;
  2) DE itself is unchanged, and
  3) doing so can reduce ES's start time.
*/

func (tes *transformingElementarySpan[T, CP, SP, DP]) resolveModifiedIncomingDependency(
	resolvedAt T,
) {
	tes.resolveIncomingDependencyAt(resolvedAt)
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) resolveUnmodifiedIncomingDependency(
	readyAt, resolvedAt T,
) {
	// If this ElementarySpan cannot shrink its incoming dependencies, or it has
	// no predecessor (and thus this is its only incoming Dependency), or if it
	// has a predecessor but originally was not blocked by that predecessor,
	// retain the original resolution time.
	if !tes.nonblockingOriginalDependenciesMayShrink ||
		!tes.hasPredecessor || !tes.initiallyBlockedByPredecessor {
		tes.resolveIncomingDependencyAt(resolvedAt)
		return
	}
	// Otherwise, allow the incoming dependency to shrink up to its ready time
	// plus the shrink offset
	tes.resolveIncomingDependencyAt(
		tes.tt.comparator().Add(readyAt, tes.nonblockingOriginalDependenciesShrinkStartOffset),
	)
}

// Resolves one of the receiver's incoming Dependencies at the provided time.
// If, after this, no pending incoming Dependencies remain, schedule.
func (tes *transformingElementarySpan[T, CP, SP, DP]) resolveIncomingDependencyAt(resolvedAt T) {
	tes.updateStart(resolvedAt)
	tes.pendingIncomingDependencies--
	if tes.pendingIncomingDependencies == 0 {
		tes.tt.schedule(tes)
	}
}

// Sets the new ElementarySpan's endpoint to the original ElementarySpan's
// duration, scaled by any applicable Span scaling factor, and added to the new
// start point.
func (tes *transformingElementarySpan[T, CP, SP, DP]) finalizeEndpoint() error {
	// Set this ElementarySpan's end point.  If its Span has a scaled duration
	// transform, scale the endpoint accordingly.
	scalingFactor := 1.0
	scalingFactorChanged := false
	for _, spanModification := range tes.newParent.spanModifications() {
		if spanModification.hasDurationScalingFactor {
			if scalingFactorChanged {
				return fmt.Errorf("multiple scaling factors applied to span %s", tes.tt.namer().SpanName(tes.originalParent()))
			}
			scalingFactor = spanModification.durationScalingFactor
			scalingFactorChanged = true
		}
	}
	esDur := tes.initialDuration * scalingFactor
	tes.newElementarySpan.WithEnd(tes.tt.comparator().Add(tes.newElementarySpan.Start(), esDur))
	return nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) schedule() error {
	// Set the new ElementarySpan's endpoint.
	if err := tes.finalizeEndpoint(); err != nil {
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
			if _, err := tes.tt.spanFromOriginal(dest.original.Span()); err != nil {
				return err
			}
			if dest.new == nil {
				return fmt.Errorf("outgoing dependency has no new destination ElementarySpan")
			}
			newDuration := tes.tt.comparator().Diff(
				dest.original.Start(),
				outgoing.original().Origin().End(),
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
				tes.tt.comparator().Add(tes.newElementarySpan.End(), newDuration),
			)
		}
	} else if tes.addedOutgoingDependency != nil {
		// If this ElementarySpan has an outgoing newly-added dependency, resolve
		// that.  Note that added dependencies are not subject to other dependency-
		// modifying transforms.
		for _, newDest := range tes.addedOutgoingDependency.destinationsByOriginalSpan {
			newDest.(*transformingElementarySpan[T, CP, SP, DP]).resolveIncomingDependencyAt(
				tes.tt.comparator().Add(tes.newElementarySpan.End(), tes.addedOutgoingDependency.adt.schedulingDelay),
			)
		}
	}
	// If this ElementarySpan has a successor, resolve that dependency at this
	// ElementarySpan's end point.  There's no scheduling delay between sibling
	// ElementarySpans.
	if tes.newSuccessor != nil {
		tes.newSuccessor.(*transformingElementarySpan[T, CP, SP, DP]).resolveIncomingDependencyAt(tes.newElementarySpan.End())
	}
	return nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) setOriginalOutgoingDependency(
	originalOutgoing trace.Dependency[T, CP, SP, DP],
) error {
	transformingOutgoing := tes.tt.dependencyFromOriginal(originalOutgoing)
	if transformingOutgoing != nil {
		if tes.newElementarySpan.Outgoing() != nil {
			return fmt.Errorf("can't set original outgoing dependency for ElementarySpan: it already has one (%v)", tes.newElementarySpan.Outgoing())
		}
		tes.newOutgoingDependency = transformingOutgoing
		if err := transformingOutgoing.setOrigin(tes); err != nil {
			return err
		}
	}
	return nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) setOriginalIncomingDependency(
	original trace.ElementarySpan[T, CP, SP, DP],
	originalIncoming trace.Dependency[T, CP, SP, DP],
) error {
	transformingIncoming := tes.tt.dependencyFromOriginal(originalIncoming)
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
		tes.pendingIncomingDependencies++
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
	addedOutgoingDependency.dep.SetOriginElementarySpan(tes.newElementarySpan)
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
		tes.pendingIncomingDependencies++
	}
	return nil
}

func (tes *transformingElementarySpan[T, CP, SP, DP]) allIncomingDependenciesAdded() {
	// If the receiver has no causal dependencies at its creation, then it is
	// ready to schedule at the same point as its original counterpart started.
	if tes.pendingIncomingDependencies == 0 {
		tes.updateStart(tes.initialStart)
		tes.tt.schedule(tes)
	}
}
