%{
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

  package parser
  import (
    "fmt"
    "strconv"
    "strings"

    "github.com/google/tracey/trace"
    "github.com/google/tracey/trace/parser/lexer"
    "github.com/google/tracey/trace/parser/predicate"
  )

  type resultType int

  const (
    spanSpecifiersType resultType = iota
    positionSpecifiersType
  )

  type pathElementType int

  const (
    literal pathElementType = iota
    regex
    star
    globstar
  )

  func (pet pathElementType) String() string {
    switch pet {
      case literal:
        return "literal"
      case regex:
        return "regex"
      case star:
        return "star"
      case globstar:
        return "globstar"
      default:
        return "<unknown>"
    }
  }

  type pathElement struct {
    t pathElementType
    str string
  }

  type spanSpecifier struct {
    categoryMatchers []*pathElement
    spanMatchers []*pathElement
  }

  func pathStr(path []*pathElement) string {
    ret := make([]string, len(path))
    for idx, pe := range path {
      ret[idx] = pe.String()
    }
    return strings.Join(ret, "/")
  }

  func (ss *spanSpecifier) String() string {
    var ret []string
    if len(ss.categoryMatchers) > 0 {
      ret = append(ret, fmt.Sprintf("cats %s", pathStr(ss.categoryMatchers)))
    }
    if len(ss.spanMatchers) > 0 {
      ret = append(ret, fmt.Sprintf("spans %s", pathStr(ss.spanMatchers)))
    }
    return strings.Join(ret, ", ")
  }

  type spanSpecifiers struct {
    spanSpecifiers []*spanSpecifier
  }

  func (ss *spanSpecifiers) String() string {
    ret := make([]string, len(ss.spanSpecifiers))
    for idx, ss := range ss.spanSpecifiers {
      ret[idx] = ss.String()
    }
    return strings.Join(ret, "; ")
  }

  type spawningPathElement struct {
    spanSpecifiers *spanSpecifiers
    directChild bool
  }

  func (spe *spawningPathElement) String() string {
    var ret string
    if spe.directChild {
      ret = "(directly spawned) "
    } else {
      ret = "(indirectly spawned) "
    }
    return ret + spe.spanSpecifiers.String()
  }

  type spawningSpanSpecifier struct {
    spawningPathElements []*spawningPathElement
    predicate *predicate.Predicate
  }

  func (sss *spawningSpanSpecifier) String() string {
    ret := make([]string, len(sss.spawningPathElements))
    for idx, spe := range sss.spawningPathElements {
      ret[idx] = spe.String()
    }
    return strings.Join(ret, " | ")
  }

  func newSpawningSpanSpecifiers(spawningPathElements ...*spawningPathElement) *spawningSpanSpecifier {
    return &spawningSpanSpecifier{
      spawningPathElements: spawningPathElements,
    }
  }

  func (sss *spawningSpanSpecifier) appendElement(spawningPathElement *spawningPathElement) *spawningSpanSpecifier {
    sss.spawningPathElements = append(sss.spawningPathElements, spawningPathElement)
    return sss
  }

  func newSpanSpecifiers(spanSpecifierSlice ...*spanSpecifier) *spanSpecifiers {
    return &spanSpecifiers{
      spanSpecifiers: spanSpecifierSlice,
    }
  }

  type positionSpecifiers struct {
    spawningSpanSpecifier *spawningSpanSpecifier
    isFrac bool
    percentage float64
    markRE string
    multiplePositionPolicy trace.MultiplePositionPolicy
  }

  func setSpanSpecifiers(l yyLexer, spawningSpanSpecifier *spawningSpanSpecifier) {
    ll, ok := l.(*lexer.Lexer[*result, *yySymType])
    if !ok {
      l.Error(fmt.Sprintf("unexpected lexer type %T", l))
      return
    }
    ll.Results = &result{
      resultType: spanSpecifiersType,
      spawningSpanSpecifier: spawningSpanSpecifier,
    }
  }

  func setPositionSpecifiers(l yyLexer, positionSpecifiers *positionSpecifiers) {
    ll, ok := l.(*lexer.Lexer[*result, *yySymType])
    if !ok {
      l.Error(fmt.Sprintf("unexpected lexer type %T", l))
      return
    }
    ll.Results = &result{
      resultType: positionSpecifiersType,
      positionSpecifiers: positionSpecifiers,
    }
  }
%}

// yySymType
%union{
  spawningSpanSpecifier *spawningSpanSpecifier
  spanSpecifiers *spanSpecifiers
  predicate *predicate.Predicate
  metricType predicate.MetricType
  comparisonOperator predicate.ComparisonOperator
  comparand *predicate.Comparand
  positionSpecifiers *positionSpecifiers
  spanSpecifier *spanSpecifier
  pathElements []*pathElement
  pathElement *pathElement
  str string
  num float64
  multiplePositionPolicy trace.MultiplePositionPolicy
}

%type <spawningSpanSpecifier> spawning_span_specifier predicated_span_specifier
%type <spanSpecifiers> span_specifiers
%type <predicate> predicate
%type <metricType> metric
%type <comparisonOperator> comparison_operator
%type <comparand> comparand
%type <spanSpecifier> span_specifier
%type <positionSpecifiers> position_specifiers
%type <positionSpecifiers> span_frac_position_specifiers
%type <positionSpecifiers> mark_re_position_specifiers
%type <multiplePositionPolicy> multiple_position_policy
%type <num> percentage
%type <pathElements> path_specifier
%type <pathElement> path_fragment
%type <str> string_literal

%token <str> STR

%nonassoc AT GT GTE LT LTE EQ NEQ AND OR SLASH PCT LPAREN RPAREN
%nonassoc COMMA STAR GLOBSTAR DIRECTLY_SPAWNING INDIRECTLY_SPAWNING
%nonassoc STR ALL ANY LATEST EARLIEST DURATION WHERE
%nonassoc TOTAL_DURATION SUSPENDED_DURATION SELF_UNSUSPENDED_DURATION TOTAL_UNSUSPENDED_DURATION

%start start

%%

start : predicated_span_specifier                   { setSpanSpecifiers(yylex, $1) }
      | LPAREN predicated_span_specifier RPAREN     { setSpanSpecifiers(yylex, $2) }
      | position_specifiers               { setPositionSpecifiers(yylex, $1) }
      | LPAREN position_specifiers RPAREN { setPositionSpecifiers(yylex, $2) }
      ;

position_specifiers : span_frac_position_specifiers { $$ = $1 }
                    | mark_re_position_specifiers   { $$ = $1 }

span_frac_position_specifiers : predicated_span_specifier AT percentage multiple_position_policy { $$ = &positionSpecifiers{$1, true, $3, "", $4} }
                              ;

mark_re_position_specifiers : predicated_span_specifier AT LPAREN string_literal RPAREN multiple_position_policy { $$ = &positionSpecifiers{$1, false, 0, $4, $6} }
                            ;

multiple_position_policy : EARLIEST { $$ = trace.EarliestMatchingPosition }
                         | LATEST   { $$ = trace.LatestMatchingPosition }
                         |          { $$ = trace.AllMatchingPositions }
                         ;

percentage : STR PCT { $$ = expectFloat(yylex, $<str>1) }
           ;

predicated_span_specifier : spawning_span_specifier                 { $$ = $1 }
                          | spawning_span_specifier WHERE predicate { $1.predicate = $3; $$ = $1 }
                          ;

predicate : metric comparison_operator comparand { $$ = predicate.NewComparatorPredicate($1, $2, $3) }
          | predicate AND predicate              { $$ = predicate.NewLogicalPredicate($1, predicate.And, $3) }
          | predicate OR predicate               { $$ = predicate.NewLogicalPredicate($1, predicate.Or, $3) }
          | LPAREN predicate RPAREN              { $$ = $2 }
          ;

metric : TOTAL_DURATION             { $$ = predicate.TotalDuration }
       | SUSPENDED_DURATION         { $$ = predicate.SuspendedDuration }
       | SELF_UNSUSPENDED_DURATION  { $$ = predicate.SelfUnsuspendedDuration }
       | TOTAL_UNSUSPENDED_DURATION { $$ = predicate.TotalUnsuspendedDuration }
       ;

comparison_operator : EQ  { $$ = predicate.Equal }
                    | NEQ { $$ = predicate.NotEqual }
                    | GT  { $$ = predicate.GreaterThan }
                    | GTE { $$ = predicate.GreaterThanOrEqual }
                    | LT  { $$ = predicate.LessThan }
                    | LTE { $$ = predicate.LessThanOrEqual }
                    ;

comparand : DURATION LPAREN STR RPAREN { $$ = predicate.Duration($3) }
          ;

spawning_span_specifier : span_specifiers
                           { $$ = newSpawningSpanSpecifiers(&spawningPathElement{$1, true}) }
                         | spawning_span_specifier DIRECTLY_SPAWNING span_specifiers
                           { $$ = $1.appendElement(&spawningPathElement{$3, true}) }
                         | spawning_span_specifier INDIRECTLY_SPAWNING span_specifiers
                           { $$ = $1.appendElement(&spawningPathElement{$3, false}) }
                         ;

span_specifiers : ALL                                  { $$ = newSpanSpecifiers(&spanSpecifier{nil, []*pathElement{&pathElement{globstar, ""}}}) }
                | ANY                                  { $$ = newSpanSpecifiers(&spanSpecifier{nil, []*pathElement{&pathElement{globstar, ""}}}) }
                | span_specifier                       { $$ = newSpanSpecifiers($1) }
                | span_specifiers COMMA span_specifier { $1.spanSpecifiers = append($1.spanSpecifiers, $3); $$ = $1 }
                ;

span_specifier : path_specifier                   { $$ = &spanSpecifier{nil, $1} }
               | path_specifier GT path_specifier { $$ = &spanSpecifier{$1, $3} }
               ;

path_specifier : path_fragment                      { $$ = []*pathElement{$1} }
               | path_specifier SLASH path_fragment { $$ = append($1, $3) }
               ;

path_fragment : string_literal               { expectNonempty(yylex, $1); $$ = &pathElement{literal, $1} }
              | LPAREN string_literal RPAREN { expectNonempty(yylex, $2); $$ = &pathElement{regex, $2} }
              | STAR                         { $$ = &pathElement{star, ""} }
              | GLOBSTAR                     { $$ = &pathElement{globstar, ""} }
              ;

string_literal : STR { $$ = $1 }
               | AT                   { $$ = $<str>1 }
               | ALL                  { $$ = $<str>1 }
               | ANY                  { $$ = $<str>1 }
               ;
%%

func expectFloat(l yyLexer, str string) float64 {
  v, err := strconv.ParseFloat(str, 64)
  if err != nil {
    l.Error(err.Error())
  }
  return v
}

func expectNonempty(l yyLexer, str string) {
  if len(str) == 0 {
    l.Error("path fragment cannot be empty")
  }
}
