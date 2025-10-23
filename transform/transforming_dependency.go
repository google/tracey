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

type transformingDependency[T any, CP, SP, DP fmt.Stringer] struct {
	// The corresponding Dependency in the original Trace.
	originalDependency trace.Dependency[T, CP, SP, DP]
	// The transformed Trace's MutableDependency.
	new trace.MutableDependency[T, CP, SP, DP]
	// All destinations of this Dependency in the transformed Trace, with deleted
	// destinations omitted.
	destinationMappings []*destination[T, CP, SP, DP]
	// All Dependency modification transformations that apply to this Dependency.
	adms []*appliedDependencyModifications[T, CP, SP, DP]
}

func (td *transformingDependency[T, CP, SP, DP]) destinations() []*destination[T, CP, SP, DP] {
	return td.destinationMappings
}

func (td *transformingDependency[T, CP, SP, DP]) original() trace.Dependency[T, CP, SP, DP] {
	return td.originalDependency
}

func (td *transformingDependency[T, CP, SP, DP]) appliedDependencyModifications() []*appliedDependencyModifications[T, CP, SP, DP] {
	return td.adms
}

func (td *transformingDependency[T, CP, SP, DP]) isOriginalDestinationDeleted(
	originalDestination trace.ElementarySpan[T, CP, SP, DP]) bool {
	for _, dest := range td.destinationMappings {
		if dest.original == originalDestination {
			return false
		}
	}
	return true
}

func (td *transformingDependency[T, CP, SP, DP]) setOrigin(
	comparator trace.Comparator[T],
	tes elementarySpanTransformer[T, CP, SP, DP],
) error {
	return td.new.SetOriginElementarySpan(comparator, tes.getTransformed())
}

func (td *transformingDependency[T, CP, SP, DP]) addDestination(
	original trace.ElementarySpan[T, CP, SP, DP],
	tes elementarySpanTransformer[T, CP, SP, DP],
) error {
	td.new.WithDestinationElementarySpan(tes.getTransformed())
	for _, dest := range td.destinationMappings {
		if dest.original == original {
			dest.new = tes
			return nil
		}
	}
	return fmt.Errorf("failed to add dependency destination: no matching original ElementarySpan")
}

// Returns the transforming dependency corresponding to the provided original
// dependency.  Returns nil if  the provided original is nil, or if the
// original dependency was fully removed (i.e., its source and all its
// destinations matched dependency deletion selectors),
func newTransformingDependency[T any, CP, SP, DP fmt.Stringer](
	tt traceTransformer[T, CP, SP, DP],
	original trace.Dependency[T, CP, SP, DP],
) dependencyTransformer[T, CP, SP, DP] {
	ret := &transformingDependency[T, CP, SP, DP]{
		originalDependency: original,
	}
	// Apply any dependency removals.
	for _, originalDestination := range original.Destinations() {
		included := true
		for _, adr := range tt.appliedTransformations().appliedDependencyRemovals {
			if adr.dependencySelection.IncludesDependencyType(original.DependencyType()) &&
				adr.dependencySelection.IncludesOriginSpan(original.TriggeringOrigin().Span()) &&
				adr.dependencySelection.IncludesDestinationSpan(originalDestination.Span()) {
				included = false
				break
			}
		}
		if included {
			ret.destinationMappings = append(ret.destinationMappings, &destination[T, CP, SP, DP]{
				original: originalDestination,
				// new is populated by addDestination().
			})
		}
	}
	if len(ret.destinationMappings) == 0 {
		// A dependency with all its destinations removed should be nulled out.
		return nil
	}
	ret.new = tt.newMutableDependency(original)
	for _, adm := range tt.appliedTransformations().appliedDependencyModifications {
		if !adm.dependencySelection.IncludesOriginSpan(original.TriggeringOrigin().Span()) ||
			!adm.dependencySelection.IncludesDependencyType(original.DependencyType()) {
			continue
		}
		ret.adms = append(ret.adms, adm)
	}
	return ret
}
