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

package criticalpath

import (
	"github.com/google/tracey/trace"
)

// Strategies specifies a set of critical path strategies, along with their
// metadata, supported by a particular trace.
type Strategies = trace.TypeEnumeration[Strategy]

// NewStrategies creates and returns a new, empty Strategies instance.
func NewStrategies() *Strategies {
	return trace.NewTypeEnumeration[Strategy]()
}

// CommonStrategies defines critical path strategies that are often used in various traces.
var CommonStrategies = NewStrategies().
	With(PreferMostWork, "most_work", "Maximize work").
	WithDescriptionAliases("Maximize work", "Maximize work (can be slow!)").
	With(PreferTemporalMostWork, "temporal_most_work", "Temporal Max work (non causal)").
	With(PreferMostProximate, "most_prox", "Traverse latest-resolving dependencies").
	With(PreferPredecessor, "predecessor", "Prefer traversing sequential dependencies").
	With(PreferLeastWork, "least_work", "Maximize dependency delay").
	WithDescriptionAliases("Maximize dependency delay", "Maximize dependency delay (can be slow!)").
	With(PreferLeastProximate, "least_prox", "Traverse earliest-resolving dependencies").
	With(PreferCausal, "causal", "Prefer traversing causal dependencies")
