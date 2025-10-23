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
	"sort"

	"github.com/google/tracey/trace"
	traceparser "github.com/google/tracey/trace/parser"
)

// Trace describes trace types that support transformation.
type Trace[T any, CP, SP, DP fmt.Stringer] interface {
	trace.Wrapper[T, CP, SP, DP]
	Transform(xf *Transform[T, CP, SP, DP]) (trace.Wrapper[T, CP, SP, DP], error)
}

// Transform bundles a set of trace-independent transformations to be
// applied to Traces.
type Transform[T any, CP, SP, DP fmt.Stringer] struct {
	modifyDependencyTransforms []*modifyDependencyTransform[T, CP, SP, DP]
	modifySpanTransforms       []*modifySpanTransform[T, CP, SP, DP]
	addDependencyTransforms    []*addDependencyTransform[T, CP, SP, DP]
	removeDependencyTransforms []*removeDependencyTransform[T, CP, SP, DP]
	gatedSpanTransforms        []*gatedSpanTransform[T, CP, SP, DP]
	categoryPayloadMapping     func(original, new trace.Category[T, CP, SP, DP]) CP
	spanPayloadMapping         func(original, new trace.Span[T, CP, SP, DP]) SP
}

// New returns a new, empty Transform.
func New[T any, CP, SP, DP fmt.Stringer]() *Transform[T, CP, SP, DP] {
	return &Transform[T, CP, SP, DP]{}
}

// WithCategoryPayloadMapping specifies a Category payload mapping function to
// be applied to the payloads of Categories in the transformed Trace.  If
// unspecified, new Trace Categories will be given the same payloads as their
// original counterparts.
func (t *Transform[T, CP, SP, DP]) WithCategoryPayloadMapping(
	categoryPayloadMapping func(original, new trace.Category[T, CP, SP, DP]) CP,
) *Transform[T, CP, SP, DP] {
	t.categoryPayloadMapping = categoryPayloadMapping
	return t
}

// WithSpanPayloadMapping specifies a Span payload mapping function to be
// applied to the payloads of Spans in the transformed Trace.  If unspecified,
// new Trace Spans will be given the same payloads as their original
// counterparts.
func (t *Transform[T, CP, SP, DP]) WithSpanPayloadMapping(
	spanPayloadMapping func(original, new trace.Span[T, CP, SP, DP]) SP,
) *Transform[T, CP, SP, DP] {
	t.spanPayloadMapping = spanPayloadMapping
	return t
}

// WithDependenciesScaledBy specifies that, during transformation, all matching
// Dependencies should have their scheduling delay (that is, the duration
// between the end of the origin ElementarySpan and the beginning of the
// destination ElementarySpan) scaled by the provided scaling factor.
// Dependencies whose origin and destination Spans match the provided matchers
// (with nil SpanMatchers matching everything) and whose type is included in
// the provided list of DependencyTypes are considered to match; if the
// provided set of DependencyTypes is empty, all DependencyTypes match.
func (t *Transform[T, CP, SP, DP]) WithDependenciesScaledBy(
	originSpanPattern, destinationSpanPattern *traceparser.SpanPattern,
	matchingDependencyTypes []trace.DependencyType,
	durationScalingFactor float64,
) *Transform[T, CP, SP, DP] {
	t.modifyDependencyTransforms = append(
		t.modifyDependencyTransforms,
		&modifyDependencyTransform[T, CP, SP, DP]{
			originSpanPattern:       originSpanPattern,
			destinationSpanPattern:  destinationSpanPattern,
			matchingDependencyTypes: matchingDependencyTypes,
			durationScalingFactor:   durationScalingFactor,
		})
	return t
}

// WithSpansScaledBy specifies that, during transformation, all Spans matching
// any of the provided matchers (with nil SpanMatchers matching everything)
// should have their non-suspended duration scaled by the provided scaling
// factor.
func (t *Transform[T, CP, SP, DP]) WithSpansScaledBy(
	spanPattern *traceparser.SpanPattern,
	durationScalingFactor float64,
) *Transform[T, CP, SP, DP] {
	t.modifySpanTransforms = append(t.modifySpanTransforms, &modifySpanTransform[T, CP, SP, DP]{
		spanPattern:              spanPattern,
		hasDurationScalingFactor: true,
		durationScalingFactor:    durationScalingFactor,
	})
	return t
}

// WithSpansCappedAboveBy specifies that, during transformation, the
// unsuspended duration of all Spans matching any of the provided matchers
// (with nil SpanMatchers matching everything) may not exceed the provided
// duration.  If a span's unsuspended duration would exceed the provided
// duration, the span is linearly scaled down to the cap.
func (t *Transform[T, CP, SP, DP]) WithSpansCappedAboveBy(
	spanPattern *traceparser.SpanPattern,
	durationCapString string,
) *Transform[T, CP, SP, DP] {
	t.modifySpanTransforms = append(t.modifySpanTransforms, &modifySpanTransform[T, CP, SP, DP]{
		spanPattern:            spanPattern,
		hasUpperDurationCap:    true,
		upperDurationCapString: durationCapString,
	})
	return t
}

// WithSpansCappedBelowBy specifies that, during transformation, the
// unsuspended duration of all Spans matching any of the provided matchers
// (with nil SpanMatchers matching everything) may not be less than the
// provided duration.  If a span's unsuspended duration would dip below the
// provided duration, the span is linearly scaled up to the cap.
func (t *Transform[T, CP, SP, DP]) WithSpansCappedBelowBy(
	spanPattern *traceparser.SpanPattern,
	durationCapString string,
) *Transform[T, CP, SP, DP] {
	t.modifySpanTransforms = append(t.modifySpanTransforms, &modifySpanTransform[T, CP, SP, DP]{
		spanPattern:            spanPattern,
		hasLowerDurationCap:    true,
		lowerDurationCapString: durationCapString,
	})
	return t
}

// WithSpanUnsuspendedDurationDelta specifies that, during transformation, all
// Spans matching any of the provided matchers (with nil SpanMatchers matching
// everything) should have the provided unsuspended duration delta added (if
// positive) or removed (if negative).  This delta is applied evenly throughout
// affected spans.  If a negative duration delta is provided, and its absolute
// value is greater than an affected span's entire unsuspended duration, the
// span is shrunk to have zero duration.
func (t *Transform[T, CP, SP, DP]) WithSpanUnsuspendedDurationDelta(
	spanPattern *traceparser.SpanPattern,
	durationDeltaString string,
) *Transform[T, CP, SP, DP] {
	t.modifySpanTransforms = append(t.modifySpanTransforms, &modifySpanTransform[T, CP, SP, DP]{
		spanPattern:         spanPattern,
		hasDurationDelta:    true,
		durationDeltaString: durationDeltaString,
	})
	return t
}

// WithSpansStartingAsEarlyAsPossible specifies that, during transformation,
// all Spans matching any of the provided matchers (with nil SpanMatchers
// matching everything) start at the specified point unless pushed back by
// later-resolving Dependencies.  Spans not affected by this transformation
// start at their original start point, unless pushed back by later-resolving
// Dependencies.
func (t *Transform[T, CP, SP, DP]) WithSpansStartingAsEarlyAsPossible(
	spanPattern *traceparser.SpanPattern,
) *Transform[T, CP, SP, DP] {
	t.modifySpanTransforms = append(t.modifySpanTransforms, &modifySpanTransform[T, CP, SP, DP]{
		spanPattern:             spanPattern,
		startsAsEarlyAsPossible: true,
	})
	return t
}

// WithShrinkableIncomingDependencies specifies that, during transformation,
// all Spans matching any of the provided matchers (with nil SpanMatchers
// matching everything) which were originally blocked by their predecessor may
// shrink their incoming dependency time up to the origin time of that
// dependency plus the provided start offset, essentially asserting that such
// dependencies' duration (minus the offset) did not represent real scheduling
// delay.  This does not apply to modified incoming dependencies.  This
// transformation can help avoid blockage inversions: when an upstream (i.e.,
// earlier) transformation shifts an ElementarySpan earlier, any long-duration
// incoming dependency that ElementarySpan might have (such as a future) should
// not artificially push that ElementarySpan's start point back.
func (t *Transform[T, CP, SP, DP]) WithShrinkableIncomingDependencies(
	destinationSpanPattern *traceparser.SpanPattern,
	dependencyTypes []trace.DependencyType,
	shrinkStartOffset float64,
) *Transform[T, CP, SP, DP] {
	t.modifyDependencyTransforms = append(t.modifyDependencyTransforms, &modifyDependencyTransform[T, CP, SP, DP]{
		originSpanPattern:                nil,
		destinationSpanPattern:           destinationSpanPattern,
		matchingDependencyTypes:          dependencyTypes,
		mayShrinkIfNotOriginallyBlocking: true,
		mayShrinkToOriginOffset:          shrinkStartOffset,
	})
	return t
}

// WithAddedDependencies specifies that, during transformation, a new
// Dependency of the specified type and with the specified scheduling delay
// should be placed between the origin position (which must be unique in the
// trace) and the destination positions.
func (t *Transform[T, CP, SP, DP]) WithAddedDependencies(
	originPositionPattern, destinationPositionPattern *traceparser.PositionPattern,
	dependencyType trace.DependencyType,
	schedulingDelay float64,
) *Transform[T, CP, SP, DP] {
	t.addDependencyTransforms = append(
		t.addDependencyTransforms,
		&addDependencyTransform[T, CP, SP, DP]{
			originPositionPattern:      originPositionPattern,
			destinationPositionPattern: destinationPositionPattern,
			dependencyType:             dependencyType,
			schedulingDelay:            schedulingDelay,
		},
	)
	return t
}

// WithRemovedDependencies specifies that, during transformation, all matching
// Dependencies should be removed.  Dependencies whose origin and destination
// Spans match the provided matchers (with nil SpanMatchers matching
// everything) and whose type is included in the provided list of
// DependencyTypes are considered to match; if the provided set of
// DependencyTypes is empty, all DependencyTypes match.
func (t *Transform[T, CP, SP, DP]) WithRemovedDependencies(
	originSpanPattern, destinationSpanPattern *traceparser.SpanPattern,
	matchingDependencyTypes []trace.DependencyType,
) *Transform[T, CP, SP, DP] {
	t.removeDependencyTransforms = append(
		t.removeDependencyTransforms,
		&removeDependencyTransform[T, CP, SP, DP]{
			originSpanPattern:       originSpanPattern,
			destinationSpanPattern:  destinationSpanPattern,
			matchingDependencyTypes: matchingDependencyTypes,
		},
	)
	return t
}

// WithSpansGatedBy specifies that, during transformation, all matching Spans
// (with nil SpanMatchers matching everything) may only start when particular
// conditions over the Trace's currently-running Spans, as determined by a
// SpanGater implementation, are satisfied.  The provided function should
// return a new SpanGater instance; each transformed Trace will get its own
// SpanGater instance.  This can be used to apply arbitrary concurrency
// constraints to a transformed Trace.
func (t *Transform[T, CP, SP, DP]) WithSpansGatedBy(
	spanPattern *traceparser.SpanPattern,
	spanGaterFn func() SpanGater[T, CP, SP, DP],
) *Transform[T, CP, SP, DP] {
	t.gatedSpanTransforms = append(t.gatedSpanTransforms, &gatedSpanTransform[T, CP, SP, DP]{
		spanPattern: spanPattern,
		spanGaterFn: spanGaterFn,
	})
	return t
}

// TransformTrace transforms the provided trace per the receiver's
// transformations, returning a new, transformed trace.
func (t *Transform[T, CP, SP, DP]) TransformTrace(
	original trace.Trace[T, CP, SP, DP],
) (trace.Trace[T, CP, SP, DP], error) {
	at, err := t.apply(original)
	if err != nil {
		return nil, err
	}
	return transformTrace(original, at)
}

// Returns an appliedTransforms specifying the receiver to the provided
// Trace and Namer.
func (t *Transform[T, CP, SP, DP]) apply(
	original trace.Trace[T, CP, SP, DP],
) (*appliedTransforms[T, CP, SP, DP], error) {
	ret := &appliedTransforms[T, CP, SP, DP]{
		categoryPayloadMapping: t.categoryPayloadMapping,
		spanPayloadMapping:     t.spanPayloadMapping,
	}
	ret.appliedSpanModifications = make([]*appliedSpanModifications[T, CP, SP, DP], len(t.modifySpanTransforms))
	for idx, mst := range t.modifySpanTransforms {
		ms, err := mst.selectModifiedSpans(original)
		if err != nil {
			return nil, err
		}
		ret.appliedSpanModifications[idx] = ms
	}
	ret.appliedSpanGates = make([]*appliedSpanGates[T, CP, SP, DP], len(t.gatedSpanTransforms))
	for idx, gst := range t.gatedSpanTransforms {
		gs, err := gst.selectGatedSpans(original)
		if err != nil {
			return nil, err
		}
		ret.appliedSpanGates[idx] = gs
	}
	ret.appliedDependencyModifications = make([]*appliedDependencyModifications[T, CP, SP, DP], len(t.modifyDependencyTransforms))
	for idx, mdt := range t.modifyDependencyTransforms {
		md, err := mdt.selectModifiedDependencies(original)
		if err != nil {
			return nil, err
		}
		ret.appliedDependencyModifications[idx] = md
	}
	ret.appliedDependencyAdditions = make([]*appliedDependencyAdditions[T, CP, SP, DP], len(t.addDependencyTransforms))
	for idx, adt := range t.addDependencyTransforms {
		ad, err := adt.selectAddedDependencies(original)
		if err != nil {
			return nil, err
		}
		ret.appliedDependencyAdditions[idx] = ad
	}
	ret.appliedDependencyRemovals = make([]*appliedDependencyRemovals[T, CP, SP, DP], len(t.removeDependencyTransforms))
	for idx, rdt := range t.removeDependencyTransforms {
		rd, err := rdt.selectRemovedDependencies(original)
		if err != nil {
			return nil, err
		}
		ret.appliedDependencyRemovals[idx] = rd
	}
	return ret, nil
}

// Applies the receiver to a particular trace and namer, returning a
// dependencyModifications instance specific to that that trace.
func (mdt *modifyDependencyTransform[T, CP, SP, DP]) selectModifiedDependencies(
	t trace.Trace[T, CP, SP, DP],
) (*appliedDependencyModifications[T, CP, SP, DP], error) {
	originSpanFinder, err := traceparser.NewSpanFinder(mdt.originSpanPattern, t)
	if err != nil {
		return nil, err
	}
	destinationSpanFinder, err := traceparser.NewSpanFinder(mdt.destinationSpanPattern, t)
	if err != nil {
		return nil, err
	}
	dependencySelection := trace.SelectDependencies(
		t, originSpanFinder, destinationSpanFinder, mdt.matchingDependencyTypes...,
	)
	return &appliedDependencyModifications[T, CP, SP, DP]{
		mdt:                 mdt,
		dependencySelection: dependencySelection,
	}, nil
}

// Applies the receiver to a particular trace and namer, returning a
// spanModifications instance specific to that that trace.
func (mst *modifySpanTransform[T, CP, SP, DP]) selectModifiedSpans(
	t trace.Trace[T, CP, SP, DP],
) (*appliedSpanModifications[T, CP, SP, DP], error) {
	spanFinder, err := traceparser.NewSpanFinder(mst.spanPattern, t)
	if err != nil {
		return nil, err
	}
	spanSelection := trace.SelectSpans(spanFinder)
	return &appliedSpanModifications[T, CP, SP, DP]{
		mst:           mst,
		spanSelection: spanSelection,
	}, nil
}

// Applies the receiver to a particular trace and namer, returning a
// dependencyAdditions instance specific to that that trace.
func (adt *addDependencyTransform[T, CP, SP, DP]) selectAddedDependencies(
	t trace.Trace[T, CP, SP, DP],
) (*appliedDependencyAdditions[T, CP, SP, DP], error) {
	originSpanFinder, err := traceparser.SpanFinderFromPosition(adt.originPositionPattern, t)
	if err != nil {
		return nil, err
	}
	originSpans := trace.SelectSpans(originSpanFinder).Spans()
	if len(originSpans) == 0 || len(originSpans) > 1 {
		return nil, fmt.Errorf("at most one origin span must be designated when adding dependencies (got %d)", len(originSpans))
	}
	destinationSpanFinder, err := traceparser.SpanFinderFromPosition(adt.destinationPositionPattern, t)
	if err != nil {
		return nil, err
	}
	destinationSelection := trace.SelectSpans(destinationSpanFinder)
	if len(destinationSelection.Spans()) == 0 {
		return nil, fmt.Errorf("at least one destination span must be designated when adding dependencies")
	}
	ret := &appliedDependencyAdditions[T, CP, SP, DP]{
		adt:                        adt,
		originSpan:                 originSpans[0],
		destinationSelection:       destinationSelection,
		destinationsByOriginalSpan: map[trace.Span[T, CP, SP, DP]]elementarySpanTransformer[T, CP, SP, DP]{},
	}
	return ret, nil
}

// Applies the receiver to a particular trace and namer, returning a
// dependencyRemovals instance specific to that that trace.
func (rdt *removeDependencyTransform[T, CP, SP, DP]) selectRemovedDependencies(
	t trace.Trace[T, CP, SP, DP],
) (*appliedDependencyRemovals[T, CP, SP, DP], error) {
	originSpanFinder, err := traceparser.NewSpanFinder(rdt.originSpanPattern, t)
	if err != nil {
		return nil, err
	}
	destinationSpanFinder, err := traceparser.NewSpanFinder(rdt.destinationSpanPattern, t)
	if err != nil {
		return nil, err
	}
	dependencySelection := trace.SelectDependencies(
		t, originSpanFinder, destinationSpanFinder, rdt.matchingDependencyTypes...,
	)
	return &appliedDependencyRemovals[T, CP, SP, DP]{
		rdt:                 rdt,
		dependencySelection: dependencySelection,
	}, nil
}

// Applies the receiver to a particular trace and namer, returning a
// spanGates instance specific to that that trace.
func (gst *gatedSpanTransform[T, CP, SP, DP]) selectGatedSpans(
	t trace.Trace[T, CP, SP, DP],
) (*appliedSpanGates[T, CP, SP, DP], error) {
	spanFinder, err := traceparser.NewSpanFinder(gst.spanPattern, t)
	if err != nil {
		return nil, err
	}
	spanSelection := trace.SelectSpans(spanFinder)
	return &appliedSpanGates[T, CP, SP, DP]{
		gst:           gst,
		gater:         gst.spanGaterFn(),
		spanSelection: spanSelection,
	}, nil
}
func (at *appliedTransforms[T, CP, SP, DP]) findDependencyAdditionsByOriginalOriginSpan(
	originalOrigin trace.Span[T, CP, SP, DP],
) []*appliedDependencyAdditions[T, CP, SP, DP] {
	var ret []*appliedDependencyAdditions[T, CP, SP, DP]
	for _, dependencyAddition := range at.appliedDependencyAdditions {
		if dependencyAddition != nil {
			if dependencyAddition.originSpan == originalOrigin {
				ret = append(ret, dependencyAddition)
			}
		}
	}
	return ret
}
func (at *appliedTransforms[T, CP, SP, DP]) findDependencyAdditionsByOriginalDestinationSpan(
	originalDestination trace.Span[T, CP, SP, DP],
) []*appliedDependencyAdditions[T, CP, SP, DP] {
	var ret []*appliedDependencyAdditions[T, CP, SP, DP]
	for _, dependencyAddition := range at.appliedDependencyAdditions {
		if dependencyAddition != nil {
			if dependencyAddition.destinationSelection.Includes(originalDestination) {
				ret = append(ret, dependencyAddition)
			}
		}
	}
	return ret
}
func (ad *addedDependency[T, CP, SP, DP]) originalMoment(
	comparator trace.Comparator[T],
	originalSpan trace.Span[T, CP, SP, DP]) (moment T, found bool) {
	if ad.outgoingHere {
		esps := trace.FindPositionInSpan(
			ad.dependencyAdditions.adt.originPositionPattern.PositionPattern(),
			comparator,
			originalSpan,
		)
		if len(esps) == 0 {
			return moment, false
		}
		return esps[0].At, true
	}
	esps := trace.FindPositionInSpan(
		ad.dependencyAdditions.adt.destinationPositionPattern.PositionPattern(),
		comparator,
		originalSpan,
	)
	if len(esps) == 0 {
		return moment, false
	}
	return esps[0].At, true
}
func (at *appliedTransforms[T, CP, SP, DP]) getAddedDependencies(
	comparator trace.Comparator[T],
	original trace.Span[T, CP, SP, DP],
) []*addedDependency[T, CP, SP, DP] {
	dependencyAdditionsByOriginalOriginSpan := at.findDependencyAdditionsByOriginalOriginSpan(original)
	dependencyAdditionsByOriginalDestinationSpan := at.findDependencyAdditionsByOriginalDestinationSpan(original)
	ret := make(
		[]*addedDependency[T, CP, SP, DP],
		0,
		len(dependencyAdditionsByOriginalOriginSpan)+len(dependencyAdditionsByOriginalDestinationSpan),
	)
	for _, da := range dependencyAdditionsByOriginalOriginSpan {
		ret = append(ret, &addedDependency[T, CP, SP, DP]{
			dependencyAdditions: da,
			outgoingHere:        true,
		})
	}
	for _, da := range dependencyAdditionsByOriginalDestinationSpan {
		ret = append(ret, &addedDependency[T, CP, SP, DP]{
			dependencyAdditions: da,
			outgoingHere:        false,
		})
	}
	sort.Slice(ret, func(a, b int) bool {
		aMoment, foundA := ret[a].originalMoment(comparator, original)
		bMoment, foundB := ret[b].originalMoment(comparator, original)
		switch {
		case foundA && foundB:
			if comparator.Equal(aMoment, bMoment) {
				// If there's a tie, make sure the outgoing change is emitted first.
				// This should avoid excess zero-width elementary spans.
				if ret[a].outgoingHere {
					return false
				}
				return true
			}
			return comparator.Less(aMoment, bMoment)
		case foundA && !foundB:
			return false
		default:
			return true
		}
	})
	return ret
}
func (at *appliedTransforms[T, CP, SP, DP]) getSpanModifications(
	original trace.Span[T, CP, SP, DP],
) []*modifySpanTransform[T, CP, SP, DP] {
	var ret []*modifySpanTransform[T, CP, SP, DP]
	for _, asm := range at.appliedSpanModifications {
		if asm.spanSelection.Includes(original) {
			ret = append(ret, asm.mst)
		}
	}
	return ret
}
func (at *appliedTransforms[T, CP, SP, DP]) getAppliedDependencyModificationsForOriginatingSpan(
	originalOriginatingSpan trace.Span[T, CP, SP, DP],
) []*appliedDependencyModifications[T, CP, SP, DP] {
	var ret []*appliedDependencyModifications[T, CP, SP, DP]
	for _, adm := range at.appliedDependencyModifications {
		if adm.dependencySelection.IncludesOriginSpan(originalOriginatingSpan) {
			ret = append(ret, adm)
		}
	}
	return ret
}
func (at *appliedTransforms[T, CP, SP, DP]) getAppliedDependencyModificationsForDestinationSpan(
	originalDestinationSpan trace.Span[T, CP, SP, DP],
) []*appliedDependencyModifications[T, CP, SP, DP] {
	var ret []*appliedDependencyModifications[T, CP, SP, DP]
	for _, adm := range at.appliedDependencyModifications {
		if adm.dependencySelection.IncludesDestinationSpan(originalDestinationSpan) {
			ret = append(ret, adm)
		}
	}
	return ret
}

type concurrencyLimiter[T any, CP, SP, DP fmt.Stringer] struct {
	allowedConcurrency, currentConcurrency int
}

// NewConcurrencyLimiter returns a function which returns a SpanGater limiting
// participating spans to the specified allowed concurrency.
func NewConcurrencyLimiter[T any, CP, SP, DP fmt.Stringer](allowedConcurrency int) func() SpanGater[T, CP, SP, DP] {
	return func() SpanGater[T, CP, SP, DP] {
		return &concurrencyLimiter[T, CP, SP, DP]{
			allowedConcurrency: allowedConcurrency,
		}
	}
}
func (cl *concurrencyLimiter[T, CP, SP, DP]) SpanStarting(span trace.Span[T, CP, SP, DP], isSelected bool) {
	if isSelected {
		cl.currentConcurrency++
	}
}
func (cl *concurrencyLimiter[T, CP, SP, DP]) SpanEnding(span trace.Span[T, CP, SP, DP], isSelected bool) {
	if isSelected {
		cl.currentConcurrency--
	}
}
func (cl *concurrencyLimiter[T, CP, SP, DP]) SpanCanStart(span trace.Span[T, CP, SP, DP]) bool {
	return cl.currentConcurrency < cl.allowedConcurrency
}
