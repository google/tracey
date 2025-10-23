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

// Package parser provides a parser for trace span and position specifiers.
package parser

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/tracey/trace"
	"github.com/google/tracey/trace/parser/lexer"
	"github.com/google/tracey/trace/parser/predicate"
	prefixtree "github.com/google/tracey/trace/parser/prefix_tree"
)

type result struct {
	resultType         resultType
	spanSpecifiers     *spanSpecifiers
	positionSpecifiers *positionSpecifiers
}

var (
	op = prefixtree.Operator[int]
	kw = prefixtree.Keyword[int]
)

var (
	tokens = map[string]*prefixtree.Token[int]{
		"@":                          op(AT),
		">":                          op(GT),
		">=":                         op(GTE),
		"<":                          op(LT),
		"<=":                         op(LTE),
		"==":                         op(EQ),
		"!=":                         op(NEQ),
		"&&":                         op(AND),
		"||":                         op(OR),
		"/":                          op(SLASH),
		"%":                          op(PCT),
		"(":                          op(LPAREN),
		")":                          op(RPAREN),
		",":                          op(COMMA),
		"*":                          op(STAR),
		"**":                         op(GLOBSTAR),
		"AT":                         kw(AT),
		"ALL":                        kw(ALL),
		"ANY":                        kw(ANY),
		"AND":                        kw(AND),
		"OR":                         kw(OR),
		"WHERE":                      kw(WHERE),
		"LATEST":                     kw(LATEST),
		"EARLIEST":                   kw(EARLIEST),
		"DURATION":                   kw(DURATION),
		"TOTAL_DURATION":             kw(TOTAL_DURATION),
		"SUSPENDED_DURATION":         kw(SUSPENDED_DURATION),
		"SELF_UNSUSPENDED_DURATION":  kw(SELF_UNSUSPENDED_DURATION),
		"TOTAL_UNSUSPENDED_DURATION": kw(TOTAL_UNSUSPENDED_DURATION),
	}
)

var (
	unquotedTerminationCharacters = map[rune]struct{}{
		'/': struct{}{},
		')': struct{}{},
		'>': struct{}{},
		',': struct{}{},
		'%': struct{}{},
		'@': struct{}{},
	}
)

func consumeFreeformString(input string, lvalue *yySymType) (ok bool, token int, consumed int, err error) {
	offset := 0
	escaped := false
	str := ""
	for {
		r, c := utf8.DecodeRuneInString(input[offset:])
		if c == 0 {
			break
		}
		if _, term := unquotedTerminationCharacters[r]; !escaped && term {
			break
		}
		escaped = !escaped && r == '\\'
		if !escaped {
			str += string(r)
		}
		offset += c
	}
	if offset > 0 {
		lvalue.str = strings.TrimSpace(str)
		return true, STR, offset, nil
	}
	return false, 0, 0, nil
}

func newLexer(input string) (*lexer.Lexer[*result, *yySymType], error) {
	ptr, err := prefixtree.New(tokens)
	if err != nil {
		return nil, err
	}
	setStr := func(str string, lvalue *yySymType) error {
		lvalue.str = str
		return nil
	}
	// On each Lex() call, attempt to consume, in this order:
	//   * Any token defined in `tokens`;
	//   * A string quoted with `'`;
	//   * A string quoted with `"`;
	//   * A 'freeform' string running until an unescaped instance of any
	//     character in [`/`, `)`, `>`, `,`, `%`, `@`] (that is, the tokens that
	//     may follow strings in the parser) or EOI.
	return lexer.New[*result, *yySymType](
		[]lexer.TokenConsumerFn[*yySymType]{
			lexer.PrefixTreeConsumer(ptr, setStr),
			lexer.QuotedStringConsumer(STR, '\'', setStr),
			lexer.QuotedStringConsumer(STR, '"', setStr),
			consumeFreeformString,
		},
		[]lexer.NonTokenConsumerFn{
			lexer.ConsumeWhitespace,
		},
		input,
	)
}

// Returns a PathElementMatcher corresponding to the provided pathElement.
func pathElementToMatcher(
	pe *pathElement,
) (trace.PathElementMatcher, error) {
	switch pe.t {
	case literal:
		return trace.NewLiteralNameMatcher(pe.str), nil
	case regex:
		return trace.NewRegexpNameMatcher(pe.str)
	case star:
		return trace.Star, nil
	case globstar:
		return trace.Globstar, nil
	default:
		return nil, fmt.Errorf("couldn't handle path element of type %v", pe.t)
	}
}

func (pe *pathElement) String() string {
	switch pe.t {
	case literal:
		return fmt.Sprintf("lit(%s)", pe.str)
	case regex:
		return fmt.Sprintf("reg(%s)", pe.str)
	case star:
		return "*"
	case globstar:
		return "**"
	default:
		return "unk"
	}
}

// Returns a slice of PathElementMatchers for the provided slice of
// pathElements.
func buildPathElementMatchers(
	pathElements []*pathElement,
) ([]trace.PathElementMatcher, error) {
	ret := make([]trace.PathElementMatcher, len(pathElements))
	for idx, pe := range pathElements {
		pem, err := pathElementToMatcher(pe)
		if err != nil {
			return nil, err
		}
		ret[idx] = pem
	}
	return ret, nil
}

// SpanPattern specifies a pattern matching trace spans.
type SpanPattern struct {
	spanPattern *trace.SpanPattern
	predicate   *predicate.Predicate
}

// Returns a SpanPattern for the provided hierarchy type and spanSpecifiers.
func buildSpanPattern(
	ht trace.HierarchyType,
	spanSpecifiers *spanSpecifiers,
) (*SpanPattern, error) {
	var spanFinderOptions []trace.SpanPatternOption
	for _, spanSpecifier := range spanSpecifiers.spanSpecifiers {
		categoryMatchers, err := buildPathElementMatchers(spanSpecifier.categoryMatchers)
		if err != nil {
			return nil, err
		}
		spanMatchers, err := buildPathElementMatchers(spanSpecifier.spanMatchers)
		if err != nil {
			return nil, err
		}
		spanFinderOptions = append(
			spanFinderOptions,
			trace.SpanAndCategoryMatchers(ht, categoryMatchers, spanMatchers),
		)
	}
	ret := trace.NewSpanPattern(spanFinderOptions...)
	return &SpanPattern{
		spanPattern: ret,
		predicate:   spanSpecifiers.predicate,
	}, nil
}

// NewSpanFinder returns a new SpanFinder applying the provided SpanPattern to
// the provided Trace, using the provided Namer.
func NewSpanFinder[T any, CP, SP, DP fmt.Stringer](
	sp *SpanPattern,
	t trace.Trace[T, CP, SP, DP],
) (trace.SpanFinder[T, CP, SP, DP], error) {
	if sp == nil {
		return trace.NewSpanFinder(nil, t), nil
	}
	var predicateFn trace.SpanFilter[T, CP, SP, DP]
	if sp.predicate != nil {
		var err error
		predicateFn, err = predicate.BuildSpanFilter[T, CP, SP, DP](t.Comparator(), sp.predicate)
		if err != nil {
			return nil, fmt.Errorf("failed to generate span specifier predicate: %w", err)
		}
	}
	return trace.NewSpanFinder(sp.spanPattern, t).WithSpanFilter(predicateFn), nil
}

// Parses the provided string, returning the completed lexer used in parsing.
func parse(input string) (*result, error) {
	l, err := newLexer(input)
	if err != nil {
		return nil, err
	}
	yyErrorVerbose = true
	p := &yyParserImpl{}
	p.Parse(l)
	if l.Err != nil {
		return nil, l.Err
	}
	return l.Results, nil
}

// ValidateSpanSpecifiers parses the provided string as a span specifier,
// returning an error if it fails to parse or doesn't parse as a span
// specifier.
func ValidateSpanSpecifiers(spanSpecifierStr string) error {
	r, err := parse(spanSpecifierStr)
	if err != nil {
		return err
	}
	if r.resultType != spanSpecifiersType {
		return errors.New("expected span specifier")
	}
	return nil
}

// ParseSpanSpecifierPatterns parses a single span_specifiers field, returning
// a SpanFinder corresponding to the parsed specifiers.  Returns an error if
// any parsing failed or if the parsed result was not a span specifiers string.
func ParseSpanSpecifierPatterns(
	ht trace.HierarchyType,
	spanSpecifierStr string,
) (*SpanPattern, error) {
	r, err := parse(spanSpecifierStr)
	if err != nil {
		return nil, err
	}
	if r.resultType != spanSpecifiersType {
		return nil, fmt.Errorf("input '%s' is not a trace span specifier string", spanSpecifierStr)
	}
	return buildSpanPattern(ht, r.spanSpecifiers)
}

// MustParseSpanSpecifiers works as ParseSpanSpecifiers, except that it
// panics if any errors are encountered.
func MustParseSpanSpecifiers(
	ht trace.HierarchyType,
	spanSpecifierStr string,
) *SpanPattern {
	sf, err := ParseSpanSpecifierPatterns(ht, spanSpecifierStr)
	if err != nil {
		panic(fmt.Sprintf("Failed to parse span specifier: %s", err))
	}
	return sf
}

// ValidatePositionSpecifiers parses the provided string as a position
// specifier, returning an error if it fails to parse or doesn't parse as a
// position specifier.
func ValidatePositionSpecifiers(positionSpecifierStr string) error {
	r, err := parse(positionSpecifierStr)
	if err != nil {
		return err
	}
	if r.resultType != positionSpecifiersType {
		return errors.New("expected position specifier")
	}
	return nil
}

// PositionPattern specifies a pattern matching trace positions.
type PositionPattern struct {
	positionPattern *trace.PositionPattern
	spanPattern     *SpanPattern
}

// PositionPattern returns the receiver's PositionPattern.
func (pp *PositionPattern) PositionPattern() *trace.PositionPattern {
	return pp.positionPattern
}

// SpanFinderFromPosition returns the SpanFinder for the provided
// PositionPattern within the provided Trace, using the provided Namer.
func SpanFinderFromPosition[T any, CP, SP, DP fmt.Stringer](
	pp *PositionPattern,
	t trace.Trace[T, CP, SP, DP],
) (trace.SpanFinder[T, CP, SP, DP], error) {
	return NewSpanFinder(pp.spanPattern, t)
}

// NewPositionFinder returns a new SpanFinder applying the provided SpanPattern to
// the provided Trace, using the provided Namer.
func NewPositionFinder[T any, CP, SP, DP fmt.Stringer](
	pp *PositionPattern,
	t trace.Trace[T, CP, SP, DP],
) (trace.PositionFinder[T, CP, SP, DP], error) {
	sf, err := SpanFinderFromPosition(pp, t)
	if err != nil {
		return nil, err
	}
	return trace.NewPositionFinder(pp.positionPattern, sf), nil
}

func buildPositionPattern(
	ht trace.HierarchyType,
	positionSpecifiers *positionSpecifiers,
) (*PositionPattern, error) {
	spanPattern, err := buildSpanPattern(ht, positionSpecifiers.spanSpecifiers)
	if err != nil {
		return nil, err
	}
	if positionSpecifiers.isFrac {
		return &PositionPattern{
			positionPattern: trace.NewSpanFractionPositionPattern(positionSpecifiers.percentage/100.0, positionSpecifiers.multiplePositionPolicy),
			spanPattern:     spanPattern,
		}, nil
	}
	positionPattern, err := trace.NewSpanMarkPositionPattern(positionSpecifiers.markRE, positionSpecifiers.multiplePositionPolicy)
	if err != nil {
		return nil, err
	}
	return &PositionPattern{
		positionPattern: positionPattern,
		spanPattern:     spanPattern,
	}, nil
}

// ParsePositionSpecifiers parses a single position_specifiers field, returning a
// Position corresponding to the parsed specifiers.  Returns an error if any
// parsing failed or if the parsed result was not a position specifiers string.
func ParsePositionSpecifiers(
	ht trace.HierarchyType,
	positionSpecifierStr string,
) (*PositionPattern, error) {
	r, err := parse(positionSpecifierStr)
	if err != nil {
		return nil, err
	}
	if r.resultType != positionSpecifiersType {
		return nil, fmt.Errorf("input '%s' is not a trace position specifier string", positionSpecifierStr)
	}
	return buildPositionPattern(ht, r.positionSpecifiers)
}

// MustParsePositionSpecifiers works as ParsePositionSpecifiers, except that it
// panics if any errors are encountered.
func MustParsePositionSpecifiers(
	ht trace.HierarchyType,
	positionSpecifierStr string,
) *PositionPattern {
	pos, err := ParsePositionSpecifiers(ht, positionSpecifierStr)
	if err != nil {
		panic(fmt.Sprintf("Failed to parse position specifier: %s", err))
	}
	return pos
}

// ParseSpecifiers parses the provided specifier string, returning a
// SpanPattern if the specifier parsed as a span specifier pattern, a
// PositionPattern if it parsed as a trace position pattern, and an error
// if it could not be parsed or if it parsed as some other type.  If the
// returned error is nil, exactly one of the returned SpanPattern and
// PositionPattern will be non-nil.
func ParseSpecifiers(
	ht trace.HierarchyType,
	specifierStr string,
) (*SpanPattern, *PositionPattern, error) {
	r, err := parse(specifierStr)
	if err != nil {
		return nil, nil, err
	}
	switch r.resultType {
	case positionSpecifiersType:
		pp, err := buildPositionPattern(ht, r.positionSpecifiers)
		return nil, pp, err
	case spanSpecifiersType:
		sp, err := buildSpanPattern(ht, r.spanSpecifiers)
		return sp, nil, err
	default:
		return nil, nil, fmt.Errorf("can't interpret specifier '%s' as a span or position pattern", specifierStr)
	}
}
