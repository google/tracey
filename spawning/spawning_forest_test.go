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

package spawning

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	testtrace "github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
)

// Returns a trace with spawning tree:
//
// - A
// --- spawns B
// ----- spawns D
// ----- calls C
// ------- spawns E
// --- calls D
// ----- spawns F
// ------- calls G
// ----- calls H
// ------- spawns I
func spawnyTrace(t *testing.T) (
	trace trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
) {
	t.Helper()
	trace = testtrace.NewTraceBuilderWithErrorHandler(func(gotErr error) {
		t.Fatal(gotErr)
	}).
		WithRootSpans(
			testtrace.RootSpan(0, 100, "A",
				testtrace.ParentCategories(),
				testtrace.Span(10, 90, "D",
					testtrace.Span(20, 80, "H"),
				),
			),
			testtrace.RootSpan(0, 100, "B",
				testtrace.ParentCategories(),
				testtrace.Span(10, 90, "C"),
			),
			testtrace.RootSpan(10, 90, "D",
				testtrace.ParentCategories(),
			),
			testtrace.RootSpan(20, 80, "E",
				testtrace.ParentCategories(),
			),
			testtrace.RootSpan(20, 80, "F",
				testtrace.ParentCategories(),
				testtrace.Span(30, 70, "G"),
			),
			testtrace.RootSpan(30, 70, "I",
				testtrace.ParentCategories(),
			),
		).
		WithDependency(
			testtrace.Spawn, "",
			testtrace.Origin(testtrace.Paths("A"), 10),
			testtrace.Destination(testtrace.Paths("B"), 10),
		).
		WithDependency(
			testtrace.Spawn, "",
			testtrace.Origin(testtrace.Paths("B"), 10),
			testtrace.Destination(testtrace.Paths("D"), 10),
		).
		WithDependency(
			testtrace.Spawn, "",
			testtrace.Origin(testtrace.Paths("B", "C"), 20),
			testtrace.Destination(testtrace.Paths("E"), 20),
		).
		WithDependency(
			testtrace.Spawn, "",
			testtrace.Origin(testtrace.Paths("A", "D"), 20),
			testtrace.Destination(testtrace.Paths("F"), 20),
		).
		WithDependency(
			testtrace.Spawn, "",
			testtrace.Origin(testtrace.Paths("A", "D", "H"), 30),
			testtrace.Destination(testtrace.Paths("I"), 30),
		).
		Build()
	return trace
}

func traceBuilderWrapper(
	builder func() (trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload], error),
) func(t *testing.T) trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload] {
	return func(t *testing.T) trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload] {
		t.Helper()
		trace, err := builder()
		if err != nil {
			t.Fatal(err)
		}
		return trace
	}
}

func TestSpawningTree(t *testing.T) {
	for _, test := range []struct {
		description         string
		buildTrace          func(t *testing.T) trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]
		options             []ForestOption
		wantSpawningTreeStr string
	}{{
		description: "Trace1",
		buildTrace:  traceBuilderWrapper(testtrace.Trace1),
		options: []ForestOption{
			Dependency(testtrace.Spawn, MustBeFirstIncomingDependency),
		},
		wantSpawningTreeStr: `
s0.0.0 was not spawned
s0.0.0/0 spawned s0.1.0
s0.0.0/0 spawned s1.0.0`,
	}, {
		description: "Spawny trace",
		buildTrace:  spawnyTrace,
		options: []ForestOption{
			Dependency(testtrace.Spawn, MustBeFirstIncomingDependency),
		},
		wantSpawningTreeStr: `
A spawned B
A was not spawned
A/D spawned F
A/D/H spawned I
B spawned D
B/C spawned E`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			tr := test.buildTrace(t)
			st, err := NewSpawningForest(tr, test.options...)
			if err != nil {
				t.Fatalf("Failed to build spawning tree: %s", err)
			}
			got := []string{""}
			for _, usr := range st.UnspawnedRootSpans {
				got = append(got,
					fmt.Sprintf(
						"%s was not spawned",
						strings.Join(trace.GetSpanDisplayPath(usr, tr.DefaultNamer()), "/"),
					),
				)
			}
			for spawned, spawner := range st.SpawnersBySpawnedRootSpan {
				got = append(got,
					fmt.Sprintf(
						"%s spawned %s",
						strings.Join(trace.GetSpanDisplayPath(spawner, tr.DefaultNamer()), "/"),
						strings.Join(trace.GetSpanDisplayPath(spawned, tr.DefaultNamer()), "/"),
					),
				)
			}
			sort.Strings(got)
			gotStr := strings.Join(got, "\n")
			if diff := cmp.Diff(test.wantSpawningTreeStr, gotStr); diff != "" {
				t.Errorf("SpawningTree is\n%s\ndiff (-want, +got) %s", gotStr, diff)
			}
		})
	}
}
