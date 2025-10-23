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

import "fmt"

// Mutable types used in trace transformation.  Types defined in this file
// should not be used for initial trace construction.

// MutableTrace is a variant of Trace which supports creation of
// MutableDependencies, and of MutableRootSpans with externally-managed sets of
// ElementarySpans.
type MutableTrace[T any, CP, SP, DP fmt.Stringer] interface {
	Trace[T, CP, SP, DP]

	// Creates and returns a new MutableRootSpan pre-populated with the provided
	// set of MutableElementarySpans.
	NewMutableRootSpan(
		elementarySpans []MutableElementarySpan[T, CP, SP, DP],
		payload SP,
	) (MutableRootSpan[T, CP, SP, DP], error)
	// Creates and returns a new MutableDependency of the
	// specified type.  MutableDependency extends Dependency, adding the ability
	// to link precomputed ElementarySpans.
	NewMutableDependency(dependencyType DependencyType, options ...DependencyOption) MutableDependency[T, CP, SP, DP]
}

// MutableSpan is a variant of Span which supports creation of mutable child
// spans with predefined sets of ElementarySpans.
type MutableSpan[T any, CP, SP, DP fmt.Stringer] interface {
	Span[T, CP, SP, DP]

	// Creates and returns a new MutableSpan, pre-populated with the provided
	// set of MutableElementarySpans, as a nested descendant of this MutableSpan.
	// Note that the Call/Return relationship between this child and its parent
	// is not created, but must be (or have been) explicitly applied to the
	// relevant MutableElementarySpans.
	NewMutableChildSpan(
		elementarySpans []MutableElementarySpan[T, CP, SP, DP],
		payload SP,
	) (MutableSpan[T, CP, SP, DP], error)
}

// MutableRootSpan is a variant of RootSpan which supports creation of mutable
// child spans with pre-populated sets of MutableElementarySpans.
type MutableRootSpan[T any, CP, SP, DP fmt.Stringer] interface {
	MutableSpan[T, CP, SP, DP]
	RootSpan[T, CP, SP, DP]
}

// MutableMark is a variant of Mark which supports mutation.
type MutableMark[T any] interface {
	Mark[T]

	WithLabel(string) MutableMark[T]
	WithMoment(T) MutableMark[T]
}

// MutableElementarySpan is a variant of ElementarySpan which supports external
// mutation of the span's start and end points.
type MutableElementarySpan[T any, CP, SP, DP fmt.Stringer] interface {
	ElementarySpan[T, CP, SP, DP]

	// Adds the provided marks, replacing any previous value.
	WithMarks([]MutableMark[T]) MutableElementarySpan[T, CP, SP, DP]
	// Sets the MutableElementarySpan's start point, replacing any previous
	// value.
	WithStart(start T) MutableElementarySpan[T, CP, SP, DP]
	// Sets the MutableElementarySpan's end point, replacing any previous value.
	WithEnd(end T) MutableElementarySpan[T, CP, SP, DP]
	// Sets the MutableElementarySpan's parent span, replacing any previous
	// value.
	withParentSpan(span MutableSpan[T, CP, SP, DP]) MutableElementarySpan[T, CP, SP, DP]
}

// MutableDependency is a variant of Dependency which supports origin and
// destination MutableElementarySpans to be set explicitly, rather than
// implicitly by {Span, point} pairs.
type MutableDependency[T any, CP, SP, DP fmt.Stringer] interface {
	Dependency[T, CP, SP, DP]

	// Sets the MutableDependency's payload.
	WithPayload(payload DP) MutableDependency[T, CP, SP, DP]
	// If the MutableDependency does not permit multiple origins, sets its origin
	// MutableElementarySpan to the one provided, returning an error if one
	// already exists.  Otherwise, adds the provided MutableElementarySpan to the
	// MutableDependency's origins.
	SetOriginElementarySpan(comparator Comparator[T], es MutableElementarySpan[T, CP, SP, DP]) error
	// If the MutableDependency does not permit multiple origins, sets its origin
	// MutableElementarySpan to the one provided, replacing any previous value.
	// Otherwise, adds the provided MutableElementarySpan to the
	// MutableDependency's origins.
	WithOriginElementarySpan(comparator Comparator[T], es MutableElementarySpan[T, CP, SP, DP]) MutableDependency[T, CP, SP, DP]
	// Adds the provided MutableElementarySpan to the MutableDependency's
	// destinations.
	WithDestinationElementarySpan(es MutableElementarySpan[T, CP, SP, DP]) MutableDependency[T, CP, SP, DP]
	// Replaces the origin elementary span with the provided new one.
	replaceOriginElementarySpan(original, new MutableElementarySpan[T, CP, SP, DP])
}
