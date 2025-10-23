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
	"regexp"
)

// MultiplePositionPolicy defines the policy to use when a position specifier
// matches multiple positions within a trace.
type MultiplePositionPolicy int

const (
	// AllMatchingPositions specifies that all matching positions are found.
	AllMatchingPositions MultiplePositionPolicy = iota
	// EarliestMatchingPosition specifies that the earliest matching position
	// should be found.  If multiple matching positions tie for earliest, the
	// selected one is arbitrary.
	EarliestMatchingPosition
	// LatestMatchingPosition specifies that the latest matching position should
	// be found.  If multiple matching positions tie for earliest, the selected
	// one is arbitrary.
	LatestMatchingPosition
)

// PositionPattern specifies a pattern matching a particular position, or set
// of positions, within Traces.  A position is a particular point in the
// unsuspended duration of a Span, and can be used to reference ElementarySpans
// in a general way (e.g., 0% through span `foo` is `foo`'s first
// ElementarySpan; 100% through span `bar` is `bar`'s last ElementarySpan.)
type PositionPattern struct {
	markRE                 *regexp.Regexp
	fractionThrough        float64 // A value between 0 and 1, inclusive.
	multiplePositionPolicy MultiplePositionPolicy
}

// NewSpanFractionPositionPattern returns a new PositionPattern with the
// specified span selection and fraction-through value (which should lie in the
// range [0.0, 1.0]).
func NewSpanFractionPositionPattern(
	fractionThrough float64,
	multiplePositionPolicy MultiplePositionPolicy,
) *PositionPattern {
	return &PositionPattern{
		fractionThrough:        fractionThrough,
		multiplePositionPolicy: multiplePositionPolicy,
	}
}

// NewSpanMarkPositionPattern returns a new PositionPattern with the specified
// span selection and fraction-through value (which should lie in the range
// [0.0, 1.0]).
func NewSpanMarkPositionPattern(
	markRegexStr string,
	multiplePositionPolicy MultiplePositionPolicy,
) (*PositionPattern, error) {
	markRE, err := regexp.Compile(markRegexStr)
	if err != nil {
		return nil, fmt.Errorf("can't produce span mark position: %w", err)
	}
	return &PositionPattern{
		markRE:                 markRE,
		multiplePositionPolicy: multiplePositionPolicy,
	}, nil
}

// SpanFraction returns the receiver's span fraction, or false if its moment is
// not determined by span fraction.
func (pp *PositionPattern) SpanFraction() (float64, bool) {
	if pp.markRE != nil {
		return 0, false
	}
	return pp.fractionThrough, true
}

// MarkRegexp returns the receiver's mark regexp, or false if its moment is
// not determined by mark regexp.
func (pp *PositionPattern) MarkRegexp() (string, bool) {
	if pp.markRE != nil {
		return pp.markRE.String(), true
	}
	return "", false
}

// Returns the earliest position in the provided slice, or nil if the slice is
// empty.
func findEarliestPosition[T any, CP, SP, DP fmt.Stringer](
	comparator Comparator[T],
	positions []*ElementarySpanPosition[T, CP, SP, DP],
) (ret *ElementarySpanPosition[T, CP, SP, DP]) {
	for _, position := range positions {
		if ret == nil || comparator.Less(position.At, ret.At) {
			ret = position
		}
	}
	return ret
}

// Returns the latest position in the provided slice, or nil if the slice is
// empty.
func findLatestPosition[T any, CP, SP, DP fmt.Stringer](
	comparator Comparator[T],
	positions []*ElementarySpanPosition[T, CP, SP, DP],
) (ret *ElementarySpanPosition[T, CP, SP, DP]) {
	for _, position := range positions {
		if ret == nil || comparator.Greater(position.At, ret.At) {
			ret = position
		}
	}
	return ret
}

func applyMultiplePositionPolicy[T any, CP, SP, DP fmt.Stringer](
	comparator Comparator[T],
	positions []*ElementarySpanPosition[T, CP, SP, DP],
	multiplePositionPolicy MultiplePositionPolicy,
) []*ElementarySpanPosition[T, CP, SP, DP] {
	switch multiplePositionPolicy {
	case AllMatchingPositions:
		return positions
	case EarliestMatchingPosition:
		earliest := findEarliestPosition(comparator, positions)
		if earliest != nil {
			return []*ElementarySpanPosition[T, CP, SP, DP]{earliest}
		}
	case LatestMatchingPosition:
		latest := findLatestPosition(comparator, positions)
		if latest != nil {
			return []*ElementarySpanPosition[T, CP, SP, DP]{latest}
		}
	default:
	}
	return nil
}

// FindPositionInSpan returns all ElementarySpanPositions matching the receiver
// in the provided Span.
func FindPositionInSpan[T any, CP, SP, DP fmt.Stringer](
	pp *PositionPattern,
	comparator Comparator[T],
	span Span[T, CP, SP, DP],
) []*ElementarySpanPosition[T, CP, SP, DP] {
	if pp.markRE != nil {
		var matchingPositions []*ElementarySpanPosition[T, CP, SP, DP]
		// Find the position as a mark.
		for _, es := range span.ElementarySpans() {
			for _, mark := range es.Marks() {
				if pp.markRE.MatchString(mark.Label()) {
					matchingPositions = append(matchingPositions, &ElementarySpanPosition[T, CP, SP, DP]{
						ElementarySpan: es,
						At:             mark.Moment(),
					})
				}
			}
		}
		return applyMultiplePositionPolicy(comparator, matchingPositions, pp.multiplePositionPolicy)
	}
	// Find the position as a fraction.
	switch pp.fractionThrough {
	case 0: // Special case 0% to the first ES
		es := span.ElementarySpans()[0]
		return []*ElementarySpanPosition[T, CP, SP, DP]{
			&ElementarySpanPosition[T, CP, SP, DP]{
				ElementarySpan: es,
				At:             es.Start(),
			},
		}
	case 1: // Special case 100% to the last ES
		ess := span.ElementarySpans()
		es := ess[len(ess)-1]
		return []*ElementarySpanPosition[T, CP, SP, DP]{
			&ElementarySpanPosition[T, CP, SP, DP]{
				ElementarySpan: es,
				At:             es.End(),
			},
		}
	default:
		var runningDuration float64
		for _, es := range span.ElementarySpans() {
			runningDuration += comparator.Diff(es.End(), es.Start())
		}
		targetDuration := runningDuration * pp.fractionThrough
		runningDuration = 0
		for _, es := range span.ElementarySpans() {
			nextRunningDuration := runningDuration + comparator.Diff(es.End(), es.Start())
			if nextRunningDuration >= targetDuration {
				return []*ElementarySpanPosition[T, CP, SP, DP]{
					&ElementarySpanPosition[T, CP, SP, DP]{
						ElementarySpan: es,
						At:             comparator.Add(es.Start(), targetDuration-runningDuration),
					},
				}
			}
			runningDuration = nextRunningDuration
		}
	}
	return nil
}

// ElementarySpanPosition couples an ElementarySpan with a point within it.
type ElementarySpanPosition[T any, CP, SP, DP fmt.Stringer] struct {
	ElementarySpan ElementarySpan[T, CP, SP, DP]
	At             T
}

// PositionFinder instances can return slices of ElementarySpanPositions from
// a particular Trace that match some selection criteria.
type PositionFinder[T any, CP, SP, DP fmt.Stringer] interface {
	SpanFinder[T, CP, SP, DP]
	FindPositions() []*ElementarySpanPosition[T, CP, SP, DP]
}

type positionFinder[T any, CP, SP, DP fmt.Stringer] struct {
	SpanFinder[T, CP, SP, DP]
	positionPattern *PositionPattern
}

func (pf *positionFinder[T, CP, SP, DP]) FindPositions() []*ElementarySpanPosition[T, CP, SP, DP] {
	var matchingESs []*ElementarySpanPosition[T, CP, SP, DP]
	for _, span := range pf.SpanFinder.FindSpans() {
		matchingESs = append(matchingESs, FindPositionInSpan(pf.positionPattern, pf.Comparator(), span)...)
	}
	return applyMultiplePositionPolicy(pf.Comparator(), matchingESs, pf.positionPattern.multiplePositionPolicy)
}

// FindSpans on positionFinder returns only the spans containing the positions
// found by the receiver.
func (pf *positionFinder[T, CP, SP, DP]) FindSpans() []Span[T, CP, SP, DP] {
	positions := pf.FindPositions()
	var ret []Span[T, CP, SP, DP]
	var lastSpan Span[T, CP, SP, DP]
	for _, pos := range positions {
		if pos.ElementarySpan.Span() != lastSpan {
			lastSpan = pos.ElementarySpan.Span()
			ret = append(ret, lastSpan)
		}
	}
	return ret
}

// FindCategories on positionFinder returns nothing.
func (pf *positionFinder[T, CP, SP, DP]) FindCategories(
	opts ...FindCategoryOption,
) []Category[T, CP, SP, DP] {
	return nil
}

// NewPositionFinder returns a new PositionFinder using the provided
// PositionPattern over the provided SpanFinder.
func NewPositionFinder[T any, CP, SP, DP fmt.Stringer](
	positionPattern *PositionPattern,
	spanFinder SpanFinder[T, CP, SP, DP],
) PositionFinder[T, CP, SP, DP] {
	return &positionFinder[T, CP, SP, DP]{
		SpanFinder:      spanFinder,
		positionPattern: positionPattern,
	}
}
