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

// Package prefixtree defines a simple value-storing prefix tree
// implementation, suitable for lexing fixed operators.  It is
// case-insensitive.
package prefixtree

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

// Token describes a single token stored within a prefix tree.
type Token[T comparable] struct {
	t                   T
	followedByNonLetter bool
}

// Keyword returns a new Token with the provided value, and indicates that
// the string generating this token is a keyword: in an input string, it must
// be followed by EOI or by a non-letter rune.
func Keyword[T comparable](t T) *Token[T] {
	return &Token[T]{
		t:                   t,
		followedByNonLetter: true,
	}
}

// Operator returns a new Token with the provided value, and indicates that the
// string generating this token is an operator: in an input string, it can be
// followed by anything.
func Operator[T comparable](t T) *Token[T] {
	return &Token[T]{
		t: t,
	}
}

// Node represents a node within a prefix tree.
type Node[T comparable] struct {
	value                    T
	hasValue                 bool
	valueFollowedByNonLetter bool
	children                 map[rune]*Node[T]
}

// New returns a new prefix tree populated from the provided string-to-value
// map.  It is case-insensitive.
func New[T comparable](valuesByInput map[string]*Token[T]) (*Node[T], error) {
	ret := &Node[T]{}
	for str, token := range valuesByInput {
		cursor := ret
		for _, r := range str {
			r = unicode.ToUpper(r)
			if cursor.children == nil {
				cursor.children = map[rune]*Node[T]{}
			}
			child, ok := cursor.children[r]
			if !ok {
				child = &Node[T]{}
				cursor.children[r] = child
			}
			cursor = child
		}
		if cursor.hasValue && cursor.value != token.t {
			return nil, fmt.Errorf("operator '%s' is associated with multiple tokens", str)
		}
		cursor.hasValue, cursor.valueFollowedByNonLetter, cursor.value = true, token.followedByNonLetter, token.t
	}
	return ret, nil
}

// FindMaximalPrefix seeks the longest prefix of str which has a value in the
// subtree rooted at the receiver.  Returns a boolean indicating whether such
// a prefix was found, and if one was, the length of that prefix, the value at
// that that prefix's node, and the matching prefix of str.
func (n *Node[T]) FindMaximalPrefix(str string) (found bool, length int, val T) {
	var r rune
	var c int
	r, c = utf8.DecodeRuneInString(str)
	r = unicode.ToUpper(r)
	if n.children != nil {
		child, ok := n.children[r]
		if ok {
			found, length, val = child.FindMaximalPrefix(str[c:])
			if found {
				return true, length + c, val
			}
		}
	}
	if n.hasValue && (!n.valueFollowedByNonLetter || (c == 0 || !unicode.IsLetter(r))) {
		return true, 0, n.value
	}
	return false, 0, val
}
