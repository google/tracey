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
package drag

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	criticalpath "github.com/google/tracey/critical_path"
	"github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
	traceparser "github.com/google/tracey/trace/parser"
)

// An adaptation of the example trace with drag from
// https://en.wikipedia.org/wiki/Critical_path_drag.
func wikipediaTrace() (trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error) {
	var err error
	tr := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
		err = gotErr
	}).
		WithRootSpans(
			testtrace.RootSpan(0, 10, "A", testtrace.ParentCategories()),
			testtrace.RootSpan(10, 30, "B", testtrace.ParentCategories()),
			testtrace.RootSpan(30, 35, "C", testtrace.ParentCategories()),
			testtrace.RootSpan(35, 45, "D", testtrace.ParentCategories()),
			testtrace.RootSpan(45, 65, "E", testtrace.ParentCategories()),
			testtrace.RootSpan(10, 25, "F", testtrace.ParentCategories()),
			testtrace.RootSpan(35, 40, "G", testtrace.ParentCategories()),
			testtrace.RootSpan(10, 25, "H", testtrace.ParentCategories()),
		).WithDependency(
		testtrace.Send,
		"",
		testtrace.Origin(testtrace.Paths("A"), 10),
		testtrace.Destination(testtrace.Paths("B"), 10),
	).WithDependency(
		testtrace.Send,
		"",
		testtrace.Origin(testtrace.Paths("A"), 10),
		testtrace.Destination(testtrace.Paths("H"), 10),
	).WithDependency(
		testtrace.Send,
		"",
		testtrace.Origin(testtrace.Paths("A"), 10),
		testtrace.Destination(testtrace.Paths("F"), 10),
	).WithDependency(
		testtrace.Send,
		"",
		testtrace.Origin(testtrace.Paths("B"), 30),
		testtrace.Destination(testtrace.Paths("C"), 30),
	).WithDependency(
		testtrace.Send,
		"",
		testtrace.Origin(testtrace.Paths("C"), 35),
		testtrace.Destination(testtrace.Paths("D"), 35),
	).WithDependency(
		testtrace.Send,
		"",
		testtrace.Origin(testtrace.Paths("C"), 35),
		testtrace.Destination(testtrace.Paths("G"), 35),
	).WithDependency(
		testtrace.Send,
		"",
		testtrace.Origin(testtrace.Paths("D"), 45),
		testtrace.Destination(testtrace.Paths("E"), 45),
	).WithDependency(
		testtrace.Send,
		"",
		testtrace.Origin(testtrace.Paths("F"), 25),
		testtrace.Destination(testtrace.Paths("G"), 35),
	).WithDependency(
		testtrace.Send,
		"",
		testtrace.Origin(testtrace.Paths("H"), 25),
		testtrace.Destination(testtrace.Paths("E"), 45),
	).WithDependency(
		testtrace.Send,
		"",
		testtrace.Origin(testtrace.Paths("G"), 40),
		testtrace.Destination(testtrace.Paths("E"), 45),
	).Build()
	return tr, err
}

func TestComputeActivity(t *testing.T) {
	tpp := testtrace.NewPrettyPrinter(testtrace.TestNamer)
	for _, test := range []struct {
		description string
		computeFn   func() ([]*activity[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error)
		wantESs     string
	}{{
		description: "test1 global",
		computeFn: func() ([]*activity[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error) {
			t, err := testtrace.Trace1()
			if err != nil {
				return nil, err
			}
			return computeGlobalActivity(t)
		},
		wantESs:
		/* 1 predecessor                 10ns
		   2 successors                  40ns 30ns */`
0 10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
    ES 10ns EF 20ns LS 20ns LF 30ns S 10ns` +
			/* 1 predecessor               20ns
			   2 successors                45ns 40ns */`
0 20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
    ES 20ns EF 30ns LS 30ns LF 40ns S 10ns` +
			/* 1 predecessor               30ns
			   2 successors                70ns 50ns */`
0 30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
    ES 30ns EF 40ns LS 40ns LF 50ns S 10ns` +
			/* 2 predecessors              40ns 60ns
			   1 successor                 90ns      */`
0 70ns-90ns [return from s0.0.0/0/3 @70ns] -> THIS -> [return to s0.0.0 @90ns]
    ES 60ns EF 80ns LS 70ns LF 90ns S 10ns` +
			/* 1 predecessor               40ns
			   1 successor                 60ns      */`
3 40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
    ES 40ns EF 50ns LS 50ns LF 60ns S 10ns` +
			/* 2 predecessors              50ns 45ns
			   1 successor                 70ns      */`
3 60ns-70ns [signal from s0.1.0 @50ns] -> THIS -> [return to s0.0.0/0 @70ns]
    ES 50ns EF 60ns LS 60ns LF 70ns S 10ns` +
			/* 0 predecessors (Origin)     0s
			   2 successors                90ns 20ns */`
s0.0.0 0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
    ES 0s EF 10ns LS 10ns LF 20ns S 10ns` +
			/* 2 predecessors              10ns 80ns
			   0 successors (Destination)  100ns     */`
s0.0.0 90ns-100ns [return from s0.0.0/0 @90ns] -> THIS -> <none>
    ES 80ns EF 90ns LS 90ns LF 100ns S 10ns` +
			/* 1 predecessor               20ns
			   1 successor                 50ns      */`
s0.1.0 30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
    ES 20ns EF 30ns LS 40ns LF 50ns S 20ns` +
			/* 2 predecessors              30ns 35ns
			   2 successors                80ns 60ns */`
s0.1.0 40ns-50ns [send from s1.0.0 @35ns] -> THIS -> [signal to s0.0.0/0/3 @60ns]
    ES 35ns EF 45ns LS 50ns LF 60ns S 15ns` +
			/* 1 predecessor               30ns
			   0 successors (Destination)  100ns     */`
s0.1.0 50ns-70ns <none> -> THIS -> <none>
    ES 45ns EF 65ns LS 80ns LF 100ns S 35ns` +
			/* 1 predecessor               30ns
			   2 successors                85ns 50ns */`
s1.0.0 30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
    ES 30ns EF 35ns LS 45ns LF 50ns S 15ns` +
			/* 1 predecessor               35ns
			   0 successors (Destination)  100ns     */`
s1.0.0 35ns-50ns <none> -> THIS -> <none>
    ES 35ns EF 50ns LS 85ns LF 100ns S 50ns`,
	}, {
		description: "wikipedia global",
		computeFn: func() ([]*activity[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error) {
			t, err := wikipediaTrace()
			if err != nil {
				return nil, err
			}
			return computeGlobalActivity(t)
		},
		wantESs: `
A 0s-10ns <none> -> THIS -> [send to H @10ns]
    ES 0s EF 10ns LS 0s LF 10ns S 0s
A 10ns-10ns <none> -> THIS -> [send to B @10ns]
    ES 10ns EF 10ns LS 10ns LF 10ns S 0s
A 10ns-10ns <none> -> THIS -> [send to F @10ns]
    ES 10ns EF 10ns LS 10ns LF 10ns S 0s
B 10ns-30ns [send from A @10ns] -> THIS -> [send to C @30ns]
    ES 10ns EF 30ns LS 10ns LF 30ns S 0s
C 30ns-35ns [send from B @30ns] -> THIS -> [send to G @35ns]
    ES 30ns EF 35ns LS 30ns LF 35ns S 0s
C 35ns-35ns <none> -> THIS -> [send to D @35ns]
    ES 35ns EF 35ns LS 35ns LF 35ns S 0s
D 35ns-45ns [send from C @35ns] -> THIS -> [send to E @45ns]
    ES 35ns EF 45ns LS 35ns LF 45ns S 0s
E 45ns-45ns [send from D @45ns] -> THIS -> <none>
    ES 45ns EF 45ns LS 45ns LF 45ns S 0s
E 45ns-45ns [send from G @40ns] -> THIS -> <none>
    ES 45ns EF 45ns LS 45ns LF 45ns S 0s
E 45ns-65ns [send from H @25ns] -> THIS -> <none>
    ES 45ns EF 65ns LS 45ns LF 65ns S 0s
F 10ns-25ns [send from A @10ns] -> THIS -> [send to G @35ns]
    ES 10ns EF 25ns LS 25ns LF 40ns S 15ns
G 35ns-35ns [send from C @35ns] -> THIS -> <none>
    ES 35ns EF 35ns LS 40ns LF 40ns S 5ns
G 35ns-40ns [send from F @25ns] -> THIS -> [send to E @45ns]
    ES 35ns EF 40ns LS 40ns LF 45ns S 5ns
H 10ns-25ns [send from A @10ns] -> THIS -> [send to E @45ns]
    ES 10ns EF 25ns LS 30ns LF 45ns S 20ns`,
	}, {
		description: "wikipedia F to E",
		computeFn: func() ([]*activity[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error) {
			t, err := wikipediaTrace()
			if err != nil {
				return nil, err
			}
			startPosSpec := "F @0%"
			startES, err := esFromPosition(t, startPosSpec)
			if err != nil {
				return nil, err
			}
			endPosSpec := "E @100%"
			endES, err := esFromPosition(t, endPosSpec)
			if err != nil {
				return nil, err
			}
			return computeActivityBetweenEndpoints(
				trace.DurationComparator,
				startES, endES,
			)
		},
		wantESs: `
E 45ns-45ns [send from G @40ns] -> THIS -> <none>
    ES 30ns EF 30ns LS 45ns LF 45ns S 15ns
E 45ns-65ns [send from H @25ns] -> THIS -> <none>
    ES 30ns EF 50ns LS 45ns LF 65ns S 15ns
F 10ns-25ns [send from A @10ns] -> THIS -> [send to G @35ns]
    ES 10ns EF 25ns LS 25ns LF 40ns S 15ns
G 35ns-40ns [send from F @25ns] -> THIS -> [send to E @45ns]
    ES 25ns EF 30ns LS 40ns LF 45ns S 15ns`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			gotActivities, err := test.computeFn()
			if err != nil {
				t.Fatalf("compute activities yielded unexpected error %v", err)
			}
			gotESSlice := []string{}
			for _, a := range gotActivities {
				gotESSlice = append(gotESSlice,
					fmt.Sprintf("%s\n    ES %v EF %v LS %v LF %v S %v",
						tpp.PrettyPrintElementarySpan(a.es),
						a.earliestStart, a.earliestFinish,
						a.latestStart, a.latestFinish,
						a.latestFinish-a.earliestStart-(a.earliestFinish-a.earliestStart),
					),
				)
			}
			sort.Strings(gotESSlice)
			gotESs := "\n" + strings.Join(gotESSlice, "\n")
			if diff := cmp.Diff(test.wantESs, gotESs); diff != "" {
				t.Errorf("Computed activity:\n%s\ndiff (-want +got) %s", gotESs, diff)
			}
		})
	}
}

type ival struct {
	s, f time.Duration
}

func (iv *ival) start() time.Duration {
	return iv.s
}

func (iv *ival) finish() time.Duration {
	return iv.f
}

func (iv *ival) payload() *ival {
	return iv
}

func (iv *ival) String() string {
	return fmt.Sprintf("(%d, %d)", iv.s, iv.f)
}

func i(s, f time.Duration) interval[time.Duration, *ival] {
	return &ival{s, f}
}

func is(is ...interval[time.Duration, *ival]) []interval[time.Duration, *ival] {
	return is
}

func TestIntersectingIntervals(t *testing.T) {
	for _, test := range []struct {
		ivals                []interval[time.Duration, *ival]
		wantIntersectionsStr string
	}{{
		ivals: is(i(0, 10), i(2, 8), i(5, 15), i(8, 10), i(10, 10), i(10, 20), i(12, 18), i(18, 30)),
		wantIntersectionsStr: `
(0, 10):
  (2, 8), (5, 15), (8, 10), (10, 10), (10, 20)
(2, 8):
  (0, 10), (5, 15), (8, 10)
(5, 15):
  (0, 10), (2, 8), (8, 10), (10, 10), (10, 20), (12, 18)
(8, 10):
  (0, 10), (2, 8), (5, 15), (10, 10), (10, 20)
(10, 10):
  (0, 10), (5, 15), (8, 10), (10, 20)
(10, 20):
  (0, 10), (5, 15), (8, 10), (10, 10), (12, 18), (18, 30)
(12, 18):
  (5, 15), (10, 20), (18, 30)
(18, 30):
  (10, 20), (12, 18)`,
	}} {
		ivs := make([]string, len(test.ivals))
		for idx, iv := range test.ivals {
			ivs[idx] = iv.payload().String()
		}
		t.Run(strings.Join(ivs, ", "), func(t *testing.T) {
			ivf := newIntersectionFinder(trace.DurationComparator, test.ivals)
			gotIntersections := make([]string, len(test.ivals))
			for idx, iv := range test.ivals {
				intersections := map[*ival]struct{}{}
				ivf.intersectingIntervals(iv, func(iiv interval[time.Duration, *ival]) {
					if iv.payload() != iiv.payload() {
						intersections[iiv.payload()] = struct{}{}
					}
				})
				intersectionSlice := make([]*ival, 0, len(intersections))
				for is := range intersections {
					intersectionSlice = append(intersectionSlice, is)
				}
				sort.Slice(intersectionSlice, func(a, b int) bool {
					if intersectionSlice[a].start() == intersectionSlice[b].start() {
						return intersectionSlice[a].finish() < intersectionSlice[b].finish()
					}
					return intersectionSlice[a].start() < intersectionSlice[b].start()
				})
				sortedIntersectionsStrs := make([]string, len(intersectionSlice))
				for idx, is := range intersectionSlice {
					sortedIntersectionsStrs[idx] = is.String()
				}
				gotIntersections[idx] = fmt.Sprintf(
					"%s:\n  %s",
					iv.payload().String(),
					strings.Join(sortedIntersectionsStrs, ", "))
			}
			gotIntersectionsStr := "\n" + strings.Join(gotIntersections, "\n")
			if diff := cmp.Diff(test.wantIntersectionsStr, gotIntersectionsStr); diff != "" {
				t.Errorf("Got intersections:\n%s\n, diff (-want +got) %s",
					gotIntersectionsStr, diff)
			}
		})
	}
}

func TestIntersectingElementarySpans(t *testing.T) {
	tpp := testtrace.NewPrettyPrinter(testtrace.TestNamer)
	for _, test := range []struct {
		description string
		computeFn   func() ([]*activity[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error)
		wantESs     string
	}{{
		description: "test1 global",
		computeFn: func() ([]*activity[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error) {
			t, err := testtrace.Trace1()
			if err != nil {
				return nil, err
			}
			return computeGlobalActivity(t)
		},
		wantESs: `
[0s-20ns] S:10 s0.0.0 0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
[10ns-30ns] S:10 0 10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
  [0s-20ns] S:10 s0.0.0
[20ns-40ns] S:10 0 20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
  [10ns-30ns] S:10 0
  [20ns-50ns] S:20 s0.1.0
[20ns-50ns] S:20 s0.1.0 30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
  [20ns-40ns] S:10 0
  [30ns-50ns] S:10 0
  [30ns-50ns] S:15 s1.0.0
  [35ns-100ns] S:50 s1.0.0
  [35ns-60ns] S:15 s0.1.0
[30ns-50ns] S:10 0 30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
  [20ns-40ns] S:10 0
  [20ns-50ns] S:20 s0.1.0
  [30ns-50ns] S:15 s1.0.0
  [35ns-100ns] S:50 s1.0.0
  [35ns-60ns] S:15 s0.1.0
[30ns-50ns] S:15 s1.0.0 30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
  [20ns-40ns] S:10 0
  [20ns-50ns] S:20 s0.1.0
  [30ns-50ns] S:10 0
[35ns-100ns] S:50 s1.0.0 35ns-50ns <none> -> THIS -> <none>
  [20ns-40ns] S:10 0
  [20ns-50ns] S:20 s0.1.0
  [30ns-50ns] S:10 0
  [30ns-50ns] S:15 s1.0.0
  [35ns-60ns] S:15 s0.1.0
  [40ns-60ns] S:10 3
  [45ns-100ns] S:35 s0.1.0
[35ns-60ns] S:15 s0.1.0 40ns-50ns [send from s1.0.0 @35ns] -> THIS -> [signal to s0.0.0/0/3 @60ns]
  [20ns-50ns] S:20 s0.1.0
  [30ns-50ns] S:10 0
  [30ns-50ns] S:15 s1.0.0
  [35ns-100ns] S:50 s1.0.0
  [40ns-60ns] S:10 3
  [45ns-100ns] S:35 s0.1.0
[40ns-60ns] S:10 3 40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
  [20ns-50ns] S:20 s0.1.0
  [30ns-50ns] S:10 0
  [30ns-50ns] S:15 s1.0.0
  [35ns-100ns] S:50 s1.0.0
  [35ns-60ns] S:15 s0.1.0
  [45ns-100ns] S:35 s0.1.0
[45ns-100ns] S:35 s0.1.0 50ns-70ns <none> -> THIS -> <none>
  [35ns-100ns] S:50 s1.0.0
  [35ns-60ns] S:15 s0.1.0
  [40ns-60ns] S:10 3
  [50ns-70ns] S:10 3
  [60ns-90ns] S:10 0
[50ns-70ns] S:10 3 60ns-70ns [signal from s0.1.0 @50ns] -> THIS -> [return to s0.0.0/0 @70ns]
  [35ns-100ns] S:50 s1.0.0
  [45ns-100ns] S:35 s0.1.0
  [60ns-90ns] S:10 0
[60ns-90ns] S:10 0 70ns-90ns [return from s0.0.0/0/3 @70ns] -> THIS -> [return to s0.0.0 @90ns]
  [35ns-100ns] S:50 s1.0.0
  [45ns-100ns] S:35 s0.1.0
  [80ns-100ns] S:10 s0.0.0
[80ns-100ns] S:10 s0.0.0 90ns-100ns [return from s0.0.0/0 @90ns] -> THIS -> <none>
  [35ns-100ns] S:50 s1.0.0
  [45ns-100ns] S:35 s0.1.0`,
	}, {
		description: "wikipedia global",
		computeFn: func() ([]*activity[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error) {
			t, err := wikipediaTrace()
			if err != nil {
				return nil, err
			}
			return computeGlobalActivity(t)
		},
		wantESs: `
[0s-10ns] S:0 A 0s-10ns <none> -> THIS -> [send to H @10ns]
[10ns-10ns] S:0 A 10ns-10ns <none> -> THIS -> [send to B @10ns]
[10ns-10ns] S:0 A 10ns-10ns <none> -> THIS -> [send to F @10ns]
[10ns-30ns] S:0 B 10ns-30ns [send from A @10ns] -> THIS -> [send to C @30ns]
  [10ns-40ns] S:15 F
  [10ns-45ns] S:20 H
[10ns-40ns] S:15 F 10ns-25ns [send from A @10ns] -> THIS -> [send to G @35ns]
  [10ns-30ns] S:0 B
  [10ns-45ns] S:20 H
[10ns-45ns] S:20 H 10ns-25ns [send from A @10ns] -> THIS -> [send to E @45ns]
  [10ns-30ns] S:0 B
  [10ns-40ns] S:15 F
[30ns-35ns] S:0 C 30ns-35ns [send from B @30ns] -> THIS -> [send to G @35ns]
  [10ns-40ns] S:15 F
  [10ns-45ns] S:20 H
[35ns-35ns] S:0 C 35ns-35ns <none> -> THIS -> [send to D @35ns]
  [10ns-40ns] S:15 F
  [10ns-45ns] S:20 H
[35ns-40ns] S:5 G 35ns-35ns [send from C @35ns] -> THIS -> <none>
  [10ns-40ns] S:15 F
  [10ns-45ns] S:20 H
[35ns-45ns] S:0 D 35ns-45ns [send from C @35ns] -> THIS -> [send to E @45ns]
  [10ns-40ns] S:15 F
  [10ns-45ns] S:20 H
  [35ns-40ns] S:5 G
  [35ns-45ns] S:5 G
[35ns-45ns] S:5 G 35ns-40ns [send from F @25ns] -> THIS -> [send to E @45ns]
  [10ns-40ns] S:15 F
  [10ns-45ns] S:20 H
  [35ns-40ns] S:5 G
  [35ns-45ns] S:0 D
[45ns-45ns] S:0 E 45ns-45ns [send from D @45ns] -> THIS -> <none>
[45ns-45ns] S:0 E 45ns-45ns [send from G @40ns] -> THIS -> <none>
[45ns-65ns] S:0 E 45ns-65ns [send from H @25ns] -> THIS -> <none>`,
	}, {
		description: "wikipedia F to E",
		computeFn: func() ([]*activity[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error) {
			t, err := wikipediaTrace()
			if err != nil {
				return nil, err
			}
			startPosSpec := "F @0%"
			startES, err := esFromPosition(t, startPosSpec)
			if err != nil {
				return nil, err
			}
			endPosSpec := "E @100%"
			endES, err := esFromPosition(t, endPosSpec)
			if err != nil {
				return nil, err
			}
			return computeActivityBetweenEndpoints(
				trace.DurationComparator,
				startES, endES,
			)
		},
		wantESs: `
[10ns-40ns] S:15 F 10ns-25ns [send from A @10ns] -> THIS -> [send to G @35ns]
[25ns-45ns] S:15 G 35ns-40ns [send from F @25ns] -> THIS -> [send to E @45ns]
  [10ns-40ns] S:15 F
  [30ns-45ns] S:15 E
  [30ns-65ns] S:15 E
[30ns-45ns] S:15 E 45ns-45ns [send from G @40ns] -> THIS -> <none>
  [30ns-65ns] S:15 E
[30ns-65ns] S:15 E 45ns-65ns [send from H @25ns] -> THIS -> <none>`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			activitiesByES, err := test.computeFn()
			if err != nil {
				t.Fatalf("compute activities yielded unexpected error %v", err)
			}
			activities := make([]interval[time.Duration, *activity[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]], 0, len(activitiesByES))
			for _, a := range activitiesByES {
				activities = append(activities, a)
			}
			finder := newIntersectionFinder(trace.DurationComparator, activities)
			gotESSlice := []string{}
			for _, a := range activities {
				esi := &esInterval[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{a.payload()}
				iia := newIntersectingIntervalAccumulator[time.Duration, *activity[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]](
					trace.DurationComparator,
					esi,
				)
				finder.intersectingIntervals(
					esi,
					iia.addIntersecting,
				)
				intersectingIntervals := iia.get()
				intersectingIntervalStrs := make([]string, 0, len(intersectingIntervals))
				for ii := range intersectingIntervals {
					intersectingIntervalStrs = append(
						intersectingIntervalStrs,
						fmt.Sprintf("  [%v-%v] S:%v %s",
							ii.start(), ii.finish(),
							trace.DurationComparator.Diff(
								ii.finish(), ii.start(),
							)-trace.DurationComparator.Diff(
								ii.payload().es.End(),
								ii.payload().es.Start(),
							),
							ii.payload().es.Span().Payload().String(),
						),
					)
				}
				sort.Strings(intersectingIntervalStrs)
				row := fmt.Sprintf(
					"[%v-%v] S:%v %s",
					a.start(), a.finish(),
					trace.DurationComparator.Diff(
						a.finish(), a.start(),
					)-trace.DurationComparator.Diff(
						a.payload().es.End(),
						a.payload().es.Start(),
					),
					tpp.PrettyPrintElementarySpan(a.payload().es),
				)
				if len(intersectingIntervalStrs) > 0 {
					row += fmt.Sprintf("\n%s", strings.Join(intersectingIntervalStrs, "\n"))
				}
				gotESSlice = append(gotESSlice, row)
			}
			sort.Strings(gotESSlice)
			gotESs := "\n" + strings.Join(gotESSlice, "\n")
			if diff := cmp.Diff(test.wantESs, gotESs); diff != "" {
				t.Errorf("Computed activity:\n%s\ndiff (-want +got) %s", gotESs, diff)
			}
		})
	}
}

func esFromPosition(
	tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	posSpec string,
) (trace.ElementarySpan[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error) {
	pos, err := traceparser.ParsePositionSpecifiers(testtrace.None, posSpec)
	if err != nil {
		return nil, err
	}
	pf, err := traceparser.NewPositionFinder(pos, tr)
	if err != nil {
		return nil, err
	}
	poses := pf.FindPositions()
	if len(poses) != 1 {
		return nil, err
	}
	return poses[0].ElementarySpan, nil
}

func TestDrag(t *testing.T) {
	tpp := testtrace.NewPrettyPrinter(testtrace.TestNamer)
	for _, test := range []struct {
		description                  string
		traceFn                      func() (trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error)
		finderFn                     func(tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]) (*Finder[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error)
		cpStartPosSpec, cpEndPosSpec string
		cpFn                         func(tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]) (*criticalpath.Path[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error)
		wantESs                      string
	}{{
		description:    "test1 global",
		traceFn:        testtrace.Trace1,
		finderFn:       NewGlobalFinder[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		cpStartPosSpec: "s0.0.0 @0%",
		cpEndPosSpec:   "s0.0.0 @100%",
		wantESs: `
D:   10ns  s0.0.0 0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
D:   10ns  0 10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
D:   10ns  0 20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
D:   10ns  0 30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
D:   10ns  3 40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
D:   10ns  3 60ns-70ns [signal from s0.1.0 @50ns] -> THIS -> [return to s0.0.0/0 @70ns]
D:   10ns  0 70ns-90ns [return from s0.0.0/0/3 @70ns] -> THIS -> [return to s0.0.0 @90ns]
D:   10ns  s0.0.0 90ns-100ns [return from s0.0.0/0 @90ns] -> THIS -> <none>`,
	}, {
		description:    "wikipedia global",
		traceFn:        wikipediaTrace,
		finderFn:       NewGlobalFinder[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		cpStartPosSpec: "A @0%",
		cpEndPosSpec:   "E @100%",
		wantESs: `
D:   10ns  A 0s-10ns <none> -> THIS -> [send to H @10ns]
D:     0s  A 10ns-10ns <none> -> THIS -> [send to F @10ns]
D:     0s  A 10ns-10ns <none> -> THIS -> [send to B @10ns]
D:   15ns  B 10ns-30ns [send from A @10ns] -> THIS -> [send to C @30ns]
D:    5ns  C 30ns-35ns [send from B @30ns] -> THIS -> [send to G @35ns]
D:     0s  C 35ns-35ns <none> -> THIS -> [send to D @35ns]
D:    5ns  D 35ns-45ns [send from C @35ns] -> THIS -> [send to E @45ns]
D:     0s  E 45ns-45ns [send from D @45ns] -> THIS -> <none>
D:     0s  E 45ns-45ns [send from G @40ns] -> THIS -> <none>
D:   20ns  E 45ns-65ns [send from H @25ns] -> THIS -> <none>`,
	}, {
		description: "wikipedia F to E",
		traceFn:     wikipediaTrace,
		finderFn: func(tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]) (*Finder[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error) {
			startPosSpec := "F @0%"
			startES, err := esFromPosition(tr, startPosSpec)
			if err != nil {
				t.Fatalf("Failed to find start position '%s': %v", startPosSpec, err)
			}
			endPosSpec := "E @100%"
			endES, err := esFromPosition(tr, endPosSpec)
			if err != nil {
				t.Fatalf("Failed to find end position '%s': %v", endPosSpec, err)
			}
			return NewEndpointFinder[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload](
				tr, startES, endES,
			)
		},
		cpStartPosSpec: "F @0%",
		cpEndPosSpec:   "E @100%",
		wantESs: `
D:   15ns  F 10ns-25ns [send from A @10ns] -> THIS -> [send to G @35ns]
D:    5ns  G 35ns-40ns [send from F @25ns] -> THIS -> [send to E @45ns]
D:     0s  E 45ns-45ns [send from G @40ns] -> THIS -> <none>
D:   20ns  E 45ns-65ns [send from H @25ns] -> THIS -> <none>`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			tr, err := test.traceFn()
			if err != nil {
				t.Fatalf("Failed to get test trace: %v", err)
			}
			df, err := test.finderFn(tr)
			if err != nil {
				t.Fatalf("Failed to construct drag finder: %v", err)
			}
			gotESSlice := []string{}
			startES, err := esFromPosition(tr, test.cpStartPosSpec)
			if err != nil {
				t.Fatalf("Failed to find start position '%s': %v", test.cpStartPosSpec, err)
			}
			endES, err := esFromPosition(tr, test.cpEndPosSpec)
			if err != nil {
				t.Fatalf("Failed to find start position '%s': %v", test.cpEndPosSpec, err)
			}
			cp, err := criticalpath.FindBetweenElementarySpans(
				tr.Trace(),
				startES, endES,
				criticalpath.PreferMostWork,
			)
			if err != nil {
				t.Fatalf("Failed to get trace critical path: %v", err)
			}
			for _, es := range cp.CriticalPath {
				drag, err := df.Drag(es)
				if err != nil {
					t.Fatalf("Failed to get elementary span drag: %v", err)
				}
				gotESSlice = append(
					gotESSlice,
					fmt.Sprintf(
						"D: %6v  %s",
						time.Duration(drag)*time.Nanosecond,
						tpp.PrettyPrintElementarySpan(es),
					),
				)
			}
			gotESs := "\n" + strings.Join(gotESSlice, "\n")
			if diff := cmp.Diff(test.wantESs, gotESs); diff != "" {
				t.Errorf("Computed activity:\n%s\ndiff (-want +got) %s", gotESs, diff)
			}
		})
	}
}

func TestSlack(t *testing.T) {
	tpp := testtrace.NewPrettyPrinter(testtrace.TestNamer)
	for _, test := range []struct {
		description string
		traceFn     func() (trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error)
		finderFn    func(tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]) (*Finder[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error)
		wantESs     string
	}{{
		description: "test1 global",
		traceFn:     testtrace.Trace1,
		finderFn:    NewGlobalFinder[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		wantESs: `
S:   10ns  s0.0.0 0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
S:   10ns  s0.0.0 90ns-100ns [return from s0.0.0/0 @90ns] -> THIS -> <none>
S:   10ns  0 10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
S:   10ns  0 20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
S:   10ns  0 30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
S:   10ns  0 70ns-90ns [return from s0.0.0/0/3 @70ns] -> THIS -> [return to s0.0.0 @90ns]
S:   10ns  3 40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
S:   10ns  3 60ns-70ns [signal from s0.1.0 @50ns] -> THIS -> [return to s0.0.0/0 @70ns]
S:   20ns  s0.1.0 30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
S:   15ns  s0.1.0 40ns-50ns [send from s1.0.0 @35ns] -> THIS -> [signal to s0.0.0/0/3 @60ns]
S:   35ns  s0.1.0 50ns-70ns <none> -> THIS -> <none>
S:   15ns  s1.0.0 30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
S:   50ns  s1.0.0 35ns-50ns <none> -> THIS -> <none>`,
	}, {
		description: "wikipedia global",
		traceFn:     wikipediaTrace,
		finderFn:    NewGlobalFinder[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		wantESs: `
S:     0s  A 0s-10ns <none> -> THIS -> [send to H @10ns]
S:     0s  A 10ns-10ns <none> -> THIS -> [send to F @10ns]
S:     0s  A 10ns-10ns <none> -> THIS -> [send to B @10ns]
S:     0s  B 10ns-30ns [send from A @10ns] -> THIS -> [send to C @30ns]
S:     0s  C 30ns-35ns [send from B @30ns] -> THIS -> [send to G @35ns]
S:     0s  C 35ns-35ns <none> -> THIS -> [send to D @35ns]
S:     0s  D 35ns-45ns [send from C @35ns] -> THIS -> [send to E @45ns]
S:     0s  E 45ns-45ns [send from D @45ns] -> THIS -> <none>
S:     0s  E 45ns-45ns [send from G @40ns] -> THIS -> <none>
S:     0s  E 45ns-65ns [send from H @25ns] -> THIS -> <none>
S:   15ns  F 10ns-25ns [send from A @10ns] -> THIS -> [send to G @35ns]
S:    5ns  G 35ns-35ns [send from C @35ns] -> THIS -> <none>
S:    5ns  G 35ns-40ns [send from F @25ns] -> THIS -> [send to E @45ns]
S:   20ns  H 10ns-25ns [send from A @10ns] -> THIS -> [send to E @45ns]`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			tr, err := test.traceFn()
			if err != nil {
				t.Fatalf("Failed to get test trace: %v", err)
			}
			df, err := test.finderFn(tr)
			if err != nil {
				t.Fatalf("Failed to construct drag finder: %v", err)
			}
			ess := []trace.ElementarySpan[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{}
			var visit func(span trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload])
			visit = func(span trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]) {
				ess = append(ess, span.ElementarySpans()...)
				for _, child := range span.ChildSpans() {
					visit(child)
				}
			}
			for _, rootSpan := range tr.RootSpans() {
				visit(rootSpan)
			}
			gotESSlice := make([]string, len(ess))
			for idx, es := range ess {
				slack, err := df.Slack(es)
				if err != nil {
					t.Fatalf("Failed to get elementary span drag: %v", err)
				}
				gotESSlice[idx] =
					fmt.Sprintf(
						"S: %6v  %s",
						time.Duration(slack)*time.Nanosecond,
						tpp.PrettyPrintElementarySpan(es),
					)
			}
			gotESs := "\n" + strings.Join(gotESSlice, "\n")
			if diff := cmp.Diff(test.wantESs, gotESs); diff != "" {
				t.Errorf("Computed activity:\n%s\ndiff (-want +got) %s", gotESs, diff)
			}
		})
	}
}
