/*
 Copyright 2021 The CloudEvents Authors
 SPDX-License-Identifier: Apache-2.0
*/

package test

import (
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	sqlerrors "github.com/cloudevents/sdk-go/sql/v2/errors"
	"github.com/cloudevents/sdk-go/sql/v2/parser"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding/spec"
	"github.com/cloudevents/sdk-go/v2/event"
	"github.com/cloudevents/sdk-go/v2/test"
)

var TCKFileNames = []string{
	"binary_math_operators",
	"binary_logical_operators",
	"binary_comparison_operators",
	"case_sensitivity",
	"casting_functions",
	"context_attributes_access",
	"exists_expression",
	"in_expression",
	"integer_builtin_functions",
	"like_expression",
	"literals",
	"negate_operator",
	"not_operator",
	"parse_errors",
	"spec_examples",
	"string_builtin_functions",
	"sub_expression",
	"subscriptions_api_recreations",
}

type ErrorType string

const (
	ParseError              ErrorType = "parse"
	MathError               ErrorType = "math"
	CastError               ErrorType = "cast"
	MissingAttributeError   ErrorType = "missingAttribute"
	MissingFunctionError    ErrorType = "missingFunction"
	FunctionEvaluationError ErrorType = "functionEvaluation"
	GenericError            ErrorType = "generic"
)

type TckFile struct {
	Name  string        `json:"name"`
	Tests []TckTestCase `json:"tests"`
}

type TckTestCase struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`

	Result interface{} `json:"result"`
	Error  ErrorType   `json:"error"`

	Event          *cloudevents.Event     `json:"event"`
	EventOverrides map[string]interface{} `json:"eventOverrides"`
}

func (tc TckTestCase) InputEvent(tb testing.TB) cloudevents.Event {
	var inputEvent cloudevents.Event
	if tc.Event != nil {
		inputEvent = *tc.Event
	} else {
		inputEvent = test.FullEvent()
	}

	// Make sure the event is v1
	inputEvent.SetSpecVersion(event.CloudEventsVersionV1)

	for k, v := range tc.EventOverrides {
		require.NoError(tb, spec.V1.SetAttribute(inputEvent.Context, k, v))
	}

	return inputEvent
}

func (tc TckTestCase) ExpectedResult() interface{} {
	switch tc.Result.(type) {
	case int:
		return int32(tc.Result.(int))
	case float64:
		return int32(tc.Result.(float64))
	case bool:
		return tc.Result.(bool)
	}
	return tc.Result
}

func verifyErrorType(expectedType ErrorType, err error) bool {
	switch expectedType {
	case ParseError:
		return sqlerrors.IsParseError(err)
	case MathError:
		return sqlerrors.IsMathError(err)
	case CastError:
		return sqlerrors.IsCastError(err)
	case MissingFunctionError:
		return sqlerrors.IsMissingFunctionError(err)
	case FunctionEvaluationError:
		return sqlerrors.IsFunctionEvaluationError(err)
	case MissingAttributeError:
		return sqlerrors.IsMissingAttributeError(err)
	case GenericError:
		return sqlerrors.IsGenericError(err)
	default:
		return false
	}
}

func TestTCK(t *testing.T) {
	tckFiles := make([]TckFile, 0, len(TCKFileNames))

	_, basePath, _, _ := runtime.Caller(0)
	basePath, _ = path.Split(basePath)

	for _, testFile := range TCKFileNames {
		testFilePath := path.Join(basePath, "tck", testFile+".yaml")

		t.Logf("Loading file %s", testFilePath)

		file, err := os.Open(testFilePath)
		require.NoError(t, err)

		fileBytes, err := io.ReadAll(file)
		require.NoError(t, err)

		tckFileModel := TckFile{}
		require.NoError(t, yaml.Unmarshal(fileBytes, &tckFileModel))

		tckFiles = append(tckFiles, tckFileModel)
	}

	for i, file := range tckFiles {
		i := i
		t.Run(file.Name, func(t *testing.T) {
			for j, testCase := range tckFiles[i].Tests {
				j := j
				testCase := testCase
				t.Run(testCase.Name, func(t *testing.T) {
					t.Parallel()
					testCase := tckFiles[i].Tests[j]

					t.Logf("Test expression: '%s'", testCase.Expression)

					if testCase.Error == ParseError {
						_, err := parser.Parse(testCase.Expression)
						require.NotNil(t, err)
						require.True(t, sqlerrors.IsParseError(err))
						return
					}

					expr, err := parser.Parse(testCase.Expression)
					require.NoError(t, err)
					require.NotNil(t, expr)

					inputEvent := testCase.InputEvent(t)
					result, err := expr.Evaluate(inputEvent)

					if testCase.Error != "" {
						require.NotNil(t, err)
						require.Truef(t, verifyErrorType(testCase.Error, err), "should be %s error", testCase.Error)
					} else {
						require.NoError(t, err)
					}
					require.Equal(t, testCase.ExpectedResult(), result)
				})
			}
		})
	}
}

func BenchmarkTCK(b *testing.B) {
	tckFiles := make([]TckFile, 0, len(TCKFileNames))

	_, basePath, _, _ := runtime.Caller(0)
	basePath, _ = path.Split(basePath)

	for _, testFile := range TCKFileNames {
		testFilePath := path.Join(basePath, "tck", testFile+".yaml")

		b.Logf("Loading file %s", testFilePath)

		file, err := os.Open(testFilePath)
		require.NoError(b, err)

		fileBytes, err := io.ReadAll(file)
		require.NoError(b, err)

		tckFileModel := TckFile{}
		require.NoError(b, yaml.Unmarshal(fileBytes, &tckFileModel))

		tckFiles = append(tckFiles, tckFileModel)
	}

	for i, file := range tckFiles {
		i := i
		b.Run(file.Name, func(b *testing.B) {
			for j, testCase := range tckFiles[i].Tests {
				j := j
				testCase := testCase
				b.Run(fmt.Sprintf("%v parse", testCase.Name), func(b *testing.B) {
					testCase := tckFiles[i].Tests[j]
					for k := 0; k < b.N; k++ {
						_, _ = parser.Parse(testCase.Expression)
					}
				})

				if testCase.Error == ParseError {
					return
				}

				b.Run(fmt.Sprintf("%v evaluate", testCase.Name), func(b *testing.B) {
					testCase := tckFiles[i].Tests[j]

					expr, _ := parser.Parse(testCase.Expression)

					inputEvent := testCase.InputEvent(b)

					for k := 0; k < b.N; k++ {
						_, _ = expr.Evaluate(inputEvent)
					}

				})
			}
		})
	}
}
