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
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/decls"
	"github.com/google/cel-go/common/functions"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/wasmerio/wasmer-go/wasmer"
)

type WASMEnvironment int

const (
	UnknownWASMEnvironment WASMEnvironment = iota + 1
	WASIEnvironment
)

type WASMModule struct {
	Object      []byte
	Environment WASMEnvironment
	Funcs       map[string]WASMDecl
}

type WASMDecl struct {
	Params []string
	Return string
}

type wasmLib struct {
	adapter ref.TypeAdapter

	modules map[string]wasmModule
}

type wasmModule struct {
	inst  *wasmer.Instance
	mem   *wasmer.Memory
	alloc wasmer.NativeFunction
	free  wasmer.NativeFunction
	funcs map[string]wasmer.NativeFunction
	decls map[string]wasmDecl
}

type wasmDecl struct {
	params []*types.Type
	ret    *types.Type
}

// WASM returns a cel.EnvOption to configure foreign functions compiled
// to WASM.
func WASM(adapter ref.TypeAdapter, modules map[string]WASMModule) (cel.EnvOption, error) {
	if adapter == nil {
		adapter = types.DefaultTypeAdapter
	}
	mods := make(map[string]wasmModule, len(modules))
	for modName, mod := range modules {
		obj, err := expand(mod.Object)
		if err != nil {
			return nil, err
		}
		inst, funcs, err := compile(obj, mod.Funcs, mod.Environment)
		if err != nil {
			return nil, err
		}
		mem, err := inst.Exports.GetMemory("memory")
		if err != nil {
			return nil, err
		}
		decls, err := celTypes(mod.Funcs)
		if err != nil {
			return nil, err
		}
		alloc, _ := inst.Exports.GetFunction("allocate")
		free, _ := inst.Exports.GetFunction("deallocate")
		mods[modName] = wasmModule{
			inst:  inst,
			mem:   mem,
			alloc: alloc,
			free:  free,
			funcs: funcs,
			decls: decls,
		}
	}
	return cel.Lib(wasmLib{adapter: adapter, modules: mods}), nil
}

func expand(obj []byte) ([]byte, error) {
	var (
		r   io.Reader
		err error
	)
	switch {
	case bytes.HasPrefix(obj, []byte{0x00, 0x61, 0x73, 0x6d}):
		return obj, nil
	case bytes.HasPrefix(obj, []byte{0x1f, 0x8b}):
		r, err = gzip.NewReader(bytes.NewReader(obj))
		if err != nil {
			return nil, fmt.Errorf("invalid object: %w", err)
		}
	case bytes.HasPrefix(obj, []byte{0x42, 0x5a, 0x68}):
		r = bzip2.NewReader(bytes.NewReader(obj))
	default:
		return nil, errors.New("invalid object: unrecognized magic bytes")
	}
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compile(obj []byte, decls map[string]WASMDecl, env WASMEnvironment) (*wasmer.Instance, map[string]wasmer.NativeFunction, error) {
	store := wasmer.NewStore(wasmer.NewEngine())
	module, err := wasmer.NewModule(store, obj)
	if err != nil {
		return nil, nil, err
	}
	var importObject *wasmer.ImportObject
	switch env {
	case UnknownWASMEnvironment:
		importObject = wasmer.NewImportObject()
	case WASIEnvironment:
		wasi, err := wasmer.NewWasiStateBuilder("wasi-program").Finalize()
		if err != nil {
			return nil, nil, err
		}
		importObject, err = wasi.GenerateImportObject(store, module)
		if err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, fmt.Errorf("invalid environment: %v", env)
	}
	inst, err := wasmer.NewInstance(module, importObject)
	if err != nil {
		return nil, nil, err
	}
	funcs := make(map[string]wasmer.NativeFunction, len(decls)+2)
	for n := range decls {
		funcs[n], err = inst.Exports.GetFunction(n)
		if err != nil {
			return nil, nil, err
		}
	}
	return inst, funcs, nil
}

func celTypes(decls map[string]WASMDecl) (map[string]wasmDecl, error) {
	ds := make(map[string]wasmDecl, len(decls))
	for fn, decl := range decls {
		ret, err := celType(decl.Return)
		if err != nil {
			return nil, err
		}
		params := make([]*types.Type, len(decl.Params))
		for i, p := range decl.Params {
			params[i], err = celType(p)
			if err != nil {
				return nil, err
			}
		}
		ds[fn] = wasmDecl{
			params: params,
			ret:    ret,
		}
	}
	return ds, nil
}

func celType(typ string) (*types.Type, error) {
	ct, ok := typesTable[typ]
	if !ok {
		return nil, fmt.Errorf("no type for %s", typ)
	}
	return ct, nil
}

var typesTable = map[string]*types.Type{
	"bool":   types.BoolType,
	"bytes":  types.BytesType, // Currently only C string.
	"double": types.DoubleType,
	"int":    types.IntType,
	"string": types.StringType, // Currently only C string.
}

func (l wasmLib) CompileOptions() []cel.EnvOption {
	var opts []cel.EnvOption
	for modName, mod := range l.modules {
		for funcName, decl := range mod.decls {
			var binding decls.OverloadOpt
			fn := mod.funcs[funcName]
			switch len(decl.params) {
			case 1:
				binding = cel.UnaryBinding(unary(mod.inst, mod.mem, mod.alloc, mod.free, fn, decl, funcName))
			case 2:
				binding = cel.BinaryBinding(binary(mod.inst, mod.mem, mod.alloc, mod.free, fn, decl, funcName))
			default:
				binding = cel.FunctionBinding(variadic(mod.inst, mod.mem, mod.alloc, mod.free, fn, decl, funcName))
			}
			opts = append(opts, cel.Function(modName+"_"+funcName,
				cel.Overload(
					"wasm_"+modName+"_"+funcName,
					decl.params,
					decl.ret,
					binding,
				),
			))
		}
	}
	return opts
}

func (wasmLib) ProgramOptions() []cel.ProgramOption { return nil }

func unary(inst *wasmer.Instance, mem *wasmer.Memory, alloc, free wasmer.NativeFunction, fn wasmer.NativeFunction, decl wasmDecl, name string) functions.UnaryOp {
	return func(arg ref.Val) ref.Val {
		val0, free0, err := convertToWASM(arg, decl.params[0], mem, alloc, free)
		if err != nil {
			return types.NewErr("failed type conversion to wasm for %s: %v", name, err)
		}
		defer free0()

		ret, err := fn(val0)
		if err != nil {
			return types.NewErr("failed wasm call %s(%v): %v", name, errArg(arg), err)
		}

		ret, err = convertFromWASM(ret, decl.ret, mem, free)
		if err != nil {
			return types.NewErr("failed type conversion from wasm for %s: %v", name, err)
		}

		return types.DefaultTypeAdapter.NativeToValue(ret)
	}
}

func binary(inst *wasmer.Instance, mem *wasmer.Memory, alloc, free wasmer.NativeFunction, fn wasmer.NativeFunction, decl wasmDecl, name string) functions.BinaryOp {
	return func(arg0, arg1 ref.Val) ref.Val {
		val0, free0, err := convertToWASM(arg0, decl.params[0], mem, alloc, free)
		if err != nil {
			return types.NewErr("failed type conversion to wasm for %s arg 0: %v", name, err)
		}
		defer free0()
		val1, free1, err := convertToWASM(arg1, decl.params[1], mem, alloc, free)
		if err != nil {
			return types.NewErr("failed type conversion to wasm for %s arg 1: %v", name, err)
		}
		defer free1()

		ret, err := fn(val0, val1)
		if err != nil {
			return types.NewErr("failed wasm call %s(%v, %v): %v", name, errArg(arg0), errArg(arg1), err)
		}

		ret, err = convertFromWASM(ret, decl.ret, mem, free)
		if err != nil {
			return types.NewErr("failed type conversion from wasm for %s: %v", name, err)
		}

		return types.DefaultTypeAdapter.NativeToValue(ret)
	}
}

func variadic(inst *wasmer.Instance, mem *wasmer.Memory, alloc, free wasmer.NativeFunction, fn wasmer.NativeFunction, decl wasmDecl, name string) functions.FunctionOp {
	return func(args ...ref.Val) ref.Val {
		vals := make([]any, len(args))
		for i, arg := range args {
			val, free, err := convertToWASM(arg, decl.params[i], mem, alloc, free)
			if err != nil {
				return types.NewErr("failed type conversion to wasm for %s arg %d: %v", name, i, err)
			}
			defer free()
			vals[i] = val
		}

		ret, err := fn(vals...)
		if err != nil {
			return types.NewErr("failed wasm call %s(%v): %v", name, errArgs(args), err)
		}

		ret, err = convertFromWASM(ret, decl.ret, mem, free)
		if err != nil {
			return types.NewErr("failed type conversion from wasm for %s: %v", name, err)
		}

		return types.DefaultTypeAdapter.NativeToValue(ret)
	}
}

func convertToWASM(arg ref.Val, typ *types.Type, mem *wasmer.Memory, alloc, free wasmer.NativeFunction) (any, func(), error) {
	val := arg.Value()
	switch typ {
	case types.BoolType, types.DoubleType, types.IntType:
		return val, noop, nil
	case types.StringType, types.BytesType:
		var s string
		switch val := val.(type) {
		case string:
			s = val
		case []byte:
			s = string(val)
		default:
			var want string
			switch typ {
			case types.StringType:
				want = "string"
			case types.BytesType:
				want = "bytes"
			}
			return nil, noop, fmt.Errorf("%v is not a %s: %[1]T", val, want)
		}
		return cstring(s, mem, alloc, free)
	default:
		panic("invalid type")
	}
}

func convertFromWASM(ret any, typ *types.Type, mem *wasmer.Memory, free wasmer.NativeFunction) (any, error) {
	switch typ {
	case types.BoolType, types.DoubleType, types.IntType:
		return ret, nil
	case types.StringType, types.BytesType:
		ptr, ok := ret.(int32)
		if !ok {
			return nil, fmt.Errorf("%v is not a pointer: %[1]T", ret)
		}
		s, err := gostring(ptr, mem, free)
		if err != nil {
			return nil, err
		}
		if typ == types.StringType {
			return s, nil
		}
		return []byte(s), nil
	default:
		panic("invalid type")
	}
}

func cstring(s string, mem *wasmer.Memory, alloc, free wasmer.NativeFunction) (int32, func(), error) {
	if alloc == nil {
		return 0, noop, errors.New("no allocator")
	}
	if free == nil {
		return 0, noop, errors.New("no deallocator")
	}
	ptr, err := alloc(len(s) + 1)
	if err != nil {
		return 0, noop, err
	}
	addr, ok := ptr.(int32)
	if !ok {
		return 0, noop, errors.New("null pointer")
	}
	data := mem.Data()[addr : int(addr)+len(s)+1]
	copy(data, s)
	data[len(s)] = 0
	return addr, func() {
		free(addr, len(s)+1)
	}, nil
}

func noop() {}

func gostring(addr int32, mem *wasmer.Memory, free wasmer.NativeFunction) (string, error) {
	if free == nil {
		return "", errors.New("no deallocator")
	}
	data := mem.Data()
	b, _, ok := bytes.Cut(data[addr:], []byte{0})
	if !ok {
		return "", errors.New("no null")
	}
	_, err := free(addr, len(b)+1)
	return string(b), err
}

func errArg(v ref.Val) string {
	const limit = 10

	buf := limitWriter{limit: limit}
	fmt.Fprintf(&buf, "%#v", v)
	return buf.String()
}

func errArgs(v []ref.Val) string {
	const (
		limit = 10
		more  = "..."
	)
	n := len(v)
	if n > limit {
		n = limit
	}
	args := make([]string, n)
	if len(v) > len(args) {
		n--
		args[n] = more
	}
	for i := range args[:n] {
		args[i] = errArg(v[i])
	}
	return strings.Join(args, ", ")
}

type limitWriter struct {
	buf   strings.Builder
	limit int
}

func (w *limitWriter) String() string {
	return w.buf.String()
}

func (w *limitWriter) Write(b []byte) (int, error) {
	const more = "..."
	n := w.limit - w.buf.Len()
	if n <= 0 {
		w.buf.WriteString(more)
		return len(more), io.EOF
	}
	if n < len(b) {
		n -= len(more)
	}
	if n < 0 {
		n = 0
	} else if n > len(b) {
		n = len(b)
	}
	n, err := w.buf.Write(b[:n])
	if n < len(b) {
		w.buf.WriteString(more)
	}
	return n, err
}
