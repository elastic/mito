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
	"encoding/base64"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/google/cel-go/interpreter"
	"github.com/google/go-cmp/cmp"
	"github.com/rogpeppe/go-internal/testscript"

	"github.com/elastic/mito/lib"
)

var update = flag.Bool("update", false, "update testscript output files")

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"mito": Main,
	}))
}

func TestScripts(t *testing.T) {
	t.Parallel()

	p := testscript.Params{
		Dir:           filepath.Join("testdata"),
		UpdateScripts: *update,
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"base64":    bas64decode,
			"serve":     serve,
			"serve_tls": serveTLS,
			"expand":    expand,
		},
	}
	testscript.Run(t, p)
}

func bas64decode(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("unsupported: ! base64")
	}
	if len(args) != 2 {
		ts.Fatalf("usage: base64 src dst")
	}
	src, err := os.ReadFile(ts.MkAbs(args[0]))
	ts.Check(err)
	dst := make([]byte, base64.StdEncoding.DecodedLen(len(src)))
	n, err := base64.StdEncoding.Decode(dst, src)
	ts.Check(err)
	ts.Check(os.WriteFile(ts.MkAbs(args[1]), dst[:n], 0o644))
}

func serve(ts *testscript.TestScript, neg bool, args []string) {
	server(ts, neg, "serve", httptest.NewServer, args)
}

func serveTLS(ts *testscript.TestScript, neg bool, args []string) {
	server(ts, neg, "serve_tls", httptest.NewTLSServer, args)
}

func server(ts *testscript.TestScript, neg bool, name string, newServer func(handler http.Handler) *httptest.Server, args []string) {
	if neg {
		ts.Fatalf("unsupported: ! %s", name)
	}
	if len(args) != 1 && len(args) != 3 {
		ts.Fatalf("usage: %s body [user password]", name)
	}
	var user, pass string
	body, err := os.ReadFile(ts.MkAbs(args[0]))
	ts.Check(err)
	if len(args) == 3 {
		user = args[1]
		pass = args[2]
	}
	srv := newServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		u, p, _ := req.BasicAuth()
		// Obvious security anti-patterns are obvious; for testing.
		if user != "" && user != u {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("user mismatch"))
			return
		}
		if pass != "" && pass != p {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("password mismatch"))
			return
		}
		w.Write(body)
	}))
	ts.Setenv("URL", srv.URL)
	ts.Defer(func() { srv.Close() })
}

func expand(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("unsupported: ! expand")
	}
	if len(args) != 2 {
		ts.Fatalf("usage: expand src dst")
	}
	src, err := os.ReadFile(ts.MkAbs(args[0]))
	ts.Check(err)
	src = []byte(os.Expand(string(src), func(key string) string {
		return ts.Getenv(key)
	}))
	err = os.WriteFile(ts.MkAbs(args[1]), src, 0o644)
	ts.Check(err)
}

func TestSend(t *testing.T) {
	chans := map[string]chan interface{}{"ch": make(chan interface{})}
	send := lib.Send(chans)

	var got interface{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		got = <-chans["ch"]
	}()

	res, _, err := eval(`42.send_to("ch").close("ch")`, "", nil, send)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if res != "true" {
		t.Errorf("unexpected false result")
	}
	wg.Wait()
	if got != int64(42) {
		t.Errorf("unexpected sent result: got:%v want:42", got)
	}
}

func TestVars(t *testing.T) {
	loc, err := time.LoadLocation("GMT")
	if err != nil {
		t.Fatalf("failed to get time zone: %v", err)
	}
	vars := lib.Globals(map[string]interface{}{
		"i":   int(42),
		"i64": int64(42),
		"u":   uint(42),
		"u64": uint64(42),
		"f32": float32(42),
		"f64": float64(42),
		"s":   "forty two",
		"r":   []byte("forty two"),
		"b":   true,
		"t":   time.Date(1978, time.March, 8, 10, 30, 0, 0, loc),
		"d":   119 * time.Second,
		"ii":  []int{6, 9, 42},
		"msd": map[string]interface{}{
			"question":        "What do you get if you multiply six by nine?",
			"answer":          42,
			"answer_in_words": []byte("Forty two."),
		},
		"mss": map[string]string{
			"question": "What do you get if you multiply six by nine?",
			"answer":   "Forty two.",
		},
	})
	const (
		src = `
{
	"b": b,
	"d": d,
	"i": i,
	"i64": i64,
	"ii": ii,
	"f32": f32,
	"f64": f64,
	"msd": msd,
	"msd.answer": msd.answer,
	"msd.question": msd.question,
	"msd.answer_in_words": string(msd.answer_in_words),
	"mss": mss,
	"mss.answer": mss.answer,
	"mss.question": mss.question,
	"r": string(r), // This tests that it is properly converted to a bytes.
	"s": s,
	"t": t,
	"u": u,
	"u64": u64,
}
`
		want = `{
	"b": true,
	"d": "119s",
	"f32": 42,
	"f64": 42,
	"i": 42,
	"i64": 42,
	"ii": [
		6,
		9,
		42
	],
	"msd": {
		"answer": 42,
		"answer_in_words": "Rm9ydHkgdHdvLg==",
		"question": "What do you get if you multiply six by nine?"
	},
	"msd.answer": 42,
	"msd.answer_in_words": "Forty two.",
	"msd.question": "What do you get if you multiply six by nine?",
	"mss": {
		"answer": "Forty two.",
		"question": "What do you get if you multiply six by nine?"
	},
	"mss.answer": "Forty two.",
	"mss.question": "What do you get if you multiply six by nine?",
	"r": "forty two",
	"s": "forty two",
	"t": "1978-03-08T10:30:00Z",
	"u": 42,
	"u64": 42
}`
	)

	got, _, err := eval(src, "", interpreter.EmptyActivation(), vars)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("unexpected result: got:- want:+\n%v", cmp.Diff(got, want))
	}
}

var regexpTests = []struct {
	name    string
	regexps map[string]*regexp.Regexp
	src     string
	want    string
}{
	{
		name: "match",
		regexps: map[string]*regexp.Regexp{
			"foo": regexp.MustCompile("foo"),
		},
		src: `['food'.re_match('foo'), b'food'.re_match('foo')]`,
		want: `[
	true,
	true
]`,
	},
	{
		name: "find",
		regexps: map[string]*regexp.Regexp{
			"foo": regexp.MustCompile("foo"),
		},
		src: `['food'.re_find('foo'), b'food'.re_find('foo')]`,
		want: `[
	"foo",
	"Zm9v"
]`,
	},
	{
		name: "find_all",
		regexps: map[string]*regexp.Regexp{
			"foo": regexp.MustCompile("foo."),
		},
		src: `['food fool'.re_find_all('foo'), b'food fool'.re_find_all('foo')]`,
		want: `[
	[
		"food",
		"fool"
	],
	[
		"Zm9vZA==",
		"Zm9vbA=="
	]
]`,
	},
	{
		name: "find_submatch",
		regexps: map[string]*regexp.Regexp{
			"foo": regexp.MustCompile("foo(.)"),
		},
		src: `['food fool'.re_find_submatch('foo'), b'food fool'.re_find_submatch('foo')]`,
		want: `[
	[
		"food",
		"d"
	],
	[
		"Zm9vZA==",
		"ZA=="
	]
]`,
	},
	{
		name: "find_all_submatch",
		regexps: map[string]*regexp.Regexp{
			"foo": regexp.MustCompile("foo(.)"),
		},
		src: `['food fool'.re_find_all_submatch('foo'), b'food fool'.re_find_all_submatch('foo')]`,
		want: `[
	[
		[
			"food",
			"d"
		],
		[
			"fool",
			"l"
		]
	],
	[
		[
			"Zm9vZA==",
			"ZA=="
		],
		[
			"Zm9vbA==",
			"bA=="
		]
	]
]`,
	},
	{
		name: "replace_all",
		regexps: map[string]*regexp.Regexp{
			"foo": regexp.MustCompile("(f)oo([ld])"),
		},
		src: `['food fool'.re_replace_all('foo', '${1}u${2}'), string(b'food fool'.re_replace_all('foo', b'${1}u${2}'))]`,
		want: `[
	"fud ful",
	"fud ful"
]`,
	},
}

func TestRegaxp(t *testing.T) {
	for _, test := range regexpTests {
		t.Run(test.name, func(t *testing.T) {
			got, _, err := eval(test.src, "", interpreter.EmptyActivation(), lib.Regexp(test.regexps))
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Errorf("unexpected result: got:- want:+\n%v", cmp.Diff(got, test.want))
			}
		})
	}
}
