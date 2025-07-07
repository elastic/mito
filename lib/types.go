// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package lib

import (
	"reflect"
	"time"

	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	// OptionalTypesVersion is the version of the optional types library
	// used by mito.
	OptionalTypesVersion = 2

	// TwoVarComprehensionVersion is the version of the two variable
	// comprehension library used by mito.
	TwoVarComprehensionVersion = 2
)

// Types used in overloads.
var (
	typeV        = cel.TypeParamType("V")
	typeK        = cel.TypeParamType("K")
	mapKV        = cel.MapType(typeK, typeV)
	mapStringDyn = cel.MapType(cel.StringType, cel.DynType)
	listV        = cel.ListType(typeV)
	listK        = cel.ListType(typeK)
	listDyn      = cel.ListType(cel.DynType)
	listString   = cel.ListType(cel.StringType)
)

// Types used for conversion to native.
var (
	// encodableTypes is the preferred type correspondence between CEL types
	// and Go types. This mapping must be kept in agreement with the types in
	// cel-go/common/types.
	encodableTypes = map[ref.Type]reflect.Type{
		types.BoolType:      reflectBoolType,
		types.BytesType:     reflectByteSliceType,
		types.DoubleType:    reflect.TypeOf(float64(0)),
		types.DurationType:  reflect.TypeOf(time.Duration(0)),
		types.IntType:       reflectInt64Type,
		types.ListType:      reflect.TypeOf([]interface{}(nil)),
		types.MapType:       reflectMapStringAnyType,
		types.StringType:    reflectStringType,
		types.TimestampType: reflect.TypeOf(time.Time{}),
		types.UintType:      reflect.TypeOf(uint64(0)),
		types.UnknownType:   reflect.TypeOf((*types.Unknown)(nil)),
	}

	// Linear search for proto.Message mappings and others.
	protobufTypes = []reflect.Type{
		structpbValueType,
		reflect.TypeOf((*structpb.ListValue)(nil)),
		reflect.TypeOf((*structpb.Struct)(nil)),
		// Catch all.
		reflect.TypeOf((*anypb.Any)(nil)),
	}
)

// Types used for reflect conversion.
var (
	anyVal any

	reflectAnyType                  = reflect.TypeOf(&anyVal).Elem()
	reflectBoolType                 = reflect.TypeOf(true)
	reflectByteSliceType            = reflect.TypeOf([]byte(nil))
	reflectIntType                  = reflect.TypeOf(0)
	reflectInt64Type                = reflect.TypeOf(int64(0))
	reflectMapStringAnyType         = reflect.TypeOf(map[string]interface{}(nil))
	reflectMapStringStringSliceType = reflect.TypeOf(map[string][]string(nil))
	reflectStringType               = reflect.TypeOf("")
	reflectStringSliceType          = reflect.TypeOf([]string(nil))

	structpbValueType = reflect.TypeOf((*structpb.Value)(nil))
)
