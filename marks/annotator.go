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

// Package marks provides types and functions for working with trace marks.
package marks

import (
	"fmt"

	"github.com/google/tracey/trace"
	traceparser "github.com/google/tracey/trace/parser"
)

type dependencyDirection int

const (
	incoming dependencyDirection = iota
	outgoing
)

type markSpecifier struct {
	direction      dependencyDirection
	dependencyType trace.DependencyType
	markPrefix     string
}

type spanAndMarks struct {
	spanPattern    *traceparser.SpanPattern
	markSpecifiers []*markSpecifier
}

// Annotator instances can annotate traces with mark events at specific points.
type Annotator struct {
	spanAndMarksBySpanPattern map[*traceparser.SpanPattern]*spanAndMarks
}

// NewAnnotator returns a new, empty Annotator.
func NewAnnotator() *Annotator {
	return &Annotator{
		spanAndMarksBySpanPattern: map[*traceparser.SpanPattern]*spanAndMarks{},
	}
}

func (a *Annotator) getSpanAndMarks(spanPattern *traceparser.SpanPattern) *spanAndMarks {
	sam, ok := a.spanAndMarksBySpanPattern[spanPattern]
	if !ok {
		sam = &spanAndMarks{
			spanPattern: spanPattern,
		}
		a.spanAndMarksBySpanPattern[spanPattern] = sam
	}
	return sam
}

func (a *Annotator) annotateInternal(
	spanPattern *traceparser.SpanPattern,
	direction dependencyDirection,
	dependencyType trace.DependencyType,
	markPrefix string,
) {
	sam := a.getSpanAndMarks(spanPattern)
	sam.markSpecifiers = append(
		sam.markSpecifiers,
		&markSpecifier{
			direction:      direction,
			dependencyType: dependencyType,
			markPrefix:     markPrefix,
		},
	)
}

// AnnotateIncoming specifies that marks with the specified prefix should be
// added at all incoming dependencies of the specified type within spans
// matching the provided SpanPattern.  It supports streaming invocation.
func (a *Annotator) AnnotateIncoming(
	spanPattern *traceparser.SpanPattern,
	dependencyType trace.DependencyType,
	markPrefix string,
) *Annotator {
	a.annotateInternal(spanPattern, incoming, dependencyType, markPrefix)
	return a
}

// AnnotateOutgoing specifies that marks with the specified prefix should be
// added at all outgoing dependencies of the specified type within spans
// matching the provided SpanPattern.  It supports streaming invocation.
func (a *Annotator) AnnotateOutgoing(
	spanPattern *traceparser.SpanPattern,
	dependencyType trace.DependencyType,
	markPrefix string,
) *Annotator {
	a.annotateInternal(spanPattern, outgoing, dependencyType, markPrefix)
	return a
}

// AnnotateTrace applies the provided Annotator to the provided Trace.
func AnnotateTrace[T any, CP, SP, DP fmt.Stringer](
	a *Annotator,
	tr trace.Trace[T, CP, SP, DP],
) error {
	for _, sam := range a.spanAndMarksBySpanPattern {
		spanFinder, err := traceparser.NewSpanFinder(sam.spanPattern, tr)
		if err != nil {
			return err
		}
		for _, markSpecifier := range sam.markSpecifiers {
			// Iterate through the same spans for each mark specifier to avoid
			// a map lookup on mark index.
			for _, span := range spanFinder.FindSpans() {
				idx := 0
				for _, es := range span.ElementarySpans() {
					switch markSpecifier.direction {
					case incoming:
						if es.Incoming() != nil && es.Incoming().DependencyType() == markSpecifier.dependencyType {
							if err := span.Mark(tr.Comparator(), fmt.Sprintf("%s%d", markSpecifier.markPrefix, idx), es.Start()); err != nil {
								return fmt.Errorf("failed to add incoming mark: %v", err)
							}
							idx++
						}
					case outgoing:
						if es.Outgoing() != nil && es.Outgoing().DependencyType() == markSpecifier.dependencyType {
							if err := span.Mark(tr.Comparator(), fmt.Sprintf("%s%d", markSpecifier.markPrefix, idx), es.End()); err != nil {
								return fmt.Errorf("failed to add outgoing mark: %v", err)
							}
							idx++
						}
					}
				}
			}
		}
	}
	return nil
}
