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

package parser

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/tracey/spawning"
	testtrace "github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
	"github.com/google/tracey/trace/parser/lexer"
)

func TestParseErrors(t *testing.T) {
	for _, test := range []struct {
		description    string
		input          string
		wantErrPointer string
	}{{
		description: "allll good",
		input:       "a/b > c/d",
	}, {
		description: "numbers are fine",
		input:       "1/2 > 3/4.5",
	}, {
		description:    "unterminated quote",
		input:          "a/'b/c",
		wantErrPointer: "  ^",
	}, {
		description:    "unterminated paren",
		input:          "a/(b/c",
		wantErrPointer: "    ^",
	}, {
		description:    "extra cat separator",
		input:          "a/b > c/d > **",
		wantErrPointer: "          ^",
	}, {
		description:    "extra specifier separator",
		input:          "a/b, c/d,",
		wantErrPointer: "         ^",
	}, {
		description:    "trailing path element separator",
		input:          "a/b/",
		wantErrPointer: "    ^",
	}, {
		description:    "doubled path element separator",
		input:          "a//b",
		wantErrPointer: "  ^",
	}} {
		t.Run(test.description, func(t *testing.T) {
			_, err := parse(test.input)
			gotErrPointer := ""
			if err != nil {
				e := &lexer.Error{}
				if errors.As(err, &e) {
					gotErrPointer = strings.Repeat(" ", e.Offset()) + "^"
				} else {
					gotErrPointer = "unexpected error type"
				}
			}
			if gotErrPointer != test.wantErrPointer {
				t.Logf("Input: %s", test.input)
				t.Logf("Got:   %s", gotErrPointer)
				t.Logf("Want:  %s", test.wantErrPointer)
				t.Errorf("Unexpected error '%s'", err)
			}
		})
	}
}

func TestSpanFinderParsing(t *testing.T) {
	tr1 := testtrace.NewTestingTraceBuilder(t).
		WithRootCategories(
			testtrace.RootCategory(testtrace.Structural, "process 1",
				testtrace.Category("thread 1"),
			),
			testtrace.RootCategory(testtrace.Causal, "cpu 1"),
			testtrace.RootCategory(testtrace.Causal, "cpu 2"),
		).
		WithRootSpans(
			testtrace.RootSpan(0, 20, "root",
				testtrace.ParentCategories(testtrace.FindCategory(testtrace.Structural, "process 1")),
			),
			testtrace.RootSpan(20, 40, "root",
				testtrace.ParentCategories(testtrace.FindCategory(testtrace.Structural, "process 1", "thread 1")),
			),
			testtrace.RootSpan(40, 60, "root",
				testtrace.ParentCategories(testtrace.FindCategory(testtrace.Causal, "cpu 1")),
			),
			testtrace.RootSpan(60, 100, "root",
				testtrace.ParentCategories(testtrace.FindCategory(testtrace.Causal, "cpu 2")),
				testtrace.Span(80, 100, "child"),
			),
		).Build()
	tr2 := testtrace.NewTestingTraceBuilder(t).
		WithRootCategories(
			testtrace.RootCategory(testtrace.Structural, "cat"),
		).
		WithRootSpans(
			testtrace.RootSpan(0, 100, "a",
				testtrace.ParentCategories(testtrace.FindCategory(testtrace.Structural, "cat")),
				testtrace.Span(10, 40, "b",
					testtrace.Span(20, 30, "c",
						testtrace.Span(22, 24, "e"),
						testtrace.Span(26, 28, "/"),
					),
				),
				testtrace.Span(60, 90, "d",
					testtrace.Span(65, 75, "c"),
					testtrace.Span(77, 85, "c"),
				),
			),
		).Build()
	trace1, err := testtrace.Trace1()
	if err != nil {
		t.Fatalf("failed to build Trace1: %v", err)
	}
	for _, test := range []struct {
		description           string
		tr                    trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]
		spanFinderStr         string
		hierarchyType         trace.HierarchyType
		spanFinderOpts        []SpanFinderOption[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]
		wantSelectedSpanPaths string
	}{{
		description:           "** > root, structural hierarchy",
		tr:                    tr1,
		spanFinderStr:         "** > root",
		hierarchyType:         testtrace.Structural,
		wantSelectedSpanPaths: "process 1 > root, process 1/thread 1 > root",
	}, {
		description:           "** > root, causal hierarchy",
		tr:                    tr1,
		spanFinderStr:         "** > root",
		hierarchyType:         testtrace.Causal,
		wantSelectedSpanPaths: "cpu 1 > root, cpu 2 > root",
	}, {
		description:           "** > **, causal hierarchy",
		tr:                    tr1,
		spanFinderStr:         "** > **",
		hierarchyType:         testtrace.Causal,
		wantSelectedSpanPaths: "cpu 1 > root, cpu 2 > root, cpu 2 > root/child",
	}, {
		description:           "process 1 > root, structural hierarchy",
		tr:                    tr1,
		spanFinderStr:         `process\ 1 > root`,
		hierarchyType:         testtrace.Structural,
		wantSelectedSpanPaths: "process 1 > root",
	}, {
		description:           "process 1 > root, structural hierarchy, quoted",
		tr:                    tr1,
		spanFinderStr:         `'process\ 1' > root`,
		hierarchyType:         testtrace.Structural,
		wantSelectedSpanPaths: "process 1 > root",
	}, {
		description:           "all spans with total duration at least 80ns",
		tr:                    trace1,
		spanFinderStr:         "** where total_duration >= duration(80ns)",
		hierarchyType:         testtrace.Causal,
		wantSelectedSpanPaths: "p0/t0.0/r0.0.0 > s0.0.0, p0/t0.0/r0.0.0 > s0.0.0/0",
	}, {
		description:           "all spans with self-unsuspended duration at least 30ns",
		tr:                    trace1,
		spanFinderStr:         "** where self_unsuspended_duration >= duration(30ns)",
		hierarchyType:         testtrace.Causal,
		wantSelectedSpanPaths: "p0/t0.0/r0.0.0 > s0.0.0/0, p0/t0.0/t0.1/r0.1.0 > s0.1.0",
	}, {
		description:   "trace1, all spans spawned at least one level under s0.0.0",
		tr:            trace1,
		spanFinderStr: "s0.0.0/** -> **",
		hierarchyType: testtrace.Causal,
		spanFinderOpts: []SpanFinderOption[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
			SpawningForestFetcher(
				func() (*spawning.Forest[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error) {
					return spawning.NewSpawningForest(
						trace1,
						spawning.Dependency(testtrace.Spawn, spawning.MustBeFirstIncomingDependency),
					)
				},
			),
		},
		wantSelectedSpanPaths: "p0/p1/t1.0/r1.0.0 > s1.0.0, p0/t0.0/t0.1/r0.1.0 > s0.1.0",
	}, {
		description:           "all cs",
		tr:                    tr2,
		spanFinderStr:         "**/c",
		hierarchyType:         trace.SpanOnlyHierarchyType,
		wantSelectedSpanPaths: "a/b/c, a/d/c, a/d/c",
	}, {
		description:           "all cs another way",
		tr:                    tr2,
		spanFinderStr:         "*/*/c",
		hierarchyType:         trace.SpanOnlyHierarchyType,
		wantSelectedSpanPaths: "a/b/c, a/d/c, a/d/c",
	}, {
		description:           "everything at or under a/d",
		tr:                    tr2,
		spanFinderStr:         "a/d/**",
		hierarchyType:         trace.SpanOnlyHierarchyType,
		wantSelectedSpanPaths: "a/d, a/d/c, a/d/c",
	}, {
		description:           "everything at or under a/d",
		tr:                    tr2,
		spanFinderStr:         "a/d/**",
		hierarchyType:         trace.SpanOnlyHierarchyType,
		wantSelectedSpanPaths: "a/d, a/d/c, a/d/c",
	}, {
		description:           "all spans whose name is a vowel",
		tr:                    tr2,
		spanFinderStr:         "**/([aeiou])",
		hierarchyType:         trace.SpanOnlyHierarchyType,
		wantSelectedSpanPaths: "a, a/b/c/e",
	}, {
		description:           "everything just under a",
		tr:                    tr2,
		spanFinderStr:         "a/*",
		hierarchyType:         trace.SpanOnlyHierarchyType,
		wantSelectedSpanPaths: "a/b, a/d",
	}, {
		description:           "multiple selectors",
		tr:                    tr2,
		spanFinderStr:         `a/b/c,a/d/c`,
		hierarchyType:         trace.SpanOnlyHierarchyType,
		wantSelectedSpanPaths: `a/b/c, a/d/c, a/d/c`,
	}, {
		description:           "did you really name that span '/'?",
		tr:                    tr2,
		spanFinderStr:         `**/\/`,
		hierarchyType:         trace.SpanOnlyHierarchyType,
		wantSelectedSpanPaths: `a/b/c//`,
	}, {
		description:           "fragment with initial 'at' is ok",
		tr:                    tr2,
		spanFinderStr:         `**/attribute`,
		hierarchyType:         trace.SpanOnlyHierarchyType,
		wantSelectedSpanPaths: ``,
	}} {
		t.Run(test.description, func(t *testing.T) {
			sfps, err := ParseSpanSpecifierPatterns(test.hierarchyType, test.spanFinderStr)
			if err != nil {
				t.Fatalf("failed to parse path matchers string: %s", err)
			}
			sf, err := NewSpanFinder(sfps, test.tr, test.spanFinderOpts...)
			if err != nil {
				t.Fatalf("failed to build span finder: %s", err)
			}
			spans := sf.FindSpans()
			var spanPaths []string
			for _, span := range spans {
				rootSpan := span.RootSpan()
				var thisPath string
				if test.hierarchyType != trace.SpanOnlyHierarchyType {
					thisPath = strings.Join(
						trace.GetCategoryDisplayPath(
							rootSpan.ParentCategory(test.hierarchyType),
							testtrace.TestNamer,
						),
						"/",
					) + " > "
				}
				thisPath += strings.Join(trace.GetSpanDisplayPath(span, testtrace.TestNamer), "/")
				spanPaths = append(spanPaths, thisPath)
			}
			sort.Strings(spanPaths)
			gotSelectedSpanPaths := strings.Join(spanPaths, ", ")
			if gotSelectedSpanPaths != test.wantSelectedSpanPaths {
				t.Errorf("Got span paths '%s', wanted '%s'", gotSelectedSpanPaths, test.wantSelectedSpanPaths)
			}
		})
	}
}

func TestTracePositionParsing(t *testing.T) {
	tr, err := testtrace.Trace1()
	if err != nil {
		t.Fatalf("Failed to build test trace: %s", err)
	}
	for _, test := range []struct {
		description                      string
		positionStr                      string
		hierarchyType                    trace.HierarchyType
		fetchSpawningForest              func() (*spawning.Forest[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error)
		wantedSelectedElementarySpansStr string
	}{{
		description:                      "50% through 0 and 3 with @",
		positionStr:                      "**/(^0|3$) @ 50%",
		hierarchyType:                    testtrace.None,
		wantedSelectedElementarySpansStr: "s0.0.0/0 30ns-40ns, s0.0.0/0/3 40ns-50ns",
	}, {
		description:                      "50% through 0 and 3 with 'at'",
		positionStr:                      "**/(^0|3$) at 50%",
		hierarchyType:                    testtrace.None,
		wantedSelectedElementarySpansStr: "s0.0.0/0 30ns-40ns, s0.0.0/0/3 40ns-50ns",
	}, {
		description:                      "100% through spans matching '1', latest",
		positionStr:                      "**/(1) at 100% latest",
		hierarchyType:                    testtrace.None,
		wantedSelectedElementarySpansStr: "s0.1.0 50ns-70ns",
	}, {
		description:                      "100% through spans matching '1', earliest",
		positionStr:                      "**/(1) at 100% earliest",
		hierarchyType:                    testtrace.None,
		wantedSelectedElementarySpansStr: "s1.0.0 35ns-50ns",
	}, {
		description:                      "'start' mark",
		positionStr:                      "** at (start)",
		hierarchyType:                    testtrace.None,
		wantedSelectedElementarySpansStr: "s0.0.0 0s-10ns",
	}} {
		t.Run(test.description, func(t *testing.T) {
			pos, err := ParsePositionSpecifiers(test.hierarchyType, test.positionStr)
			if err != nil {
				t.Fatalf("Failed to parse position specifier: %s", err)
			}
			var gotSelectedElementarySpansStrs []string
			pf, err := NewPositionFinder(pos, tr, SpawningForestFetcher(test.fetchSpawningForest))
			if err != nil {
				t.Fatalf("Failed to build position finder: %s", err)
			}
			for _, esp := range pf.FindPositions() {
				s := fmt.Sprintf("%s %v-%v",
					strings.Join(trace.GetSpanDisplayPath(esp.ElementarySpan.Span(), testtrace.TestNamer), "/"),
					esp.ElementarySpan.Start(), esp.ElementarySpan.End(),
				)
				gotSelectedElementarySpansStrs = append(gotSelectedElementarySpansStrs, s)
			}
			sort.Strings(gotSelectedElementarySpansStrs)
			gotSelectedElementarySpansStr := strings.Join(gotSelectedElementarySpansStrs, ", ")
			if gotSelectedElementarySpansStr != test.wantedSelectedElementarySpansStr {
				t.Errorf("Pos %s found '%s', wanted '%s'", test.positionStr, gotSelectedElementarySpansStr, test.wantedSelectedElementarySpansStr)
			}
		})
	}
}
