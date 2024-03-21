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
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"hash"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter/functions"
	"github.com/google/uuid"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Crypto returns a cel.EnvOption to configure extended functions for
// cryptographic hash functions and encoding.
//
// # Base64
//
// Returns a string of the base64 encoding of a string or bytes:
//
//	base64(<bytes>) -> <string>
//	base64(<string>) -> <string>
//	<bytes>.base64() -> <string>
//	<string>.base64() -> <string>
//
// Examples:
//
//	"hello world".base64()  // return "aGVsbG8gd29ybGQ="
//
// # Base64 Decode
//
// Returns a bytes from the base64 encoding in a string:
//
//	base64_decode(<string>) -> <bytes>
//	<string>.base64_decode() -> <bytes>
//
// Examples:
//
//	"aGVsbG8gd29ybGQ=".base64_decode()  // return b"hello world"
//
// # Base64 Raw
//
// Returns a string of the raw unpadded base64 encoding of a string or bytes:
//
//	base64_raw(<bytes>) -> <string>
//	base64_raw(<string>) -> <string>
//	<bytes>.base64_raw() -> <string>
//	<string>.base64_raw() -> <string>
//
// Examples:
//
//	"hello world".base64_raw()  // return "aGVsbG8gd29ybGQ"
//
// # Base64 Raw Decode
//
// Returns a bytes from the raw base64 encoding in a string:
//
//	base64_raw_decode(<string>) -> <bytes>
//	<string>.base64_raw_decode() -> <bytes>
//
// Examples:
//
//	"aGVsbG8gd29ybGQ".base64_raw_decode()  // return b"hello world"
//
// # Hex
//
// Returns a string of the hexadecimal representation of a string or bytes:
//
//	hex(<bytes>) -> <string>
//	hex(<string>) -> <string>
//	<bytes>.hex() -> <string>
//	<string>.hex() -> <string>
//
// Examples:
//
//	"hello world".hex()  // return "68656c6c6f20776f726c64"
//
// # MD5
//
// Returns a bytes of the md5 hash of a string or bytes:
//
//	md5(<bytes>) -> <bytes>
//	md5(<string>) -> <bytes>
//	<bytes>.md5() -> <bytes>
//	<string>.md5() -> <bytes>
//
// Examples:
//
//	"hello world".md5()       // return "XrY7u+Ae7tCTyyK7j1rNww=="
//	"hello world".md5().hex() // return "5eb63bbbe01eeed093cb22bb8f5acdc3"
//
// # SHA-1
//
// Returns a bytes of the sha-1 hash of a string or bytes:
//
//	sha1(<bytes>) -> <bytes>
//	sha1(<string>) -> <bytes>
//	<bytes>.sha1() -> <bytes>
//	<string>.sha1() -> <bytes>
//
// Examples:
//
//	"hello world".sha1()       // return "Kq5sNclPz7QV2+lfQIuc6R7oRu0="
//	"hello world".sha1().hex() // return "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed"
//
// # SHA-256
//
// Returns a bytes of the sha-256 cryptographic hash of a string or bytes:
//
//	sha256(<bytes>) -> <bytes>
//	sha256(<string>) -> <bytes>
//	<bytes>.sha256() -> <bytes>
//	<string>.sha256() -> <bytes>
//
// Examples:
//
//	"hello world".sha1()        // return "uU0nuZNNPgilLlLX2n2r+sSE7+N6U4DukIj3rOLvzek="
//	"hello world".sha1().hex()  // return "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
//
// # HMAC
//
// Returns a bytes of the HMAC keyed MAC of a string or bytes using either
// the sha-1 or sha-256 hash function depending on the the second parameter:
//
//	hmac(<bytes>, <string>, <bytes>) -> <bytes>
//	hmac(<string>, <string>, <bytes>) -> <bytes>
//	<bytes>.hmac(<string>, <bytes>) -> <bytes>
//	<string>.hmac(<string>, <bytes>) -> <bytes>
//
// Examples:
//
//	"hello world".hmac("sha256", b"key")        // return "C6BvH5pjAEYeQ0VFNdw8QiPkex01cHPXU26ukOwJW+E="
//	"hello world".hmac("sha256", b"key").hex()  // return "0ba06f1f9a6300461e43454535dc3c4223e47b1d357073d7536eae90ec095be1"
//
// # UUID
//
// Returns a string of a random (Version 4) UUID based on the the Go crypto/rand
// source:
//
//	uuid() -> <string>
//
// Examples:
//
//	uuid()  // return "582fc58b-f983-4c35-abb1-65c507c1dc0c"
func Crypto() cel.EnvOption {
	return cel.Lib(cryptoLib{})
}

type cryptoLib struct{}

func (cryptoLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			decls.NewFunction("base64",
				decls.NewOverload(
					"base64_bytes",
					[]*expr.Type{decls.Bytes},
					decls.String,
				),
				decls.NewInstanceOverload(
					"bytes_base64",
					[]*expr.Type{decls.Bytes},
					decls.String,
				),
				decls.NewOverload(
					"base64_string",
					[]*expr.Type{decls.String},
					decls.String,
				),
				decls.NewInstanceOverload(
					"string_base64",
					[]*expr.Type{decls.String},
					decls.String,
				),
			),
			decls.NewFunction("base64_decode",
				decls.NewOverload(
					"base64_decode_string",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
				decls.NewInstanceOverload(
					"string_base64_decode",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
			),
			decls.NewFunction("base64_raw",
				decls.NewOverload(
					"base64_raw_bytes",
					[]*expr.Type{decls.Bytes},
					decls.String,
				),
				decls.NewInstanceOverload(
					"bytes_base64_raw",
					[]*expr.Type{decls.Bytes},
					decls.String,
				),
				decls.NewOverload(
					"base64_raw_string",
					[]*expr.Type{decls.String},
					decls.String,
				),
				decls.NewInstanceOverload(
					"string_base64_raw",
					[]*expr.Type{decls.String},
					decls.String,
				),
			),
			decls.NewFunction("base64_raw_decode",
				decls.NewOverload(
					"base64_raw_decode_string",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
				decls.NewInstanceOverload(
					"string_base64_raw_decode",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
			),
			decls.NewFunction("hex",
				decls.NewOverload(
					"hex_bytes",
					[]*expr.Type{decls.Bytes},
					decls.String,
				),
				decls.NewInstanceOverload(
					"bytes_hex",
					[]*expr.Type{decls.Bytes},
					decls.String,
				),
				decls.NewOverload(
					"hex_string",
					[]*expr.Type{decls.String},
					decls.String,
				),
				decls.NewInstanceOverload(
					"string_hex",
					[]*expr.Type{decls.String},
					decls.String,
				),
			),
			decls.NewFunction("md5",
				decls.NewOverload(
					"md5_bytes",
					[]*expr.Type{decls.Bytes},
					decls.Bytes,
				),
				decls.NewInstanceOverload(
					"bytes_md5",
					[]*expr.Type{decls.Bytes},
					decls.Bytes,
				),
				decls.NewOverload(
					"md5_string",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
				decls.NewInstanceOverload(
					"string_md5",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
			),
			decls.NewFunction("sha1",
				decls.NewOverload(
					"sha1_bytes",
					[]*expr.Type{decls.Bytes},
					decls.Bytes,
				),
				decls.NewInstanceOverload(
					"bytes_sha1",
					[]*expr.Type{decls.Bytes},
					decls.Bytes,
				),
				decls.NewOverload(
					"sha1_string",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
				decls.NewInstanceOverload(
					"string_sha1",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
			),
			decls.NewFunction("sha256",
				decls.NewOverload(
					"sha256_bytes",
					[]*expr.Type{decls.Bytes},
					decls.Bytes,
				),
				decls.NewInstanceOverload(
					"bytes_sha256",
					[]*expr.Type{decls.Bytes},
					decls.Bytes,
				),
				decls.NewOverload(
					"sha256_string",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
				decls.NewInstanceOverload(
					"string_sha256",
					[]*expr.Type{decls.String},
					decls.Bytes,
				),
			),
			decls.NewFunction("hmac",
				decls.NewOverload(
					"hmac_bytes_string_bytes",
					[]*expr.Type{decls.Bytes, decls.String, decls.Bytes},
					decls.Bytes,
				),
				decls.NewInstanceOverload(
					"bytes_hmac_string_bytes",
					[]*expr.Type{decls.Bytes, decls.String, decls.Bytes},
					decls.Bytes,
				),
				decls.NewOverload(
					"hmac_string_string_bytes",
					[]*expr.Type{decls.String, decls.String, decls.Bytes},
					decls.Bytes,
				),
				decls.NewInstanceOverload(
					"string_hmac_string_bytes",
					[]*expr.Type{decls.String, decls.String, decls.Bytes},
					decls.Bytes,
				),
			),
			decls.NewFunction("uuid",
				decls.NewOverload(
					"uuid_string",
					nil,
					decls.String,
				),
			),
		),
	}
}

func (cryptoLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			&functions.Overload{
				Operator: "base64_bytes",
				Unary:    base64Encode,
			},
			&functions.Overload{
				Operator: "bytes_base64",
				Unary:    base64Encode,
			},
			&functions.Overload{
				Operator: "base64_string",
				Unary:    base64Encode,
			},
			&functions.Overload{
				Operator: "string_base64",
				Unary:    base64Encode,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "base64_decode_string",
				Unary:    base64Decode,
			},
			&functions.Overload{
				Operator: "string_base64_decode",
				Unary:    base64Decode,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "base64_raw_bytes",
				Unary:    base64RawEncode,
			},
			&functions.Overload{
				Operator: "bytes_base64_raw",
				Unary:    base64RawEncode,
			},
			&functions.Overload{
				Operator: "base64_raw_string",
				Unary:    base64RawEncode,
			},
			&functions.Overload{
				Operator: "string_base64_raw",
				Unary:    base64RawEncode,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "base64_raw_decode_string",
				Unary:    base64RawDecode,
			},
			&functions.Overload{
				Operator: "string_base64_raw_decode",
				Unary:    base64RawDecode,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "hex_bytes",
				Unary:    hexEncode,
			},
			&functions.Overload{
				Operator: "bytes_hex",
				Unary:    hexEncode,
			},
			&functions.Overload{
				Operator: "hex_string",
				Unary:    hexEncode,
			},
			&functions.Overload{
				Operator: "string_hex",
				Unary:    hexEncode,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "md5_bytes",
				Unary:    md5Hash,
			},
			&functions.Overload{
				Operator: "bytes_md5",
				Unary:    md5Hash,
			},
			&functions.Overload{
				Operator: "md5_string",
				Unary:    md5Hash,
			},
			&functions.Overload{
				Operator: "string_md5",
				Unary:    md5Hash,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "sha1_bytes",
				Unary:    sha1Hash,
			},
			&functions.Overload{
				Operator: "bytes_sha1",
				Unary:    sha1Hash,
			},
			&functions.Overload{
				Operator: "sha1_string",
				Unary:    sha1Hash,
			},
			&functions.Overload{
				Operator: "string_sha1",
				Unary:    sha1Hash,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "sha256_bytes",
				Unary:    sha256Hash,
			},
			&functions.Overload{
				Operator: "bytes_sha256",
				Unary:    sha256Hash,
			},
			&functions.Overload{
				Operator: "sha256_string",
				Unary:    sha256Hash,
			},
			&functions.Overload{
				Operator: "string_sha256",
				Unary:    sha256Hash,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "hmac_bytes_string_bytes",
				Function: hmacHash,
			},
			&functions.Overload{
				Operator: "bytes_hmac_string_bytes",
				Function: hmacHash,
			},
			&functions.Overload{
				Operator: "hmac_string_string_bytes",
				Function: hmacHash,
			},
			&functions.Overload{
				Operator: "string_hmac_string_bytes",
				Function: hmacHash,
			},
		),
		cel.Functions(
			&functions.Overload{
				Operator: "uuid_string",
				Function: uuidString,
			},
		),
	}
}

func base64Encode(val ref.Val) ref.Val {
	switch val := val.(type) {
	case types.Bytes:
		return types.String(base64.StdEncoding.EncodeToString(val))
	case types.String:
		return types.String(base64.StdEncoding.EncodeToString([]byte(val)))
	default:
		return types.NewErr("invalid type for base64: %s", val.Type())
	}
}

func base64Decode(val ref.Val) ref.Val {
	switch val := val.(type) {
	case types.String:
		b, err := base64.StdEncoding.DecodeString(string(val))
		if err != nil {
			return types.NewErr("invalid base64 encoding: %w", err)
		}
		return types.Bytes(b)
	default:
		return types.NewErr("invalid type for base64_decode: %s", val.Type())
	}
}

func base64RawEncode(val ref.Val) ref.Val {
	switch val := val.(type) {
	case types.Bytes:
		return types.String(base64.RawStdEncoding.EncodeToString(val))
	case types.String:
		return types.String(base64.RawStdEncoding.EncodeToString([]byte(val)))
	default:
		return types.NewErr("invalid type for base64_raw: %s", val.Type())
	}
}

func base64RawDecode(val ref.Val) ref.Val {
	switch val := val.(type) {
	case types.String:
		b, err := base64.RawStdEncoding.DecodeString(string(val))
		if err != nil {
			return types.NewErr("invalid raw base64 encoding: %w", err)
		}
		return types.Bytes(b)
	default:
		return types.NewErr("invalid type for base64_raw_decode: %s", val.Type())
	}
}

func hexEncode(val ref.Val) ref.Val {
	switch val := val.(type) {
	case types.Bytes:
		return types.String(hex.EncodeToString(val))
	case types.String:
		return types.String(hex.EncodeToString([]byte(val)))
	default:
		return types.NewErr("invalid type for hex: %s", val.Type())
	}
}

func md5Hash(val ref.Val) ref.Val {
	switch val := val.(type) {
	case types.Bytes:
		h := md5.New()
		h.Write(val)
		return types.Bytes(h.Sum(nil))
	case types.String:
		h := md5.New()
		h.Write([]byte(val))
		return types.Bytes(h.Sum(nil))
	default:
		return types.NewErr("invalid type for md5: %s", val.Type())
	}
}

func sha1Hash(val ref.Val) ref.Val {
	switch val := val.(type) {
	case types.Bytes:
		h := sha1.New()
		h.Write(val)
		return types.Bytes(h.Sum(nil))
	case types.String:
		h := sha1.New()
		h.Write([]byte(val))
		return types.Bytes(h.Sum(nil))
	default:
		return types.NewErr("invalid type for sha1: %s", val.Type())
	}
}

func sha256Hash(val ref.Val) ref.Val {
	switch val := val.(type) {
	case types.Bytes:
		h := sha256.New()
		h.Write(val)
		return types.Bytes(h.Sum(nil))
	case types.String:
		h := sha256.New()
		h.Write([]byte(val))
		return types.Bytes(h.Sum(nil))
	default:
		return types.NewErr("invalid type for sha256: %s", val.Type())
	}
}

func hmacHash(args ...ref.Val) ref.Val {
	if len(args) != 3 {
		return types.NewErr("no such overload for hmac")
	}
	var val []byte
	switch arg := args[0].(type) {
	case types.Bytes:
		val = []byte(arg)
	case types.String:
		val = []byte(arg)
	default:
		return types.NoSuchOverloadErr()
	}
	hashName, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(args[1], "no such overload")
	}
	key, ok := args[2].(types.Bytes)
	if !ok {
		return types.ValOrErr(args[2], "no such overload")
	}
	var mac hash.Hash
	switch hashName {
	case "sha1":
		mac = hmac.New(sha1.New, key)
	case "sha256":
		mac = hmac.New(sha256.New, key)
	default:
		return types.NewErr("invalid hash for hmac: %s", hashName)
	}
	mac.Write([]byte(val))
	return types.Bytes(mac.Sum(nil))
}

func uuidString(args ...ref.Val) ref.Val {
	id, err := uuid.NewRandom()
	if err != nil {
		return types.NewErr("failed to create uuid: %v", err)
	}
	return types.String(id.String())
}
