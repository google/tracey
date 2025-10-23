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
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func strs(strs ...string) []string {
	return strs
}

func literalNameMatchers(path ...string) []PathElementMatcher {
	matchers := make([]PathElementMatcher, len(path))
	for idx, pathEl := range path {
		matchers[idx] = NewLiteralNameMatcher(pathEl)
	}
	return matchers
}

func literalIDMatchers(path ...string) []PathElementMatcher {
	matchers := make([]PathElementMatcher, len(path))
	for idx, pathEl := range path {
		matchers[idx] = NewLiteralIDMatcher(pathEl)
	}
	return matchers
}

func pathMatcher(matchers ...PathElementMatcher) []PathElementMatcher {
	return matchers
}

func testTrace1(t *testing.T) Trace[time.Duration, payload, payload, payload] {
	t.Helper()
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	a := trace.NewRootSpan(0, 100, "a")
	ab, err := a.NewChildSpan(DurationComparator, 10, 40, "b")
	if err != nil {
		t.Fatal(err.Error())
	}
	_, err = ab.NewChildSpan(DurationComparator, 20, 30, "c")
	if err != nil {
		t.Fatal(err.Error())
	}
	ac, err := a.NewChildSpan(DurationComparator, 50, 90, "c")
	if err != nil {
		t.Fatal(err.Error())
	}
	acd, err := ac.NewChildSpan(DurationComparator, 60, 80, "d")
	if err != nil {
		t.Fatal(err.Error())
	}
	_, err = acd.NewChildSpan(DurationComparator, 65, 75, "e")
	if err != nil {
		t.Fatal(err.Error())
	}
	trace.NewRootSpan(0, 50, "f")
	return trace
}

func TestFindSpans(t *testing.T) {
	trace := testTrace1(t)

	for _, test := range []struct {
		description          string
		matchers             []PathElementMatcher
		spanFilter           SpanFilter[time.Duration, payload, payload, payload]
		wantMatchingSpansStr string
	}{{
		description: "find span by literal name path",
		matchers:    literalNameMatchers("a", "b", "c"),
		wantMatchingSpansStr: `
c: 20ns-30ns`,
	}, {
		description: "find span by literal ID path",
		matchers:    literalIDMatchers("id:a", "id:b", "id:c"),
		wantMatchingSpansStr: `
c: 20ns-30ns`,
	}, {
		description: "find **/c",
		matchers: pathMatcher(
			Globstar,
			NewLiteralNameMatcher("c"),
		),
		wantMatchingSpansStr: `
c: 50ns-90ns
c: 20ns-30ns`,
	}, {
		description: "find a/c/**",
		matchers: pathMatcher(
			NewLiteralNameMatcher("a"),
			NewLiteralNameMatcher("c"),
			Globstar,
		),
		wantMatchingSpansStr: `
c: 50ns-90ns
d: 60ns-80ns
e: 65ns-75ns`,
	}, {
		description: "find a/*/*",
		matchers: pathMatcher(
			NewLiteralNameMatcher("a"),
			Star,
			Star,
		),
		wantMatchingSpansStr: `
c: 20ns-30ns
d: 60ns-80ns`,
	}, {
		description: "find ** under root span f",
		matchers: pathMatcher(
			Globstar,
		),
		spanFilter: func(s Span[time.Duration, payload, payload, payload]) (include, prune bool) {
			filtered := s.ParentSpan() == nil && s.Payload() == "f"
			return filtered, !filtered
		},
		wantMatchingSpansStr: `
f: 0s-50ns`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			matchingSpans := findSpans(trace, trace.DefaultNamer(), test.spanFilter, test.matchers, SpanOnlyHierarchyType, nil)
			var gotMatchingSpansStrs []string
			for _, span := range matchingSpans {
				gotMatchingSpansStrs = append(
					gotMatchingSpansStrs,
					fmt.Sprintf("%s: %v-%v", span.Payload(), span.Start(), span.End()),
				)
			}
			gotMatchingSpansStr := "\n" + strings.Join(gotMatchingSpansStrs, "\n")
			if diff := cmp.Diff(test.wantMatchingSpansStr, gotMatchingSpansStr); diff != "" {
				t.Errorf("Got trace spans\n%s\ndiff (-want +got) %s", gotMatchingSpansStr, diff)
			}
		})
	}
}

func TestFindCategories(t *testing.T) {
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	a := trace.NewRootCategory(0, "a")
	ab := a.NewChildCategory("b")
	ab.NewChildCategory("c")
	ac := a.NewChildCategory("c")
	acd := ac.NewChildCategory("d")
	acd.NewChildCategory("e")

	for _, test := range []struct {
		description         string
		matchers            []PathElementMatcher
		wantMatchingCatsStr string
	}{{
		description: "find category by literal path",
		matchers: []PathElementMatcher{
			NewLiteralNameMatcher("a"),
			NewLiteralNameMatcher("b"),
			NewLiteralNameMatcher("c"),
		},
		wantMatchingCatsStr: `a/b/c`,
	}, {
		description: "find **/c",
		matchers: []PathElementMatcher{
			Globstar,
			NewLiteralNameMatcher("c"),
		},
		wantMatchingCatsStr: `a/c, a/b/c`,
	}, {
		description: "find a/c/**",
		matchers: []PathElementMatcher{
			NewLiteralNameMatcher("a"),
			NewLiteralNameMatcher("c"),
			Globstar,
		},
		wantMatchingCatsStr: `a/c, a/c/d, a/c/d/e`,
	}, {
		description: "find a/*/*",
		matchers: []PathElementMatcher{
			NewLiteralNameMatcher("a"),
			Star,
			Star,
		},
		wantMatchingCatsStr: `a/b/c, a/c/d`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			matchingCats := findCategories[time.Duration, payload, payload, payload](trace, &testNamer{}, 0, test.matchers)
			var gotMatchingCatsStrs []string
			for _, cat := range matchingCats {
				var path []string
				for cat != nil {
					path = append(path, cat.Payload().String())
					cat = cat.Parent()
				}
				slices.Reverse(path)
				gotMatchingCatsStrs = append(gotMatchingCatsStrs, strings.Join(path, "/"))
			}
			gotMatchingCatsStr := strings.Join(gotMatchingCatsStrs, ", ")
			if diff := cmp.Diff(test.wantMatchingCatsStr, gotMatchingCatsStr); diff != "" {
				t.Errorf("Got trace cats\n%s\ndiff (-want +got) %s", gotMatchingCatsStr, diff)
			}
		})
	}
}
