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

package testtrace

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/tracey/trace"
)

var tpp = NewPrettyPrinter[time.Duration, StringPayload, StringPayload, StringPayload](TestNamer)

func TestTraceBuildingAndPrettyprinting(t *testing.T) {
	for _, test := range []struct {
		description string
		buildTrace  func() (
			trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
			error,
		)
		hierarchyType trace.HierarchyType
		wantErr       bool
		wantTraceStr  string
	}{{
		description: "suspends appear",
		buildTrace: func() (
			trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
			error,
		) {
			var err error
			tr := NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					RootSpan(0, 100, "a", ParentCategories()),
					RootSpan(0, 100, "b", ParentCategories()),
				).
				WithSuspend(Paths("a"), 60, 80).
				WithDependency(Send, "", Origin(Paths("a"), 20), Destination(Paths("b"), 80)).
				Build()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err.Error())
			}
			return tr, err
		},
		hierarchyType: None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-20ns <none> -> THIS -> [send to b @80ns]
      20ns-60ns <none> -> THIS -> <none>
      80ns-100ns <none> -> THIS -> <none>
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-80ns <none> -> THIS -> <none>
      80ns-100ns [send from a @20ns] -> THIS -> <none>`,
	}, {
		description: "trace1 spans only",
		buildTrace: func() (
			trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
			error,
		) {
			return Trace1()
		},
		hierarchyType: None,
		wantTraceStr: `
Trace spans:
  Span 's0.0.0' (0s-100ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
        'start' @0s
      90ns-100ns [return from s0.0.0/0 @90ns] -> THIS -> <none>
        'end' @100ns
    Span '0' (10ns-90ns) (s0.0.0/0)
      Elementary spans:
        10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
        20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
        30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
        70ns-90ns [return from s0.0.0/0/3 @70ns] -> THIS -> [return to s0.0.0 @90ns]
      Span '3' (40ns-70ns) (s0.0.0/0/3)
        Elementary spans:
          40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
          60ns-70ns [signal from s0.1.0 @50ns] -> THIS -> [return to s0.0.0/0 @70ns]
  Span 's0.1.0' (30ns-70ns) (s0.1.0)
    Elementary spans:
      30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
      40ns-50ns [send from s1.0.0 @35ns] -> THIS -> [signal to s0.0.0/0/3 @60ns]
      50ns-70ns <none> -> THIS -> <none>
  Span 's1.0.0' (30ns-50ns) (s1.0.0)
    Elementary spans:
      30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
      35ns-50ns <none> -> THIS -> <none>`,
	}, {
		description: "trace1 causal",
		buildTrace: func() (
			trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
			error,
		) {
			return Trace1()
		},
		hierarchyType: Causal,
		wantTraceStr: `
Trace (causal):
  Category 'p0' (p0)
    Category 't0.0' (p0/t0.0)
      Category 'r0.0.0' (p0/t0.0/r0.0.0)
        Span 's0.0.0' (0s-100ns) (s0.0.0)
          Elementary spans:
            0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
              'start' @0s
            90ns-100ns [return from s0.0.0/0 @90ns] -> THIS -> <none>
              'end' @100ns
          Span '0' (10ns-90ns) (s0.0.0/0)
            Elementary spans:
              10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
              20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
              30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
              70ns-90ns [return from s0.0.0/0/3 @70ns] -> THIS -> [return to s0.0.0 @90ns]
            Span '3' (40ns-70ns) (s0.0.0/0/3)
              Elementary spans:
                40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
                60ns-70ns [signal from s0.1.0 @50ns] -> THIS -> [return to s0.0.0/0 @70ns]
      Category 't0.1' (p0/t0.0/t0.1)
        Category 'r0.1.0' (p0/t0.0/t0.1/r0.1.0)
          Span 's0.1.0' (30ns-70ns) (s0.1.0)
            Elementary spans:
              30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
              40ns-50ns [send from s1.0.0 @35ns] -> THIS -> [signal to s0.0.0/0/3 @60ns]
              50ns-70ns <none> -> THIS -> <none>
    Category 'p1' (p0/p1)
      Category 't1.0' (p0/p1/t1.0)
        Category 'r1.0.0' (p0/p1/t1.0/r1.0.0)
          Span 's1.0.0' (30ns-50ns) (s1.0.0)
            Elementary spans:
              30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
              35ns-50ns <none> -> THIS -> <none>`,
	}, {
		description: "trace1 structural",
		buildTrace: func() (
			trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
			error,
		) {
			return Trace1()
		},
		hierarchyType: Structural,
		wantTraceStr: `
Trace (structural):
  Category 'p0' (p0)
    Category 't0.0' (p0/t0.0)
      Category 'r0.0.0' (p0/t0.0/r0.0.0)
        Span 's0.0.0' (0s-100ns) (s0.0.0)
          Elementary spans:
            0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
              'start' @0s
            90ns-100ns [return from s0.0.0/0 @90ns] -> THIS -> <none>
              'end' @100ns
          Span '0' (10ns-90ns) (s0.0.0/0)
            Elementary spans:
              10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
              20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
              30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
              70ns-90ns [return from s0.0.0/0/3 @70ns] -> THIS -> [return to s0.0.0 @90ns]
            Span '3' (40ns-70ns) (s0.0.0/0/3)
              Elementary spans:
                40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
                60ns-70ns [signal from s0.1.0 @50ns] -> THIS -> [return to s0.0.0/0 @70ns]
    Category 't0.1' (p0/t0.1)
      Category 'r0.1.0' (p0/t0.1/r0.1.0)
        Span 's0.1.0' (30ns-70ns) (s0.1.0)
          Elementary spans:
            30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
            40ns-50ns [send from s1.0.0 @35ns] -> THIS -> [signal to s0.0.0/0/3 @60ns]
            50ns-70ns <none> -> THIS -> <none>
  Category 'p1' (p1)
    Category 't1.0' (p1/t1.0)
      Category 'r1.0.0' (p1/t1.0/r1.0.0)
        Span 's1.0.0' (30ns-50ns) (s1.0.0)
          Elementary spans:
            30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
            35ns-50ns <none> -> THIS -> <none>`,
	}, {
		description: "multiple origins, OR semantics",
		buildTrace: func() (
			trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
			error,
		) {
			var err error
			originalTrace := NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					RootSpan(0, 50, "a launcher", ParentCategories()),
					RootSpan(0, 100, "b launcher", ParentCategories()),
					RootSpan(50, 100, "cached", ParentCategories()),
					RootSpan(50, 100, "a", ParentCategories()),
					RootSpan(100, 150, "b", ParentCategories()),
				).
				WithDependency(
					Spawn,
					"",
					Origin(Paths("a launcher"), 50),
					Destination(Paths("a"), 50),
				).
				WithDependency(
					Spawn,
					"",
					Origin(Paths("b launcher"), 100),
					Destination(Paths("b"), 100),
				).
				WithDependency(
					Send,
					"",
					trace.MultipleOriginsWithOrSemantics,
					Origin(Paths("a"), 50),
					Origin(Paths("b"), 100),
					Destination(Paths("cached"), 50),
				).
				Build()
			return originalTrace, err
		},
		hierarchyType: trace.SpanOnlyHierarchyType,
		wantTraceStr: `
Trace spans:
  Span 'a' (50ns-100ns) (a)
    Elementary spans:
      50ns-50ns [spawn from a launcher @50ns] -> THIS -> [send to cached @50ns]
      50ns-100ns <none> -> THIS -> <none>
  Span 'a launcher' (0s-50ns) (a launcher)
    Elementary spans:
      0s-50ns <none> -> THIS -> [spawn to a @50ns]
  Span 'b' (100ns-150ns) (b)
    Elementary spans:
      100ns-100ns [spawn from b launcher @100ns] -> THIS -> [send to cached @50ns]
      100ns-150ns <none> -> THIS -> <none>
  Span 'b launcher' (0s-100ns) (b launcher)
    Elementary spans:
      0s-100ns <none> -> THIS -> [spawn to b @100ns]
  Span 'cached' (50ns-100ns) (cached)
    Elementary spans:
      50ns-100ns [send from (triggering) a @50ns (also nontriggering b @100ns)] -> THIS -> <none>`,
	}, {
		description: "multiple origins, AND semantics",
		buildTrace: func() (
			trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
			error,
		) {
			var err error
			originalTrace := NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					RootSpan(100, 150, "blocked", ParentCategories()),
					RootSpan(0, 50, "a", ParentCategories()),
					RootSpan(0, 100, "b", ParentCategories()),
				).
				WithDependency(
					Send,
					"",
					trace.MultipleOriginsWithAndSemantics,
					Origin(Paths("a"), 50),
					Origin(Paths("b"), 100),
					Destination(Paths("blocked"), 100),
				).
				Build()
			return originalTrace, err
		},
		hierarchyType: trace.SpanOnlyHierarchyType,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-50ns) (a)
    Elementary spans:
      0s-50ns <none> -> THIS -> [send to blocked @100ns]
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-100ns <none> -> THIS -> [send to blocked @100ns]
  Span 'blocked' (100ns-150ns) (blocked)
    Elementary spans:
      100ns-150ns [send from (triggering) b @100ns (also nontriggering a @50ns)] -> THIS -> <none>`,
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
			if test.hierarchyType == None {
				gotTraceStr = TPP.PrettyPrintTraceSpans(trace)
			} else {
				gotTraceStr = TPP.PrettyPrintTrace(trace, test.hierarchyType)
			}
			if diff := cmp.Diff(test.wantTraceStr, gotTraceStr); diff != "" {
				t.Errorf("got trace string\n%s\n, diff (-want +got) %s", gotTraceStr, diff)
			}
		})
	}
}
