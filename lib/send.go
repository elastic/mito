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
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Send returns a cel.EnvOption to configure extended functions for sending
// values on a Go channel during expression evaluation. The channel should be
// received from by a goroutine running in the host program. Send to calls
// will allow error values to be passed as arguments in the <dyn> position.
//
// # Send ref.Val To
//
// Sends a value as a ref.Val to the named channel and returns the value:
//
//	<dyn>.send_to(<string>) -> <dyn>
//	send_to(<dyn>, <string>) -> <dyn>
//
// # Send To
//
// Sends a value to the named channel and returns the value:
//
//	<dyn>.send_to(<string>) -> <dyn>
//	send_to(<dyn>, <string>) -> <dyn>
//
// # Close
//
// Closes the named channel and returns true. It will cause an error if the
// same name is closed more than once in an expression. The dyn received is
// ignored.
//
//	<dyn>.close(<string>) -> <bool>
func Send(ch map[string]chan interface{}) cel.EnvOption {
	return cel.Lib(sendLib(ch))
}

type sendLib map[string]chan interface{}

func (ch sendLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("send_refval_to",
			cel.MemberOverload(
				"dyn_send_refval_string",
				[]*cel.Type{cel.DynType, cel.StringType},
				cel.DynType,
				cel.BinaryBinding(ch.sendRefVal),
				cel.OverloadIsNonStrict(),
			),
			cel.Overload(
				"send_dyn_refval_string",
				[]*cel.Type{cel.DynType, cel.StringType},
				cel.DynType,
				cel.BinaryBinding(ch.sendRefVal),
				cel.OverloadIsNonStrict(),
			),
		),
		cel.Function("send_to",
			cel.MemberOverload(
				"dyn_send_string",
				[]*cel.Type{cel.DynType, cel.StringType},
				cel.DynType,
				cel.BinaryBinding(ch.send),
				cel.OverloadIsNonStrict(),
			),
			cel.Overload(
				"send_dyn_string",
				[]*cel.Type{cel.DynType, cel.StringType},
				cel.DynType,
				cel.BinaryBinding(ch.send),
				cel.OverloadIsNonStrict(),
			),
		),
		cel.Function("close",
			cel.MemberOverload(
				"dyn_close_string",
				[]*cel.Type{cel.DynType, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(ch.close),
			),
		),
	}
}

func (sendLib) ProgramOptions() []cel.ProgramOption { return nil }

func (ch sendLib) close(_, arg ref.Val) ref.Val {
	name, ok := arg.(types.String)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	c, ok := ch[string(name)]
	if !ok {
		return types.NewErr("no channel %s", name)
	}
	close(c)
	return types.Bool(true)
}

func (ch sendLib) sendRefVal(val, arg ref.Val) ref.Val {
	name, ok := arg.(types.String)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	c, ok := ch[string(name)]
	if !ok {
		return types.NewErr("no channel %s", name)
	}
	c <- val
	return val
}

func (ch sendLib) send(val, arg ref.Val) ref.Val {
	name, ok := arg.(types.String)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	c, ok := ch[string(name)]
	if !ok {
		return types.NewErr("no channel %s", name)
	}
	var (
		v   interface{}
		err error
	)
	typ, ok := encodableTypes[val.Type()]
	if ok {
		v, err = val.ConvertToNative(typ)
		if err != nil {
			// This should never happen.
			panic(fmt.Sprintf("type mapping out of sync: %v", err))
		}
	} else {
		for _, typ := range protobufTypes {
			v, err = val.ConvertToNative(typ)
			if err != nil {
				v = nil
			} else {
				break
			}
		}
	}
	if v == nil {
		return types.NewErr("failed to get native value to send")
	}
	c <- v
	return val
}
