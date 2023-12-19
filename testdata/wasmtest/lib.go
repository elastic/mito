//go:generate tinygo build -o go_lib.wasm -target=wasi lib.go
//go:generate gzip go_lib.wasm
//go:generate bash -c "base64 < go_lib.wasm.gz | fold > go_lib.base64"
//go:generate rm go_lib.wasm.gz

package main

import "C"

import (
	"unsafe"
)

func main() {}

var allocs = map[uintptr][]byte{
	zero: nil,
}

var zero uintptr

//export allocate
func allocate(n uintptr) uintptr {
	if n == 0 {
		return zero
	}
	m := make([]byte, n)
	p := uintptr(unsafe.Pointer(unsafe.SliceData(m)))
	allocs[p] = m
	return p
}

//export deallocate
func deallocate(p, _ uintptr) {
	if p != zero {
		if _, ok := allocs[p]; !ok {
			panic("invalid free address")
		}
		delete(allocs, p)
	}
}

//export sum
func sum(x, y int64) int64 {
	return x + y
}

//export add_one
func addOne(x int64) int64 {
	return x + 1
}

//export concat
func concat(a, b *C.char) *C.char {
	// Use our own cString function to hook into our allocator rather
	// than using the Cgo malloc, but use C.GoString to save work since
	// the runtime already has null finding code and allocates with mallocgc.
	return cString(C.GoString(a) + C.GoString(b))
}

func cString(s string) *C.char {
	p := allocate(uintptr(len(s) + 1))
	if p == zero {
		return nil
	}
	m := allocs[p]
	copy(m, s)
	m[len(m)-1] = 0
	return (*C.char)(unsafe.Pointer(unsafe.SliceData(m)))
}
