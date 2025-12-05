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

// Package stitch provides types and functions for stitching multiple Tracey
// traces together.
package stitch

import (
	"fmt"

	"github.com/google/tracey/trace"
	traceparser "github.com/google/tracey/trace/parser"
)

type dependency struct {
	dependencyType                                                  trace.DependencyType
	originPositionPatternStrings, destinationPositionPatternStrings []string
	opts                                                            []trace.DependencyOption
}

type stitcher[T any, CP, SP, DP fmt.Stringer] struct {
	hierarchyType          trace.HierarchyType
	originalTraces         []trace.Trace[T, CP, SP, DP]
	deps                   []*dependency
	newTrace               trace.MutableTrace[T, CP, SP, DP]
	updateCategoryUniqueID func(CP, string)
	updateSpanUniqueID     func(SP, string)
}

func newStitcher[T any, CP, SP, DP fmt.Stringer](opts ...Option[T, CP, SP, DP]) (*stitcher[T, CP, SP, DP], error) {
	ret := &stitcher[T, CP, SP, DP]{
		hierarchyType: trace.SpanOnlyHierarchyType,
	}
	for _, opt := range opts {
		if err := opt(ret); err != nil {
			return nil, err
		}
	}
	return ret, nil
}

// Option represents a single argument to Stitch().
type Option[T any, CP, SP, DP fmt.Stringer] func(s *stitcher[T, CP, SP, DP]) error

// UpdateUniqueIDs ensures that the unique IDs of spans and categories remain
// unique by appending a trace-specific prefix to each, using the provided
// unique ID updater functions.
func UpdateUniqueIDs[T any, CP, SP, DP fmt.Stringer](
	updateCategoryUniqueID func(cp CP, newPrefix string),
	updateSpanUniqueID func(sp SP, newPrefix string),
) Option[T, CP, SP, DP] {
	return func(s *stitcher[T, CP, SP, DP]) error {
		s.updateCategoryUniqueID = updateCategoryUniqueID
		s.updateSpanUniqueID = updateSpanUniqueID
		return nil
	}
}

// Trace adds a trace to be stitched in.  Traces are stitched in order, and are
// currently stitched in at the root, but this may eventually accept a set of
// {hierarchy type, path specifier} pairs specifying categories under which
// this trace should be placed.
func Trace[T any, CP, SP, DP fmt.Stringer](t trace.Trace[T, CP, SP, DP]) Option[T, CP, SP, DP] {
	return func(s *stitcher[T, CP, SP, DP]) error {
		if s.newTrace == nil {
			s.newTrace = trace.NewMutableTrace(
				t.Comparator(),
				t.DefaultNamer(),
			)
		}
		s.originalTraces = append(s.originalTraces, t)
		return nil
	}
}

// HierarchyType specifies the default hierarchy type to use while parsing
// stitch positions.  If unspecified, the span-only hierarchy type is used.
func HierarchyType[T any, CP, SP, DP fmt.Stringer](ht trace.HierarchyType) Option[T, CP, SP, DP] {
	return func(s *stitcher[T, CP, SP, DP]) error {
		s.hierarchyType = ht
		return nil
	}
}

// DependencyOption represents an option applied to a dependency.
type DependencyOption func(dep *dependency) error

// Origins is a DependencyOption specifying the origins of a stitched-in
// dependency as parsed strings.
func Origins(originPositionPatternStrings ...string) DependencyOption {
	return func(dep *dependency) error {
		dep.originPositionPatternStrings = append(dep.originPositionPatternStrings, originPositionPatternStrings...)
		return nil
	}
}

// Destinations is a DependencyOption specifying the destinations of a
// stitched-in dependency as parsed strings.
func Destinations(destinationPositionPatternStrings ...string) DependencyOption {
	return func(dep *dependency) error {
		dep.destinationPositionPatternStrings = append(dep.destinationPositionPatternStrings, destinationPositionPatternStrings...)
		return nil
	}
}

// Dependency adds a dependency to the stitched trace, of the specified type,
// between the specified origin and destination
func Dependency[T any, CP, SP, DP fmt.Stringer](
	dependencyType trace.DependencyType,
	opts ...DependencyOption,
) Option[T, CP, SP, DP] {
	return func(s *stitcher[T, CP, SP, DP]) error {
		newDep := &dependency{
			dependencyType: dependencyType,
		}
		for _, opt := range opts {
			if err := opt(newDep); err != nil {
				return err
			}
		}
		s.deps = append(s.deps, newDep)
		return nil
	}
}

func (s *stitcher[T, CP, SP, DP]) findPositionsFromPatternString(
	positionPatternString string,
) ([]*trace.ElementarySpanPosition[T, CP, SP, DP], error) {
	positionPattern, err := traceparser.ParsePositionSpecifiers(
		s.hierarchyType,
		positionPatternString,
	)
	if err != nil {
		return nil, err
	}
	pf, err := traceparser.NewPositionFinder(positionPattern, s.newTrace)
	if err != nil {
		return nil, err
	}
	return pf.FindPositions(), nil
}

// Stitch stitches together two or more provided traces, and adds any specified
// additional dependencies, returning the new, stitched trace.
func Stitch[T any, CP, SP, DP fmt.Stringer](
	opts ...Option[T, CP, SP, DP],
) (trace.Trace[T, CP, SP, DP], error) {
	s, err := newStitcher(opts...)
	if err != nil {
		return nil, err
	}
	// Create a fully stitched trace.
	for {
		ok, err := s.stitchInNextTrace()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
	}
	// Add dependencies.
	for depIdx, dep := range s.deps {
		var origins []*trace.ElementarySpanPosition[T, CP, SP, DP]
		for _, originPositionPatternString := range dep.originPositionPatternStrings {
			esps, err := s.findPositionsFromPatternString(originPositionPatternString)
			if err != nil {
				return nil, err
			}
			origins = append(origins, esps...)
		}
		var destinations []*trace.ElementarySpanPosition[T, CP, SP, DP]
		for _, destinationPositionPatternString := range dep.destinationPositionPatternStrings {
			esps, err := s.findPositionsFromPatternString(destinationPositionPatternString)
			if err != nil {
				return nil, err
			}
			destinations = append(destinations, esps...)
		}
		newDep := s.newTrace.NewMutableDependency(dep.dependencyType, dep.opts...)
		for _, origin := range origins {
			if err := newDep.SetOriginSpan(
				s.newTrace.Comparator(),
				origin.ElementarySpan.Span(),
				origin.At,
			); err != nil {
				return nil, fmt.Errorf("failed to add origin for stitching dependency %d: %w", depIdx, err)
			}
		}
		for _, destination := range destinations {
			if err := newDep.AddDestinationSpan(
				s.newTrace.Comparator(),
				destination.ElementarySpan.Span(),
				destination.At,
			); err != nil {
				return nil, fmt.Errorf("failed to add destination for stitching dependency %d: %w", depIdx, err)
			}
		}
	}
	return s.newTrace, nil
}
