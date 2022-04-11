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
	"flag"
	"os"
	"path/filepath"
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
	}
	testscript.Run(t, p)
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

	res, err := eval(`42.send_to("ch").close("ch")`, "", nil, send)
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

	got, err := eval(src, "", interpreter.EmptyActivation(), vars)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("unexpected result: got:- want:+\n%v", cmp.Diff(got, want))
	}
}
