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

package parser

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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
		description: "two scaled spans",
		input:       `scale spans(foo) by .5; scale spans(bar) by .8;`,
	}, {
		description: "scaled dependencies",
		input:       `scale dependencies(all) from spans(**) to spans(**/(foo.**)/**) by 0.0;`,
	}, {
		description: "added dependencies",
		input:       `add dependency(send) from position(foo/bar @ 30%) to positions(foo/baz @ 50%);`,
	}, {
		description: "removed dependencies",
		input:       `remove dependencies(signal) from spans(foo/bar) to spans(foo/baz);`,
	}, {
		description: "applied concurrency",
		input:       `apply concurrency(10) to spans(**/(foo_[0-9]+));`,
	}, {
		description: "start spans as early as possible",
		input:       `start spans(**) as early as possible;`,
	}, {
		description:    "can't scale that bad span",
		input:          `scale spans(a//b) by .5;`,
		wantErrPointer: "              ^",
	}, {
		description:    "where's that at?!",
		input:          `add dependency(send) from position(a @ 30%) to positions(b);`,
		wantErrPointer: "                                                        ^",
	}, {
		description:    "no remove span functionality yet",
		input:          `remove spans(a);`,
		wantErrPointer: "       ^",
	}, {
		description:    "float concurrency",
		input:          `apply concurrency(1.5) to spans(**);`,
		wantErrPointer: "                 ^",
	}, {
		description:    "garbage scale factor concurrency",
		input:          `scale spans(a, b) by ONE THOUSAND;`,
		wantErrPointer: "                     ^",
	}, {
		description:    "typo",
		input:          `SCALE SPANS(a) BO 3.5;`,
		wantErrPointer: "               ^",
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
		wantErr                bool
	}{{
		description: "unknown dependency errors",
		buildTrace:  testtrace.Trace1,
		transformationTemplate: `
REMOVE DEPENDENCIES(foo) FROM SPANS(any) TO SPANS(any);`,
		hierarchyType: testtrace.None,
		wantErr:       true,
	}, {
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
		description: "leading multiline comment is ignored",
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
// scale spans(b) by .5;
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
		description: "inline comment is ignored",
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
		transformationTemplate: `scale spans(a) by /* 3 */ .5;`,
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
		description: "scaled spans' downstream deps are not pushed earlier with incoming deps",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "QUS", testtrace.ParentCategories()),
					testtrace.RootSpan(
						200, 300, "FPPC",
						testtrace.ParentCategories(),
						testtrace.Span(210, 270, "TS")),
				).
				WithDependency(
					testtrace.Signal,
					"",
					testtrace.Origin(testtrace.Paths("QUS"), 99),
					testtrace.Destination(testtrace.Paths("FPPC"), 280),
				).
				WithDependency(
					testtrace.Signal,
					"",
					testtrace.Origin(testtrace.Paths("QUS"), 100),
					testtrace.Destination(testtrace.Paths("FPPC"), 200),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			return originalTrace, err
		},
		transformationTemplate: `scale spans(FPPC/TS) by 0;`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'FPPC' (200ns-300ns) (FPPC)
    Elementary spans:
      200ns-210ns [signal from QUS @100ns] -> THIS -> [call to FPPC/TS @210ns]
      210ns-220ns [return from FPPC/TS @210ns] -> THIS -> <none>
      280ns-300ns [signal from QUS @99ns] -> THIS -> <none>
    Span 'TS' (210ns-210ns) (FPPC/TS)
      Elementary spans:
        210ns-210ns [call from FPPC @210ns] -> THIS -> [return to FPPC @210ns]
  Span 'QUS' (0s-100ns) (QUS)
    Elementary spans:
      0s-99ns <none> -> THIS -> [signal to FPPC @280ns]
      99ns-100ns <none> -> THIS -> [signal to FPPC @200ns]`,
	}, {
		description: "scaled spans' downstream deps are pushed earlier with incoming deps when ideal",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "QUS", testtrace.ParentCategories()),
					testtrace.RootSpan(
						200, 300, "FPPC",
						testtrace.ParentCategories(),
						testtrace.Span(210, 270, "TS")),
				).
				WithDependency(
					testtrace.Signal,
					"",
					testtrace.Origin(testtrace.Paths("QUS"), 99),
					testtrace.Destination(testtrace.Paths("FPPC"), 280),
				).
				WithDependency(
					testtrace.Signal,
					"",
					testtrace.Origin(testtrace.Paths("QUS"), 100),
					testtrace.Destination(testtrace.Paths("FPPC"), 200),
				).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			return originalTrace, err
		},
		transformationTemplate: `
          scale spans(FPPC/TS) by 0;
          scale dependencies(all) from spans(all) to spans(all) by 0;
          start spans(all) as early as possible;`,
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'FPPC' (100ns-140ns) (FPPC)
    Elementary spans:
      100ns-110ns [signal from QUS @100ns] -> THIS -> [call to FPPC/TS @110ns]
      110ns-120ns [return from FPPC/TS @110ns] -> THIS -> <none>
      120ns-140ns [signal from QUS @99ns] -> THIS -> <none>
    Span 'TS' (110ns-110ns) (FPPC/TS)
      Elementary spans:
        110ns-110ns [call from FPPC @110ns] -> THIS -> [return to FPPC @110ns]
  Span 'QUS' (0s-100ns) (QUS)
    Elementary spans:
      0s-99ns <none> -> THIS -> [signal to FPPC @120ns]
      99ns-100ns <none> -> THIS -> [signal to FPPC @100ns]`,
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
		transformationTemplate: `add dependency(signal) from position(b @ 80%) to positions(c @ 0%);`,
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
					testtrace.RootSpan(0, 100, "b", testtrace.ParentCategories(),
						testtrace.Mark("origin", 80),
					),
					testtrace.RootSpan(50, 100, "c", testtrace.ParentCategories(),
						testtrace.Mark("destination", 50),
					),
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
		transformationTemplate: `add dependency(signal) from position(b @ (origin)) to positions(c @ (dest.*));`,
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
		description: "mark scaling",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 50, "z", testtrace.ParentCategories()),
					testtrace.RootSpan(50, 150, "a", testtrace.ParentCategories(),
						testtrace.Mark("mark", 100),
					),
				).
				Build()
			return originalTrace, err
		},
		transformationTemplate: `scale spans(a) by .5; start spans(a) as early as possible;`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-50ns) (a)
    Elementary spans:
      0s-50ns <none> -> THIS -> <none>
        'mark' @25ns
  Span 'z' (0s-50ns) (z)
    Elementary spans:
      0s-50ns <none> -> THIS -> <none>`,
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
			return originalTrace, err
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
			return originalTrace, err
		},
		transformationTemplate: `remove dependencies(all) from spans(a) to spans(b);
  add dependency(signal) from position(b @ 100%) to positions(a @ 0%);
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
		description: "added and removed dependencies 2",
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
					testtrace.RootSpan(100, 150, "c", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					testtrace.Origin(testtrace.Paths("a"), 50),
					testtrace.Destination(testtrace.Paths("b"), 50),
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
		transformationTemplate: `remove dependencies(all) from spans(b) to spans(c);
  add dependency(signal) from position(a @ 100%) to positions(c @ 0%);
  start spans(c) as early as possible;`,
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-50ns) (a)
    Elementary spans:
      0s-50ns <none> -> THIS -> [signal to c @50ns]
      50ns-50ns <none> -> THIS -> [send to b @50ns]
  Span 'b' (50ns-100ns) (b)
    Elementary spans:
      50ns-100ns [send from a @50ns] -> THIS -> <none>
  Span 'c' (0s-100ns) (c)
    Elementary spans:
      0s-0s <none> -> THIS -> <none>
      50ns-100ns [signal from a @50ns] -> THIS -> <none>`,
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
      0s-5ns <none> -> THIS -> [send to dest-shrink @35ns]
      5ns-5ns <none> -> THIS -> [send to dest-shrink @40ns]
      5ns-5ns <none> -> THIS -> [send to dest-noshrink @60ns]
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
		transformationTemplate: `apply concurrency(1) to spans(a,b);`,
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
		transformationTemplate: `apply concurrency(3) to spans(a,b,c);`,
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
		description: "caching OR semantics",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 50, "a launcher", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 100, "b launcher", testtrace.ParentCategories()),
					testtrace.RootSpan(50, 100, "cached", testtrace.ParentCategories()),
					testtrace.RootSpan(50, 100, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(100, 150, "b", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Spawn,
					"",
					testtrace.Origin(testtrace.Paths("a launcher"), 50),
					testtrace.Destination(testtrace.Paths("a"), 50),
				).
				WithDependency(
					testtrace.Spawn,
					"",
					testtrace.Origin(testtrace.Paths("b launcher"), 100),
					testtrace.Destination(testtrace.Paths("b"), 100),
				).
				WithDependency(
					testtrace.Send,
					"",
					trace.MultipleOriginsWithOrSemantics,
					testtrace.Origin(testtrace.Paths("a"), 50),
					testtrace.Origin(testtrace.Paths("b"), 100),
					testtrace.Destination(testtrace.Paths("cached"), 50),
				).
				Build()
			return originalTrace, err
		},
		transformationTemplate: `
scale spans(a launcher) by 2.0;
scale spans(b launcher) by .5;`,
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (100ns-150ns) (a)
    Elementary spans:
      100ns-100ns [spawn from a launcher @100ns] -> THIS -> [send to cached @50ns]
      100ns-150ns <none> -> THIS -> <none>
  Span 'a launcher' (0s-100ns) (a launcher)
    Elementary spans:
      0s-100ns <none> -> THIS -> [spawn to a @100ns]
  Span 'b' (50ns-100ns) (b)
    Elementary spans:
      50ns-50ns [spawn from b launcher @50ns] -> THIS -> [send to cached @50ns]
      50ns-100ns <none> -> THIS -> <none>
  Span 'b launcher' (0s-50ns) (b launcher)
    Elementary spans:
      0s-50ns <none> -> THIS -> [spawn to b @50ns]
  Span 'cached' (50ns-100ns) (cached)
    Elementary spans:
      50ns-100ns [send from (triggering) b @50ns (also nontriggering a @100ns)] -> THIS -> <none>`,
	}, {
		description: "blocking on multiple items AND semantics",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(100, 150, "blocked", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 50, "a", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 100, "b", testtrace.ParentCategories()),
				).
				WithDependency(
					testtrace.Send,
					"",
					trace.MultipleOriginsWithAndSemantics,
					testtrace.Origin(testtrace.Paths("b"), 100),
					testtrace.Origin(testtrace.Paths("a"), 50),
					testtrace.Destination(testtrace.Paths("blocked"), 100),
				).
				Build()
			return originalTrace, err
		},
		transformationTemplate: `
scale spans((^b$)) by .4;`,
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-50ns) (a)
    Elementary spans:
      0s-50ns <none> -> THIS -> [send to blocked @50ns]
  Span 'b' (0s-40ns) (b)
    Elementary spans:
      0s-40ns <none> -> THIS -> [send to blocked @50ns]
  Span 'blocked' (50ns-100ns) (blocked)
    Elementary spans:
      50ns-100ns [send from (triggering) a @50ns (also nontriggering b @40ns)] -> THIS -> <none>`,
	}, {
		description: "ideal (all dependencies scaled by 0x, all spans start as early as possible) testtrace.Trace1",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `scale dependencies(all) from spans(**) to spans(**) by 0; start spans(**) as early as possible;`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-90ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
        'start' @0s
      80ns-90ns [return from s0.0.0/0 @80ns] -> THIS -> <none>
        'end' @90ns
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
        'start' @0s
      85ns-95ns [return from s0.0.0/0 @85ns] -> THIS -> <none>
        'end' @95ns
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
		transformationTemplate: `add dependency(signal) from position(s1.0.0 @ 100%) to positions(s0.1.0 @ 30%);`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-109ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
        'start' @0s
      99ns-109ns [return from s0.0.0/0 @99ns] -> THIS -> <none>
        'end' @109ns
    Span '0' (10ns-99ns) (s0.0.0/0)
      Elementary spans:
        10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
        20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
        30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
        79ns-99ns [return from s0.0.0/0/3 @79ns] -> THIS -> [return to s0.0.0 @99ns]
      Span '3' (40ns-79ns) (s0.0.0/0/3)
        Elementary spans:
          40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
          69ns-79ns [signal from s0.1.0 @59ns] -> THIS -> [return to s0.0.0/0 @79ns]
  Span 's0.1.0' (30ns-79ns) (s0.1.0)
    Elementary spans:
      30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
      40ns-41ns [send from s1.0.0 @35ns] -> THIS -> <none>
      50ns-59ns [signal from s1.0.0 @50ns] -> THIS -> [signal to s0.0.0/0/3 @69ns]
      59ns-79ns <none> -> THIS -> <none>
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
        'start' @0s
      80ns-90ns [return from s0.0.0/0 @80ns] -> THIS -> <none>
        'end' @90ns
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
	}, {
		description: "testtrace.Trace1 with s0.0.0 increased by 10ns",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `change spans(s0.0.0) by duration(10ns);`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-110ns) (s0.0.0)
    Elementary spans:
      0s-15ns <none> -> THIS -> [call to s0.0.0/0 @15ns]
        'start' @0s
      95ns-110ns [return from s0.0.0/0 @95ns] -> THIS -> <none>
        'end' @110ns
    Span '0' (15ns-95ns) (s0.0.0/0)
      Elementary spans:
        15ns-25ns [call from s0.0.0 @15ns] -> THIS -> [spawn to s0.1.0 @35ns]
        25ns-35ns <none> -> THIS -> [spawn to s1.0.0 @35ns]
        35ns-45ns <none> -> THIS -> [call to s0.0.0/0/3 @45ns]
        75ns-95ns [return from s0.0.0/0/3 @75ns] -> THIS -> [return to s0.0.0 @95ns]
      Span '3' (45ns-75ns) (s0.0.0/0/3)
        Elementary spans:
          45ns-55ns [call from s0.0.0/0 @45ns] -> THIS -> <none>
          65ns-75ns [signal from s0.1.0 @55ns] -> THIS -> [return to s0.0.0/0 @75ns]
  Span 's0.1.0' (35ns-75ns) (s0.1.0)
    Elementary spans:
      35ns-45ns [spawn from s0.0.0/0 @25ns] -> THIS -> <none>
      45ns-55ns [send from s1.0.0 @40ns] -> THIS -> [signal to s0.0.0/0/3 @65ns]
      55ns-75ns <none> -> THIS -> <none>
  Span 's1.0.0' (35ns-55ns) (s1.0.0)
    Elementary spans:
      35ns-40ns [spawn from s0.0.0/0 @35ns] -> THIS -> [send to s0.1.0 @45ns]
      40ns-55ns <none> -> THIS -> <none>`,
	}, {
		description: "testtrace.Trace1 with s0.0.0 decreased by 6ns",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `change spans(s0.0.0) by duration(-6ns);`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-94ns) (s0.0.0)
    Elementary spans:
      0s-7ns <none> -> THIS -> [call to s0.0.0/0 @7ns]
        'start' @0s
      87ns-94ns [return from s0.0.0/0 @87ns] -> THIS -> <none>
        'end' @94ns
    Span '0' (7ns-87ns) (s0.0.0/0)
      Elementary spans:
        7ns-17ns [call from s0.0.0 @7ns] -> THIS -> [spawn to s0.1.0 @27ns]
        17ns-27ns <none> -> THIS -> [spawn to s1.0.0 @27ns]
        27ns-37ns <none> -> THIS -> [call to s0.0.0/0/3 @37ns]
        67ns-87ns [return from s0.0.0/0/3 @67ns] -> THIS -> [return to s0.0.0 @87ns]
      Span '3' (37ns-67ns) (s0.0.0/0/3)
        Elementary spans:
          37ns-47ns [call from s0.0.0/0 @37ns] -> THIS -> <none>
          57ns-67ns [signal from s0.1.0 @47ns] -> THIS -> [return to s0.0.0/0 @67ns]
  Span 's0.1.0' (27ns-67ns) (s0.1.0)
    Elementary spans:
      27ns-37ns [spawn from s0.0.0/0 @17ns] -> THIS -> <none>
      37ns-47ns [send from s1.0.0 @32ns] -> THIS -> [signal to s0.0.0/0/3 @57ns]
      47ns-67ns <none> -> THIS -> <none>
  Span 's1.0.0' (27ns-47ns) (s1.0.0)
    Elementary spans:
      27ns-32ns [spawn from s0.0.0/0 @27ns] -> THIS -> [send to s0.1.0 @37ns]
      32ns-47ns <none> -> THIS -> <none>`,
	}, {
		description: "testtrace.Trace1 with s0.0.0/0 decreased by 25ns",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `change spans(s0.0.0/0) by duration(-25ns);`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-85ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
        'start' @0s
      75ns-85ns [return from s0.0.0/0 @75ns] -> THIS -> <none>
        'end' @85ns
    Span '0' (10ns-75ns) (s0.0.0/0)
      Elementary spans:
        10ns-15ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @25ns]
        15ns-20ns <none> -> THIS -> [spawn to s1.0.0 @20ns]
        20ns-25ns <none> -> THIS -> [call to s0.0.0/0/3 @25ns]
        65ns-75ns [return from s0.0.0/0/3 @65ns] -> THIS -> [return to s0.0.0 @75ns]
      Span '3' (25ns-65ns) (s0.0.0/0/3)
        Elementary spans:
          25ns-35ns [call from s0.0.0/0 @25ns] -> THIS -> <none>
          55ns-65ns [signal from s0.1.0 @45ns] -> THIS -> [return to s0.0.0/0 @65ns]
  Span 's0.1.0' (25ns-65ns) (s0.1.0)
    Elementary spans:
      25ns-35ns [spawn from s0.0.0/0 @15ns] -> THIS -> <none>
      35ns-45ns [send from s1.0.0 @25ns] -> THIS -> [signal to s0.0.0/0/3 @55ns]
      45ns-65ns <none> -> THIS -> <none>
  Span 's1.0.0' (20ns-40ns) (s1.0.0)
    Elementary spans:
      20ns-25ns [spawn from s0.0.0/0 @20ns] -> THIS -> [send to s0.1.0 @35ns]
      25ns-40ns <none> -> THIS -> <none>`,
	}, {
		description: "testtrace.Trace1 with all spans scaled by .5 but capped at 20ns",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `scale spans(s0.0.0/0) by .5; cap spans(s0.0.0/0) below duration(20ns);`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-85ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
        'start' @0s
      75ns-85ns [return from s0.0.0/0 @75ns] -> THIS -> <none>
        'end' @85ns
    Span '0' (10ns-75ns) (s0.0.0/0)
      Elementary spans:
        10ns-15ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @25ns]
        15ns-20ns <none> -> THIS -> [spawn to s1.0.0 @20ns]
        20ns-25ns <none> -> THIS -> [call to s0.0.0/0/3 @25ns]
        65ns-75ns [return from s0.0.0/0/3 @65ns] -> THIS -> [return to s0.0.0 @75ns]
      Span '3' (25ns-65ns) (s0.0.0/0/3)
        Elementary spans:
          25ns-35ns [call from s0.0.0/0 @25ns] -> THIS -> <none>
          55ns-65ns [signal from s0.1.0 @45ns] -> THIS -> [return to s0.0.0/0 @65ns]
  Span 's0.1.0' (25ns-65ns) (s0.1.0)
    Elementary spans:
      25ns-35ns [spawn from s0.0.0/0 @15ns] -> THIS -> <none>
      35ns-45ns [send from s1.0.0 @25ns] -> THIS -> [signal to s0.0.0/0/3 @55ns]
      45ns-65ns <none> -> THIS -> <none>
  Span 's1.0.0' (20ns-40ns) (s1.0.0)
    Elementary spans:
      20ns-25ns [spawn from s0.0.0/0 @20ns] -> THIS -> [send to s0.1.0 @35ns]
      25ns-40ns <none> -> THIS -> <none>`,
	}, {
		description: "testtrace.Trace1 with all spans scaled by 2 but capped at 50ns",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `scale spans(**) by 2;  cap spans(**) above duration(50ns);`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-137ns) (s0.0.0)` /* scaled from 20ns->40ns */ + `
    Elementary spans:
      0s-20ns <none> -> THIS -> [call to s0.0.0/0 @20ns]
        'start' @0s
      117ns-137ns [return from s0.0.0/0 @117ns] -> THIS -> <none>
        'end' @137ns
    Span '0' (20ns-117ns) (s0.0.0/0)` /* already 50ns, so unscaled */ + `
      Elementary spans:
        20ns-30ns [call from s0.0.0 @20ns] -> THIS -> [spawn to s0.1.0 @40ns]
        30ns-40ns <none> -> THIS -> [spawn to s1.0.0 @40ns]
        40ns-50ns <none> -> THIS -> [call to s0.0.0/0/3 @50ns]
        97ns-117ns [return from s0.0.0/0/3 @97ns] -> THIS -> [return to s0.0.0 @117ns]
      Span '3' (50ns-97ns) (s0.0.0/0/3)` /* scaled from 20ns->40ns */ + `
        Elementary spans:
          50ns-70ns [call from s0.0.0/0 @50ns] -> THIS -> <none>
          77ns-97ns [signal from s0.1.0 @67ns] -> THIS -> [return to s0.0.0/0 @97ns]
  Span 's0.1.0' (40ns-92ns) (s0.1.0)` /* scaled from 40ns->49ns (rounding error) */ + `
    Elementary spans:
      40ns-52ns [spawn from s0.0.0/0 @30ns] -> THIS -> <none>
      55ns-67ns [send from s1.0.0 @50ns] -> THIS -> [signal to s0.0.0/0/3 @77ns]
      67ns-92ns <none> -> THIS -> <none>
  Span 's1.0.0' (40ns-80ns) (s1.0.0)` /* scaled from 20ns->40ns */ + `
    Elementary spans:
      40ns-50ns [spawn from s0.0.0/0 @40ns] -> THIS -> [send to s0.1.0 @55ns]
      50ns-80ns <none> -> THIS -> <none>`,
	}, {
		description: "testtrace.Trace1 with s0.0.0/0 capped at 25ns",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			originalTrace, err := testtrace.Trace1()
			return originalTrace, err
		},
		transformationTemplate: `cap spans(s0.0.0/0) above duration(25ns);`,
		hierarchyType:          testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-85ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
        'start' @0s
      75ns-85ns [return from s0.0.0/0 @75ns] -> THIS -> <none>
        'end' @85ns
    Span '0' (10ns-75ns) (s0.0.0/0)
      Elementary spans:
        10ns-15ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @25ns]
        15ns-20ns <none> -> THIS -> [spawn to s1.0.0 @20ns]
        20ns-25ns <none> -> THIS -> [call to s0.0.0/0/3 @25ns]
        65ns-75ns [return from s0.0.0/0/3 @65ns] -> THIS -> [return to s0.0.0 @75ns]
      Span '3' (25ns-65ns) (s0.0.0/0/3)
        Elementary spans:
          25ns-35ns [call from s0.0.0/0 @25ns] -> THIS -> <none>
          55ns-65ns [signal from s0.1.0 @45ns] -> THIS -> [return to s0.0.0/0 @65ns]
  Span 's0.1.0' (25ns-65ns) (s0.1.0)
    Elementary spans:
      25ns-35ns [spawn from s0.0.0/0 @15ns] -> THIS -> <none>
      35ns-45ns [send from s1.0.0 @25ns] -> THIS -> [signal to s0.0.0/0/3 @55ns]
      45ns-65ns <none> -> THIS -> <none>
  Span 's1.0.0' (20ns-40ns) (s1.0.0)
    Elementary spans:
      20ns-25ns [spawn from s0.0.0/0 @20ns] -> THIS -> [send to s0.1.0 @35ns]
      25ns-40ns <none> -> THIS -> <none>`,
	}, {
		description: "caps apply after all other duration transforms",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			originalTrace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "grow", testtrace.ParentCategories()),
					testtrace.RootSpan(0, 100, "shrink", testtrace.ParentCategories()),
				).
				Build()
			return originalTrace, err
		},
		transformationTemplate: `
scale spans(grow) by 1.5; change spans(grow) by duration(100ns); cap spans(grow) above duration(200ns);
scale spans(shrink) by .5; change spans(shrink) by duration(-50ns); cap spans(shrink) below duration(25ns);`,
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'grow' (0s-200ns) (grow)
    Elementary spans:
      0s-200ns <none> -> THIS -> <none>
  Span 'shrink' (0s-25ns) (shrink)
    Elementary spans:
      0s-25ns <none> -> THIS -> <none>`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			originalTrace, err := test.buildTrace()
			if err != nil {
				t.Fatalf("Got unexpected error %v", err)
			}
			transform, err := ParseTransformTemplate(trace.SpanOnlyHierarchyType, originalTrace.DefaultNamer(), test.transformationTemplate)
			if err != nil != test.wantErr {
				t.Fatalf("unexpected error parsing transform template: %s", err)
			}
			if err != nil {
				return
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
