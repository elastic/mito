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
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"reflect"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

// Stream returns a cel.EnvOption to configure stream producer functions.
// Stream producers wrap decompression or archive readers around in-memory
// bytes or another stream, returning an opaque streamVal that can be passed to
// lazy decode functions (decode_json_stream_lazy, etc.) for streaming
// decompression and decoding.
//
// # stream_gzip
//
// stream_gzip returns a stream wrapping a gzip reader over the receiver, which
// may be bytes or a stream. When the receiver is bytes, the compressed bytes
// remain in memory; decompression happens on demand through a ~32 KB internal
// buffer. The gzip header is checked when the stream is created.
//
//	<bytes>.stream_gzip() -> <stream>
//	<stream>.stream_gzip() -> <stream>
//
// # stream_bzip2
//
// stream_bzip2 returns a stream wrapping a bzip2 reader over the receiver,
// which may be bytes or a stream. Concatenated bzip2 streams are decoded as
// one. The bzip2 header is checked when the stream is created. Errors in the
// data after the header are reported when the stream is consumed.
//
//	<bytes>.stream_bzip2() -> <stream>
//	<stream>.stream_bzip2() -> <stream>
//
// # stream_tar
//
// stream_tar returns a stream over the contents of a tar archive held in the
// receiver, which may be bytes or a stream.
//
// With an index, the stream is the contents of one entry. The index is
// zero-based, so stream_tar(0) selects the first regular file.
// Only regular files are counted: directories, symbolic links, hard links and
// other special entries are skipped and do not affect the numbering. Entries
// are numbered in archive order. A negative index, or an index that is not
// less than the number of regular files, is an error.
//
// This differs from stream_zip, which counts every entry in the archive, and
// from the list of entries returned for the application/x-tar MIME type, which
// includes entries that are not regular files. The Type field of those entries
// identifies them.
//
// Without an index, the stream is the concatenated contents of all regular
// file entries in archive order, read in a single pass. No separator is added
// between entries, so entries that are decoded together should each be
// self-delimiting (for example, individually compressed).
//
// A tar archive can only be read from the start, so selecting an entry reads
// and discards everything before it, and a stream receiver is consumed up to
// that entry. To process several entries, use the form without an index.
//
//	<bytes>.stream_tar(<int>) -> <stream>
//	<stream>.stream_tar(<int>) -> <stream>
//	<bytes>.stream_tar() -> <stream>
//	<stream>.stream_tar() -> <stream>
//
// Examples:
//
//	body.stream_tar(0).decode_lines()   // lines of the first regular file
//	body.stream_tar(2).decode_lines()   // lines of the third regular file
//	body.stream_tar().decode_lines()    // lines of all regular files
//
// Streams can be consumed once. The following reads every record in a
// .tar.bz2 of bzip2-compressed NDJSON files:
//
//	body.stream_bzip2().stream_tar().stream_bzip2().decode_json_stream_lazy()
//
// # stream_zip
//
// stream_zip returns a stream wrapping the decompression reader for the
// specified entry (by index) in a zip archive held in the receiver bytes.
// The zip archive must fit in memory since archive/zip requires random access.
//
//	<bytes>.stream_zip(<int>) -> <stream>
func Stream() cel.EnvOption {
	return cel.Lib(streamLib{})
}

type streamLib struct{}

func (streamLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("stream_gzip",
			cel.MemberOverload(
				"bytes_stream_gzip",
				[]*cel.Type{cel.BytesType},
				streamCELType,
				cel.UnaryBinding(catch(streamGzip)),
			),
			cel.MemberOverload(
				"stream_stream_gzip",
				[]*cel.Type{streamCELType},
				streamCELType,
				cel.UnaryBinding(catch(streamGzip)),
			),
		),

		cel.Function("stream_bzip2",
			cel.MemberOverload(
				"bytes_stream_bzip2",
				[]*cel.Type{cel.BytesType},
				streamCELType,
				cel.UnaryBinding(catch(streamBzip2)),
			),
			cel.MemberOverload(
				"stream_stream_bzip2",
				[]*cel.Type{streamCELType},
				streamCELType,
				cel.UnaryBinding(catch(streamBzip2)),
			),
		),

		cel.Function("stream_zip",
			cel.MemberOverload(
				"bytes_stream_zip_int",
				[]*cel.Type{cel.BytesType, cel.IntType},
				streamCELType,
				cel.BinaryBinding(catch(streamZip)),
			),
		),

		cel.Function("stream_tar",
			cel.MemberOverload(
				"bytes_stream_tar_int",
				[]*cel.Type{cel.BytesType, cel.IntType},
				streamCELType,
				cel.BinaryBinding(catch(streamTarEntry)),
			),
			cel.MemberOverload(
				"stream_stream_tar_int",
				[]*cel.Type{streamCELType, cel.IntType},
				streamCELType,
				cel.BinaryBinding(catch(streamTarEntry)),
			),
			cel.MemberOverload(
				"bytes_stream_tar",
				[]*cel.Type{cel.BytesType},
				streamCELType,
				cel.UnaryBinding(catch(streamTarAll)),
			),
			cel.MemberOverload(
				"stream_stream_tar",
				[]*cel.Type{streamCELType},
				streamCELType,
				cel.UnaryBinding(catch(streamTarAll)),
			),
		),
	}
}

func (streamLib) ProgramOptions() []cel.ProgramOption { return nil }

// streamCELType is the compile-time CEL type for stream values.
var streamCELType = cel.ObjectType("stream")

// streamRefType is the runtime ref.Type for stream values.
var streamRefType = types.NewObjectType("stream")

var _ ref.Val = (*streamVal)(nil)

// streamVal is an opaque ref.Val wrapping an io.Reader. It advertises no
// traits: it is not iterable, indexable, sizable, or comparable. The only
// useful thing to do with a stream is pass it to a consuming function such
// as decode_json_stream_lazy.
type streamVal struct {
	reader io.Reader
}

func (s *streamVal) ConvertToNative(typeDesc reflect.Type) (any, error) {
	return nil, fmt.Errorf("stream values cannot be converted to %v", typeDesc)
}

func (s *streamVal) ConvertToType(typeVal ref.Type) ref.Val {
	if typeVal == types.TypeType {
		return types.NewTypeValue("stream")
	}
	return types.NewErr("type conversion error from 'stream' to '%s'", typeVal.TypeName())
}

func (s *streamVal) Equal(other ref.Val) ref.Val {
	return types.NewErr("streams are not comparable")
}

func (s *streamVal) Type() ref.Type { return streamRefType }

func (s *streamVal) Value() any { return s.reader }

// sourceReader returns the reader underlying a bytes or stream value.
func sourceReader(val ref.Val) (io.Reader, bool) {
	switch v := val.(type) {
	case *streamVal:
		return v.reader, true
	case types.Bytes:
		return bytes.NewReader(v), true
	}
	return nil, false
}

func streamGzip(val ref.Val) ref.Val {
	src, ok := sourceReader(val)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	r, err := gzip.NewReader(src)
	if err != nil {
		return types.NewErr("stream_gzip: %v", err)
	}
	return &streamVal{reader: prefixReader{r: r, name: "stream_gzip"}}
}

// prefixReader wraps r so that read errors, other than io.EOF, are prefixed
// with the name of the stream function that produced the reader.
type prefixReader struct {
	r    io.Reader
	name string
}

func (p prefixReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if err != nil && err != io.EOF {
		err = fmt.Errorf("%s: %w", p.name, err)
	}
	return n, err
}

func streamBzip2(val ref.Val) ref.Val {
	src, ok := sourceReader(val)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	br := bufio.NewReader(src)
	hdr, err := br.Peek(4)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		return types.NewErr("stream_bzip2: reading header: %v", err)
	}
	if string(hdr[:3]) != "BZh" || hdr[3] < '1' || hdr[3] > '9' {
		return types.NewErr("stream_bzip2: bzip2 data invalid: bad magic value")
	}
	return &streamVal{reader: prefixReader{r: bzip2.NewReader(br), name: "stream_bzip2"}}
}

func streamZip(data, index ref.Val) ref.Val {
	b, ok := data.(types.Bytes)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	idx, ok := index.(types.Int)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	br := bytes.NewReader(b)
	zr, err := zip.NewReader(br, br.Size())
	if err != nil {
		return types.NewErr("stream_zip: %v", err)
	}
	i := int(idx)
	if i < 0 || i >= len(zr.File) {
		return types.NewErr("stream_zip: index %d out of range [0, %d)", i, len(zr.File))
	}
	rc, err := zr.File[i].Open()
	if err != nil {
		return types.NewErr("stream_zip: %v", err)
	}
	return &streamVal{reader: prefixReader{r: rc, name: "stream_zip"}}
}

func streamTarEntry(data, index ref.Val) ref.Val {
	src, ok := sourceReader(data)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	idx, ok := index.(types.Int)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	// Keep the index as an int64 so that a large value is not truncated to a
	// small one on 32-bit platforms.
	i := int64(idx)
	if i < 0 {
		return types.NewErr("stream_tar: negative index %d", i)
	}
	tr := tar.NewReader(src)
	var n int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return types.NewErr("stream_tar: index %d out of range [0, %d)", i, n)
		}
		if err != nil {
			return types.NewErr("stream_tar: %v", err)
		}
		if !regularFile(h) {
			continue
		}
		if n == i {
			return &streamVal{reader: prefixReader{r: tr, name: "stream_tar"}}
		}
		n++
	}
}

// regularFile returns whether h describes an entry that holds file data.
// Directories, links, devices and pax headers do not.
func regularFile(h *tar.Header) bool {
	switch h.Typeflag {
	case tar.TypeReg, tar.TypeCont, tar.TypeGNUSparse:
		return true
	}
	return false
}

func streamTarAll(val ref.Val) ref.Val {
	src, ok := sourceReader(val)
	if !ok {
		return types.NoSuchOverloadErr()
	}
	return &streamVal{reader: prefixReader{r: &tarJoin{tr: tar.NewReader(src)}, name: "stream_tar"}}
}

// tarJoin is an io.Reader that reads the contents of each regular file entry
// of a tar archive in turn.
type tarJoin struct {
	tr      *tar.Reader
	started bool
}

func (t *tarJoin) Read(p []byte) (int, error) {
	for {
		if t.started {
			n, err := t.tr.Read(p)
			if err != io.EOF {
				return n, err
			}
			if n > 0 {
				return n, nil
			}
		}
		for {
			h, err := t.tr.Next()
			if err == io.EOF {
				return 0, io.EOF
			}
			if err != nil {
				return 0, err
			}
			if regularFile(h) {
				t.started = true
				break
			}
		}
	}
}
