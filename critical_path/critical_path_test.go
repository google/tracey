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

package criticalpath

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tt "github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
	"github.com/google/go-cmp/cmp"
)

func ns(dur time.Duration) time.Duration {
	return dur
}

type endpointSpec struct {
	spanPath string
	at       time.Duration
}

func endpoint(spanPath string, at time.Duration) *endpointSpec {
	return &endpointSpec{
		spanPath: spanPath,
		at:       at,
	}
}

func getEndpoint(
	t trace.Trace[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload],
	spanPathStr string,
	at time.Duration,
) (*Endpoint[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload], error) {
	matchers, err := tt.PathMatchersFromPattern([]string{spanPathStr})
	if err != nil {
		return nil, err
	}
	spans := trace.FindSpans[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload](
		t, t.DefaultNamer(), matchers,
	)
	if len(spans) != 1 {
		return nil, fmt.Errorf("exactly one span must match the path pattern '%s' (got %d)", spanPathStr, len(spans))
	}
	return &Endpoint[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload]{
		Span: spans[0],
		At:   at,
	}, nil
}

func TestFind(t *testing.T) {
	for _, test := range []struct {
		description string
		buildTrace  func() (
			trace.Trace[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload],
			error,
		)
		strategy  Strategy
		from, to  *endpointSpec
		wantCPStr string
	}{{
		description: "trace1 e2e prefer causal",
		buildTrace:  tt.Trace1,
		strategy:    PreferCausal,
		from:        endpoint("s0.0.0", ns(0)),
		to:          endpoint("s0.0.0", ns(100)),
		wantCPStr: `
s0.0.0: 0s-10ns
0: 10ns-20ns
0: 20ns-30ns
s1.0.0: 30ns-35ns
s0.1.0: 40ns-50ns
3: 60ns-70ns
0: 70ns-90ns
s0.0.0: 90ns-100ns`,
	}, {
		description: "trace1 e2e prefer predecessor",
		buildTrace:  tt.Trace1,
		strategy:    PreferPredecessor,
		from:        endpoint("s0.0.0", ns(0)),
		to:          endpoint("s0.0.0", ns(100)),
		wantCPStr: `
s0.0.0: 0s-10ns
s0.0.0: 90ns-100ns`,
	}, {
		description: "trace1 e2e prefer most proximate",
		buildTrace:  tt.Trace1,
		strategy:    PreferMostProximate,
		from:        endpoint("s0.0.0", ns(0)),
		to:          endpoint("s0.0.0", ns(100)),
		wantCPStr: `
s0.0.0: 0s-10ns
0: 10ns-20ns
s0.1.0: 30ns-40ns
s0.1.0: 40ns-50ns
3: 60ns-70ns
0: 70ns-90ns
s0.0.0: 90ns-100ns`,
	}, {
		// Same as 'prefer predecessor' in this trace.
		description: "trace1 e2e prefer least proximate",
		buildTrace:  tt.Trace1,
		strategy:    PreferLeastProximate,
		from:        endpoint("s0.0.0", ns(0)),
		to:          endpoint("s0.0.0", ns(100)),
		wantCPStr: `
s0.0.0: 0s-10ns
s0.0.0: 90ns-100ns`,
	}, {
		description: "trace1 e2e prefer most work",
		buildTrace:  tt.Trace1,
		strategy:    PreferMostWork,
		from:        endpoint("s0.0.0", ns(0)),
		to:          endpoint("s0.0.0", ns(100)),
		wantCPStr: `
s0.0.0: 0s-10ns
0: 10ns-20ns
0: 20ns-30ns
0: 30ns-40ns
3: 40ns-50ns
3: 60ns-70ns
0: 70ns-90ns
s0.0.0: 90ns-100ns`,
	}, {
		description: "trace1 e2e prefer least work",
		buildTrace:  tt.Trace1,
		strategy:    PreferLeastWork,
		from:        endpoint("s0.0.0", ns(0)),
		to:          endpoint("s0.0.0", ns(100)),
		wantCPStr: `
s0.0.0: 0s-10ns
s0.0.0: 90ns-100ns`,
	}, {
		description: "trace cycles are tolerated under 'most work'",
		buildTrace: func() (
			trace trace.Trace[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload],
			err error,
		) {
			trace = tt.NewTraceBuilderWithErrorHandler(func(gotErr error) {
				err = gotErr
			}).WithRootSpans(
				tt.RootSpan(0, 100, "a", nil,
					tt.Span(50, 50, "b"),
				),
			).Build()
			return trace, err
		},
		strategy: PreferMostWork,
		from:     endpoint("a", ns(0)),
		to:       endpoint("a", ns(100)),
		wantCPStr: `
a: 0s-50ns
a: 50ns-50ns
a: 50ns-100ns`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			tr, err := test.buildTrace()
			if err != nil {
				t.Fatalf("Trace building failed: %v", err)
			}
			from, err := getEndpoint(tr, test.from.spanPath, test.from.at)
			if err != nil {
				t.Fatalf("Failed to find specified from point: %v", err)
			}
			to, err := getEndpoint(tr, test.to.spanPath, test.to.at)
			if err != nil {
				t.Fatalf("Failed to find specified to point: %v", err)
			}
			cp, err := Find(
				trace.DurationComparator,
				test.strategy,
				from, to,
			)
			if err != nil {
				t.Fatalf("Failed to find critical path: %v", err)
			}
			ret := []string{}
			for _, es := range cp {
				ret = append(
					ret,
					fmt.Sprintf(
						"%s: %s-%s",
						tr.DefaultNamer().SpanName(es.Span()),
						es.Start(), es.End(),
					),
				)
			}
			gotCPStr := "\n" + strings.Join(ret, "\n")
			if diff := cmp.Diff(test.wantCPStr, gotCPStr); diff != "" {
				t.Errorf("CP was\n%s\ndiff (-want +got) %s", gotCPStr, diff)
			}
		})
	}
}