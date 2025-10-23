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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestSpanSelectionFullExpansion(t *testing.T) {
	trace := testTrace1(t)
	for _, test := range []struct {
		description          string
		matchers             []PathElementMatcher
		wantMatchingSpansStr string // Lexically sorted for stability.
	}{{
		description: "find span by literal path",
		matchers: pathMatcher(
			NewLiteralNameMatcher("a"),
			NewLiteralNameMatcher("b"),
			NewLiteralNameMatcher("c"),
		),
		wantMatchingSpansStr: `
c: 20ns-30ns`,
	}, {
		description: "find **/c",
		matchers: pathMatcher(
			Globstar,
			NewLiteralNameMatcher("c"),
		),
		wantMatchingSpansStr: `
c: 20ns-30ns
c: 50ns-90ns`,
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
	}} {
		t.Run(test.description, func(t *testing.T) {
			selection := SelectSpans(
				NewSpanFinder(
					NewSpanPattern(SpanMatchers(test.matchers)),
					trace,
				),
			)
			matchingSpans := selection.Spans()
			var gotMatchingSpansStrs []string
			for _, span := range matchingSpans {
				gotMatchingSpansStrs = append(
					gotMatchingSpansStrs,
					fmt.Sprintf("%s: %v-%v", span.Payload(), span.Start(), span.End()),
				)
			}
			sort.Strings(gotMatchingSpansStrs)
			gotMatchingSpansStr := "\n" + strings.Join(gotMatchingSpansStrs, "\n")
			if diff := cmp.Diff(test.wantMatchingSpansStr, gotMatchingSpansStr); diff != "" {
				t.Errorf("Got trace spans\n%s\ndiff (-want +got) %s", gotMatchingSpansStr, diff)
			}
		})
	}
}

func TestMultiplePaths(t *testing.T) {
	trace := testTrace1(t)
	for _, test := range []struct {
		description          string
		matchers             [][]PathElementMatcher
		wantMatchingSpansStr string // Lexically sorted for stability.
	}{{
		description: "a/b/c, a/c/d",
		matchers: [][]PathElementMatcher{
			pathMatcher(
				NewLiteralNameMatcher("a"),
				NewLiteralNameMatcher("b"),
				NewLiteralNameMatcher("c"),
			),
			pathMatcher(
				NewLiteralNameMatcher("a"),
				NewLiteralNameMatcher("c"),
				NewLiteralNameMatcher("d"),
			),
		},
		wantMatchingSpansStr: `
c: 20ns-30ns
d: 60ns-80ns`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			sp := NewSpanPattern(SpanMatchers(test.matchers...))
			selection := SelectSpans(
				NewSpanFinder(sp, trace),
			)
			matchingSpans := selection.Spans()
			var gotMatchingSpansStrs []string
			for _, span := range matchingSpans {
				gotMatchingSpansStrs = append(
					gotMatchingSpansStrs,
					fmt.Sprintf("%s: %v-%v", span.Payload(), span.Start(), span.End()),
				)
			}
			sort.Strings(gotMatchingSpansStrs)
			gotMatchingSpansStr := "\n" + strings.Join(gotMatchingSpansStrs, "\n")
			if diff := cmp.Diff(test.wantMatchingSpansStr, gotMatchingSpansStr); diff != "" {
				t.Errorf("Got trace spans\n%s\ndiff (-want +got) %s", gotMatchingSpansStr, diff)
			}
		})
	}
}

func TestSpanSelectionIncludes(t *testing.T) {
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	span := trace.NewRootSpan(0, 100, "parent")
	child, err := span.NewChildSpan(DurationComparator, 30, 60, "child")
	if err != nil {
		t.Error(err.Error())
	}
	grandchild, err := child.NewChildSpan(DurationComparator, 40, 50, "grandchild")
	if err != nil {
		t.Error(err.Error())
	}
	ss := SelectSpans(
		NewSpanFinder(
			NewSpanPattern(
				SpanMatchers(
					pathMatcher(
						NewLiteralNameMatcher("parent"),
						Star,
						Star,
					),
				),
			),
			trace,
		),
	)
	if !ss.Includes(grandchild) {
		t.Errorf("expected grandchild to match, but it didn't")
	}
}

func testTrace2(t *testing.T) Trace[time.Duration, payload, payload, payload] {
	t.Helper()
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	a := trace.NewRootCategory(1, "a")
	ab := a.NewChildCategory("b")
	ab.NewChildCategory("c")
	ac := a.NewChildCategory("c")
	acd := ac.NewChildCategory("d")
	acd.NewChildCategory("e")
	return trace
}

func TestCategorySelectionFullExpansion(t *testing.T) {
	trace := testTrace2(t)
	for _, test := range []struct {
		description               string
		pattern                   *SpanPattern
		wantMatchingCategoriesStr string // Lexically sorted for stability.
	}{{
		description: "find category by literal path",
		pattern: NewSpanPattern(
			SpanAndCategoryMatchers(
				1,
				[]PathElementMatcher{
					NewLiteralNameMatcher("a"),
					NewLiteralNameMatcher("b"),
					NewLiteralNameMatcher("c"),
				},
				nil,
			),
		),
		wantMatchingCategoriesStr: `c`,
	}, {
		description: "find **/c",
		pattern: NewSpanPattern(
			SpanAndCategoryMatchers(
				1,
				[]PathElementMatcher{
					Globstar,
					NewLiteralNameMatcher("c"),
				},
				nil,
			),
		),
		wantMatchingCategoriesStr: `c,c`,
	}, {
		description: "find a/c/**",
		pattern: NewSpanPattern(
			SpanAndCategoryMatchers(
				1,
				[]PathElementMatcher{
					NewLiteralNameMatcher("a"),
					NewLiteralNameMatcher("c"),
					Globstar,
				},
				nil,
			),
		),
		wantMatchingCategoriesStr: `c,d,e`,
	}, {
		description: "find a/*/*",
		pattern: NewSpanPattern(
			SpanAndCategoryMatchers(
				1,
				[]PathElementMatcher{
					NewLiteralNameMatcher("a"),
					Star,
					Star,
				},
				nil,
			),
		),
		wantMatchingCategoriesStr: `c,d`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			sf := NewSpanFinder(test.pattern, trace)
			selection := SelectCategories(sf)
			matchingCategories := selection.Categories(0)
			var gotMatchingCategoriesStrs []string
			for _, category := range matchingCategories {
				gotMatchingCategoriesStrs = append(
					gotMatchingCategoriesStrs,
					fmt.Sprintf("%s", category.Payload()),
				)
			}
			sort.Strings(gotMatchingCategoriesStrs)
			gotMatchingCategoriessStr := strings.Join(gotMatchingCategoriesStrs, ",")
			if diff := cmp.Diff(test.wantMatchingCategoriesStr, gotMatchingCategoriessStr); diff != "" {
				t.Errorf("Got trace categories\n%s\ndiff (-want +got) %s", gotMatchingCategoriessStr, diff)
			}
		})
	}
}

func TestCategorySelectionIncludes(t *testing.T) {
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	span := trace.NewRootCategory(1, "parent")
	child := span.NewChildCategory("child")
	grandchild := child.NewChildCategory("grandchild")
	ss := SelectCategories(
		NewSpanFinder(
			NewSpanPattern(
				SpanAndCategoryMatchers(
					1,
					[]PathElementMatcher{
						NewLiteralNameMatcher("parent"),
						Star,
						Star,
					},
					nil,
				),
			),
			trace,
		),
	)
	if !ss.Includes(grandchild) {
		t.Errorf("expected grandchild to match, but it didn't")
	}
}

func TestDependencySelection(t *testing.T) {
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	span := trace.NewRootSpan(0, 100, "parent")
	child, err := span.NewChildSpan(DurationComparator, 30, 60, "child")
	if err != nil {
		t.Error(err.Error())
	}
	grandchild, err := child.NewChildSpan(DurationComparator, 40, 50, "grandchild")
	if err != nil {
		t.Error(err.Error())
	}
	if err := trace.NewDependency(3, "").
		SetOriginSpan(trace.Comparator(), grandchild, 45); err != nil {
		t.Error(err.Error())
	}
	if err := trace.NewDependency(3, "").
		AddDestinationSpan(trace.Comparator(), span, 60); err != nil {
		t.Error(err.Error())
	}

	sd := SelectDependencies(
		trace,
		NewSpanFinder(
			NewSpanPattern(
				SpanMatchers(literalNameMatchers("parent", "child", "grandchild")),
			),
			trace,
		),
		NewSpanFinder(
			NewSpanPattern(
				SpanMatchers(literalNameMatchers("parent")),
			),
			trace,
		),
		3,
	)
	if !sd.Includes(grandchild, span, 3) {
		t.Errorf("expected child->grandchild dependency of type 3, but didn't find it")
	}
}
