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
	"strings"
	"testing"
	"time"

	"github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
	"github.com/google/go-cmp/cmp"
)

func spanFinder(t *testing.T, pathMatcherStrs ...string) *trace.SpanFinder[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload] {
	t.Helper()
	ret, err := testtrace.SpanFinderFromPattern(pathMatcherStrs)
	if err != nil {
		t.Fatalf("failed to parse path pattern: %s", err)
	}
	return ret
}

func dependencyTypes(dts ...trace.DependencyType) []trace.DependencyType {
	return dts
}

func prettyPrintSpan(
	span trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	namer trace.Namer[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
) string {
	return strings.Join(trace.GetSpanDisplayPath(span, namer), "/")
}

func prettyPrintSpanSelection(
	ss *trace.SpanSelection[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	namer trace.Namer[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
) string {
	spans := ss.Spans()
	ret := make([]string, len(spans))
	for idx, span := range spans {
		ret[idx] = prettyPrintSpan(span, namer)
	}
	return "[" + strings.Join(ret, ", ") + "]"
}

func prettyPrintAppliedSpanModifications(
	indent string,
	asm *appliedSpanModifications[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	namer trace.Namer[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
) string {
	ret := indent + prettyPrintSpanSelection(asm.spanSelection, namer)
	if asm.mst.startsAsEarlyAsPossible {
		ret = ret + fmt.Sprintf(" start as early as possible")
	}
	if asm.mst.hasDurationScalingFactor {
		ret = ret + fmt.Sprintf(" scale * %.2f%%", asm.mst.durationScalingFactor*100.0)
	}
	return ret
}

func prettyPrintDependencySelection(
	ds *trace.DependencySelection[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	trace trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
) string {
	dts := ds.SelectedDependencyTypes(trace)
	sort.Slice(dts, func(a, b int) bool {
		return dts[a] < dts[b]
	})
	dtsStrs := make([]string, len(dts))
	for idx, dt := range dts {
		dtsStrs[idx] = trace.DefaultNamer().DependencyTypeNames()[dt]
	}
	return fmt.Sprintf(
		"types [%s] %s -> %s",
		strings.Join(dtsStrs, ", "),
		prettyPrintSpanSelection(ds.OriginSelection, trace.DefaultNamer()),
		prettyPrintSpanSelection(ds.DestinationSelection, trace.DefaultNamer()),
	)
}

func prettyPrintAppliedDependencyModifications(
	indent string,
	adm *appliedDependencyModifications[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	trace trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
) string {
	return indent +
		prettyPrintDependencySelection(adm.dependencySelection, trace) +
		fmt.Sprintf(" scale * %.2f%%", adm.mdt.durationScalingFactor*100.0)
}

func prettyPrintAppliedDependencyAdditions(
	indent string,
	ada *appliedDependencyAdditions[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	trace trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
) string {
	return indent +
		fmt.Sprintf("type %v %s@%.2f%% -> %s@%.2f%%",
			ada.adt.dependencyType,
			prettyPrintSpan(ada.originSpan, trace.DefaultNamer()), ada.adt.originPosition.FractionThrough()*100.0,
			prettyPrintSpanSelection(ada.selectedDestinationSpans, trace.DefaultNamer()), ada.adt.destinationPosition.FractionThrough()*100.0,
		)
}

func prettyPrintAppliedTransforms(
	indent string,
	at *appliedTransforms[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	trace trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
) string {
	ret := []string{}
	if len(at.appliedSpanModifications) > 0 {
		ret = append(ret, indent+"applied span modifications:")
		for _, asm := range at.appliedSpanModifications {
			ret = append(
				ret,
				prettyPrintAppliedSpanModifications(indent+"  ", asm, trace.DefaultNamer()),
			)
		}
	}
	if len(at.appliedSpanGates) > 0 {
		ret = append(ret, indent+"applied span gates:")
		for _, asg := range at.appliedSpanGates {
			ret = append(
				ret,
				indent+"  on "+prettyPrintSpanSelection(asg.spanSelection, trace.DefaultNamer()),
			)
		}
	}
	if len(at.appliedDependencyModifications) > 0 {
		ret = append(ret, indent+"applied dependency modifications:")
		for _, adm := range at.appliedDependencyModifications {
			ret = append(
				ret,
				prettyPrintAppliedDependencyModifications(indent+"  ", adm, trace),
			)
		}
	}
	if len(at.appliedDependencyAdditions) > 0 {
		ret = append(ret, indent+"applied dependency additions:")
		for _, ada := range at.appliedDependencyAdditions {
			if ada != nil {
				ret = append(
					ret,
					prettyPrintAppliedDependencyAdditions(indent+"  ", ada, trace),
				)
			}
		}
	}
	if len(at.appliedDependencyRemovals) > 0 {
		ret = append(ret, indent+"applied dependency removals:")
		for _, adr := range at.appliedDependencyRemovals {
			ret = append(
				ret,
				indent+"  "+prettyPrintDependencySelection(adr.dependencySelection, trace),
			)
		}
	}
	return strings.Join(ret, "\n")
}

func TestTransformationConstruction(t *testing.T) {
	for _, test := range []struct {
		description string
		transform   *Transform[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]
		buildTrace  func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		)
		wantAppliedTransformStr string
	}{{
		description: "ideal (all dependencies scaled by 0x, all spans start as early as possible) testtrace.Trace1",
		transform: New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
			WithDependenciesScaledBy(nil, nil, nil, 0).
			WithSpansStartingAsEarlyAsPossible(nil),
		buildTrace: testtrace.Trace1,
		wantAppliedTransformStr: `
applied span modifications:
  [s0.0.0, s0.1.0, s1.0.0, s0.0.0/0, s0.0.0/0/3] start as early as possible
applied dependency modifications:
  types [call, return, spawn, send, signal] [s0.0.0, s0.1.0, s1.0.0, s0.0.0/0, s0.0.0/0/3] -> [s0.0.0, s0.1.0, s1.0.0, s0.0.0/0, s0.0.0/0/3] scale * 0.00%`,
	}, {
		description: "testtrace.Trace1, s0.1.0 is 50% faster",
		transform: New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
			WithSpansScaledBy(
				spanFinder(t, "s0.1.0"),
				.5,
			),
		buildTrace: testtrace.Trace1,
		wantAppliedTransformStr: `
applied span modifications:
  [s0.1.0] scale * 50.00%`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			tr, err := test.buildTrace()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			at, err := test.transform.apply(tr)
			if err != nil {
				t.Fatalf("failed to apply transform: %s", err.Error())
			}
			gotAppliedTransformStr := "\n" + prettyPrintAppliedTransforms("", at, tr)
			if diff := cmp.Diff(
				test.wantAppliedTransformStr, gotAppliedTransformStr,
			); diff != "" {
				t.Errorf("Applied transform: \n%s\ndiff (-want +got) %s",
					diff, gotAppliedTransformStr,
				)
			}
		})
	}
}

func TestTraceTransforms(t *testing.T) {
	for _, test := range []struct {
		description string
		buildTrace  func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		)
		hierarchyType trace.HierarchyType
		wantErr       bool
		wantTraceStr  string
	}{{
		description: "single-span speedup by .5x",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(100, 200, "b", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 100),
					testtrace.Destination(testtrace.Paths("b"), 100),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithSpansScaledBy(spanFinder(t, "a"), .5)
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-50ns) (a)
    Elementary spans:
      0s-50ns <none> -> THIS -> [send to b @50ns]
  Span 'b' (50ns-150ns) (b)
    Elementary spans:
      50ns-150ns [send from a @50ns] -> THIS -> <none>`,
	}, {
		description: "single-dependency slowdown by 2x",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(50, 100, "b", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 50),
					testtrace.Destination(testtrace.Paths("b"), 60)).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithDependenciesScaledBy(
					spanFinder(t, "a"), spanFinder(t, "b"),
					dependencyTypes(testtrace.Send),
					2,
				)
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-50ns <none> -> THIS -> [send to b @70ns]
      50ns-100ns <none> -> THIS -> <none>
  Span 'b' (50ns-110ns) (b)
    Elementary spans:
      50ns-60ns <none> -> THIS -> <none>
      70ns-110ns [send from a @50ns] -> THIS -> <none>`,
	}, {
		description: "added dependency",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 50, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 100, "b", testtrace.ParentCategories()),
					testtrace.RootSpan(50, 100, "c", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 20),
					testtrace.Destination(testtrace.Paths("b"), 30),
				).
				WithDependency(
					testtrace.Signal,
					"",
					testtrace.Origin(testtrace.Paths("a"), 50),
					testtrace.Destination(testtrace.Paths("c"), 50),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithAddedDependencies(
					trace.NewPosition(spanFinder(t, "b"), 0.8),
					trace.NewPosition(spanFinder(t, "c"), 0.0),
					testtrace.Signal,
					0, // The new dependencies are 0-duration...
				)
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-50ns) (a)
    Elementary spans:
      0s-20ns <none> -> THIS -> [send to b @30ns]
      20ns-50ns <none> -> THIS -> [signal to c @50ns]
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-30ns <none> -> THIS -> <none>
      30ns-80ns [send from a @20ns] -> THIS -> [signal to c @80ns]
      80ns-100ns <none> -> THIS -> <none>
  Span 'c' (50ns-130ns) (c)
    Elementary spans:
      50ns-50ns [signal from a @50ns] -> THIS -> <none>
      80ns-130ns [signal from b @80ns] -> THIS -> <none>`,
	}, {
		description: "added dependency where deps already existed",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 50, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 50, "b", testtrace.ParentCategories()),
					testtrace.RootSpan(50, 100, "c", testtrace.ParentCategories()),
					testtrace.RootSpan(50, 100, "d", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 50),
					testtrace.Destination(testtrace.Paths("c"), 50),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				// Now multiple incoming deps in c @ 50
				WithAddedDependencies(
					trace.NewPosition(spanFinder(t, "b"), 1.0),
					trace.NewPosition(spanFinder(t, "c"), 0.0),
					testtrace.Signal,
					0, // The new dependencies are 0-duration...
				).
				// Now multiple outgoing deps in a @ 50
				WithAddedDependencies(
					trace.NewPosition(spanFinder(t, "a"), 1.0),
					trace.NewPosition(spanFinder(t, "d"), 0.0),
					testtrace.Signal,
					0, // The new dependencies are 0-duration...
				)
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-50ns) (a)
    Elementary spans:
      0s-50ns <none> -> THIS -> [signal to d @50ns]
      50ns-50ns <none> -> THIS -> [send to c @50ns]
  Span 'b' (0s-50ns) (b)
    Elementary spans:
      0s-50ns <none> -> THIS -> [signal to c @50ns]
  Span 'c' (50ns-100ns) (c)
    Elementary spans:
      50ns-50ns [send from a @50ns] -> THIS -> <none>
      50ns-100ns [signal from b @50ns] -> THIS -> <none>
  Span 'd' (50ns-100ns) (d)
    Elementary spans:
      50ns-100ns [signal from a @50ns] -> THIS -> <none>`,
	}, {
		description: "added dependencies without matching endpoints are dropped",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 50, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(50, 100, "b", testtrace.ParentCategories()),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				// This dep should be added...
				WithAddedDependencies(
					trace.NewPosition(spanFinder(t, "a"), 1.0),
					trace.NewPosition(spanFinder(t, "b"), 0.0),
					testtrace.Signal,
					0, // The new dependencies are 0-duration...
				).
				// But these can't be.
				WithAddedDependencies(
					trace.NewPosition(spanFinder(t, "c"), 1.0),
					trace.NewPosition(spanFinder(t, "b"), 0.0),
					testtrace.Signal,
					0, // The new dependencies are 0-duration...
				).
				WithAddedDependencies(
					trace.NewPosition(spanFinder(t, "a"), 1.0),
					trace.NewPosition(spanFinder(t, "d"), 0.0),
					testtrace.Signal,
					0, // The new dependencies are 0-duration...
				)
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-50ns) (a)
    Elementary spans:
      0s-50ns <none> -> THIS -> [signal to b @50ns]
  Span 'b' (50ns-100ns) (b)
    Elementary spans:
      50ns-100ns [signal from a @50ns] -> THIS -> <none>`,
	}, {
		description: "adjusted start",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(50, 150, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(100, 150, "b", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 100),
					testtrace.Destination(testtrace.Paths("b"), 100),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithSpansStartingAsEarlyAsPossible(spanFinder(t, "a"))
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (50ns-150ns) (a)
    Elementary spans:
      50ns-100ns <none> -> THIS -> [send to b @100ns]
      100ns-150ns <none> -> THIS -> <none>
  Span 'b' (100ns-150ns) (b)
    Elementary spans:
      100ns-150ns [send from a @100ns] -> THIS -> <none>`,
	}, {
		description: "removed dependence",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(50, 150, "b", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 50),
					testtrace.Destination(testtrace.Paths("b"), 50),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithRemovedDependencies(
					spanFinder(t, "a"), spanFinder(t, "b"),
					dependencyTypes(testtrace.Send),
				).
				WithSpansStartingAsEarlyAsPossible(spanFinder(t, "b"))

			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-100ns <none> -> THIS -> <none>
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-100ns <none> -> THIS -> <none>`,
	}, {
		description: "added and removed dependencies",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 50, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(50, 100, "b", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 50),
					testtrace.Destination(testtrace.Paths("b"), 50),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithRemovedDependencies(
					spanFinder(t, "a"), spanFinder(t, "b"),
					nil,
				).
				WithAddedDependencies(
					trace.NewPosition(spanFinder(t, "b"), 1.0),
					trace.NewPosition(spanFinder(t, "a"), 0.0),
					testtrace.Signal,
					0, // The new dependencies are 0-duration...
				).
				WithSpansStartingAsEarlyAsPossible(spanFinder(t, "b"))
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (50ns-100ns) (a)
    Elementary spans:
      50ns-100ns [signal from b @50ns] -> THIS -> <none>
  Span 'b' (0s-50ns) (b)
    Elementary spans:
      0s-50ns <none> -> THIS -> [signal to a @50ns]`,
	}, {
		description: "nonblocking dependency shrinkage",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 10, "source", testtrace.ParentCategories()),
					testtrace.RootSpan(20, 100, "dest-shrink", testtrace.ParentCategories()),
					testtrace.RootSpan(20, 100, "dest-noshrink", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send, "",
					testtrace.Origin(testtrace.Paths("source"), 5),
					testtrace.Destination(testtrace.Paths("dest-shrink"), 50),
				).
				WithDependency(
					testtrace.Send, "",
					testtrace.Origin(testtrace.Paths("source"), 5),
					testtrace.Destination(testtrace.Paths("dest-shrink"), 70),
				).
				WithSuspend(testtrace.Paths("dest-shrink"), 60, 70).
				WithDependency(
					testtrace.Send, "",
					testtrace.Origin(testtrace.Paths("source"), 5),
					testtrace.Destination(testtrace.Paths("dest-noshrink"), 60),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithSpansScaledBy(
					spanFinder(t, "dest-shrink", "dest-noshrink"),
					.5,
				).
				WithShrinkableIncomingDependencies(
					spanFinder(t, "dest-shrink"),
					nil,
					2,
				)
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'dest-noshrink' (20ns-80ns) (dest-noshrink)
    Elementary spans:
      20ns-40ns <none> -> THIS -> <none>
      60ns-80ns [send from source @5ns] -> THIS -> <none>
  Span 'dest-shrink' (20ns-55ns) (dest-shrink)
    Elementary spans:
      20ns-35ns <none> -> THIS -> <none>
      35ns-40ns [send from source @5ns] -> THIS -> <none>
      40ns-55ns [send from source @5ns] -> THIS -> <none>
  Span 'source' (0s-10ns) (source)
    Elementary spans:
      0s-5ns <none> -> THIS -> [send to dest-noshrink @60ns]
      5ns-5ns <none> -> THIS -> [send to dest-shrink @40ns]
      5ns-5ns <none> -> THIS -> [send to dest-shrink @35ns]
      5ns-10ns <none> -> THIS -> <none>`,
	}, {
		description: "transformations introducing loops fail",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 100, "b", testtrace.ParentCategories()),
				).
				WithDependency(testtrace.Send, "", testtrace.Origin(testtrace.Paths("a"), 20), testtrace.Destination(testtrace.Paths("b"), 20)).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithAddedDependencies(
					trace.NewPosition(spanFinder(t, "b"), 0.30),
					trace.NewPosition(spanFinder(t, "a"), 0.10),
					testtrace.Signal,
					0, // The new dependencies are 0-duration...
				)
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantErr:       true,
	}, {
		description: "transformations with unresolvable span gates fail",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 100, "b", testtrace.ParentCategories()),
				).
				WithDependency(testtrace.Send, "", testtrace.Origin(testtrace.Paths("a"), 20), testtrace.Destination(testtrace.Paths("b"), 20)).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithSpansGatedBy(spanFinder(t, "a"),
					func() SpanGater[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload] {
						return &concurrencyLimiter[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
							allowedConcurrency: 0,
						}
					})
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantErr:       true,
	}, {
		description: "add-dependency transform with multiple origins is dropped",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 100, "b", testtrace.ParentCategories()),
				).
				WithDependency(testtrace.Send, "", testtrace.Origin(testtrace.Paths("a"), 20), testtrace.Destination(testtrace.Paths("b"), 20)).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithAddedDependencies(
					trace.NewPosition(spanFinder(t, "*"), 0.80),
					trace.NewPosition(spanFinder(t, "c"), 0.0),
					testtrace.Signal,
					0, // The new dependencies are 0-duration...
				)
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-20ns <none> -> THIS -> [send to b @20ns]
      20ns-100ns <none> -> THIS -> <none>
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-20ns <none> -> THIS -> <none>
      20ns-100ns [send from a @20ns] -> THIS -> <none>`,
	}, {
		description: "limit concurrency to 1 between 'a' and 'b'",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 100, "b", testtrace.ParentCategories()),
					testtrace.RootSpan(100, 200, "c", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 100),
					testtrace.Destination(testtrace.Paths("c"), 100),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("b"), 100),
					testtrace.Destination(testtrace.Paths("c"), 100),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithSpansGatedBy(
					spanFinder(t, "a", "b"),
					func() SpanGater[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload] {
						return &concurrencyLimiter[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
							allowedConcurrency: 1,
						}
					},
				)
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-100ns <none> -> THIS -> [send to c @100ns]
  Span 'b' (100ns-200ns) (b)
    Elementary spans:
      100ns-200ns <none> -> THIS -> [send to c @200ns]
  Span 'c' (100ns-300ns) (c)
    Elementary spans:
      100ns-100ns [send from a @100ns] -> THIS -> <none>
      200ns-300ns [send from b @200ns] -> THIS -> <none>`,
	}, {
		description: "limit concurrency to 2 between 'a' and 'b', and 'c'",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 100, "b", testtrace.ParentCategories()),
					testtrace.RootSpan(100, 200, "c", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 100),
					testtrace.Destination(testtrace.Paths("c"), 100),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("b"), 100),
					testtrace.Destination(testtrace.Paths("c"), 100),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithSpansGatedBy(
					spanFinder(t, "a", "b", "c"),
					func() SpanGater[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload] {
						return &concurrencyLimiter[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
							allowedConcurrency: 2,
						}
					},
				)
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-100ns <none> -> THIS -> [send to c @100ns]
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-100ns <none> -> THIS -> [send to c @100ns]
  Span 'c' (100ns-200ns) (c)
    Elementary spans:
      100ns-100ns [send from a @100ns] -> THIS -> <none>
      100ns-200ns [send from b @100ns] -> THIS -> <none>`,
	}, {
		description: "payload mapping",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootCategories(
					testtrace.RootCategory(testtrace.Structural, "A"),
					testtrace.RootCategory(testtrace.Structural, "B"),
				).
				WithRootSpans(
					testtrace.RootSpan(
						0, 100, "a",
						testtrace.ParentCategories(
							testtrace.FindCategory(testtrace.Structural, "A"),
						),
					),
					testtrace.RootSpan(
						100, 200, "b",
						testtrace.ParentCategories(
							testtrace.FindCategory(testtrace.Structural, "B"),
						),
					),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 100),
					testtrace.Destination(testtrace.Paths("b"), 100),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithCategoryPayloadMapping(func(original, new trace.Category[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]) testtrace.StringPayload {
					return "New_" + original.Payload()
				}).
				WithSpanPayloadMapping(func(original, new trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]) testtrace.StringPayload {
					return "new_" + original.Payload()
				})
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.Structural,
		wantTraceStr: `
Trace (structural):
  Category 'New_A' (New_A)
    Span 'new_a' (0s-100ns) (new_a)
      Elementary spans:
        0s-100ns <none> -> THIS -> [send to new_b @100ns]
  Category 'New_B' (New_B)
    Span 'new_b' (100ns-200ns) (new_b)
      Elementary spans:
        100ns-200ns [send from new_a @100ns] -> THIS -> <none>`,
	}, {
		description: "ideal (all dependencies scaled by 0x, all spans start as early as possible) testtrace.Trace1",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithDependenciesScaledBy(
					nil, nil,
					nil,
					0,
				).
				WithSpansStartingAsEarlyAsPossible(
					nil,
				)
			originalTrace, err := testtrace.Trace1()
			if err != nil {
				return nil, err
			}
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-90ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
      80ns-90ns [return from s0.0.0/0 @80ns] -> THIS -> <none>
    Span '0' (10ns-80ns) (s0.0.0/0)
      Elementary spans:
        10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @20ns]
        20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
        30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
        60ns-80ns [return from s0.0.0/0/3 @60ns] -> THIS -> [return to s0.0.0 @80ns]
      Span '3' (40ns-60ns) (s0.0.0/0/3)
        Elementary spans:
          40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
          50ns-60ns [signal from s0.1.0 @45ns] -> THIS -> [return to s0.0.0/0 @60ns]
  Span 's0.1.0' (20ns-65ns) (s0.1.0)
    Elementary spans:
      20ns-30ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
      35ns-45ns [send from s1.0.0 @35ns] -> THIS -> [signal to s0.0.0/0/3 @50ns]
      45ns-65ns <none> -> THIS -> <none>
  Span 's1.0.0' (30ns-50ns) (s1.0.0)
    Elementary spans:
      30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @35ns]
      35ns-50ns <none> -> THIS -> <none>`,
	}, {
		description: "testtrace.Trace1, s0.1.0 is 50% faster",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithSpansScaledBy(
					spanFinder(t, "s0.1.0"),
					.5,
				)
			originalTrace, err := testtrace.Trace1()
			if err != nil {
				return nil, err
			}
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-95ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
      85ns-95ns [return from s0.0.0/0 @85ns] -> THIS -> <none>
    Span '0' (10ns-85ns) (s0.0.0/0)
      Elementary spans:
        10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
        20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
        30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
        65ns-85ns [return from s0.0.0/0/3 @65ns] -> THIS -> [return to s0.0.0 @85ns]
      Span '3' (40ns-65ns) (s0.0.0/0/3)
        Elementary spans:
          40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
          55ns-65ns [signal from s0.1.0 @45ns] -> THIS -> [return to s0.0.0/0 @65ns]
  Span 's0.1.0' (30ns-55ns) (s0.1.0)
    Elementary spans:
      30ns-35ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
      40ns-45ns [send from s1.0.0 @35ns] -> THIS -> [signal to s0.0.0/0/3 @55ns]
      45ns-55ns <none> -> THIS -> <none>
  Span 's1.0.0' (30ns-50ns) (s1.0.0)
    Elementary spans:
      30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
      35ns-50ns <none> -> THIS -> <none>`,
	}, {
		description: "testtrace.Trace1 with extra Signal from the end of s1.0.0 to 30% through s0.1.0",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithAddedDependencies(
					trace.NewPosition(spanFinder(t, "s1.0.0"), 1.0),
					trace.NewPosition(spanFinder(t, "s0.1.0"), 0.30),
					testtrace.Signal,
					2, // The new dependencies have duration of 2ns...
				)
			originalTrace, err := testtrace.Trace1()
			if err != nil {
				return nil, err
			}
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-110ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
      100ns-110ns [return from s0.0.0/0 @100ns] -> THIS -> <none>
    Span '0' (10ns-100ns) (s0.0.0/0)
      Elementary spans:
        10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
        20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
        30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
        80ns-100ns [return from s0.0.0/0/3 @80ns] -> THIS -> [return to s0.0.0 @100ns]
      Span '3' (40ns-80ns) (s0.0.0/0/3)
        Elementary spans:
          40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
          70ns-80ns [signal from s0.1.0 @60ns] -> THIS -> [return to s0.0.0/0 @80ns]
  Span 's0.1.0' (30ns-80ns) (s0.1.0)
    Elementary spans:
      30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
      40ns-42ns [send from s1.0.0 @35ns] -> THIS -> <none>
      52ns-60ns [signal from s1.0.0 @50ns] -> THIS -> [signal to s0.0.0/0/3 @70ns]
      60ns-80ns <none> -> THIS -> <none>
  Span 's1.0.0' (30ns-50ns) (s1.0.0)
    Elementary spans:
      30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
      35ns-50ns <none> -> THIS -> [signal to s0.1.0 @52ns]`,
	}, {
		description: "testtrace.Trace1 with Signal from s0.1.0 to s1.0.0 removed",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithRemovedDependencies(
					spanFinder(t, "s0.1.0"), spanFinder(t, "s0.0.0/0/3"),
					dependencyTypes(testtrace.Signal),
				)
			originalTrace, err := testtrace.Trace1()
			if err != nil {
				return nil, err
			}
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-90ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
      80ns-90ns [return from s0.0.0/0 @80ns] -> THIS -> <none>
    Span '0' (10ns-80ns) (s0.0.0/0)
      Elementary spans:
        10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
        20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
        30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
        60ns-80ns [return from s0.0.0/0/3 @60ns] -> THIS -> [return to s0.0.0 @80ns]
      Span '3' (40ns-60ns) (s0.0.0/0/3)
        Elementary spans:
          40ns-60ns [call from s0.0.0/0 @40ns] -> THIS -> [return to s0.0.0/0 @60ns]
  Span 's0.1.0' (30ns-70ns) (s0.1.0)
    Elementary spans:
      30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
      40ns-70ns [send from s1.0.0 @35ns] -> THIS -> <none>
  Span 's1.0.0' (30ns-50ns) (s1.0.0)
    Elementary spans:
      30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
      35ns-50ns <none> -> THIS -> <none>`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			trace, err := test.buildTrace()
			if err != nil != test.wantErr {
				t.Errorf("Got unexpected error %v", err)
			}
			if err != nil {
				return
			}
			var gotTraceStr string
			if test.hierarchyType == testtrace.None {
				gotTraceStr = testtrace.TPP.PrettyPrintTraceSpans(trace)
			} else {
				gotTraceStr = testtrace.TPP.PrettyPrintTrace(trace, test.hierarchyType)
			}
			if diff := cmp.Diff(test.wantTraceStr, gotTraceStr); diff != "" {
				t.Errorf("got trace string\n%s\n, diff (-want +got) %s", gotTraceStr, diff)
			}
		})
	}
}
