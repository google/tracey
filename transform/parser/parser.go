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

// Package parser provides a parser for trace transformation templates.
package parser

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/tracey/trace"
	traceparser "github.com/google/tracey/trace/parser"
	"github.com/google/tracey/trace/parser/lexer"
	prefixtree "github.com/google/tracey/trace/parser/prefix_tree"
	xf "github.com/google/tracey/transform"
)

type result struct {
	transforms []*transform
}

var (
	op = prefixtree.Operator[int]
	kw = prefixtree.Keyword[int]
)

var (
	tokens = map[string]*prefixtree.Token[int]{
		"ABOVE":        kw(ABOVE),
		"ADD":          kw(ADD),
		"APPLY":        kw(APPLY),
		"AS":           kw(AS),
		"BELOW":        kw(BELOW),
		"BY":           kw(BY),
		"CAP":          kw(CAP),
		"CHANGE":       kw(CHANGE),
		"CONCURRENCY":  kw(CONCURRENCY),
		"DEPENDENCIES": kw(DEPENDENCIES),
		"DEPENDENCY":   kw(DEPENDENCY),
		"DURATION":     kw(DURATION),
		"EARLY":        kw(EARLY),
		"FROM":         kw(FROM),
		"NONBLOCKING":  kw(NONBLOCKING),
		"POSITION":     kw(POSITION),
		"POSITIONS":    kw(POSITIONS),
		"POSSIBLE":     kw(POSSIBLE),
		"REMOVE":       kw(REMOVE),
		"SCALE":        kw(SCALE),
		"SHRINK":       kw(SHRINK),
		"SPANS":        kw(SPANS),
		"START":        kw(START),
		"TO":           kw(TO),
		";":            op(SEMICOLON),
	}
)

func consumeComment(input string) (consumed int, err error) {
	singleLineComment := strings.HasPrefix(input, "#") || strings.HasPrefix(input, "//")
	multiLineComment := strings.HasPrefix(input, "/*")
	var offset int
	if singleLineComment {
		offset = strings.Index(input, "\n")
		if offset == -1 {
			offset = len(input)
		}
	} else if multiLineComment {
		offset = strings.Index(input, "*/") + len("*/")
		if offset == -1 {
			offset = len(input)
		}
	} else {
		return 0, nil
	}
	wsOffset, err := lexer.ConsumeWhitespace(input[offset:])
	if err != nil {
		return 0, err
	}
	nextOffset, err := consumeComment(input[offset+wsOffset:])
	return offset + wsOffset + nextOffset, err
}

func consumeParenthesizedString(input string, lvalue *yySymType) (ok bool, token int, consumed int, err error) {
	offset := 0
	escaped := false
	str := ""
	openParens := 0
	r, c := utf8.DecodeRuneInString(input)
	if r != '(' {
		return false, 0, 0, nil
	}
	str += string(r)
	offset += c
	for {
		r, c := utf8.DecodeRuneInString(input[offset:])
		offset += c
		if c == 0 {
			return false, 0, 0, errors.New("unescaped, unbalanced parens in string")
		}
		if openParens == 0 && !escaped && r == ')' {
			str += string(r)
			lvalue.str = str
			return true, PARENTHESIZED_STR, offset, nil
		}
		if !escaped && r == '(' {
			openParens++
		}
		if !escaped && r == ')' {
			openParens--
		}
		if openParens < 0 {
			return false, 0, 0, errors.New("unescaped, unbalanced parens in string")
		}
		escaped = !escaped && r == '\\'
		if !escaped {
			str += string(r)
		}
	}
}

func consumeToSemicolon(input string, lvalue *yySymType) (ok bool, token int, consumed int, err error) {
	offset := 0
	escaped := false
	str := ""
	for {
		r, c := utf8.DecodeRuneInString(input[offset:])
		if c == 0 || r == ';' {
			lvalue.str = str
			return true, STR, offset, nil
		}
		escaped = !escaped && r == '\\'
		if !escaped {
			str += string(r)
		}
		offset += c
	}
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
	//   * A parenthesized string (any text enclosed in matching unescaped
	//     parentheses);
	//   * All characters up to but not including the first unescaped semicolon
	//     or the end of the input.
	return lexer.New[*result, *yySymType](
		[]lexer.TokenConsumerFn[*yySymType]{
			lexer.PrefixTreeConsumer(ptr, setStr),
			consumeParenthesizedString,
			consumeToSemicolon,
		},
		[]lexer.NonTokenConsumerFn{
			lexer.ConsumeWhitespace,
			consumeComment,
			lexer.ConsumeWhitespace,
		},
		input,
	)
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

// ParseTransformTemplate parses the provided trace transformation template,
// returning a corresponding Transform object, or any error encountered.
func ParseTransformTemplate[T any, CP, SP, DP fmt.Stringer](
	ht trace.HierarchyType,
	namer trace.Namer[T, CP, SP, DP],
	transformationTemplate string,
) (*xf.Transform[T, CP, SP, DP], error) {
	r, err := parse(transformationTemplate)
	if err != nil {
		return nil, err
	}
	dtCount := len(namer.DependencyTypes().OrderedTypeData())
	allDTs := make([]trace.DependencyType, dtCount)
	for idx, dt := range namer.DependencyTypes().OrderedTypeData() {
		allDTs[idx] = dt.Type
	}
	getDependencyTypeList := func(dependencyTypeNames ...string) ([]trace.DependencyType, error) {
		if len(dependencyTypeNames) == 1 {
			dt := dependencyTypeNames[0]
			if dt == "*" || dt == "**" || strings.ToUpper(dt) == "ALL" {
				return allDTs, nil
			}
		}
		ret := make([]trace.DependencyType, len(dependencyTypeNames))
		for idx, dtn := range dependencyTypeNames {
			dtd, err := namer.DependencyTypes().ByName(dtn)
			if err != nil {
				return nil, err
			}
			ret[idx] = dtd.Type
		}
		return ret, nil
	}
	ret := xf.New[T, CP, SP, DP]()
	// In the traceparser invocations below, any parse errors should already have
	// been caught by validateSpanSpecifier or validatePositionSpecifier calls
	// within the parser.  If any of these is surfaced, it's a problem.
	for _, t := range r.transforms {
		switch t.transformType {
		case scaleSpan:
			spanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.scaleSpanTransform.spanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			ret.WithSpansScaledBy(spanFinders, t.scaleSpanTransform.scaleFactor)
		case scaleDependency:
			fromSpanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.scaleDependencyTransform.fromSpanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			toSpanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.scaleDependencyTransform.toSpanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			dependencyTypes, err := getDependencyTypeList(t.scaleDependencyTransform.dependencyList...)
			if err != nil {
				return nil, err
			}
			ret.WithDependenciesScaledBy(fromSpanFinders, toSpanFinders, dependencyTypes, t.scaleDependencyTransform.scaleFactor)
		case addDependency:
			fromPosition, err := traceparser.ParsePositionSpecifiers(ht, t.addDependencyTransform.fromPositionSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			toPosition, err := traceparser.ParsePositionSpecifiers(ht, t.addDependencyTransform.toPositionSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			dependencyTypes, err := getDependencyTypeList(t.addDependencyTransform.dependency)
			if err != nil {
				return nil, err
			}
			ret.WithAddedDependencies(fromPosition, toPosition, dependencyTypes[0], 0)
		case removeDependency:
			fromSpanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.removeDependencyTransform.fromSpanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			toSpanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.removeDependencyTransform.toSpanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			dependencyTypes, err := getDependencyTypeList(t.removeDependencyTransform.dependencyList...)
			if err != nil {
				return nil, err
			}
			ret.WithRemovedDependencies(fromSpanFinders, toSpanFinders, dependencyTypes)
		case applyConcurrency:
			spanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.applyConcurrencyTransform.spanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			ret.WithSpansGatedBy(
				spanFinders,
				xf.NewConcurrencyLimiter[T, CP, SP, DP](int(t.applyConcurrencyTransform.concurrency)),
			)
		case startAsEarlyAsPossible:
			spanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.startAsEarlyAsPossibleTransform.spanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			ret.WithSpansStartingAsEarlyAsPossible(spanFinders)
		case shrinkNonblockingDependencies:
			toSpanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.shrinkNonblockingDependenciesTransform.toSpanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			dependencyTypes, err := getDependencyTypeList(t.shrinkNonblockingDependenciesTransform.dependencyList...)
			if err != nil {
				return nil, err
			}
			ret.WithShrinkableIncomingDependencies(toSpanFinders, dependencyTypes, 0)
		case changeSpansDuration:
			spanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.changeSpansDurationTransform.spanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			ret.WithSpanUnsuspendedDurationDelta(spanFinders, t.changeSpansDurationTransform.duration)
		case capSpansAbove:
			spanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.capSpansAboveTransform.spanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			ret.WithSpansCappedAboveBy(spanFinders, t.capSpansAboveTransform.duration)
		case capSpansBelow:
			spanFinders, err := traceparser.ParseSpanSpecifierPatterns(ht, t.capSpansBelowTransform.spanSpecifier)
			if err != nil {
				return nil, fmt.Errorf("uncaught span specifier parser error! %w", err)
			}
			ret.WithSpansCappedBelowBy(spanFinders, t.capSpansBelowTransform.duration)
		}
	}
	return ret, nil
}
