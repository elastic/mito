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
	"encoding/binary"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	gotypes "go/types"
	"io"
	"math"
	"strconv"
	"strings"
	"unsafe"

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
	Funcs       []string
	Environment WASMEnvironment
	Object      []byte
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
	params []typeMapping
	ret    typeMapping
}

func (d wasmDecl) paramTypes() []*types.Type {
	typs := make([]*types.Type, 0, len(d.params))
	for _, p := range d.params {
		typs = append(typs, p.celType)
	}
	return typs
}

type typeMapping struct {
	name    string
	celType *types.Type
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
		typs, err := funcTypes(modName, mod.Funcs)
		if err != nil {
			return nil, err
		}
		inst, funcs, err := compile(obj, typs, mod.Environment)
		if err != nil {
			return nil, err
		}
		mem, err := inst.Exports.GetMemory("memory")
		if err != nil {
			return nil, err
		}
		decls, err := celTypes(typs)
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

func compile(obj []byte, decls map[string]goWASMDecl, env WASMEnvironment) (*wasmer.Instance, map[string]wasmer.NativeFunction, error) {
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

type goWASMDecl struct {
	Params []gotypes.Type
	Return gotypes.Type
}

func funcTypes(mod string, funcs []string) (map[string]goWASMDecl, error) {
	if len(funcs) == 0 {
		return nil, nil
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "",
		`package `+mod+`
import "C"
func `+strings.Join(funcs, "\nfunc "), 0)
	if err != nil {
		return nil, err
	}
	config := gotypes.Config{
		IgnoreFuncBodies: true,
		FakeImportC:      true,
		Importer:         nil,
	}
	_, err = config.Check(mod, fset, []*ast.File{f}, nil)
	if err != nil {
		return nil, err
	}

	types := make(map[string]goWASMDecl)
	for _, decl := range f.Decls {
		decl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		params, err := paramTypes(decl.Type.Params.List)
		if err != nil {
			return nil, fmt.Errorf("invalid signature %s: %w", decl.Name.Name, err)
		}
		ret, err := returnType(decl.Type.Results.List)
		if err != nil {
			return nil, fmt.Errorf("invalid signature %s: %w", decl.Name.Name, err)
		}
		types[decl.Name.Name] = goWASMDecl{Params: params, Return: ret}
	}
	return types, nil
}

func paramTypes(list []*ast.Field) ([]gotypes.Type, error) {
	var types []gotypes.Type
	for _, f := range list {
		n, typ, err := fieldType(f)
		if err != nil {
			return nil, err
		}
		for i := 0; i < n; i++ {
			types = append(types, typ)
		}
	}
	return types, nil
}

func returnType(list []*ast.Field) (gotypes.Type, error) {
	if len(list) != 1 {
		return nil, fmt.Errorf("invalid return: must have one return value: have %d", len(list))
	}
	n, typ, err := fieldType(list[0])
	if n != 1 {
		return nil, fmt.Errorf("invalid return: must have one return value: have %d", n)
	}
	return typ, err
}

func fieldType(field *ast.Field) (int, gotypes.Type, error) {
	switch typ := field.Type.(type) {
	case *ast.ArrayType:
		elem, ok := typ.Elt.(*ast.Ident)
		if !ok {
			return 0, nil, fmt.Errorf("unsupported type: %T", typ)
		}
		var a gotypes.Type
		switch len := typ.Len.(type) {
		case nil:
			a = gotypes.NewSlice(gotypes.Universe.Lookup(elem.Name).Type())
		case *ast.BasicLit:
			n, err := strconv.ParseInt(len.Value, 10, 64)
			if err != nil {
				return 0, nil, fmt.Errorf("unsupported type: %T", typ)
			}
			a = gotypes.NewArray(gotypes.Universe.Lookup(elem.Name).Type(), n)
		default:
			return 0, nil, fmt.Errorf("unsupported type: %T", typ)
		}
		if len(field.Names) == 0 {
			return 1, a, nil
		}
		return len(field.Names), a, nil
	case *ast.StarExpr:
		expr, ok := typ.X.(*ast.SelectorExpr)
		if !ok || expr.Sel.Name != "char" {
			return 0, nil, fmt.Errorf("unsupported type: %T", typ)
		}
		id, ok := expr.X.(*ast.Ident)
		if !ok || id.Name != "C" {
			return 0, nil, fmt.Errorf("unsupported type: %T", typ)
		}
		if len(field.Names) == 0 {
			return 1, cStringType, nil
		}
		return len(field.Names), cStringType, nil
	case *ast.Ident:
		if len(field.Names) == 0 {
			return 1, gotypes.Universe.Lookup(typ.Name).Type(), nil
		}
		return len(field.Names), gotypes.Universe.Lookup(typ.Name).Type(), nil
	default:
		return 0, nil, fmt.Errorf("unsupported type: %T", typ)
	}
}

var (
	// Avoid having to build a Cgo package to be able to use a *C.char sentinel.
	// This is more effort than we really need to go to, but it feels better.
	cStringType = gotypes.NewPointer(gotypes.NewNamed(gotypes.NewTypeName(0, nil, "_Ctype_char", i8), i8, nil))
	i8          = gotypes.Universe.Lookup("int8").Type()
)

func celTypes(decls map[string]goWASMDecl) (map[string]wasmDecl, error) {
	ds := make(map[string]wasmDecl, len(decls))
	for fn, decl := range decls {
		ret, err := celType(decl.Return.String())
		if err != nil {
			return nil, err
		}
		params := make([]typeMapping, len(decl.Params))
		for i, p := range decl.Params {
			params[i], err = celType(p.String())
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

func celType(typ string) (typeMapping, error) {
	ct, ok := typesTable[typ]
	if !ok {
		return typeMapping{}, fmt.Errorf("no type for %s", typ)
	}
	return typeMapping{name: typ, celType: ct}, nil
}

var typesTable = map[string]*types.Type{
	"*_Ctype_char": types.StringType,
	"bool":         types.BoolType,
	"float64":      types.DoubleType,
	"int64":        types.IntType,
	"string":       types.StringType,

	"[]bool":    types.NewListType(types.BoolType),
	"[]byte":    types.BytesType,
	"[]float64": types.NewListType(types.DoubleType),
	"[]int64":   types.NewListType(types.IntType),
	"[]string":  types.NewListType(types.StringType),
}

func (l wasmLib) CompileOptions() []cel.EnvOption {
	var opts []cel.EnvOption
	for modName, mod := range l.modules {
		for funcName, decl := range mod.decls {
			var binding decls.OverloadOpt
			fn := mod.funcs[funcName]
			switch len(decl.params) {
			case 1:
				binding = cel.UnaryBinding(unaryCall(mod.mem, mod.alloc, mod.free, fn, decl, funcName))
			case 2:
				binding = cel.BinaryBinding(binaryCall(mod.mem, mod.alloc, mod.free, fn, decl, funcName))
			default:
				binding = cel.FunctionBinding(variadicCall(mod.mem, mod.alloc, mod.free, fn, decl, funcName))
			}
			opts = append(opts, cel.Function(modName+"_"+funcName,
				cel.Overload(
					"wasm_"+modName+"_"+funcName,
					decl.paramTypes(),
					decl.ret.celType,
					binding,
				),
			))
		}
	}
	return opts
}

func (wasmLib) ProgramOptions() []cel.ProgramOption { return nil }

func unaryCall(mem *wasmer.Memory, alloc, free wasmer.NativeFunction, fn wasmer.NativeFunction, decl wasmDecl, name string) functions.UnaryOp {
	return func(arg ref.Val) ref.Val {
		val0, free0, err := convertToWASM(arg, decl.params[0], mem, alloc, free)
		if err != nil {
			return types.NewErr("failed type conversion to wasm for %s: %v", name, err)
		}
		defer free0()

		return call(mem, alloc, free, fn, decl, name, val0)
	}
}

func binaryCall(mem *wasmer.Memory, alloc, free wasmer.NativeFunction, fn wasmer.NativeFunction, decl wasmDecl, name string) functions.BinaryOp {
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

		return call(mem, alloc, free, fn, decl, name, val0, val1)
	}
}

func variadicCall(mem *wasmer.Memory, alloc, free wasmer.NativeFunction, fn wasmer.NativeFunction, decl wasmDecl, name string) functions.FunctionOp {
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

		return call(mem, alloc, free, fn, decl, name, vals...)
	}
}

func call(mem *wasmer.Memory, alloc, free wasmer.NativeFunction, fn wasmer.NativeFunction, decl wasmDecl, name string, args ...any) ref.Val {
	wasmArgs, recRet, err := expandArgs(decl.ret, mem, alloc, free, args...)
	if err != nil {
		return types.NewErr("failed wasm call prep %s(%v): %v", name, errArgs(args), err)
	}
	ret, err := fn(wasmArgs...)
	if err != nil {
		return types.NewErr("failed wasm call %s(%v): %v", name, errArgs(args), err)
	}
	if recRet != nil {
		ret = recRet
	}

	ret, err = convertFromWASM(ret, decl.ret, mem, free)
	if err != nil {
		return types.NewErr("failed type conversion from wasm for %s: %v", name, err)
	}

	return types.DefaultTypeAdapter.NativeToValue(ret)

}

func expandArgs(retMapping typeMapping, mem *wasmer.Memory, alloc, free wasmer.NativeFunction, vals ...any) (args []any, ret func() any, err error) {
	var n int
	for _, v := range vals {
		switch v.(type) {
		case stringHeader:
			n += 2
		case sliceHeader:
			n += 3
		default:
			n++
		}
	}
	switch retMapping.name {
	case "string":
		var addr int32
		args, addr, err = allocRet(n, unsafe.Sizeof(stringHeader{}), alloc)
		if err != nil {
			return nil, nil, err
		}
		ret = func() any {
			m := mem.Data()
			h := m[addr : int(addr)+int(unsafe.Sizeof(stringHeader{}))]
			ptr := int32(binary.LittleEndian.Uint32(h[:4]))
			len := int32(binary.LittleEndian.Uint32(h[4:8]))
			s := string(m[ptr : ptr+len])
			free(addr, int(unsafe.Sizeof(stringHeader{})))
			return s
		}
	default:
		if !strings.HasPrefix(retMapping.name, "[]") {
			args = make([]any, 0, n)
			break
		}
		var addr int32
		args, addr, err = allocRet(n, unsafe.Sizeof(sliceHeader{}), alloc)
		if err != nil {
			return nil, nil, err
		}
		m := mem.Data()
		h := m[addr : int(addr)+int(unsafe.Sizeof(sliceHeader{}))]
		switch strings.TrimPrefix(retMapping.name, "[]") {
		case "bool":
			ret = func() any {
				ptr := int32(binary.LittleEndian.Uint32(h[:4]))
				len := int32(binary.LittleEndian.Uint32(h[4:8]))
				s := make([]bool, len)
				o := m[ptr : ptr+len]
				copy(s, *(*[]bool)(unsafe.Pointer(&o)))
				free(addr, int(unsafe.Sizeof(sliceHeader{})))
				return s
			}
		case "byte":
			ret = func() any {
				ptr := int32(binary.LittleEndian.Uint32(h[:4]))
				len := int32(binary.LittleEndian.Uint32(h[4:8]))
				s := bytes.Clone(m[ptr : ptr+len])
				free(addr, int(unsafe.Sizeof(stringHeader{})))
				return s
			}
		case "float64":
			ret = func() any {
				ptr := int32(binary.LittleEndian.Uint32(h[:4]))
				len := int32(binary.LittleEndian.Uint32(h[4:8]))
				s := make([]float64, len)
				for i := range s {
					s[i] = math.Float64frombits(binary.LittleEndian.Uint64(m[ptr:]))
					ptr += int32(unsafe.Sizeof(float64(0)))
				}
				free(addr, int(unsafe.Sizeof(sliceHeader{})))
				return s
			}
		case "int64":
			ret = func() any {
				ptr := int32(binary.LittleEndian.Uint32(h[:4]))
				len := int32(binary.LittleEndian.Uint32(h[4:8]))
				s := make([]int64, len)
				for i := range s {
					s[i] = int64(binary.LittleEndian.Uint64(m[ptr:]))
					ptr += int32(unsafe.Sizeof(int64(0)))
				}
				free(addr, int(unsafe.Sizeof(sliceHeader{})))
				return s
			}
		case "string":
			ret = func() any {
				ptr := int32(binary.LittleEndian.Uint32(h[:4]))
				len := int32(binary.LittleEndian.Uint32(h[4:8]))
				s := make([]string, len)
				for i := range s {
					sptr := int32(binary.LittleEndian.Uint64(m[ptr:]))
					slen := int32(binary.LittleEndian.Uint64(m[ptr+int32(unsafe.Sizeof(int32(0))):]))
					s[i] = string(m[sptr : sptr+slen])
					ptr += int32(unsafe.Sizeof(stringHeader{}))
				}
				free(addr, int(unsafe.Sizeof(sliceHeader{})))
				return s
			}
		}
	}
	for _, v := range vals {
		switch v := v.(type) {
		case stringHeader:
			args = append(args, v.ptr, v.len)
		case sliceHeader:
			args = append(args, v.ptr, v.len, v.cap)
		case bool:
			args = append(args, i32bool(v))
		default:
			args = append(args, v)
		}
	}
	return args, ret, nil
}

func i32bool(t bool) int32 {
	if t {
		return 1
	}
	return 0
}

func allocRet(nargs int, size uintptr, alloc wasmer.NativeFunction) (args []any, retAddr int32, err error) {
	args = make([]any, 1, nargs+1)
	ptr, err := alloc(int(size))
	if err != nil {
		return nil, 0, err
	}
	addr, ok := ptr.(int32)
	if !ok {
		return nil, 0, errors.New("could not allocate return slot")
	}
	args[0] = addr
	return args, addr, nil
}

func convertToWASM(arg ref.Val, typ typeMapping, mem *wasmer.Memory, alloc, free wasmer.NativeFunction) (any, func(), error) {
	val := arg.Value()
	switch typ.celType {
	case types.BoolType, types.DoubleType, types.IntType:
		return val, noop, nil
	case types.BytesType:
		switch val := val.(type) {
		case []byte:
			switch typ.name {
			case "[]byte":
				return byteslice(val, mem, alloc, free)
			default:
				panic("unreachable")
			}
		default:
			return nil, noop, fmt.Errorf("%v is not a bytes: %[1]T", val)
		}
	case types.StringType:
		switch val := val.(type) {
		case string:
			switch typ.name {
			case "*_Ctype_char":
				return cstring(val, mem, alloc, free)
			case "string":
				return nativestring(val, mem, alloc, free)
			default:
				panic("unreachable")
			}
		default:
			return nil, noop, fmt.Errorf("%v is not a string: %[1]T", val)
		}
	default:
		panic("invalid type")
	}
}

func convertFromWASM(ret any, typ typeMapping, mem *wasmer.Memory, free wasmer.NativeFunction) (any, error) {
	if ret, ok := ret.(func() any); ok {
		return ret(), nil
	}
	switch typ.celType {
	case types.BoolType, types.DoubleType, types.IntType:
		return ret, nil
	case types.StringType, types.BytesType:
		switch ret := ret.(type) {
		case int32:
			b, err := gostring(ret, mem, free)
			if err != nil {
				return nil, err
			}
			if typ.celType == types.StringType {
				return string(b), nil
			}
			return bytes.Clone(b), nil
		default:
			return nil, fmt.Errorf("%v is not a pointer: %[1]T", ret)
		}
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

type stringHeader struct {
	ptr int32
	len int32
}

type sliceHeader struct {
	ptr int32
	len int32
	cap int32
}

func nativestring(s string, mem *wasmer.Memory, alloc, free wasmer.NativeFunction) (stringHeader, func(), error) {
	if alloc == nil {
		return stringHeader{}, noop, errors.New("no allocator")
	}
	if free == nil {
		return stringHeader{}, noop, errors.New("no deallocator")
	}
	ptr, err := alloc(len(s))
	if err != nil {
		return stringHeader{}, noop, err
	}
	addr, ok := ptr.(int32)
	if !ok {
		return stringHeader{}, noop, errors.New("null pointer")
	}
	data := mem.Data()[addr : int(addr)+len(s)]
	copy(data, s)
	return stringHeader{ptr: int32(addr), len: int32(len(s))}, func() {
		free(addr, len(s))
	}, nil
}

func byteslice(b []byte, mem *wasmer.Memory, alloc, free wasmer.NativeFunction) (sliceHeader, func(), error) {
	if alloc == nil {
		return sliceHeader{}, noop, errors.New("no allocator")
	}
	if free == nil {
		return sliceHeader{}, noop, errors.New("no deallocator")
	}
	ptr, err := alloc(len(b))
	if err != nil {
		return sliceHeader{}, noop, err
	}
	addr, ok := ptr.(int32)
	if !ok {
		return sliceHeader{}, noop, errors.New("null pointer")
	}
	data := mem.Data()[addr : int(addr)+len(b)]
	copy(data, b)
	return sliceHeader{ptr: int32(addr), len: int32(len(b)), cap: int32(len(b))}, func() {
		free(addr, len(b))
	}, nil
}

func noop() {}

func gostring(addr int32, mem *wasmer.Memory, free wasmer.NativeFunction) ([]byte, error) {
	if free == nil {
		return nil, errors.New("no deallocator")
	}
	data := mem.Data()
	b, _, ok := bytes.Cut(data[addr:], []byte{0})
	if !ok {
		return nil, errors.New("no null")
	}
	_, err := free(addr, len(b)+1)
	return b, err
}

func errArg(v any) string {
	const limit = 10

	buf := limitWriter{limit: limit}
	fmt.Fprintf(&buf, "%#v", v)
	return buf.String()
}

func errArgs(v []any) string {
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
