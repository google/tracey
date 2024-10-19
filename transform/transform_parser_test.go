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
	"regexp"
	"testing"
	"time"

	"github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
	"github.com/google/go-cmp/cmp"
)

func TestRegexes(t *testing.T) {
	t.Logf("Comment RE is '%s'", buildRegex(`#`, anything("_")))
	for _, test := range []struct {
		input string
		re    *regexp.Regexp
	}{{
		input: "# scale spans(**) by 0;",
		re:    commentRE,
	}, {
		input: "scale spans(**) by 0;",
		re:    scaleSpansRE,
	}, {
		input: `
SCALE
  SPANS(**)
BY 0
;`,
		re: scaleSpansRE,
	}, {
		input: `scale spans(foo) by .5; scale spans(bar) by .8;`,
		re:    scaleSpansRE,
	}, {
		input: `scale dependencies(all) from spans(**) to spans(**/(foo.**)/**) by 0.0;`,
		re:    scaleDependenciesRE,
	}, {
		input: `add dependency(send) from position(foo/bar at 30%) to positions(foo/baz at 50%);`,
		re:    addDependenciesRE,
	}, {
		input: `remove dependencies(signal) from spans(foo/bar) to spans(foo/baz);`,
		re:    removeDependenciesRE,
	}, {
		input: `apply concurrency 10 to spans(**/(foo_[0-9]+));`,
		re:    applyConcurrencyRE,
	}, {
		input: `apply concurrency 10 to spans(**/(foo_[0-9]+));`,
		re:    applyConcurrencyRE,
	}, {
		input: `start spans(**) as early as possible;`,
		re:    startSpansAsEarlyAsPossibleRE,
	}} {
		t.Run(test.input, func(t *testing.T) {
			gotMatch := test.re.MatchString(test.input)
			if !gotMatch {
				t.Errorf("expected input to match, but it didn't")
			}
		})
	}
}

func TestTraceTransformsViaTemplate(t *testing.T) {
	for _, test := range []struct {
		description string
		buildTrace  func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		)
		transformationTemplate string
		hierarchyType          trace.HierarchyType
		wantTraceStr           string
	}{{
		description: "leading comment is ignored",
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
			return originalTrace, err
		},
		transformationTemplate: `
# scale spans(b) by .5;
scale spans(a) by .5;`,
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
		description: "trailing comment is ignored",
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
			return originalTrace, err
		},
		transformationTemplate: `
scale spans(a) by .5;
# scale spans(b) by .5;`,
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
			return originalTrace, err
		},
		transformationTemplate: `scale spans(a) by .5;`,
		hierarchyType:          testtrace.None,
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
			return originalTrace, err
		},
		transformationTemplate: `scale dependencies(send) from spans(a) to spans(b) by 2.0;`,
		hierarchyType:          testtrace.None,
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
			return originalTrace, err
		},
		transformationTemplate: `add dependency(signal) from position(b at 80%) to positions(c at 0%);`,
		hierarchyType:          testtrace.None,
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
			transformation := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]().
				WithSpansStartingAsEarlyAsPossible(spanFinder(t, "a"))
			transformedTrace, err := transformation.TransformTrace(originalTrace)
			return transformedTrace, err
		},
		transformationTemplate: `start spans(a) as early as possible;`,
		hierarchyType:          testtrace.None,
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
		description: "removed dependency",
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
			return originalTrace, err
		},
		transformationTemplate: `remove dependencies(send) from spans(a) to spans(b); start spans(b) as early as possible;`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-100ns <none> -> THIS -> <none>
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-100ns <none> -> THIS -> <none>`,
	}, {
		description: "added and removed dependencies;",
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
			return originalTrace, err
		},
		transformationTemplate: `remove dependencies(all) from spans(a) to spans(b);
  add dependency(signal) from position(b at 100%) to positions(a at 0%);
  start spans(b) as early as possible;`,
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
			return originalTrace, err
		},
		transformationTemplate: `scale spans(dest-shrink,dest-noshrink) by .5; shrink nonblocking dependencies(all) to spans(dest-shrink);`,
		hierarchyType:          testtrace.None,
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
			return originalTrace, err
		},
		transformationTemplate: `apply concurrency 1 to spans(a,b);`,
		hierarchyType:          testtrace.None,
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
			return originalTrace, err
		},
		transformationTemplate: `apply concurrency 3 to spans(a,b,c);`,
		hierarchyType:          testtrace.None,
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
		description: "ideal (all dependencies scaled by 0x, all spans start as early as possible) testtrace.Trace1",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `scale dependencies(all) from spans(*) to spans(*) by 0; start spans(*) as early as possible;`,
		hierarchyType:          testtrace.None,
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
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `scale spans(s0.1.0) by .5;`,
		hierarchyType:          testtrace.None,
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
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `add dependency(signal) from position(s1.0.0 at 100%) to positions(s0.1.0 at 30%);`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-108ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
      98ns-108ns [return from s0.0.0/0 @98ns] -> THIS -> <none>
    Span '0' (10ns-98ns) (s0.0.0/0)
      Elementary spans:
        10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
        20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
        30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
        78ns-98ns [return from s0.0.0/0/3 @78ns] -> THIS -> [return to s0.0.0 @98ns]
      Span '3' (40ns-78ns) (s0.0.0/0/3)
        Elementary spans:
          40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
          68ns-78ns [signal from s0.1.0 @58ns] -> THIS -> [return to s0.0.0/0 @78ns]
  Span 's0.1.0' (30ns-78ns) (s0.1.0)
    Elementary spans:
      30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
      40ns-42ns [send from s1.0.0 @35ns] -> THIS -> <none>
      50ns-58ns [signal from s1.0.0 @50ns] -> THIS -> [signal to s0.0.0/0/3 @68ns]
      58ns-78ns <none> -> THIS -> <none>
  Span 's1.0.0' (30ns-50ns) (s1.0.0)
    Elementary spans:
      30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
      35ns-50ns <none> -> THIS -> [signal to s0.1.0 @50ns]`,
	}, {
		description: "testtrace.Trace1 with Signal from s0.1.0 to s1.0.0 removed",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `remove dependencies(signal) from spans(s0.1.0) to spans(s0.0.0/0/3);`,
		hierarchyType:          testtrace.None,
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
			originalTrace, err := test.buildTrace()
			if err != nil {
				t.Fatalf("Got unexpected error %v", err)
			}
			transform := New[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]()
			if err := ApplyTransformationTemplate(originalTrace.DefaultNamer(), trace.SpanOnlyHierarchyType, transform, test.transformationTemplate); err != nil {
				t.Fatalf("Failed to apply transformation template: %s", err)
			}
			transformedTrace, err := transform.TransformTrace(originalTrace)
			if err != nil {
				t.Fatalf("Failed to transform trace: %s", err)
			}
			var gotTraceStr string
			if test.hierarchyType == testtrace.None {
				gotTraceStr = testtrace.TPP.PrettyPrintTraceSpans(transformedTrace)
			} else {
				gotTraceStr = testtrace.TPP.PrettyPrintTrace(transformedTrace, test.hierarchyType)
			}
			if diff := cmp.Diff(test.wantTraceStr, gotTraceStr); diff != "" {
				t.Errorf("got trace string\n%s\n, diff (-want +got) %s", gotTraceStr, diff)
			}
		})
	}
}
