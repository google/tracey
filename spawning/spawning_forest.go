/*
	Copyright 2025 Google Inc.

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

// Package spawning provides types and functions for working with spawning
// relationships within traces.
package spawning

import (
	"fmt"

	"github.com/google/tracey/trace"
)

// Forest describes the spawning structure (that is, spawning edges between
// spawning Spans and their spawned RootSpans) of a given trace.  A rootSpan is
// spawned when it is the destination of a valid spawning dependency.  Only
// RootSpans may be spawned.
type Forest[T any, CP, SP, DP fmt.Stringer] struct {
	// All RootSpans which are not spawned in the trace.  These are the root
	// nodes of the SpawningForest.
	UnspawnedRootSpans []trace.RootSpan[T, CP, SP, DP]
	// A mapping from spawned RootSpans to spawning Spans.
	SpawnersBySpawnedRootSpan map[trace.RootSpan[T, CP, SP, DP]]trace.Span[T, CP, SP, DP]
	// A mapping from spawning Spans to spawned RootSpans.
	SpawnedRootSpansBySpawner map[trace.Span[T, CP, SP, DP]][]trace.RootSpan[T, CP, SP, DP]
}

// Describes a spawning dependency type.
type spawningDependency struct {
	dt                            trace.DependencyType
	mustBeFirstIncomingDependency bool
	mayBeDuplicated               bool
}

// Creates a new spawningDependency with the specified dependency type.
func newDependency(dt trace.DependencyType) *spawningDependency {
	return &spawningDependency{
		dt: dt,
	}
}

// DependencyOption is an option applied to a dependency.
type DependencyOption func(*spawningDependency)

// MustBeFirstIncomingDependency specifies that the receiver must be the first
// incoming dependency in the span.  If specified, it is an error for a
// RootSpan's first incoming dependency to be of a different type if it has any
// incoming dependency of this type.
func MustBeFirstIncomingDependency(dep *spawningDependency) {
	dep.mustBeFirstIncomingDependency = true
}

// MayBeDuplicated specifies that the receiver's dependency type may appear in
// several incoming dependencies in the span.  If not specified, then it is an
// error for a RootSpan to have multiple incoming dependencies of this type.
// If spawning events may be duplicated, only the first is considered spawning.
func MayBeDuplicated(dep *spawningDependency) {
	dep.mayBeDuplicated = true
}

type forestOptions struct {
	depsByType map[trace.DependencyType]*spawningDependency
}

// ForestOption is an option argument to NewSpawningForest.
type ForestOption func(fo *forestOptions) error

func newSpawningForestOptions(fos ...ForestOption) (*forestOptions, error) {
	ret := &forestOptions{
		depsByType: map[trace.DependencyType]*spawningDependency{},
	}
	for _, fo := range fos {
		if err := fo(ret); err != nil {
			return nil, err
		}
	}
	return ret, nil
}

// Dependency specifies that the provided DependencyType describes a valid
// spawning dependency for this NewSpawningForest invocation.
func Dependency(dt trace.DependencyType, opts ...DependencyOption) ForestOption {
	return func(fo *forestOptions) error {
		dep := newDependency(dt)
		for _, opt := range opts {
			opt(dep)
		}
		if _, ok := fo.depsByType[dep.dt]; ok {
			return fmt.Errorf("each spawning dependency type must be unique (duplicate dependency type %d)", dep.dt)
		}
		fo.depsByType[dep.dt] = dep
		return nil
	}
}

// NewSpawningForest generates a new spawning forest from the provided trace
// and set of options.
func NewSpawningForest[T any, CP, SP, DP fmt.Stringer](
	t trace.Trace[T, CP, SP, DP],
	options ...ForestOption,
) (*Forest[T, CP, SP, DP], error) {
	fo, err := newSpawningForestOptions(options...)
	if err != nil {
		return nil, err
	}
	ret := &Forest[T, CP, SP, DP]{
		SpawnersBySpawnedRootSpan: map[trace.RootSpan[T, CP, SP, DP]]trace.Span[T, CP, SP, DP]{},
		SpawnedRootSpansBySpawner: map[trace.Span[T, CP, SP, DP]][]trace.RootSpan[T, CP, SP, DP]{},
	}
	var visit func(
		rs trace.RootSpan[T, CP, SP, DP],
	) error
	visit = func(
		rs trace.RootSpan[T, CP, SP, DP],
	) error {
		seenFirstIncoming := false
		for _, es := range rs.ElementarySpans() {
			if es.Incoming() != nil {
				d, ok := fo.depsByType[es.Incoming().DependencyType()]
				if ok {
					updateSpawner := true
					sourceSpan := es.Incoming().TriggeringOrigin().Span()
					if otherSpawner, ok := ret.SpawnersBySpawnedRootSpan[rs]; ok {
						if otherSpawner != sourceSpan {
							if !d.mayBeDuplicated {
								return fmt.Errorf(
									"root span %s is spawned more than once",
									t.DefaultNamer().SpanName(rs),
								)
							}
							updateSpawner = false
						}
					}
					if d.mustBeFirstIncomingDependency && seenFirstIncoming {
						return fmt.Errorf(
							"root span %s has an incoming dependency of type %d, but it is not the first incoming dependency",
							t.DefaultNamer().SpanName(rs), d.dt,
						)
					}
					if updateSpawner {
						ret.SpawnersBySpawnedRootSpan[rs] = sourceSpan
					}
					ret.SpawnedRootSpansBySpawner[sourceSpan] = append(
						ret.SpawnedRootSpansBySpawner[sourceSpan], rs)
				}
				seenFirstIncoming = true
			}
		}
		return nil
	}
	for _, rootSpan := range t.RootSpans() {
		if err := visit(rootSpan); err != nil {
			return nil, err
		}
	}
	for _, rootSpan := range t.RootSpans() {
		if _, ok := ret.SpawnersBySpawnedRootSpan[rootSpan]; !ok {
			ret.UnspawnedRootSpans = append(ret.UnspawnedRootSpans, rootSpan)
		}
	}
	return ret, nil
}
