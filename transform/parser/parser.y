%{
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

  package parser
  import (
    "fmt"
    "strconv"
    "strings"

    "github.com/google/tracey/trace/parser/lexer"
  	traceparser "github.com/google/tracey/trace/parser"
  )

  type transformType int

  const (
    scaleSpan transformType = iota
    scaleDependency
    addDependency
    removeDependency
    applyConcurrency
    startAsEarlyAsPossible
    shrinkNonblockingDependencies
    changeSpansDuration
    capSpansAbove
    capSpansBelow
  )

  type scaleSpanTransform struct {
    spanSpecifier string
    scaleFactor float64
  }

  type scaleDependencyTransform struct {
    dependencyList []string
    fromSpanSpecifier, toSpanSpecifier string
    scaleFactor float64
  }

  type addDependencyTransform struct {
    dependency string
    fromPositionSpecifier, toPositionSpecifier string
  }

  type removeDependencyTransform struct {
    dependencyList []string
    fromSpanSpecifier, toSpanSpecifier string
  }

  type applyConcurrencyTransform struct {
    concurrency int64
    spanSpecifier string
  }

  type startAsEarlyAsPossibleTransform struct {
    spanSpecifier string
  }

  type shrinkNonblockingDependenciesTransform struct {
    dependencyList []string
    toSpanSpecifier string
  }

  type changeSpansDurationTransform struct {
    spanSpecifier string
    duration string
  }

  type capSpansTransform struct {
    spanSpecifier string
    duration string
  }

  type transform struct {
    transformType transformType
    scaleSpanTransform *scaleSpanTransform
    scaleDependencyTransform *scaleDependencyTransform
    addDependencyTransform *addDependencyTransform
    removeDependencyTransform *removeDependencyTransform
    applyConcurrencyTransform *applyConcurrencyTransform
    startAsEarlyAsPossibleTransform *startAsEarlyAsPossibleTransform
    shrinkNonblockingDependenciesTransform *shrinkNonblockingDependenciesTransform
    changeSpansDurationTransform *changeSpansDurationTransform
    capSpansAboveTransform *capSpansTransform
    capSpansBelowTransform *capSpansTransform
  }

  func reportTraceLexerError(l yyLexer, err error) {
    if err != nil {
      ll, ok := l.(*lexer.Lexer[*result, *yySymType])
      if ok {
        ll.WrapError(err)
      } else {
        l.Error("unexpected lexer type")
      }
    }
  }

  func validateSpanSpecifier(l yyLexer, spanSpecifier string) {
    reportTraceLexerError(l, traceparser.ValidateSpanSpecifiers(spanSpecifier))
  }

  func validatePositionSpecifier(l yyLexer, positionSpecifier string) {
    reportTraceLexerError(l, traceparser.ValidatePositionSpecifiers(positionSpecifier))
  }

  func newSpanScaleTransform(spanSpecifier string, scaleFactor float64) *transform {
    return &transform{
      transformType: scaleSpan,
      scaleSpanTransform: &scaleSpanTransform{
        spanSpecifier: spanSpecifier,
        scaleFactor: scaleFactor,
      },
    }
  }

  func newDependencyScaleTransform(dependencyList []string, fromSpanSpecifier, toSpanSpecifier string, scaleFactor float64) *transform {
    return &transform{
      transformType: scaleDependency,
      scaleDependencyTransform: &scaleDependencyTransform{
        dependencyList: dependencyList,
        fromSpanSpecifier: fromSpanSpecifier,
        toSpanSpecifier: toSpanSpecifier,
        scaleFactor: scaleFactor,
      },
    }
  }

  func newDependencyAddTransform(dependency string, fromPositionSpecifier, toPositionSpecifier string) *transform {
    return &transform{
      transformType: addDependency,
      addDependencyTransform: &addDependencyTransform{
        dependency: dependency,
        fromPositionSpecifier: fromPositionSpecifier,
        toPositionSpecifier: toPositionSpecifier,
      },
    }
  }

  func newDependencyRemoveTransform(dependencyList []string, fromSpanSpecifier, toSpanSpecifier string) *transform {
    return &transform{
      transformType: removeDependency,
      removeDependencyTransform: &removeDependencyTransform{
        dependencyList: dependencyList,
        fromSpanSpecifier: fromSpanSpecifier,
        toSpanSpecifier: toSpanSpecifier,
      },
    }
  }

  func newConcurrencyApplyTransform(concurrency int64, spanSpecifier string) *transform {
    return &transform{
      transformType: applyConcurrency,
      applyConcurrencyTransform: &applyConcurrencyTransform{
        concurrency: concurrency,
        spanSpecifier: spanSpecifier,
      },
    }
  }

  func newStartAsEarlyAsPossibleTransform(spanSpecifier string) *transform {
    return &transform{
      transformType: startAsEarlyAsPossible,
      startAsEarlyAsPossibleTransform: &startAsEarlyAsPossibleTransform{
        spanSpecifier: spanSpecifier,
      },
    }
  }

  func newNonblockingDependenciesShrinkTransform(dependencyList []string, toSpanSpecifier string) *transform {
    return &transform{
      transformType: shrinkNonblockingDependencies,
      shrinkNonblockingDependenciesTransform: &shrinkNonblockingDependenciesTransform{
        dependencyList: dependencyList,
        toSpanSpecifier: toSpanSpecifier,
      },
    }
  }

  func newChangeSpansDurationTransform(spanSpecifier string, duration string) *transform {
    return &transform {
      transformType: changeSpansDuration,
      changeSpansDurationTransform: &changeSpansDurationTransform{
        spanSpecifier: spanSpecifier,
        duration: duration,
      },
    }
  }

  func newCapSpansAboveTransform(spanSpecifier string, duration string) *transform {
    return &transform {
      transformType: capSpansAbove,
      capSpansAboveTransform: &capSpansTransform{
        spanSpecifier: spanSpecifier,
        duration: duration,
      },
    }
  }

  func newCapSpansBelowTransform(spanSpecifier string, duration string) *transform {
    return &transform {
      transformType: capSpansBelow,
      capSpansBelowTransform: &capSpansTransform{
        spanSpecifier: spanSpecifier,
        duration: duration,
      },
    }
  }

  func setTransforms(l yyLexer, transforms []*transform) {
    ll, ok := l.(*lexer.Lexer[*result, *yySymType])
    if !ok {
      l.Error(fmt.Sprintf("unexpected lexer type %T", l))
      return
    }
    ll.Results = &result{
      transforms: transforms,
    }
  }
%}

// yySymType
%union{
  transforms []*transform
  transform *transform

  strs []string
  str string
  fnum float64
  inum int64
}

%type <transforms> transform_list
%type <transform> transform
%type <str> spans
%type <str> position
%type <str> positions
%type <fnum> scale_factor
%type <strs> dependencies
//%type <strs> dependency_list
%type <str> dependency
%type <str> duration
%type <inum> concurrency

%token <str> STR PARENTHESIZED_STR

//%token <str> STR SCALE ADD REMOVE APPLY START SHRINK CAP CHANGE SPANS POSITION
//%token <str> POSITIONS DEPENDENCY DEPENDENCIES CONCURRENCY ABOVE BELOW BY FROM
//%token <str> TO AS EARLY POSSIBLE NONBLOCKING DURATION FACTOR

%nonassoc SCALE ADD REMOVE APPLY START SHRINK CAP CHANGE
%nonassoc SPANS POSITION POSITIONS DEPENDENCY DEPENDENCIES CONCURRENCY
%nonassoc ABOVE BELOW BY FROM TO AS
%nonassoc EARLY POSSIBLE NONBLOCKING DURATION FACTOR
%nonassoc SEMICOLON

%start start

%%

start : transform_list { setTransforms(yylex, $1) }

transform_list :                                    { $$ = []*transform{} }
               | transform_list transform SEMICOLON { $$ = append($1, $2) }
               ;

transform : SCALE spans BY scale_factor                             { $$ = newSpanScaleTransform($2, $4) }
          | SCALE dependencies FROM spans TO spans BY scale_factor  { $$ = newDependencyScaleTransform($2, $4, $6, $8) }
          | ADD dependency FROM position TO positions               { $$ = newDependencyAddTransform($2, $4, $6) }
          | REMOVE dependencies FROM spans TO spans                 { $$ = newDependencyRemoveTransform($2, $4, $6) }
          | APPLY CONCURRENCY concurrency TO spans                  { $$ = newConcurrencyApplyTransform($3, $5) }
          | START spans AS EARLY AS POSSIBLE                        { $$ = newStartAsEarlyAsPossibleTransform($2) }
          | SHRINK NONBLOCKING dependencies TO spans                { $$ = newNonblockingDependenciesShrinkTransform($3, $5) }
          | CHANGE spans BY duration                                { $$ = newChangeSpansDurationTransform($2, $4) }
          | CAP spans ABOVE duration                                { $$ = newCapSpansAboveTransform($2, $4) }
          | CAP spans BELOW duration                                { $$ = newCapSpansBelowTransform($2, $4) }
          ;

spans : SPANS PARENTHESIZED_STR {validateSpanSpecifier(yylex, $2); $$ = $2 }
      ;

position : POSITION PARENTHESIZED_STR { validatePositionSpecifier(yylex, $2); $$ = $2 }

positions : POSITIONS PARENTHESIZED_STR { validatePositionSpecifier(yylex, $2); $$ = $2 }
          ;

scale_factor : STR { $$ = expectFloat(yylex, $1) }
             ;

dependencies : DEPENDENCIES PARENTHESIZED_STR { $$ = strings.Split(stripParens($2), ",") }

dependency : DEPENDENCY PARENTHESIZED_STR { $$ = stripParens($2) }
           ;

concurrency : PARENTHESIZED_STR { $$ = expectInt(yylex, stripParens($1)) }
            ;

duration : DURATION PARENTHESIZED_STR { $$ = stripParens($2) }

%%

func stripParens(str string) string {
  return str[1:len(str)-1]
}

func expectFloat(l yyLexer, str string) float64 {
  v, err := strconv.ParseFloat(str, 64)
  if err != nil {
    l.Error(err.Error())
  }
  return v
}

func expectInt(l yyLexer, str string) int64 {
  v, err := strconv.ParseInt(str, 10, 64)
  if err != nil {
    l.Error(err.Error())
  }
  return v
}