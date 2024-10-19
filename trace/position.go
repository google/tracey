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
)

// Position refers to a particular position, or set of positions, within a
// Trace.  A position is a particular point in the unsuspended duration of a
// Span, and can be used to reference ElementarySpans in a general way (e.g.,
// 0% through span `foo` is `foo`'s first ElementarySpan; 100% through span
// `bar` is `bar`'s last ElementarySpan.)
type Position[T any, CP, SP, DP fmt.Stringer] struct {
	spanFinder      *SpanFinder[T, CP, SP, DP]
	fractionThrough float64 // A value between 0 and 1, inclusive.
}

// NewPosition returns a new Position with the specified span selection and
// fraction-through value (which should lie in the range [0.0, 1.0]).
func NewPosition[T any, CP, SP, DP fmt.Stringer](
	spanFinder *SpanFinder[T, CP, SP, DP],
	fractionThrough float64,
) *Position[T, CP, SP, DP] {
	return &Position[T, CP, SP, DP]{
		spanFinder:      spanFinder,
		fractionThrough: fractionThrough,
	}
}

// SpanFinder returns the receiver's SpanFinder.
func (p *Position[T, CP, SP, DP]) SpanFinder() *SpanFinder[T, CP, SP, DP] {
	return p.spanFinder
}

// FractionThrough returns the receiver's fraction-through value.
func (p *Position[T, CP, SP, DP]) FractionThrough() float64 {
	return p.fractionThrough
}

// Find returns a slice of all ElementarySpans in the provided Trace containing
// the receiving Position.
func (p *Position[T, CP, SP, DP]) Find(
	t Trace[T, CP, SP, DP],
) []ElementarySpan[T, CP, SP, DP] {
	spans := p.spanFinder.Find(t)
	matchingESs := []ElementarySpan[T, CP, SP, DP]{}
	for _, span := range spans {
		switch p.fractionThrough {
		case 0: // Special case 0% to the first ES
			matchingESs = append(matchingESs, span.ElementarySpans()[0])
		case 1: // Special case 100% to the last ES
			ess := span.ElementarySpans()
			matchingESs = append(matchingESs, ess[len(ess)-1])
		default:
			var runningDuration float64
			for _, es := range span.ElementarySpans() {
				runningDuration += t.Comparator().Diff(es.End(), es.Start())
			}
			targetDuration := runningDuration * p.fractionThrough
			runningDuration = 0
			for _, es := range span.ElementarySpans() {
				runningDuration += t.Comparator().Diff(es.End(), es.Start())
				if runningDuration >= targetDuration {
					matchingESs = append(matchingESs, es)
					break
				}
			}
		}
	}
	return matchingESs
}
