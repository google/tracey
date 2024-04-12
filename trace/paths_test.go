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

package trace

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func findSpan(
	t *testing.T,
	trace Trace[time.Duration, payload, payload, payload],
	namer Namer[time.Duration, payload, payload, payload],
	path ...string,
) Span[time.Duration, payload, payload, payload] {
	t.Helper()
	ms := literalNameMatchers(path...)
	spans := FindSpans(trace, namer, matchers(ms))
	if len(spans) != 1 {
		t.Fatalf("expected matchers to match exactly one span; matched %d", len(spans))
	}
	return spans[0]
}

func TestSpanPaths(t *testing.T) {
	trace := testTrace1(t)

	for _, test := range []struct {
		span            Span[time.Duration, payload, payload, payload]
		wantDisplayPath []string
		wantUniquePath  []string
	}{{
		span:            findSpan(t, trace, &testNamer{}, "a"),
		wantDisplayPath: strs("a"),
		wantUniquePath:  strs("id:a"),
	}, {
		span:            findSpan(t, trace, &testNamer{}, "a", "b"),
		wantDisplayPath: strs("a", "b"),
		wantUniquePath:  strs("id:a", "id:b"),
	}, {
		span:            findSpan(t, trace, &testNamer{}, "a", "b", "c"),
		wantDisplayPath: strs("a", "b", "c"),
		wantUniquePath:  strs("id:a", "id:b", "id:c"),
	}, {
		span:            findSpan(t, trace, &testNamer{}, "a", "c"),
		wantDisplayPath: strs("a", "c"),
		wantUniquePath:  strs("id:a", "id:c"),
	}, {
		span:            findSpan(t, trace, &testNamer{}, "a", "c", "d"),
		wantDisplayPath: strs("a", "c", "d"),
		wantUniquePath:  strs("id:a", "id:c", "id:d"),
	}, {
		span:            findSpan(t, trace, &testNamer{}, "a", "c", "d", "e"),
		wantDisplayPath: strs("a", "c", "d", "e"),
		wantUniquePath:  strs("id:a", "id:c", "id:d", "id:e"),
	}} {
		t.Run(strings.Join(test.wantUniquePath, "/"), func(t *testing.T) {
			gotDisplayPath := GetSpanDisplayPath[time.Duration, payload, payload, payload](test.span, &testNamer{})
			gotUniquePath := GetSpanUniquePath[time.Duration, payload, payload, payload](test.span, &testNamer{})
			if diff := cmp.Diff(test.wantDisplayPath, gotDisplayPath); diff != "" {
				t.Errorf("Got display path %v, diff (-want +got) %s", gotDisplayPath, diff)
			}
			if diff := cmp.Diff(test.wantUniquePath, gotUniquePath); diff != "" {
				t.Errorf("Got unique path %v, diff (-want +got) %s", gotUniquePath, diff)
			}
		})
	}
}

func TestCategoryPaths(t *testing.T) {
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	a := trace.NewRootCategory(0, "a")
	ab := a.NewChildCategory("b")
	abc := ab.NewChildCategory("c")
	ac := a.NewChildCategory("c")
	acd := ac.NewChildCategory("d")
	acde := acd.NewChildCategory("e")

	for _, test := range []struct {
		category        Category[time.Duration, payload, payload, payload]
		wantDisplayPath []string
		wantUniquePath  []string
	}{{
		category:        a,
		wantDisplayPath: strs("a"),
		wantUniquePath:  strs("a"),
	}, {
		category:        ab,
		wantDisplayPath: strs("a", "b"),
		wantUniquePath:  strs("a", "b"),
	}, {
		category:        abc,
		wantDisplayPath: strs("a", "b", "c"),
		wantUniquePath:  strs("a", "b", "c"),
	}, {
		category:        ac,
		wantDisplayPath: strs("a", "c"),
		wantUniquePath:  strs("a", "c"),
	}, {
		category:        acd,
		wantDisplayPath: strs("a", "c", "d"),
		wantUniquePath:  strs("a", "c", "d"),
	}, {
		category:        acde,
		wantDisplayPath: strs("a", "c", "d", "e"),
		wantUniquePath:  strs("a", "c", "d", "e"),
	}} {
		t.Run(strings.Join(test.wantUniquePath, "/"), func(t *testing.T) {
			gotDisplayPath := GetCategoryDisplayPath[time.Duration, payload, payload, payload](test.category, &testNamer{})
			gotUniquePath := GetCategoryUniquePath[time.Duration, payload, payload, payload](test.category, &testNamer{})
			if diff := cmp.Diff(test.wantDisplayPath, gotDisplayPath); diff != "" {
				t.Errorf("Got display path %v, diff (-want +got) %s", gotDisplayPath, diff)
			}
			if diff := cmp.Diff(test.wantUniquePath, gotUniquePath); diff != "" {
				t.Errorf("Got unique path %v, diff (-want +got) %s", gotUniquePath, diff)
			}
		})
	}
}

func TestPathEncodingAndDecoding(t *testing.T) {
	for _, test := range [][]string{
		strs("a", "b", "c"),
		strs(":", "/", "::", ":"),
		strs("दुक्ख", "समुदाय", "निरोध", "मार्ग"),
		strs("", " ", "	"),
	} {
		t.Run(strings.Join(test, "/"), func(t *testing.T) {
			enc := EncodePath(test...)
			dec, err := DecodePath(enc)
			if err != nil {
				t.Fatalf("failed to decode path: %s", err)
			}
			if diff := cmp.Diff(test, dec); diff != "" {
				t.Errorf("Got decoded path %v, diff (-want +got) %s", dec, diff)
			}
		})
	}
}
