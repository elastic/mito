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

package mito

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elastic/mito/lib"
	"github.com/google/cel-go/cel"
)

var (
	fastMarshal = flag.Bool("fast_marshal", false, "specify fast/non-pretty marshaling of the result")
	sampleBench = flag.Bool("sample_bench", false, "log one benchmark result for each benchmark")
)

var benchmarks = []struct {
	name  string
	setup func(*testing.B) (prg cel.Program, state any, err error)
}{
	// Self-contained.
	{
		name: "hello_world_static",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(
				`"hello world"`,
				root,
			)
			return prg, nil, err
		},
	},
	{
		name: "hello_world_object_static",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(
				`{"greeting":"hello world"}`,
				root,
			)
			return prg, nil, err
		},
	},
	{
		name: "nested_static",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(
				`{"a":{"b":{"c":{"d":{"e":"f"}}}}}`,
				root,
			)
			return prg, nil, err
		},
	},
	{
		name: "encode_json_static",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(
				`{"a":{"b":{"c":{"d":{"e":"f"}}}}}.encode_json()`,
				root,
				lib.JSON(nil),
			)
			return prg, nil, err
		},
	},
	{
		name: "nested_collate_static",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(
				`{"a":{"b":{"c":{"d":{"e":"f"}}}}}.collate("a.b.c.d.e")`,
				root,
				lib.Collections(),
			)
			return prg, nil, err
		},
	},

	// From state.
	{
		name: "hello_world_state",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(root, root)
			state := map[string]any{root: "hello world"}
			return prg, state, err
		},
	},
	{
		name: "hello_world_object_state",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(
				`{"greeting":state.greeting}`,
				root,
			)
			state := map[string]any{root: mustParseJSON(`{"greeting": "hello world}"}`)}
			return prg, state, err
		},
	},
	{
		name: "nested_state",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(root, root)
			state := map[string]any{root: mustParseJSON(`{"a":{"b":{"c":{"d":{"e":"f"}}}}}`)}
			return prg, state, err
		},
	},
	{
		name: "encode_json_state",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(`state.encode_json()`,
				root,
				lib.JSON(nil),
			)
			state := map[string]any{root: mustParseJSON(`{"a":{"b":{"c":{"d":{"e":"f"}}}}}`)}
			return prg, state, err
		},
	},
	// These two have additional complexity due to a wart that requires
	// that we elevate the value to get the environment to know that state
	// is a map.
	//
	// There should be an easier way to do this. It has come up in DX discussions.
	//
	// Similar in the net versions below.
	{
		name: "nested_collate_list_state",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(`[state].collate("a.b.c.d.e")`,
				root,
				lib.Collections(),
			)
			state := map[string]any{root: mustParseJSON(`{"a":{"b":{"c":{"d":{"e":"f"}}}}}`)}
			return prg, state, err
		},
	},
	{
		name: "nested_collate_map_state",
		setup: func(b *testing.B) (cel.Program, any, error) {
			prg, err := compile(`{"state": state}.collate("state.a.b.c.d.e")`,
				root,
				lib.Collections(),
			)
			state := map[string]any{root: mustParseJSON(`{"a":{"b":{"c":{"d":{"e":"f"}}}}}`)}
			return prg, state, err
		},
	},

	// From net.
	{
		// null_net just does a GET and uses the result in the cheapest way.
		// This is to get an idea of how much of the bench work is coming from
		// the test server.
		name: "null_net",
		setup: func(b *testing.B) (cel.Program, any, error) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			b.Cleanup(func() { srv.Close() })
			prg, err := compile(
				fmt.Sprintf(`get(%q).size()`, srv.URL),
				root,
				lib.HTTP(srv.Client(), nil, nil),
			)
			return prg, nil, err
		},
	},
	{
		name: "hello_world_net",
		setup: func(b *testing.B) (cel.Program, any, error) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Write([]byte("hello world"))
			}))
			b.Cleanup(func() { srv.Close() })
			prg, err := compile(
				fmt.Sprintf(`string(get(%q).Body)`, srv.URL),
				root,
				lib.HTTP(srv.Client(), nil, nil),
			)
			return prg, nil, err
		},
	},
	{
		name: "hello_world_object_net",
		setup: func(b *testing.B) (cel.Program, any, error) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Write([]byte(`{"greeting":"hello world"}`))
			}))
			b.Cleanup(func() { srv.Close() })
			prg, err := compile(
				fmt.Sprintf(`{"greeting":bytes(get(%q).Body).decode_json().greeting}`, srv.URL),
				root,
				lib.HTTP(srv.Client(), nil, nil),
				lib.JSON(nil),
			)
			return prg, nil, err
		},
	},
	{
		name: "nested_net",
		setup: func(b *testing.B) (cel.Program, any, error) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Write([]byte(`{"a":{"b":{"c":{"d":{"e":"f"}}}}}`))
			}))
			b.Cleanup(func() { srv.Close() })
			prg, err := compile(
				fmt.Sprintf(`bytes(get(%q).Body).decode_json()`, srv.URL),
				root,
				lib.HTTP(srv.Client(), nil, nil),
				lib.JSON(nil),
			)
			return prg, nil, err
		},
	},
	{
		name: "encode_json_null_net",
		setup: func(b *testing.B) (cel.Program, any, error) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Write([]byte(`{"a":{"b":{"c":{"d":{"e":"f"}}}}}`))
			}))
			b.Cleanup(func() { srv.Close() })
			prg, err := compile(
				fmt.Sprintf(`get(%q).Body`, srv.URL),
				root,
				lib.HTTP(srv.Client(), nil, nil),
				lib.JSON(nil),
			)
			return prg, nil, err
		},
	},
	{
		// encode_json_net should bead assessed with reference to encode_json_null_net
		// which performs the same request but does not round-trip the object.
		name: "encode_json_net",
		setup: func(b *testing.B) (cel.Program, any, error) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Write([]byte(`{"a":{"b":{"c":{"d":{"e":"f"}}}}}`))
			}))
			b.Cleanup(func() { srv.Close() })
			prg, err := compile(
				fmt.Sprintf(`bytes(get(%q).Body).decode_json().encode_json()`, srv.URL),
				root,
				lib.HTTP(srv.Client(), nil, nil),
				lib.JSON(nil),
			)
			return prg, nil, err
		},
	},
	{
		name: "nested_collate_list_net",
		setup: func(b *testing.B) (cel.Program, any, error) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Write([]byte(`{"a":{"b":{"c":{"d":{"e":"f"}}}}}`))
			}))
			b.Cleanup(func() { srv.Close() })
			prg, err := compile(
				fmt.Sprintf(`[bytes(get(%q).Body).decode_json()].collate("a.b.c.d.e")`, srv.URL),
				root,
				lib.HTTP(srv.Client(), nil, nil),
				lib.JSON(nil),
				lib.Collections(),
			)
			return prg, nil, err
		},
	},
	{
		name: "nested_collate_map_net",
		setup: func(b *testing.B) (cel.Program, any, error) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Write([]byte(`{"a":{"b":{"c":{"d":{"e":"f"}}}}}`))
			}))
			b.Cleanup(func() { srv.Close() })
			prg, err := compile(
				fmt.Sprintf(`{"body": bytes(get(%q).Body).decode_json()}.collate("body.a.b.c.d.e")`, srv.URL),
				root,
				lib.HTTP(srv.Client(), nil, nil),
				lib.JSON(nil),
				lib.Collections(),
			)
			return prg, nil, err
		},
	},
}

func BenchmarkMito(b *testing.B) {
	for _, bench := range benchmarks {
		sampled := false
		b.Run(bench.name, func(b *testing.B) {
			b.StopTimer()
			prg, state, err := bench.setup(b)
			if err != nil {
				b.Fatalf("failed setup: %v", err)
			}
			b.StartTimer()

			for i := 0; i < b.N; i++ {
				v, _, err := run(prg, *fastMarshal, state)
				if err != nil {
					b.Fatalf("failed operation: %v", err)
				}
				if *sampleBench && !sampled {
					sampled = true
					b.Logf("\n%s", v)
				}
			}
		})
	}
}

func mustParseJSON(s string) any {
	var v any
	err := json.Unmarshal([]byte(s), &v)
	if err != nil {
		panic(err)
	}
	return v
}
