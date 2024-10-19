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

package smartdependencies

import (
	"testing"
	"time"

	"github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
	"github.com/google/go-cmp/cmp"
)

func spanFinder(t *testing.T, strs ...string) *trace.SpanFinder[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload] {
	t.Helper()
	ret, err := testtrace.SpanFinderFromPattern(strs)
	if err != nil {
		t.Fatalf("failed to build path matchers: %s", err)
	}
	return ret
}

func abTrace(t *testing.T) trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload] {
	t.Helper()
	var err error
	tr := testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
		err = gotErr
	}).
		WithRootSpans(
			testtrace.RootSpan(0, 100, "a", testtrace.ParentCategories()),
			testtrace.RootSpan(0, 100, "b", testtrace.ParentCategories()),
		).Build()
	if err != nil {
		t.Fatalf("failed to construct AB test: %s", err.Error())
	}
	return tr
}

func TestSmartDependencies(t *testing.T) {
	for _, test := range []struct {
		description string
		buildTrace  func(t *testing.T) trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]

		attachDeps func(
			t *testing.T,
			tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			sds *SmartDependencies[string, time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		)
		wantSDErr     bool
		hierarchyType trace.HierarchyType
		wantTraceStr  string
		wantMetrics   *Metrics // metrics not collected if nil
	}{{
		description: "deps without origins are dropped",
		buildTrace:  abTrace,
		attachDeps: func(
			t *testing.T,
			tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			sds *SmartDependencies[string, time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		) {
			noOrigin, _, err := sds.GetIndexed(testtrace.Send, "", "no_origin", Defaults)
			if err != nil {
				t.Fatalf("failed to get indexed dependency: %s", err.Error())
			}
			noOrigin.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				100,
			)
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
		description: "deps without destinations are dropped",
		buildTrace:  abTrace,
		attachDeps: func(
			t *testing.T,
			tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			sds *SmartDependencies[string, time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		) {
			noDestination, _, err := sds.GetIndexed(testtrace.Send, "", "no_destination", Defaults)
			if err != nil {
				t.Fatalf("failed to get indexed dependency: %s", err.Error())
			}
			noDestination.WithOrigin(
				spanFinder(t, "a").Find(tr)[0],
				100,
			)
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
		description: "keeping dep without origin",
		buildTrace:  abTrace,
		attachDeps: func(
			t *testing.T,
			tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			sds *SmartDependencies[string, time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		) {
			noOrigin := sds.NewUnindexed(testtrace.Send, "", KeepDependenciesWithoutOriginsOrDestinations)
			noOrigin.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				50,
			)
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-100ns <none> -> THIS -> <none>
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-50ns <none> -> THIS -> <none>
      50ns-100ns [send from <unknown>] -> THIS -> <none>`,
	}, {
		description: "keeping dep without destination",
		buildTrace:  abTrace,
		attachDeps: func(
			t *testing.T,
			tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			sds *SmartDependencies[string, time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		) {
			noDestination := sds.NewUnindexed(testtrace.Send, "", KeepDependenciesWithoutOriginsOrDestinations)
			noDestination.WithOrigin(
				spanFinder(t, "a").Find(tr)[0],
				100,
			)
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-100ns <none> -> THIS -> [send to <none>]
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-100ns <none> -> THIS -> <none>`,
		wantMetrics: newMetrics().
			withCreatedDependencies(testtrace.Send, 1),
	}, {
		description: "smart dep is created",
		buildTrace:  abTrace,
		attachDeps: func(
			t *testing.T,
			tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			sds *SmartDependencies[string, time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		) {
			dep, _, err := sds.GetIndexed(testtrace.Send, "", "dep", Defaults)
			if err != nil {
				t.Fatalf("failed to get indexed dependency: %s", err.Error())
			}
			dep.WithOrigin(
				spanFinder(t, "a").Find(tr)[0],
				50,
			)
			dep.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				50,
			)
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-50ns <none> -> THIS -> [send to b @50ns]
      50ns-100ns <none> -> THIS -> <none>
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-50ns <none> -> THIS -> <none>
      50ns-100ns [send from a @50ns] -> THIS -> <none>`,
	}, {
		description: "reuse without AllowReuse fails",
		buildTrace:  abTrace,
		attachDeps: func(
			t *testing.T,
			tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			sds *SmartDependencies[string, time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		) {
			dep, _, err := sds.GetIndexed(testtrace.Send, "", "dep", Defaults)
			if err != nil {
				t.Fatalf("failed to get indexed dependency: %s", err.Error())
			}
			dep.WithOrigin(
				spanFinder(t, "a").Find(tr)[0],
				10,
			)
			dep.WithOrigin(
				spanFinder(t, "a").Find(tr)[0],
				30,
			)
			dep.WithOrigin(
				spanFinder(t, "a").Find(tr)[0],
				60,
			)
			dep.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				5,
			)
			dep.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				20,
			)
			dep.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				40,
			)
			dep.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				50,
			)
		},
		wantSDErr:     true,
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
		description: "reuse with AllowReuse works",
		buildTrace:  abTrace,
		attachDeps: func(
			t *testing.T,
			tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
			sds *SmartDependencies[string, time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
		) {
			dep, _, err := sds.GetIndexed(testtrace.Send, "", "dep", AllowReuse)
			if err != nil {
				t.Fatalf("failed to get indexed dependency: %s", err.Error())
			}
			dep.WithOrigin(
				spanFinder(t, "a").Find(tr)[0],
				10,
			)
			dep.WithOrigin(
				spanFinder(t, "a").Find(tr)[0],
				30,
			)
			dep.WithOrigin(
				spanFinder(t, "a").Find(tr)[0],
				60,
			)
			dep.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				5,
			)
			dep.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				20,
			)
			dep.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				40,
			)
			dep.WithDestination(
				spanFinder(t, "b").Find(tr)[0],
				50,
			)
		},
		hierarchyType: testtrace.None,
		wantTraceStr: `
Trace spans:
  Span 'a' (0s-100ns) (a)
    Elementary spans:
      0s-10ns <none> -> THIS -> [send to b @20ns]
      10ns-30ns <none> -> THIS -> [send to b @40ns, b @50ns]
      30ns-100ns <none> -> THIS -> <none>
  Span 'b' (0s-100ns) (b)
    Elementary spans:
      0s-20ns <none> -> THIS -> <none>
      20ns-40ns [send from a @10ns] -> THIS -> <none>
      40ns-50ns [send from a @30ns] -> THIS -> <none>
      50ns-100ns [send from a @30ns] -> THIS -> <none>`,
		wantMetrics: newMetrics().
			withUnpairedOrigins(testtrace.Send, 1).      // @60
			withUnpairedDestinations(testtrace.Send, 1). // @5
			withCreatedDependencies(testtrace.Send, 2).
			withPairedDependencies(testtrace.Send, 2).
			withDroppedDependencies(testtrace.Send, 2), // <none> -> 5, 60 -> <none>
	}} {
		t.Run(test.description, func(t *testing.T) {
			trace := test.buildTrace(t)
			sds := New[string](trace)
			test.attachDeps(t, trace, sds)
			var sdErr error
			var gotMetrics *Metrics
			if test.wantMetrics == nil {
				sdErr = sds.Close()
			} else {
				gotMetrics, sdErr = sds.CloseWithMetrics()
			}
			if sdErr != nil != test.wantSDErr {
				t.Fatalf("unexpected error at SmartDependencies.Close(): got %v", sdErr)
			}
			if sdErr != nil {
				return
			}
			if test.wantMetrics != nil {
				if diff := cmp.Diff(test.wantMetrics, gotMetrics); diff != "" {
					t.Fatalf("unexpected closure metrics, diff (-want +got) %s", diff)
				}
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
