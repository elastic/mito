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
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/uuid"
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
		cel.Function("base64",
			cel.MemberOverload(
				"bytes_base64",
				[]*cel.Type{cel.BytesType},
				cel.StringType,
				cel.UnaryBinding(base64Encode),
			),
			cel.Overload(
				"base64_bytes",
				[]*cel.Type{cel.BytesType},
				cel.StringType,
				cel.UnaryBinding(base64Encode),
			),
			cel.MemberOverload(
				"string_base64",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(base64Encode),
			),
			cel.Overload(
				"base64_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(base64Encode),
			),
		),

		cel.Function("base64_decode",
			cel.MemberOverload(
				"string_base64_decode",
				[]*cel.Type{cel.StringType},
				cel.BytesType,
				cel.UnaryBinding(base64Decode),
			),
			cel.Overload(
				"base64_decode_string",
				[]*cel.Type{cel.StringType},
				cel.BytesType,
				cel.UnaryBinding(base64Decode),
			),
		),

		cel.Function("base64_raw",
			cel.MemberOverload(
				"bytes_base64_raw",
				[]*cel.Type{cel.BytesType},
				cel.StringType,
				cel.UnaryBinding(base64RawEncode),
			),
			cel.Overload(
				"base64_raw_bytes",
				[]*cel.Type{cel.BytesType},
				cel.StringType,
				cel.UnaryBinding(base64RawEncode),
			),
			cel.MemberOverload(
				"string_base64_raw",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(base64RawEncode),
			),
			cel.Overload(
				"base64_raw_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(base64RawEncode),
			),
		),

		cel.Function("base64_raw_decode",
			cel.MemberOverload(
				"string_base64_raw_decode",
				[]*cel.Type{cel.StringType},
				cel.BytesType,
				cel.UnaryBinding(base64RawDecode),
			),
			cel.Overload(
				"base64_raw_decode_string",
				[]*cel.Type{cel.StringType},
				cel.BytesType,
				cel.UnaryBinding(base64RawDecode),
			),
		),

		cel.Function("hex",
			cel.MemberOverload(
				"bytes_hex",
				[]*cel.Type{cel.BytesType},
				cel.StringType,
				cel.UnaryBinding(hexEncode),
			),
			cel.Overload(
				"hex_bytes",
				[]*cel.Type{cel.BytesType},
				cel.StringType,
				cel.UnaryBinding(hexEncode),
			),
			cel.MemberOverload(
				"string_hex",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(hexEncode),
			),
			cel.Overload(
				"hex_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(hexEncode),
			),
		),

		cel.Function("md5",
			cel.MemberOverload(
				"bytes_md5",
				[]*cel.Type{cel.BytesType},
				cel.BytesType,
				cel.UnaryBinding(md5Hash),
			),
			cel.Overload(
				"md5_bytes",
				[]*cel.Type{cel.BytesType},
				cel.BytesType,
				cel.UnaryBinding(md5Hash),
			),
			cel.MemberOverload(
				"string_md5",
				[]*cel.Type{cel.StringType},
				cel.BytesType,
				cel.UnaryBinding(md5Hash),
			),
			cel.Overload(
				"md5_string",
				[]*cel.Type{cel.StringType},
				cel.BytesType,
				cel.UnaryBinding(md5Hash),
			),
		),

		cel.Function("sha1",
			cel.MemberOverload(
				"bytes_sha1",
				[]*cel.Type{cel.BytesType},
				cel.BytesType,
				cel.UnaryBinding(sha1Hash),
			),
			cel.Overload(
				"sha1_bytes",
				[]*cel.Type{cel.BytesType},
				cel.BytesType,
				cel.UnaryBinding(sha1Hash),
			),
			cel.MemberOverload(
				"string_sha1",
				[]*cel.Type{cel.StringType},
				cel.BytesType,
				cel.UnaryBinding(sha1Hash),
			),
			cel.Overload(
				"sha1_string",
				[]*cel.Type{cel.StringType},
				cel.BytesType,
				cel.UnaryBinding(sha1Hash),
			),
		),

		cel.Function("sha256",
			cel.MemberOverload(
				"bytes_sha256",
				[]*cel.Type{cel.BytesType},
				cel.BytesType,
				cel.UnaryBinding(sha256Hash),
			),
			cel.Overload(
				"sha256_bytes",
				[]*cel.Type{cel.BytesType},
				cel.BytesType,
				cel.UnaryBinding(sha256Hash),
			),
			cel.MemberOverload(
				"string_sha256",
				[]*cel.Type{cel.StringType},
				cel.BytesType,
				cel.UnaryBinding(sha256Hash),
			),
			cel.Overload(
				"sha256_string",
				[]*cel.Type{cel.StringType},
				cel.BytesType,
				cel.UnaryBinding(sha256Hash),
			),
		),

		cel.Function("hmac",
			cel.MemberOverload(
				"bytes_hmac_string_bytes",
				[]*cel.Type{cel.BytesType, cel.StringType, cel.BytesType},
				cel.BytesType,
				cel.FunctionBinding(hmacHash),
			),
			cel.Overload(
				"hmac_bytes_string_bytes",
				[]*cel.Type{cel.BytesType, cel.StringType, cel.BytesType},
				cel.BytesType,
				cel.FunctionBinding(hmacHash),
			),
			cel.MemberOverload(
				"string_hmac_string_bytes",
				[]*cel.Type{cel.StringType, cel.StringType, cel.BytesType},
				cel.BytesType,
				cel.FunctionBinding(hmacHash),
			),
			cel.Overload(
				"hmac_string_string_bytes",
				[]*cel.Type{cel.StringType, cel.StringType, cel.BytesType},
				cel.BytesType,
				cel.FunctionBinding(hmacHash),
			),
		),

		cel.Function("uuid",
			cel.Overload(
				"uuid_string",
				nil,
				cel.StringType,
				cel.FunctionBinding(uuidString),
			),
		),
	}
}

func (cryptoLib) ProgramOptions() []cel.ProgramOption { return nil }

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
