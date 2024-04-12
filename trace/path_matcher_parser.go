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
		matcherSeparator: "/",
		pathSeparator:    ",",
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
	// Split the path by unescaped instances of the separator.
	pathMatcherStrs := splitOnUnescapedSeparator(pathMatcherStr, pp.po.matcherSeparator)
	ret := make([]PathElementMatcher[T, CP, SP, DP], len(pathMatcherStrs))
	for idx, pathMatcherStr := range pathMatcherStrs {
		switch {
		case pathMatcherStr == "*":
			ret[idx] = &star[T, CP, SP, DP]{}
		case pathMatcherStr == "**":
			ret[idx] = &globstar[T, CP, SP, DP]{}
		case pathMatcherStr[0] == '(' && pathMatcherStr[len(pathMatcherStr)-1] == ')':
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
