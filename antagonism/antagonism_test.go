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

package antagonism

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/tracey/critical_path"
	"github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
	traceparser "github.com/google/tracey/trace/parser"
)

type testVictim struct {
	victim      trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]
	antagonisms map[trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]]time.Duration
}

func (tv *testVictim) prettyPrint() string {
	ret := make([]string, len(tv.antagonisms)+1)
	ret[0] = fmt.Sprintf("  Victim %s:", testtrace.TestNamer.SpanName(tv.victim))
	orderedAntagonists := make([]trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], 0, len(tv.antagonisms))
	for antagonist := range tv.antagonisms {
		orderedAntagonists = append(orderedAntagonists, antagonist)
	}
	sort.Slice(orderedAntagonists, func(a, b int) bool {
		return testtrace.TestNamer.SpanName(orderedAntagonists[a]) <
			testtrace.TestNamer.SpanName(orderedAntagonists[b])
	})
	for idx, antagonist := range orderedAntagonists {
		ret[idx+1] = fmt.Sprintf("    antagonised by %s: %s", testtrace.TestNamer.SpanName(antagonist), tv.antagonisms[antagonist])
	}
	return strings.Join(ret, "\n")
}

func (tv *testVictim) logAntagonism(
	antagonists map[ElementarySpanner[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]]struct{},
	start, end time.Duration,
) {
	for antagonist := range antagonists {
		tv.antagonisms[antagonist.ElementarySpan().Span()] += end - start
	}
}

type testGroup struct {
	group   *Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]
	victims map[trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]]*testVictim
}

func (tg *testGroup) logAntagonism(
	victim ElementarySpanner[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	antagonists map[ElementarySpanner[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]]struct{},
	start, end time.Duration,
) {
	tv, ok := tg.victims[victim.ElementarySpan().Span()]
	if !ok {
		tv = &testVictim{
			victim:      victim.ElementarySpan().Span(),
			antagonisms: map[trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]]time.Duration{},
		}
		tg.victims[victim.ElementarySpan().Span()] = tv
	}
	tv.logAntagonism(antagonists, start, end)
}

func (tg *testGroup) prettyPrint() string {
	orderedVictims := make([]trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], 0, len(tg.victims))
	for victim := range tg.victims {
		orderedVictims = append(orderedVictims, victim)
	}
	sort.Slice(orderedVictims, func(a, b int) bool {
		return testtrace.TestNamer.SpanName(orderedVictims[a]) <
			testtrace.TestNamer.SpanName(orderedVictims[b])
	})
	ret := make([]string, len(orderedVictims)+1)
	ret[0] = fmt.Sprintf("Antagonism group '%s':", tg.group.Name())
	for idx, victim := range orderedVictims {
		ret[idx+1] = tg.victims[victim].prettyPrint()
	}
	return strings.Join(ret, "\n")
}

type testLogger struct {
	groups map[*Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]]*testGroup
}

func (tl *testLogger) LogAntagonism(
	group *Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	victims, antagonists map[ElementarySpanner[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]]struct{},
	start, end time.Duration,
) error {
	tg, ok := tl.groups[group]
	if !ok {
		tg = &testGroup{
			group:   group,
			victims: map[trace.Span[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]]*testVictim{},
		}
		tl.groups[group] = tg
	}
	for victim := range victims {
		tg.logAntagonism(victim, antagonists, start, end)
	}
	return nil
}

func (tl *testLogger) prettyPrint() string {
	orderedGroups := make([]*Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], 0, len(tl.groups))
	for group := range tl.groups {
		orderedGroups = append(orderedGroups, group)
	}
	sort.Slice(orderedGroups, func(a, b int) bool {
		return orderedGroups[a].Name() < orderedGroups[b].Name()
	})
	ret := make([]string, len(orderedGroups))
	for idx, group := range orderedGroups {
		ret[idx] = tl.groups[group].prettyPrint()
	}
	return strings.Join(ret, "\n")
}

func pm(
	pms ...trace.PathElementMatcher,
) []trace.PathElementMatcher {
	return pms
}

func pms(
	pms ...[]trace.PathElementMatcher,
) [][]trace.PathElementMatcher {
	return pms
}

func criticalAntagonismsFunction(cpSpanPattern ...string) func(t trace.Wrapper[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]) (
	[]trace.ElementarySpan[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
	error,
) {
	return func(t trace.Wrapper[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]) (
		[]trace.ElementarySpan[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		error,
	) {
		rootSpanPattern, err := traceparser.ParseSpanSpecifierPatterns(trace.SpanOnlyHierarchyType, strings.Join(cpSpanPattern, "/"))
		if err != nil {
			return nil, err
		}
		rootSpanFinder, err := traceparser.NewSpanFinder(rootSpanPattern, t.Trace())
		if err != nil {
			return nil, err
		}
		rootSpans := rootSpanFinder.FindSpans()
		if len(rootSpans) != 1 {
			return nil, fmt.Errorf("expected exactly one span matching '%s'; got %d", strings.Join(cpSpanPattern, "/"), len(rootSpans))
		}
		path, err := criticalpath.FindBetweenEndpoints(
			t.Trace(),
			&criticalpath.Endpoint[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
				Span: rootSpans[0],
				At:   rootSpans[0].Start(),
			},
			&criticalpath.Endpoint[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
				Span: rootSpans[0],
				At:   rootSpans[0].End(),
			},
			criticalpath.PreferMostWork,
		)
		if err != nil {
			return nil, err
		}
		return path.CriticalPath, nil
	}
}

func spanSpec(t *testing.T, spec string) *traceparser.SpanPattern {
	t.Helper()
	sp, err := traceparser.ParseSpanSpecifierPatterns(
		trace.SpanOnlyHierarchyType, spec,
	)
	if err != nil {
		t.Fatal(err.Error())
	}
	return sp
}

func TestFindAntagonisms(t *testing.T) {
	for _, test := range []struct {
		description string
		buildTrace  func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		)
		antagonismGroups   []*Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]
		wantAntagonistsStr string
	}{{
		description: "round-robin scheduling",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			trace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 25, "a1", nil),
					testtrace.RootSpan(50, 75, "a2", nil),
					testtrace.RootSpan(25, 50, "b1", nil),
					testtrace.RootSpan(75, 100, "b2", nil),
				).
				WithDependency(
					testtrace.Send, "",
					testtrace.Origin(testtrace.Paths("a1"), 25),
					testtrace.Destination(testtrace.Paths("a2"), 50),
				).
				WithDependency(
					testtrace.Send, "",
					testtrace.Origin(testtrace.Paths("b1"), 50),
					testtrace.Destination(testtrace.Paths("b2"), 75),
				).
				Build()
			return trace, err
		},
		antagonismGroups: []*Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
			NewGroup[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]("all").
				WithVictimSpanPattern(spanSpec(t, "**")).
				WithAntagonistSpanPattern(spanSpec(t, "**")),
		},
		wantAntagonistsStr: `Antagonism group 'all':
  Victim a2:
    antagonised by b1: 25ns
  Victim b1:
    antagonised by a1: 25ns
  Victim b2:
    antagonised by a2: 25ns`,
	}, {
		description: "rescheduling delay",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			trace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", nil),
					testtrace.RootSpan(0, 75, "b", nil),
					testtrace.RootSpan(50, 75, "c", nil),
				).
				WithDependency(
					testtrace.Send, "",
					testtrace.Origin(testtrace.Paths("b"), 50),
					testtrace.DestinationAfterWait(testtrace.Paths("a"), 25, 75),
				).
				Build()
			return trace, err
		},
		antagonismGroups: []*Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
			NewGroup[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]("all").
				WithVictimSpanPattern(spanSpec(t, "**")).
				WithAntagonistSpanPattern(spanSpec(t, "**")),
		},
		wantAntagonistsStr: `Antagonism group 'all':
  Victim a:
    antagonised by b: 25ns
    antagonised by c: 25ns
  Victim c:
    antagonised by a: 25ns
    antagonised by b: 50ns`}, {
		description: "suspends contribute to antagonism",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			trace := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", nil),
					testtrace.RootSpan(0, 100, "b", nil),
				).
				WithSuspend(testtrace.Paths("a"), 0, 20).
				WithSuspend(testtrace.Paths("a"), 40, 60).
				WithSuspend(testtrace.Paths("a"), 80, 100).
				WithSuspend(testtrace.Paths("b"), 20, 40).
				WithSuspend(testtrace.Paths("b"), 60, 80).
				Build()
			return trace, err
		},
		antagonismGroups: []*Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
			NewGroup[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]("all").
				WithVictimSpanPattern(spanSpec(t, "**")).
				WithAntagonistSpanPattern(spanSpec(t, "**")),
		},
		wantAntagonistsStr: `Antagonism group 'all':
  Victim a:
    antagonised by b: 60ns
  Victim b:
    antagonised by a: 40ns`,
	}, {
		description: "simple critical antagonism",
		buildTrace: func() (
			trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			error,
		) {
			var err error
			tr := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).
				WithRootSpans(
					testtrace.RootSpan(0, 100, "a", nil,
						testtrace.Span(10, 90, "b"),
					),
					testtrace.RootSpan(30, 70, "c", nil),
					testtrace.RootSpan(10, 90, "d", nil),
				).
				// b suspends just longer than its spawn to c,
				WithSuspend(testtrace.Paths("a", "b"), 15, 31).
				// and again just longer than c's signal back to it.  This ensures b
				// doesn't make the max-work critical path.
				WithSuspend(testtrace.Paths("a", "b"), 69, 80).
				WithDependency(
					testtrace.Spawn, "",
					testtrace.Origin(testtrace.Paths("a", "b"), 15),
					testtrace.Destination(testtrace.Paths("c"), 30),
				).
				WithDependency(
					testtrace.Signal, "",
					testtrace.Origin(testtrace.Paths("c"), 70),
					testtrace.Destination(testtrace.Paths("a", "b"), 80),
				).
				Build()
				/*
					Trace (Span-only):
						Span 'a' (0s-100ns) (a)
							Elementary spans:
								0s-10ns <none> -> THIS -> [call to a/b @10ns]
								10ns-10ns <none> -> THIS -> <none>
								90ns-100ns [return from a/b @90ns] -> THIS -> <none>
							Span 'b' (10ns-90ns) (a/b)
								Elementary spans:
									10ns-15ns [call from a @10ns] -> THIS -> [spawn to c @30ns]
									31ns-69ns <none> -> THIS -> <none>
									80ns-90ns [signal from c @70ns] -> THIS -> [return to a @90ns]
						Span 'c' (30ns-70ns) (c)
							Elementary spans:
								30ns-70ns [spawn from a/b @15ns] -> THIS -> [signal to a/b @80ns]
						Span 'd' (10ns-90ns) (d)
							Elementary spans:
								10ns-90ns <none> -> THIS -> <none>

					The end-to-end max-work critical span of 'a' will then be:
						a: 0s-10ns    (nothing else running)
						b: 10ns-15ns  (no initial delay)
						c: 30ns-70ns  (antagonized by d for 15ns)
						b: 80ns-90ns  (antagonized by d for 10ns)
						a: 90ns-100ns (nothing else running)
				*/
			return tr, err
		},
		antagonismGroups: []*Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
			NewGroup[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]("critical victims vs all").
				WithVictimElementarySpansFn(
					criticalAntagonismsFunction("a"),
				).
				WithAntagonistSpanPattern(spanSpec(t, "**")),
		}, wantAntagonistsStr: `Antagonism group 'critical victims vs all':
  Victim b:
    antagonised by d: 10ns
  Victim c:
    antagonised by d: 15ns`,
	}, {
		description: "critical antagonisms",
		/*
		   Trace1 is:
		     Category 'p0' (p0)
		       Category 't0.0' (p0/t0.0)
		         Category 'r0.0.0' (p0/t0.0/r0.0.0)
		           Span 's0.0.0' (0s-100ns) (s0.0.0)
		             Elementary spans:
		               0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
		               90ns-100ns [return from s0.0.0/0 @90ns] -> THIS -> <none>
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
		                 35ns-50ns <none> -> THIS -> <none>
		   In Trace1, span s0.0.0's end-to-end max work critical path is:
		     s0.0.0: 0s-10ns     (nothing else running)
		     0: 10ns-20ns        (no initial delay)
		     0: 20ns-30ns        (no initial delay)
		     0: 30ns-40ns        (no initial delay)
		     3: 40ns-50ns        (no initial delay)
		     3: 60ns-70ns        (antagonized for 10ns by s0.1.0)
		     0: 70ns-90ns        (no initial delay)
		     s0.0.0: 90ns-100ns  (nothing else running)
		*/
		buildTrace: testtrace.Trace1,
		antagonismGroups: []*Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
			NewGroup[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]("critical victims vs all").
				WithVictimElementarySpansFn(
					func(t trace.Wrapper[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]) (
						[]trace.ElementarySpan[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
						error,
					) {
						rootSpanPattern, err := traceparser.ParseSpanSpecifierPatterns(trace.SpanOnlyHierarchyType, "s0.0.0")
						if err != nil {
							return nil, err
						}
						rootSpanFinder, err := traceparser.NewSpanFinder(rootSpanPattern, t.Trace())
						if err != nil {
							return nil, err
						}
						rootSpans := rootSpanFinder.FindSpans()
						if len(rootSpans) != 1 {
							return nil, fmt.Errorf("expected exactly one span matching 's0.0.0'; got %d", len(rootSpans))
						}
						path, err := criticalpath.FindBetweenEndpoints(
							t.Trace(),
							&criticalpath.Endpoint[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
								Span: rootSpans[0],
								At:   rootSpans[0].Start(),
							},
							&criticalpath.Endpoint[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]{
								Span: rootSpans[0],
								At:   rootSpans[0].End(),
							},
							criticalpath.PreferMostWork,
						)
						if err != nil {
							return nil, err
						}
						return path.CriticalPath, nil
					},
				).
				WithAntagonistSpanPattern(spanSpec(t, "**")),
		},
		wantAntagonistsStr: `Antagonism group 'critical victims vs all':
  Victim 3:
    antagonised by s0.1.0: 10ns`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			tr, err := test.buildTrace()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err)
			}
			logger := &testLogger{
				groups: map[*Group[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]]*testGroup{},
			}
			if err := Analyze(
				tr,
				logger,
				test.antagonismGroups,
			); err != nil {
				t.Fatalf("failed to process trace: %s", err)
			}
			gotAntagonistsStr := logger.prettyPrint()
			if diff := cmp.Diff(test.wantAntagonistsStr, gotAntagonistsStr); diff != "" {
				t.Errorf("Got antagonists:\n%s\ndiff(-want +got) %s", gotAntagonistsStr, diff)
			}
		})
	}
}
