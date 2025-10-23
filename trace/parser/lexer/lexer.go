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

// Package lexer provides a goyacc-compatible lexer suitable for trace analysis
// parsing applications.
package lexer

import (
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	prefixtree "github.com/google/tracey/trace/parser/prefix_tree"
)

// TokenConsumerFn defines functions that consumes an arbitrary token from a
// provided input string, setting any appropriate fields in the provided
// lvalue, and returning the consumed token, how many bytes were consumed to
// match the token, and any errors encountered.
type TokenConsumerFn[YY_SYM_TYPE any] func(input string, lvalue YY_SYM_TYPE) (ok bool, token int, consumed int, err error)

// QuotedStringConsumer returns a ConsumerFn that consumes any string token
// wrapped by unescaped instances of the provided quote rune, returning the
// number of bytes consumed (including the quotes) and the provided token type.
// If the provided input does not start with the quote rune, it does not
// consume anything.  If it starts with the quote but does not have a matching
// unescaped terminal quote, it returns an error.  Otherwise it consumes until
// the first unescaped quote rune after the initial one.  If a non-nil setStrFn
// is provided, the consumed input between the bounding quotes is set.
func QuotedStringConsumer[YY_SYM_TYPE any](
	tokenType int, quote rune,
	setStrFn func(str string, lvalue YY_SYM_TYPE) error,
) TokenConsumerFn[YY_SYM_TYPE] {
	return func(input string, lvalue YY_SYM_TYPE) (ok bool, token int, consumed int, err error) {
		r, c := utf8.DecodeRuneInString(input)
		if c == 0 || r != quote {
			return false, 0, 0, nil
		}
		offset := c
		escaped := false
		var str string
		for {
			r, c = utf8.DecodeRuneInString(input[offset:])
			if c == 0 {
				return false, 0, 0, errors.New("unterminated quote")
			}
			if !escaped && r == quote {
				if setStrFn != nil {
					if err := setStrFn(str, lvalue); err != nil {
						return false, 0, 0, err
					}
				}
				offset += c
				return true, tokenType, offset, nil
			}
			escaped = !escaped && r == '\\'
			if !escaped {
				str += string(r)
			}
			offset += c
		}
	}
}

// PrefixTreeConsumer returns a ConsumerFn consuming any string token defined
// in the provided prefix tree, and returning the consumed token and its
// length.
func PrefixTreeConsumer[YY_SYM_TYPE any](
	prefixTreeRoot *prefixtree.Node[int],
	setStrFn func(str string, lvalue YY_SYM_TYPE) error,
) TokenConsumerFn[YY_SYM_TYPE] {
	return func(input string, lvalue YY_SYM_TYPE) (ok bool, token int, consumed int, err error) {
		// If a prefix of l.input is an operator, consume that prefix and return the
		// operator.  If multiple prefixes are operators, choose the longest.
		found, operatorLength, val := prefixTreeRoot.FindMaximalPrefix(input)
		if found {
			if setStrFn != nil {
				if err := setStrFn(input[:operatorLength], lvalue); err != nil {
					return false, 0, 0, err
				}
			}
			return true, val, operatorLength, nil
		}
		return false, 0, 0, nil
	}
}

// NonTokenConsumerFn defines functions that consume nontoken input, such as
// whitespace or comments, from a provided input string, returning the number
// of bytes consumed.
type NonTokenConsumerFn func(input string) (consumed int, err error)

// ConsumeWhitespace consumes whitespace from the beginning of the provided
// input.
func ConsumeWhitespace(input string) (consumed int, err error) {
	// Consume runes until an EOF, error, or non-whitespace rune is encountered.
	var r rune
	var c int
	var offset int
	for {
		r, c = utf8.DecodeRuneInString(input[offset:])
		if !unicode.Is(unicode.White_Space, r) {
			break
		}
		offset += c
	}
	return offset, nil
}

// Lexer is a semi-generic lexer implementation compatible with goyacc.
type Lexer[R any, YY_SYM_TYPE any] struct {
	// Parse results.
	Results R
	Err     error

	// Lexer state
	tokenConsumers    []TokenConsumerFn[YY_SYM_TYPE]
	nonTokenConsumers []NonTokenConsumerFn
	input             string // The input to lex.
	offset            int    // The offset into 'input' of the start of the next rune to read in the input.
	lastValidOffset   int    // The last offset into 'input' at which prior input was parsed as valid.
}

// New returns a new Lexer which attempts to produce tokens on the provided
// input using the provided TokenConsumerFns, applying the latter in order.
// Prior to each lexing, the provided NonTokenConsumerFns are executed in order
// to consume non-token input such as whitespace or comments.
func New[R any, YY_SYM_TYPE any](
	tokenConsumers []TokenConsumerFn[YY_SYM_TYPE],
	nonTokenConsumers []NonTokenConsumerFn,
	input string,
) (*Lexer[R, YY_SYM_TYPE], error) {
	return &Lexer[R, YY_SYM_TYPE]{
		tokenConsumers:    tokenConsumers,
		nonTokenConsumers: nonTokenConsumers,
		input:             input,
		offset:            0,
	}, nil
}

// Lex lexes the next single token from the input, returning that token's
// value (or -1 if the input is exhausted) and setting any relevant fields
// within lvalue.
func (l *Lexer[R, YY_SYM_TYPE]) Lex(lvalue YY_SYM_TYPE) int {
	// If a parser rule action raises an error, the parser won't know that it is
	// in error until the next time it calls Lex.  Handle that case.
	if l.Err != nil {
		// EOF
		return -1
	}
	l.lastValidOffset = l.offset
	if l.offset >= len(l.input) {
		// EOF
		return -1
	}
	for _, consumer := range l.nonTokenConsumers {
		offset, err := consumer(l.input[l.offset:])
		if err != nil {
			l.Error(err.Error())
			return -1
		}
		l.offset += offset
		l.lastValidOffset += offset
	}
	if l.offset >= len(l.input) {
		// EOF
		return -1
	}
	for _, consumer := range l.tokenConsumers {
		ok, ret, offset, err := consumer(l.input[l.offset:], lvalue)
		if err != nil {
			l.Error(err.Error())
			return -1
		}
		if ok {
			l.offset += offset
			return ret
		}
	}
	l.Error("no valid token")
	return -1
}

// Error defines a parse error.  The offset of the error is parameterized to
// support adjustment when parsed strings are embedded in larger parsed text,
// such as trace transformation templates.
type Error struct {
	offset int
	Input  string
	Err    error
}

// Offset returns the offset at which the error occurred.  If e.err is another
// instance of Error (e.g., from a nested parser), it returns the receiver's
// offset added to the inner Error's Offset(); otherwise it just returns the
// receiver's offset.
func (e *Error) Offset() int {
	oe := &Error{}
	if errors.As(e.Err, &oe) {
		return e.offset + oe.Offset()
	}
	return e.offset
}

func (e *Error) Error() string {
	return fmt.Sprintf("parse error in '%s' at offset %d: %s", e.Input, e.Offset(), e.Err.Error())
}

func (l *Lexer[R, YY_SYM_TYPE]) Error(e string) {
	if l.Err == nil {
		l.Err = &Error{
			offset: l.lastValidOffset,
			Input:  l.input,
			Err:    errors.New(e),
		}
	}
}

// WrapError sets the receiver in erroring state, if it is not already in
// error.  If the provided error is an Error instance, it is assumed to come
// from a nested sub-parser, and the start offset of the last valid token
// (penultimateValueOffset) is used as the error's offset, since the nested
// Error will be adding its own offset to this.  Otherwise, the lexer's
// last valid offset (i.e., the start of the current token) is used.
func (l *Lexer[R, YY_SYM_TYPE]) WrapError(err error) {
	if l.Err == nil {
		l.Err = &Error{
			offset: l.lastValidOffset,
			Input:  l.input,
			Err:    err,
		}
	}
}
