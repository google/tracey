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
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
)

// Returns a path, as a string slice with the root at index 0 and the Category
// itself at the end, for the provided Category, using the provided Namer.  If
// useUniqueID is true, the path is composed of the unique IDs of the Category
// and its ancestors; otherwise it is composed of their display names.
func categoryPath[T any, CP, SP, DP fmt.Stringer](
	category Category[T, CP, SP, DP],
	useUniqueID bool,
	namer Namer[T, CP, SP, DP],
) []string {
	ret := []string{}
	for cursor := category; cursor != nil; cursor = cursor.Parent() {
		if useUniqueID {
			ret = append(ret, namer.CategoryUniqueID(cursor))
		} else {
			ret = append(ret, namer.CategoryName(cursor))
		}
	}
	slices.Reverse(ret)
	return ret
}

// GetCategoryDisplayPath returns the provided category's path as a slice of
// path elements, with each path element being that element's name per the
// provided namer.  The returned slice runs from the root category ancestor
// (first) to the provided category (last).
func GetCategoryDisplayPath[T any, CP, SP, DP fmt.Stringer](
	category Category[T, CP, SP, DP],
	namer Namer[T, CP, SP, DP],
) []string {
	return categoryPath(category, false, namer)
}

// GetCategoryUniquePath returns the provided category's path as a slice of
// path elements, with each path element being that element's unique ID per the
// provided namer.  The returned slice runs from the root category ancestor
// (first) to the provided category (last).
func GetCategoryUniquePath[T any, CP, SP, DP fmt.Stringer](
	category Category[T, CP, SP, DP],
	namer Namer[T, CP, SP, DP],
) []string {
	return categoryPath(category, true, namer)
}

// Returns a path, as a string slice with the root at index 0 and the Span
// itself at the end, for the provided Span, using the provided Namer.  If
// useUniqueID is true, the path is composed of the unique IDs of the Span
// and its ancestors; otherwise it is composed of their display names.
func spanPath[T any, CP, SP, DP fmt.Stringer](
	span Span[T, CP, SP, DP],
	useUniqueID bool,
	namer Namer[T, CP, SP, DP],
) []string {
	ret := []string{}
	for cursor := span; cursor != nil; cursor = cursor.ParentSpan() {
		if useUniqueID {
			ret = append(ret, namer.SpanUniqueID(cursor))
		} else {
			ret = append(ret, namer.SpanName(cursor))
		}
	}
	slices.Reverse(ret)
	return ret
}

// GetSpanDisplayPath returns the provided span's path as a slice of path
// elements, with each path element being that element's name per the provided
// namer.  The returned slice runs from the root span ancestor (first) to the
// provided span (last).
func GetSpanDisplayPath[T any, CP, SP, DP fmt.Stringer](
	span Span[T, CP, SP, DP],
	namer Namer[T, CP, SP, DP],
) []string {
	return spanPath(span, false, namer)
}

// GetSpanUniquePath returns the provided span's path as a slice of path
// elements, with each path element being that element's unique ID per the
// provided namer.  The returned slice runs from the root span ancestor
// (first) to the provided span (last).
func GetSpanUniquePath[T any, CP, SP, DP fmt.Stringer](
	span Span[T, CP, SP, DP],
	namer Namer[T, CP, SP, DP],
) []string {
	return spanPath(span, true, namer)
}

// EncodePath encodes the provided path slice into a single string.
func EncodePath(decodedPath ...string) string {
	ret := make([]string, len(decodedPath))
	for idx, decodedPathElement := range decodedPath {
		ret[idx] = base64.StdEncoding.EncodeToString([]byte(decodedPathElement))
	}
	return strings.Join(ret, ":")
}

// DecodePath decodes the provided path string, encoded by EncodePath, into a
// path slice.
func DecodePath(encodedPath string) ([]string, error) {
	ret := []string{}
	for _, encodedPathElement := range strings.Split(encodedPath, ":") {
		decodedPathElement, err := base64.StdEncoding.DecodeString(encodedPathElement)
		if err != nil {
			return nil, err
		}
		ret = append(ret, string(decodedPathElement))
	}
	return ret, nil
}
