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

// Package transform provides types and functions for applying causal or
// temporal transformations (or simulations) over traces.
//
// Trace transformation is mediated by the Transforms type, which bundles
// together a set of Trace-independent transformations which can be applied to
// a particular trace with Transform.TransformTrace().  Available
// transformations include:
//   - WithCategoryPayloadMapping(): replaces Category payloads in the
//     transformed trace.
//   - WithSpanPayloadMapping(): replaces Span payloads in the transformed
//     trace.
//   - WithDependenciesScaledBy(): scales the scheduling delay of matching
//     Dependencies by a specified factor.
//   - WithSpansScaledBy(): scales the non-suspended duration of matching Spans
//     by a specified factor.
//   - WithSpansStartingAt(): replaces the default start time of matching
//     Spans.  Without this, Spans start by default at the same time as their
//     original-Trace counterparts.
//   - WithAddedDependencies(): adds additional Dependencies between matching
//     origin and destination spans.
//   - WithRemovedDependencies(): removes matching Dependencies from the Trace.
//
// Trace transformation relies on the decomposition of Spans into
// ElementarySpans.  During Trace construction, Spans are decomposed into
// non-overlapping ElementarySpans, with all incoming Dependencies at the
// start of an ElementarySpan and all outgoing Dependencies at the end of one.
// Within a Span, a suspended interval is represented as a gap between adjacent
// ElementarySpans.  Because ElementarySpans can only depend on things that
// happened before they start, they provide an ideal granularity for the Trace
// transformation or simulation algorithm.  At a high level, this algorithm:
//
//   - Finds all *entry ElementarySpans*: ElementarySpans with no causal
//     predecessors.
//   - Places all entry ElementarySpans into a queue SQ of 'schedulable'
//     ElementarySpans
//   - While SQ is nonempty, pops the first ElementarySpan off of SQ and
//     schedules it, if it is not the first ElementarySpan of a gated Span.
//     Scheduling an ElementarySpan may result in additional ElementarySpans
//     being added to SQ.
//   - Once SQ is empty, the transformed trace is assembled from the scheduled
//     ElementarySpans.
//
// When an ElementarySpan ES is scheduled,
//
//   - Its endpoint is computed to be its original counterpart's duration,
//     scaled by any applicable span scaling factor;
//   - All of its outgoing Dependencies (some of which may be added as part of
//     the transformation) are resolved at its new endpoint.  When a Dependency
//     is resolved in this way, all of its destination ElementarySpans have
//     their start point updated to the origin's endpoint plus the Dependency's
//     scheduling delay scaled by any applicable Dependency scaling factor.
//     Any destination ElementarySpan with no unresolved incoming Dependencies
//     becomes schedulable and is pushed onto SQ.  Deleted Dependencies are
//     ignored.
//
// It is possible to define Transforms which yield unscheduled, or
// orphaned, ElementarySpans.  SpanGates, for example, may never allow certain
// Spans to proceed.  Added Dependencies can also orphan ElementarySpans: for
// example, given a trace in which Span A ends by unblocking Span B with the
// Send dependence 's1':
//
// Span A:   [           (s1)]
// Span B:                   [(s1)       ]
// time      1   2   3   4   5   6   7   8
//
// A transformation adding a new Send Dependency s2 from the end of Span B to
// the beginning of Span A would introduce a causal paradox (s2 would depend on
// s1, and s1 would depend on s2) and a related temporal paradox (s2 would send
// a message at time 8 that would be (have been?) received earlier, at time 1).
//
// If a Trace transformation would orphan any ElementarySpans, it instead fails
// with a suitable error message.  Note that Traces may be defined with causal
// violations (for example, when the origin and destination of a Dependency
// occur in different and skewed clock domains); it is possible for even a null
// Transform on such a trace to fail with orphaned ElementarySpans.
package transform

import (
	"fmt"

	"github.com/google/tracey/trace"
	traceparser "github.com/google/tracey/trace/parser"
)

// A trace-independent specification for a particular dependency transform
// (i.e., changing dependency scheduling time.)
type modifyDependencyTransform[T any, CP, SP, DP fmt.Stringer] struct {
	originSpanPattern, destinationSpanPattern *traceparser.SpanPattern
	matchingDependencyTypes                   []trace.DependencyType
	durationScalingFactor                     float64

	mayShrinkIfNotOriginallyBlocking bool
	mayShrinkToOriginOffset          float64
}

// A modified-Dependency transform applied to a particular Trace.
type appliedDependencyModifications[T any, CP, SP, DP fmt.Stringer] struct {
	mdt                 *modifyDependencyTransform[T, CP, SP, DP]
	dependencySelection *trace.DependencySelection[T, CP, SP, DP]
}

// A trace-independent specification for a particular span transform
// (i.e., changing span start time or duration.)
type modifySpanTransform[T any, CP, SP, DP fmt.Stringer] struct {
	spanPattern *traceparser.SpanPattern

	hasDurationScalingFactor bool
	durationScalingFactor    float64

	hasDurationDelta    bool
	durationDeltaString string

	hasUpperDurationCap    bool
	upperDurationCapString string

	hasLowerDurationCap    bool
	lowerDurationCapString string

	startsAsEarlyAsPossible bool
}

// A modified-Span transform applied to a particular Trace.
type appliedSpanModifications[T any, CP, SP, DP fmt.Stringer] struct {
	mst           *modifySpanTransform[T, CP, SP, DP]
	spanSelection *trace.SpanSelection[T, CP, SP, DP]
}

// A trace-independent specification for a particular added-dependency
// transform.
type addDependencyTransform[T any, CP, SP, DP fmt.Stringer] struct {
	originPositionPattern, destinationPositionPattern *traceparser.PositionPattern
	dependencyType                                    trace.DependencyType
	// The scheduling delay of the added dependency.  Should be the output of
	// Comparator[T].Diff().
	schedulingDelay float64
	// The percentage through the origin at which the dependency originates.
}

// An added-Dependency transform applied to a particular Trace.
type appliedDependencyAdditions[T any, CP, SP, DP fmt.Stringer] struct {
	adt                        *addDependencyTransform[T, CP, SP, DP]
	originSpan                 trace.Span[T, CP, SP, DP]
	destinationSelection       *trace.SpanSelection[T, CP, SP, DP]
	dep                        trace.MutableDependency[T, CP, SP, DP]
	destinationsByOriginalSpan map[trace.Span[T, CP, SP, DP]]elementarySpanTransformer[T, CP, SP, DP]
}

// A trace-independent specification for a particular removed-dependency
// transform.
type removeDependencyTransform[T any, CP, SP, DP fmt.Stringer] struct {
	originSpanPattern, destinationSpanPattern *traceparser.SpanPattern
	matchingDependencyTypes                   []trace.DependencyType
}

// A removed-Dependency transform applied to a particular Trace.
type appliedDependencyRemovals[T any, CP, SP, DP fmt.Stringer] struct {
	rdt                 *removeDependencyTransform[T, CP, SP, DP]
	dependencySelection *trace.DependencySelection[T, CP, SP, DP]
}

// SpanGater instances can delay selected spans from running until arbitrary
// conditions, based on which spans are running, are satisfied.
type SpanGater[T any, CP, SP, DP fmt.Stringer] interface {
	// Notes that the specified span is starting.  The provided bool is true if
	// the span is also selected by the relevant span gating transform.
	SpanStarting(span trace.Span[T, CP, SP, DP], isSelected bool)
	// Notes that the specified span is ending.  The provided bool is true if
	// the span is also selected by the relevant span gating transform.
	SpanEnding(span trace.Span[T, CP, SP, DP], isSelected bool)
	// Returns true iff the provided span (which is selected by the relevant
	// span gating transform) is allowed to run.
	SpanCanStart(span trace.Span[T, CP, SP, DP]) bool
}

// A trace-independent specification for a particular gated-span transform.
type gatedSpanTransform[T any, CP, SP, DP fmt.Stringer] struct {
	spanPattern *traceparser.SpanPattern

	spanGaterFn func() SpanGater[T, CP, SP, DP]
}

// A gated-Span transform applied to a particular Trace.
type appliedSpanGates[T any, CP, SP, DP fmt.Stringer] struct {
	gst           *gatedSpanTransform[T, CP, SP, DP]
	gater         SpanGater[T, CP, SP, DP]
	spanSelection *trace.SpanSelection[T, CP, SP, DP]
}

// A set of transforms applied to a particular Trace.
type appliedTransforms[T any, CP, SP, DP fmt.Stringer] struct {
	// The transformations applied within this trace.
	appliedSpanModifications       []*appliedSpanModifications[T, CP, SP, DP]
	appliedSpanGates               []*appliedSpanGates[T, CP, SP, DP]
	appliedDependencyModifications []*appliedDependencyModifications[T, CP, SP, DP]
	// If nil, the added dependency is inapplicable to the trace.
	appliedDependencyAdditions []*appliedDependencyAdditions[T, CP, SP, DP]
	appliedDependencyRemovals  []*appliedDependencyRemovals[T, CP, SP, DP]
	categoryPayloadMapping     func(original, new trace.Category[T, CP, SP, DP]) CP
	spanPayloadMapping         func(original, new trace.Span[T, CP, SP, DP]) SP
}

// An added Dependency in the transforming Trace.
type addedDependency[T any, CP, SP, DP fmt.Stringer] struct {
	dependencyAdditions *appliedDependencyAdditions[T, CP, SP, DP]
	outgoingHere        bool
}

// A Dependency destination in the transforming Trace.
type destination[T any, CP, SP, DP fmt.Stringer] struct {
	original trace.ElementarySpan[T, CP, SP, DP]
	new      elementarySpanTransformer[T, CP, SP, DP]
}

// The external interface of a transformingDependency -- a Dependency in the
// process of being transformed.  If nil, the entire Dependency was removed
// from the trace.
type dependencyTransformer[T any, CP, SP, DP fmt.Stringer] interface {
	// Returns the original trace Dependency.
	original() trace.Dependency[T, CP, SP, DP]
	// Returns the Dependency's destinations (not including any deleted
	// destinations).
	destinations() []*destination[T, CP, SP, DP]
	// Returns the dependency modifications that were applied to this Dependency.
	appliedDependencyModifications() []*appliedDependencyModifications[T, CP, SP, DP]
	// Returns true if the provided original destination was deleted from the
	// Dependency.
	isOriginalDestinationDeleted(originalDestination trace.ElementarySpan[T, CP, SP, DP]) bool
	// Sets the Dependency's transformed origin.
	setOrigin(comparator trace.Comparator[T], est elementarySpanTransformer[T, CP, SP, DP]) error
	// Add a transformed destination to the Dependency.
	addDestination(
		originalDestination trace.ElementarySpan[T, CP, SP, DP],
		newDestination elementarySpanTransformer[T, CP, SP, DP],
	) error
}

// The external interface of a transformingElementarySpan -- an ElementarySpan
// in the process of being transformed.
type elementarySpanTransformer[T any, CP, SP, DP fmt.Stringer] interface {
	// Returns the original parent Span of the ElementarySpan.
	originalParent() trace.Span[T, CP, SP, DP]
	// Returns the transformed MutableElementarySpan.
	getTransformed() trace.MutableElementarySpan[T, CP, SP, DP]
	// Returns true if the ElementarySpan is the first in its Span.
	isStartOfSpan() bool
	// Returns true if the ElementarySpan is the last in its Span.
	isEndOfSpan() bool
	// Signals the ElementarySpan that all its incoming Dependencies have been
	// added (via set{Original, New}IncomingDependency()).
	allIncomingDependenciesAdded()
	// Updates the non-dependency start time: the original or adjusted span start
	// time, if the ElementarySpan is span-initial, or the end of the
	// ElementarySpan's predecessor once it's scheduled.
	updateNonDependencyStart(point T)
	// Updates the ElementarySpan's incoming dependency start time.  If the
	// dependency has multiple origins, chooseEarliest dictates whether the
	// ElementarySpan's incoming dependencies will be resolved at the earliest
	// origin's resolution or at the latest origin's resolution.
	updateIncomingDependencyResolved(point T, chooseEarliest bool)

	originalDuration() float64
	// Sets the new (i.e., transformed) duration of the elementary span.
	setNewDuration(newDuration float64)
	// Sets the ElementarySpan's outgoing dependency following the provided
	// original ElementarySpan's outgoing dependency
	setOriginalOutgoingDependency(
		originalOutgoingDependency trace.Dependency[T, CP, SP, DP],
	)
	// Sets the ElementarySpan's incoming dependency following the provided
	// original ElementarySpan's incoming dependency
	setOriginalIncomingDependency(
		originalElementarySpan trace.ElementarySpan[T, CP, SP, DP],
		originalIncomingDependency trace.Dependency[T, CP, SP, DP],
	) error
	// Sets the ElementarySpan's outgoing dependency following the provided
	// Dependency addition.
	setNewOutgoingDependency(
		outgoingADA *appliedDependencyAdditions[T, CP, SP, DP],
	) error
	// Sets the ElementarySpan's incoming dependency following the provided
	// Dependency addition.
	setNewIncomingDependency(incomingADA *appliedDependencyAdditions[T, CP, SP, DP]) error
	hasOutgoingDep() bool
	// Schedules the ElementarySpan, resolving its end time and all of its
	// outgoing Dependencies.
	schedule() error
}

// The external interface of a transformingSpan -- a Span in the process of
// being transformed.
type spanTransformer[T any, CP, SP, DP fmt.Stringer] interface {
	traceTransformer() traceTransformer[T, CP, SP, DP]
	// Returns the original Span.
	original() trace.Span[T, CP, SP, DP]
	// Returns the Span's ElementarySpans.
	elementarySpans() []elementarySpanTransformer[T, CP, SP, DP]
}

// The external interface of a transformingTrace -- a Trace in the process of
// being transformed.
type traceTransformer[T any, CP, SP, DP fmt.Stringer] interface {
	// Returns a Comparator suitable for the trace being transformed.
	comparator() trace.Comparator[T]
	// Returns the earliest moment in the original trace.
	start() T
	// Returns a Namer for the trace being transformed.
	namer() trace.Namer[T, CP, SP, DP]
	// Returns the set of transformations to be applied.
	appliedTransformations() *appliedTransforms[T, CP, SP, DP]
	// Schedules the provided transformed ElementarySpan into the trace.
	schedule(est elementarySpanTransformer[T, CP, SP, DP])
	// Returns the transforming Span corresponding to the provided original Span,
	// creating it if necessary.
	spanFromOriginal(original trace.Span[T, CP, SP, DP]) (spanTransformer[T, CP, SP, DP], error)
	// Returns the transforming Dependency corresponding to the provided original
	// Dependency, creating it if necessary.
	dependencyFromOriginal(original trace.Dependency[T, CP, SP, DP]) dependencyTransformer[T, CP, SP, DP]
	// Returns a new MutableDependency of the specified type in the transformed
	// trace.
	newMutableDependency(original trace.Dependency[T, CP, SP, DP]) trace.MutableDependency[T, CP, SP, DP]
}
