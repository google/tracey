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

	"github.com/google/go-cmp/cmp"
	tt "github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
	traceparser "github.com/google/tracey/trace/parser"
)

const (
	rootE2EType Type = FirstUserDefinedType
)

var (
	rootE2E = &TypeData{
		Type:        rootE2EType,
		Name:        "rootE2E",
		Description: "Root span, end to end",
	}
)

type testTrace struct {
	tr        trace.Trace[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload]
	endpoints map[Type]*Endpoints[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload]
}

func newTestTrace(t *testing.T) *testTrace {
	tr, err := tt.Trace1()
	if err != nil {
		t.Fatal(err.Error())
	}
	// A default CP runs from s0.0.0 start to end.
	s000Spec, err := traceparser.ParseSpanSpecifierPatterns(trace.SpanOnlyHierarchyType, "s0.0.0")
	if err != nil {
		t.Fatalf("Failed to parse path specifiers: %v", err)
	}
	sf, err := traceparser.NewSpanFinder(s000Spec, tr)
	if err != nil {
		t.Fatalf("Failed to build SpanFinder")
	}
	spans := sf.FindSpans()
	if len(spans) != 1 {
		t.Fatalf("Failed to find unique span s0.0.0")
	}
	s000 := spans[0]
	from := EndpointFromElementarySpan(s000.ElementarySpans()[0], true)
	to := EndpointFromElementarySpan(s000.ElementarySpans()[len(s000.ElementarySpans())-1], false)
	return &testTrace{
		tr: tr,
		endpoints: map[Type]*Endpoints[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload]{
			rootE2EType: &Endpoints[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload]{
				Start: from,
				End:   to,
			},
		},
	}
}

func (tt *testTrace) Trace() trace.Trace[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload] {
	return tt.tr
}

func (tt *testTrace) GetEndpoints(td *TypeData) (*Endpoints[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload], bool) {
	ep, ok := tt.endpoints[td.Type]
	return ep, ok
}

func (tt *testTrace) SupportedTypeData() []*TypeData {
	return []*TypeData{rootE2E}
}

func TestFinder(t *testing.T) {
	tr := newTestTrace(t)
	for _, test := range []struct {
		description string
		finderFn    func() (*Finder, error)
		wantCPStr   string
	}{{
		description: "root e2e",
		finderFn: func() (*Finder, error) {
			return NewFinder(rootE2E, trace.SpanOnlyHierarchyType, "", "", PreferMostWork)
		},
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
		description: "s0.0.0 @0% to s0.1.0 @100%",
		finderFn: func() (*Finder, error) {
			return NewFinder(CustomCriticalPathTypeData, trace.SpanOnlyHierarchyType, "s0.0.0 @0%", "s0.1.0 @100%", PreferMostWork)
		},
		wantCPStr: `
s0.0.0: 0s-10ns
0: 10ns-20ns
0: 20ns-30ns
s1.0.0: 30ns-35ns
s0.1.0: 40ns-50ns
s0.1.0: 50ns-70ns`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			finder, err := test.finderFn()
			if err != nil {
				t.Fatalf("Failed to construct finder: %v", err)
			}
			gotCP, err := Find(finder, tr, nil)
			if err != nil {
				t.Fatalf("Failed to find CP: %v", err)
			}
			var ret []string
			for _, es := range gotCP.CriticalPath {
				ret = append(
					ret,
					fmt.Sprintf(
						"%s: %s-%s",
						tt.TestNamer.SpanName(es.Span()),
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
