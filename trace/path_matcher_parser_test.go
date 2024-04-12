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
)

func TestPathElementParsing(t *testing.T) {
	tr := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	spanA := tr.NewRootSpan(0, 100, "a")
	spanB, err := spanA.NewChildSpan(DurationComparator, 10, 40, "b")
	if err != nil {
		t.Fatalf("failed to add span B to the trace")
	}
	spanC1, err := spanB.NewChildSpan(DurationComparator, 20, 30, "c")
	if err != nil {
		t.Fatalf("failed to add span C (1) to the trace")
	}
	if _, err := spanC1.NewChildSpan(DurationComparator, 22, 24, "e"); err != nil {
		t.Fatalf("failed to add span E to the trace")
	}
	if _, err := spanC1.NewChildSpan(DurationComparator, 26, 28, "/"); err != nil {
		t.Fatalf("failed to add span / to the trace")
	}
	spanD, err := spanA.NewChildSpan(DurationComparator, 60, 90, "d")
	if err != nil {
		t.Fatalf("failed to add span D to the trace")
	}
	if _, err := spanD.NewChildSpan(DurationComparator, 65, 75, "c"); err != nil {
		t.Fatalf("failed to add span C (2) to the trace")
	}
	if _, err := spanD.NewChildSpan(DurationComparator, 77, 85, "c"); err != nil {
		t.Fatalf("failed to add span C (3) to the trace")
	}
	pmp, err := NewPathMatcherParser[time.Duration, payload, payload, payload]()
	if err != nil {
		t.Fatalf("failed to build path matcher parser: %s", err)
	}
	for _, test := range []struct {
		description           string
		pathMatchersStr       string
		wantSelectedSpanPaths string
	}{{
		description:           "all cs",
		pathMatchersStr:       "**/c",
		wantSelectedSpanPaths: "a/b/c, a/d/c, a/d/c",
	}, {
		description:           "all cs another way",
		pathMatchersStr:       "*/*/c",
		wantSelectedSpanPaths: "a/b/c, a/d/c, a/d/c",
	}, {
		description:           "everything at or under a/d",
		pathMatchersStr:       "a/d/**",
		wantSelectedSpanPaths: "a/d, a/d/c, a/d/c",
	}, {
		description:           "everything at or under a/d",
		pathMatchersStr:       "a/d/**",
		wantSelectedSpanPaths: "a/d, a/d/c, a/d/c",
	}, {
		description:           "all spans whose name is a vowel",
		pathMatchersStr:       "**/([aeiou])",
		wantSelectedSpanPaths: "a, a/b/c/e",
	}, {
		description:           "everything just under a",
		pathMatchersStr:       "a/*",
		wantSelectedSpanPaths: "a/b, a/d",
	}, {
		description:           "did you really name that span '/'?",
		pathMatchersStr:       `**/\/`,
		wantSelectedSpanPaths: `a/b/c//`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			pms, err := pmp.ParsePathMatchersStr(test.pathMatchersStr)
			if err != nil {
				t.Fatalf("failed to parse path matchers string: %s", err)
			}
			spans := FindSpans[time.Duration, payload, payload, payload](tr, &testNamer{}, pms)
			spanPaths := []string{}
			for _, span := range spans {
				spanPaths = append(spanPaths, strings.Join(GetSpanDisplayPath[time.Duration, payload, payload, payload](span, &testNamer{}), "/"))
			}
			gotSelectedSpanPaths := strings.Join(spanPaths, ", ")
			if gotSelectedSpanPaths != test.wantSelectedSpanPaths {
				t.Errorf("Got span paths '%s', wanted '%s'", gotSelectedSpanPaths, test.wantSelectedSpanPaths)
			}
		})
	}
}
