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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	testtrace "github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
)

func spawningForest(
	t *testing.T,
	tr trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload],
) *Forest[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload] {
	t.Helper()
	sf, err := NewSpawningForest(
		tr,
		Dependency(testtrace.Spawn, MustBeFirstIncomingDependency),
	)
	if err != nil {
		t.Fatalf("Failed to build spawning forest: %v", err)
	}
	return sf
}

func pems(ms ...trace.PathElementMatcher) []trace.PathElementMatcher {
	return ms
}

func TestMatching(t *testing.T) {
	for _, test := range []struct {
		description           string
		buildTrace            func(t *testing.T) trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]
		spanPatternBuilder    *SpanPattern
		wantMatchingSpanNames []string
	}{{
		description: "Trace1 all spans spawned at least one level under s0.0.0",
		buildTrace:  traceBuilderWrapper(testtrace.Trace1),
		spanPatternBuilder: NewSpanPatternBuilder(
			trace.NewSpanPattern(
				trace.SpanMatchers(
					pems(trace.NewLiteralNameMatcher("s0.0.0"), trace.Globstar),
				),
			),
		).EventuallySpawning(
			trace.NewSpanPattern(
				trace.SpanMatchers(pems(trace.Globstar)),
			),
		),
		wantMatchingSpanNames: []string{"s0.1.0", "s1.0.0"},
	}, {
		description: "Spawny trace, all spans spawned exactly two levels under A",
		buildTrace:  spawnyTrace,
		spanPatternBuilder: NewSpanPatternBuilder(
			trace.NewSpanPattern(
				trace.SpanMatchers(
					pems(trace.NewLiteralNameMatcher("A")),
				),
			),
		).DirectlySpawning(
			trace.NewSpanPattern(
				trace.SpanMatchers(
					pems(trace.Globstar),
				),
			),
		).DirectlySpawning(
			trace.NewSpanPattern(
				trace.SpanMatchers(
					pems(trace.Globstar),
				),
			),
		),
		wantMatchingSpanNames: []string{"D", "E"},
	}, {
		description: "Spawny trace, everything spawned under a non-root span",
		buildTrace:  spawnyTrace,
		spanPatternBuilder: NewSpanPatternBuilder(
			trace.NewSpanPattern(
				trace.SpanMatchers(
					pems(trace.Star, trace.Star, trace.Globstar),
				),
			),
		).DirectlySpawning(
			trace.NewSpanPattern(
				trace.SpanMatchers(
					pems(trace.Globstar),
				),
			),
		),
		wantMatchingSpanNames: []string{"E", "F", "F/G", "I"},
	}} {
		t.Run(test.description, func(t *testing.T) {
			tr := test.buildTrace(t)
			sf := NewSpanFinder(
				test.spanPatternBuilder,
				tr,
				spawningForest(t, tr),
			)
			gotSpans := sf.FindSpans()
			var got []string
			for _, span := range gotSpans {
				got = append(
					got,
					strings.Join(
						trace.GetSpanDisplayPath(span, tr.DefaultNamer()),
						"/",
					),
				)
			}
			sort.Strings(got)
			if diff := cmp.Diff(test.wantMatchingSpanNames, got); diff != "" {
				t.Errorf("Matched spans:\n%s\ndiff (-want +got) %s", got, diff)
			}
		})
	}
}
