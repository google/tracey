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
	"regexp"
	"strings"
)

type parseOptions struct {
	// The string that separates an (optional) category path specifier from a
	// subsequent span specifier.
	categorySeparator string
	// The string that separates individual path element matcher strings within a
	// single path.
	matcherSeparator string
	// The string that separates multiple paths.
	pathSeparator string
}

// ParseOption specifies an option applied during parsing.
type ParseOption func(po *parseOptions) error

// PathElementMatcherSeparator defines the path element separator to use, defaulting to '/'.
func PathElementMatcherSeparator(separator rune) ParseOption {
	return func(po *parseOptions) error {
		po.matcherSeparator = string(separator)
		return nil
	}
}

// PathSeparator defines the paths separator to use, defaulting to ','.
func PathSeparator(separator rune) ParseOption {
	return func(po *parseOptions) error {
		po.pathSeparator = string(separator)
		return nil
	}
}

// PathMatcherParser provides methods that facilitate parsing trace path
// matcher strings into slices of PathElementMatchers.
type PathMatcherParser[T any, CP, SP, DP fmt.Stringer] struct {
	po *parseOptions
}

// NewPathMatcherParser builds and returns a new PathMatcherParser with the
// provided set of options.
func NewPathMatcherParser[T any, CP, SP, DP fmt.Stringer](opts ...ParseOption) (*PathMatcherParser[T, CP, SP, DP], error) {
	po := &parseOptions{
		categorySeparator: ">",
		matcherSeparator:  "/",
		pathSeparator:     ",",
	}
	for _, opt := range opts {
		if err := opt(po); err != nil {
			return nil, err
		}
	}
	return &PathMatcherParser[T, CP, SP, DP]{
		po: po,
	}, nil
}

func splitOnUnescapedSeparator(str, sep string) []string {
	elements := []string{}
	lastElementEndedWithEscape := false
	splitStr := strings.Split(str, sep)
	for _, chunk := range splitStr {
		if lastElementEndedWithEscape {
			elements[len(elements)-1] = elements[len(elements)-1] + sep + chunk
		} else {
			elements = append(elements, chunk)
		}
		trailingEscapes := 0
		for i := len(chunk) - 1; i >= 0 && chunk[i] == '\\'; i-- {
			trailingEscapes++
		}
		lastElementEndedWithEscape = trailingEscapes%2 == 1
	}
	return elements
}

func reduceEscapes(str string) string {
	ret := ""
	lastElementWasEscape := false
	for _, char := range strings.Split(str, "") {
		if lastElementWasEscape {
			ret = ret + char
			lastElementWasEscape = false
		} else if char == "\\" {
			lastElementWasEscape = true
		} else {
			ret = ret + char
		}
	}
	return ret
}

// ParsePathMatcherStr parses the provided path matcher string into a slice of
// PathElementMatchers.  `pathMatcherStr` is first split by unescaped instances
// of the receiving PathMatcherParser's matcher separator, then each element of
// that split string is parsed as a PathElementMatcher:
//
//	'*' :      converts to Star();
//	'**':      converts to Globstar();
//	'(<re>)':  converts to NewRegexpNameMatcher(re)
//	'<other>': converts to NewLiteralNameMatcher(other)
//
// If a literal instance of the receiver's matcher or path separator is
// required as part of a regular expression or string literal, it must be
// escaped with `\`; literal `\`s must also be escaped.
func (pp *PathMatcherParser[T, CP, SP, DP]) ParsePathMatcherStr(
	pathMatcherStr string,
) ([]PathElementMatcher[T, CP, SP, DP], error) {
	pathMatcherStr = strings.TrimSpace(pathMatcherStr)
	// Split the path by unescaped instances of the separator.
	pathMatcherStrs := splitOnUnescapedSeparator(pathMatcherStr, pp.po.matcherSeparator)
	ret := make([]PathElementMatcher[T, CP, SP, DP], len(pathMatcherStrs))
	for idx, pathMatcherStr := range pathMatcherStrs {
		switch {
		case pathMatcherStr == "*":
			ret[idx] = &star[T, CP, SP, DP]{}
		case pathMatcherStr == "**":
			ret[idx] = &globstar[T, CP, SP, DP]{}
		case len(pathMatcherStr) > 0 && pathMatcherStr[0] == '(' && pathMatcherStr[len(pathMatcherStr)-1] == ')':
			re, err := regexp.Compile(pathMatcherStr[1 : len(pathMatcherStr)-1])
			if err != nil {
				return nil, err
			}
			ret[idx] = &regexpNameMatcher[T, CP, SP, DP]{re}
		default:
			ret[idx] = &literalNameMatcher[T, CP, SP, DP]{reduceEscapes(pathMatcherStr)}
		}
	}
	return ret, nil
}

// ParsePathMatchersStr parses the provided path matchers string into a slice
// of []PathElementMatcher.  `pathMatchersStr` is first split by unescaped
// instances of the  receiving PathMatcherParser's path separator, then each
// element of that split string is parsed as a []PathElementMatcher using
// ParsePathMatcherStr().
func (pp *PathMatcherParser[T, CP, SP, DP]) ParsePathMatchersStr(
	pathMatchersStr string,
) ([][]PathElementMatcher[T, CP, SP, DP], error) {
	pathMatchersStr = strings.TrimSpace(pathMatchersStr)
	// Split the path by unescaped instances of the separator.
	pathMatchersStrs := splitOnUnescapedSeparator(pathMatchersStr, pp.po.pathSeparator)
	ret := make([][]PathElementMatcher[T, CP, SP, DP], len(pathMatchersStr))
	for idx, pathMatchersStr := range pathMatchersStrs {
		pms, err := pp.ParsePathMatcherStr(pathMatchersStr)
		if err != nil {
			return nil, err
		}
		ret[idx] = pms
	}
	return ret, nil
}

// ParseSpanFinderStr parses the provided span finder string, which may specify
// both a set of category matchers as well as a set of span matchers, returning
// a SpanFinder that can be used to find matching spans in specific traces.  It
// uses the provided namer, and interpreting category matchers per the
// specified hierarchy type.
func (pp *PathMatcherParser[T, CP, SP, DP]) ParseSpanFinderStr(
	namer Namer[T, CP, SP, DP],
	hierarchyType HierarchyType,
	spanFinderStr string,
) (*SpanFinder[T, CP, SP, DP], error) {
	spanFinderStr = strings.TrimSpace(spanFinderStr)
	parts := splitOnUnescapedSeparator(spanFinderStr, pp.po.categorySeparator)
	var spanMatchers, categoryMatchers [][]PathElementMatcher[T, CP, SP, DP]
	var err error
	switch len(parts) {
	case 1:
		spanMatchers, err = pp.ParsePathMatchersStr(parts[0])
		if err != nil {
			return nil, fmt.Errorf("failed to parse span matchers: %s", err)
		}
	case 2:
		categoryMatchers, err = pp.ParsePathMatchersStr(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, fmt.Errorf("failed to parse category matchers: %s", err)
		}
		spanMatchers, err = pp.ParsePathMatchersStr(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("failed to parse span matchers: %s", err)
		}
	case 0:
		return nil, fmt.Errorf("failed to parse span finder string '%s'", spanFinderStr)
	default:
		return nil, fmt.Errorf("failed to parse span finder string: at most one category separator ('%s') must be specified", pp.po.categorySeparator)
	}
	if len(spanMatchers) == 0 {
		return nil, fmt.Errorf("span finder string matches no spans")
	}
	ret := NewSpanFinder(namer).WithSpanMatchers(spanMatchers...)
	if len(categoryMatchers) > 0 && hierarchyType != SpanOnlyHierarchyType {
		ret.WithCategoryMatchers(hierarchyType, categoryMatchers...)
	}
	return ret, nil
}

var (
	tracePositionRE = regexp.MustCompile(
		`(?P<span_finder>.+?)\s*((?i:AT)|\@)\s*(?P<percentage>[+-]?([0-9]*[.])?[0-9]+)%`,
	)
)

// ParseTracePositionStr parses the provided trace position string, which
// includes both a span finder string and a percentage through matching spans,
// returning a Position object.
func (pp *PathMatcherParser[T, CP, SP, DP]) ParseTracePositionStr(
	namer Namer[T, CP, SP, DP],
	hierarchyType HierarchyType,
	tracePositionStr string,
) (*Position[T, CP, SP, DP], error) {
	tracePositionStr = strings.TrimSpace(tracePositionStr)
	matches := tracePositionRE.FindStringSubmatch(tracePositionStr)
	matchNames := tracePositionRE.SubexpNames()
	if len(matches) != len(matchNames) {
		return nil, fmt.Errorf("failed to parse position string")
	}
	m := map[string]string{}
	for idx, name := range matchNames {
		m[name] = matches[idx]
	}
	sf, err := pp.ParseSpanFinderStr(namer, hierarchyType, m["span_finder"])
	if err != nil {
		return nil, err
	}
	var percentage float64
	n, err := fmt.Sscanf(m["percentage"], "%f", &percentage)
	if n == 0 {
		return nil, fmt.Errorf("failed to parse percentage '%s' as float", m["percentage"])
	}
	if err != nil {
		return nil, fmt.Errorf("failed to parse percentage '%s' as float: %s", m["percentage"], err)
	}
	percentage = percentage / 100.0
	return NewPosition(sf, percentage), nil
}
