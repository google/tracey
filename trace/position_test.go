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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestTracePositionFinding(t *testing.T) {
	tr := testTrace1(t)
	for _, test := range []struct {
		description            string
		trace                  Trace[time.Duration, payload, payload, payload]
		spanFinderPattern      *SpanPattern
		fractionThrough        float64
		multiplePositionPolicy MultiplePositionPolicy
		wantESs                string
	}{{
		description: "a/b @100%",
		trace:       testTrace1(t),
		spanFinderPattern: NewSpanPattern(
			SpanMatchers(literalNameMatchers("a", "b")),
		),
		fractionThrough: 1.0,
		wantESs:         `a/b 30ns-40ns @40ns`,
	}, {
		description: "**/c @0%",
		trace:       testTrace1(t),
		spanFinderPattern: NewSpanPattern(
			SpanMatchers(pathMatcher(Globstar, NewLiteralNameMatcher("c"))),
		),
		fractionThrough: 0.0,
		wantESs: `a/c 50ns-60ns @50ns
a/b/c 20ns-30ns @20ns`,
	}, {
		description: "**/c @0% earliest",
		trace:       testTrace1(t),
		spanFinderPattern: NewSpanPattern(
			SpanMatchers(pathMatcher(Globstar, NewLiteralNameMatcher("c"))),
		),
		fractionThrough:        0.0,
		multiplePositionPolicy: EarliestMatchingPosition,
		wantESs:                `a/b/c 20ns-30ns @20ns`,
	}, {
		description: "**/c @0% latest",
		trace:       testTrace1(t),
		spanFinderPattern: NewSpanPattern(
			SpanMatchers(pathMatcher(Globstar, NewLiteralNameMatcher("c"))),
		),
		fractionThrough:        0.0,
		multiplePositionPolicy: LatestMatchingPosition,
		wantESs:                `a/c 50ns-60ns @50ns`,
	}, {
		description: "**/d @30%",
		trace:       testTrace1(t),
		spanFinderPattern: NewSpanPattern(
			SpanMatchers(pathMatcher(Globstar, NewLiteralNameMatcher("d"))),
		),
		fractionThrough: 0.3,
		wantESs:         `a/c/d 60ns-65ns @63ns`,
	}, {
		description: "**/d @70%",
		trace:       testTrace1(t),
		spanFinderPattern: NewSpanPattern(
			SpanMatchers(pathMatcher(Globstar, NewLiteralNameMatcher("d"))),
		),
		fractionThrough: 0.7,
		wantESs:         `a/c/d 75ns-80ns @76ns`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			pp := NewSpanFractionPositionPattern(test.fractionThrough, test.multiplePositionPolicy)
			esps := NewPositionFinder(
				pp,
				NewSpanFinder(test.spanFinderPattern, tr),
			).FindPositions()
			var gotESsStrs []string
			for _, esp := range esps {
				gotESsStrs = append(
					gotESsStrs,
					fmt.Sprintf(
						"%s %v-%v @%v",
						strings.Join(
							GetSpanDisplayPath[time.Duration, payload, payload, payload](
								esp.ElementarySpan.Span(),
								&testNamer{},
							),
							"/",
						),
						esp.ElementarySpan.Start(), esp.ElementarySpan.End(), esp.At,
					),
				)
			}
			gotESs := strings.Join(gotESsStrs, "\n")
			if diff := cmp.Diff(test.wantESs, gotESs); diff != "" {
				t.Errorf("Found positions:\n%s\ndiff (-want +got) %s", gotESs, diff)
			}
		})
	}
}
