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
)

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
	originSpanFinder, destinationSpanFinder *trace.SpanFinder[T, CP, SP, DP],
	matchingDependencyTypes []trace.DependencyType,
	durationScalingFactor float64,
) *Transform[T, CP, SP, DP] {
	t.modifyDependencyTransforms = append(
		t.modifyDependencyTransforms,
		&modifyDependencyTransform[T, CP, SP, DP]{
			originSpanFinder:        originSpanFinder,
			destinationSpanFinder:   destinationSpanFinder,
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
	spanFinder *trace.SpanFinder[T, CP, SP, DP],
	durationScalingFactor float64,
) *Transform[T, CP, SP, DP] {
	t.modifySpanTransforms = append(t.modifySpanTransforms, &modifySpanTransform[T, CP, SP, DP]{
		spanFinder:               spanFinder,
		hasDurationScalingFactor: true,
		durationScalingFactor:    durationScalingFactor,
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
	spanFinder *trace.SpanFinder[T, CP, SP, DP],
) *Transform[T, CP, SP, DP] {
	t.modifySpanTransforms = append(t.modifySpanTransforms, &modifySpanTransform[T, CP, SP, DP]{
		spanFinder:              spanFinder,
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
	destinationSpanFinder *trace.SpanFinder[T, CP, SP, DP],
	dependencyTypes []trace.DependencyType,
	shrinkStartOffset float64,
) *Transform[T, CP, SP, DP] {
	t.modifyDependencyTransforms = append(t.modifyDependencyTransforms, &modifyDependencyTransform[T, CP, SP, DP]{
		originSpanFinder:                 nil,
		destinationSpanFinder:            destinationSpanFinder,
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
	originPosition, destinationPosition *trace.Position[T, CP, SP, DP],
	dependencyType trace.DependencyType,
	schedulingDelay float64,
) *Transform[T, CP, SP, DP] {
	t.addDependencyTransforms = append(
		t.addDependencyTransforms,
		&addDependencyTransform[T, CP, SP, DP]{
			originPosition:      originPosition,
			destinationPosition: destinationPosition,
			dependencyType:      dependencyType,
			schedulingDelay:     schedulingDelay,
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
	originSpanFinder, destinationSpanFinder *trace.SpanFinder[T, CP, SP, DP],
	matchingDependencyTypes []trace.DependencyType,
) *Transform[T, CP, SP, DP] {
	t.removeDependencyTransforms = append(
		t.removeDependencyTransforms,
		&removeDependencyTransform[T, CP, SP, DP]{
			originSpanFinder:        originSpanFinder,
			destinationSpanFinder:   destinationSpanFinder,
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
	spanFinder *trace.SpanFinder[T, CP, SP, DP],
	spanGaterFn func() SpanGater[T, CP, SP, DP],
) *Transform[T, CP, SP, DP] {
	t.gatedSpanTransforms = append(t.gatedSpanTransforms, &gatedSpanTransform[T, CP, SP, DP]{
		spanFinder:  spanFinder,
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
		ms := mst.selectModifiedSpans(original)
		ret.appliedSpanModifications[idx] = ms
	}
	ret.appliedSpanGates = make([]*appliedSpanGates[T, CP, SP, DP], len(t.gatedSpanTransforms))
	for idx, gst := range t.gatedSpanTransforms {
		gs := gst.selectGatedSpans(original)
		ret.appliedSpanGates[idx] = gs
	}
	ret.appliedDependencyModifications = make([]*appliedDependencyModifications[T, CP, SP, DP], len(t.modifyDependencyTransforms))
	for idx, mdt := range t.modifyDependencyTransforms {
		md := mdt.selectModifiedDependencies(original)
		ret.appliedDependencyModifications[idx] = md
	}
	ret.appliedDependencyAdditions = make([]*appliedDependencyAdditions[T, CP, SP, DP], len(t.addDependencyTransforms))
	for idx, adt := range t.addDependencyTransforms {
		ad := adt.selectAddedDependencies(original)
		ret.appliedDependencyAdditions[idx] = ad
	}
	ret.appliedDependencyRemovals = make([]*appliedDependencyRemovals[T, CP, SP, DP], len(t.removeDependencyTransforms))
	for idx, rdt := range t.removeDependencyTransforms {
		rd := rdt.selectRemovedDependencies(original)
		ret.appliedDependencyRemovals[idx] = rd
	}
	return ret, nil
}

// Applies the receiver to a particular trace and namer, returning a
// dependencyModifications instance specific to that that trace.
func (mdt *modifyDependencyTransform[T, CP, SP, DP]) selectModifiedDependencies(
	t trace.Trace[T, CP, SP, DP],
) *appliedDependencyModifications[T, CP, SP, DP] {
	dependencySelection := trace.SelectDependencies(
		t,
		mdt.originSpanFinder,
		mdt.destinationSpanFinder,
		mdt.matchingDependencyTypes...,
	)
	return &appliedDependencyModifications[T, CP, SP, DP]{
		mdt:                 mdt,
		dependencySelection: dependencySelection,
	}
}

// Applies the receiver to a particular trace and namer, returning a
// spanModifications instance specific to that that trace.
func (mst *modifySpanTransform[T, CP, SP, DP]) selectModifiedSpans(
	t trace.Trace[T, CP, SP, DP],
) *appliedSpanModifications[T, CP, SP, DP] {
	spanSelection := trace.SelectSpans(
		t,
		mst.spanFinder,
	)
	return &appliedSpanModifications[T, CP, SP, DP]{
		mst:           mst,
		spanSelection: spanSelection,
	}
}

// Applies the receiver to a particular trace and namer, returning a
// dependencyAdditions instance specific to that that trace.
func (adt *addDependencyTransform[T, CP, SP, DP]) selectAddedDependencies(
	t trace.Trace[T, CP, SP, DP],
) *appliedDependencyAdditions[T, CP, SP, DP] {
	originSpans := trace.SelectSpans(t, adt.originPosition.SpanFinder()).Spans()
	if len(originSpans) == 0 || len(originSpans) > 1 {
		return nil
	}
	destinationSpans := trace.SelectSpans(t, adt.destinationPosition.SpanFinder())
	if len(destinationSpans.Spans()) == 0 {
		return nil
	}
	ret := &appliedDependencyAdditions[T, CP, SP, DP]{
		adt:                        adt,
		originSpan:                 originSpans[0],
		selectedDestinationSpans:   destinationSpans,
		destinationsByOriginalSpan: map[trace.Span[T, CP, SP, DP]]elementarySpanTransformer[T, CP, SP, DP]{},
	}
	return ret
}

// Applies the receiver to a particular trace and namer, returning a
// dependencyRemovals instance specific to that that trace.
func (rdt *removeDependencyTransform[T, CP, SP, DP]) selectRemovedDependencies(
	t trace.Trace[T, CP, SP, DP],
) *appliedDependencyRemovals[T, CP, SP, DP] {
	dependencySelection := trace.SelectDependencies(
		t,
		rdt.originSpanFinder,
		rdt.destinationSpanFinder,
		rdt.matchingDependencyTypes...,
	)
	return &appliedDependencyRemovals[T, CP, SP, DP]{
		rdt:                 rdt,
		dependencySelection: dependencySelection,
	}
}

// Applies the receiver to a particular trace and namer, returning a
// spanGates instance specific to that that trace.
func (gst *gatedSpanTransform[T, CP, SP, DP]) selectGatedSpans(
	t trace.Trace[T, CP, SP, DP],
) *appliedSpanGates[T, CP, SP, DP] {
	spanSelection := trace.SelectSpans(
		t,
		gst.spanFinder,
	)
	return &appliedSpanGates[T, CP, SP, DP]{
		gst:           gst,
		gater:         gst.spanGaterFn(),
		spanSelection: spanSelection,
	}
}

func (at *appliedTransforms[T, CP, SP, DP]) findDependencyAdditionsByOriginalOriginSpan(
	originalOrigin trace.Span[T, CP, SP, DP],
) []*appliedDependencyAdditions[T, CP, SP, DP] {
	ret := []*appliedDependencyAdditions[T, CP, SP, DP]{}
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
	ret := []*appliedDependencyAdditions[T, CP, SP, DP]{}
	for _, dependencyAddition := range at.appliedDependencyAdditions {
		if dependencyAddition != nil {
			if dependencyAddition.selectedDestinationSpans.Includes(originalDestination) {
				ret = append(ret, dependencyAddition)
			}
		}
	}
	return ret
}

func (ad *addedDependency[T, CP, SP, DP]) fractionThrough() float64 {
	if ad.outgoingHere {
		return ad.dependencyAdditions.adt.originPosition.FractionThrough()
	}
	return ad.dependencyAdditions.adt.destinationPosition.FractionThrough()
}

func (at *appliedTransforms[T, CP, SP, DP]) getAddedDependencies(
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
		aFractionThrough := ret[a].fractionThrough()
		bFractionThrough := ret[b].fractionThrough()
		if aFractionThrough == bFractionThrough {
			// If there's a tie, make sure the outgoing change is emitted first.
			// This should avoid excess zero-width elementary spans.
			if ret[a].outgoingHere {
				return false
			}
			return true
		}
		return ret[a].fractionThrough() < ret[b].fractionThrough()
	})
	return ret
}

func (at *appliedTransforms[T, CP, SP, DP]) getSpanModifications(
	original trace.Span[T, CP, SP, DP],
) []*modifySpanTransform[T, CP, SP, DP] {
	ret := []*modifySpanTransform[T, CP, SP, DP]{}
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
	ret := []*appliedDependencyModifications[T, CP, SP, DP]{}
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
	ret := []*appliedDependencyModifications[T, CP, SP, DP]{}
	for _, adm := range at.appliedDependencyModifications {
		if adm.dependencySelection.IncludesDestinationSpan(originalDestinationSpan) {
			ret = append(ret, adm)
		}
	}
	return ret
}
