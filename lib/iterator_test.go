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
	"bufio"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/traits"
)

var errTestRead = errors.New("test read failure")

// TestIteratorErrors checks that lazy decoders report a decoding or read
// error exactly once, and then end, rather than losing the error or never
// ending.
func TestIteratorErrors(t *testing.T) {
	long := strings.Repeat("x", bufio.MaxScanTokenSize+1)
	tests := []struct {
		name      string
		iter      traits.Iterator
		wantVals  int
		wantError string
	}{
		{
			name:      "json_syntax",
			iter:      newJSONIter(strings.NewReader(`{"a":1} {bad`)),
			wantVals:  1,
			wantError: "decode:",
		},
		{
			name:      "json_truncated",
			iter:      newJSONIter(strings.NewReader(`{"a":1} {"b":`)),
			wantVals:  1,
			wantError: "unexpected EOF",
		},
		{
			name:      "json_read",
			iter:      newJSONIter(io.MultiReader(strings.NewReader("{\"a\":1}\n"), iotest.ErrReader(errTestRead))),
			wantVals:  1,
			wantError: errTestRead.Error(),
		},
		{
			name:     "json_clean",
			iter:     newJSONIter(strings.NewReader("{\"a\":1}\n{\"b\":2}\n")),
			wantVals: 2,
		},
		{
			name:      "lines_too_long",
			iter:      newLineIter(strings.NewReader("a\n" + long + "\nb\n")),
			wantVals:  1,
			wantError: bufio.ErrTooLong.Error(),
		},
		{
			name:      "lines_read",
			iter:      newLineIter(io.MultiReader(strings.NewReader("a\nb\n"), iotest.ErrReader(errTestRead))),
			wantVals:  2,
			wantError: errTestRead.Error(),
		},
		{
			name:     "lines_clean",
			iter:     newLineIter(strings.NewReader("a\nb\n")),
			wantVals: 2,
		},
		{
			name:      "csv_syntax",
			iter:      newCSVIter(strings.NewReader("1,2\n3,\"4\n")),
			wantVals:  1,
			wantError: "csv:",
		},
		{
			name:      "csv_read",
			iter:      newCSVIter(io.MultiReader(strings.NewReader("1,2\n"), iotest.ErrReader(errTestRead))),
			wantVals:  1,
			wantError: errTestRead.Error(),
		},
		{
			// A row with a different number of fields from the first is
			// reported once, and the iterator ends there, dropping that
			// row and any rows after it.
			name:      "csv_field_count",
			iter:      newCSVIter(strings.NewReader("a,b\n1,2\n3\n4,5\n")),
			wantVals:  2,
			wantError: "wrong number of fields",
		},
		{
			name:     "csv_clean",
			iter:     newCSVIter(strings.NewReader("1,2\n3,4\n")),
			wantVals: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			vals, errs, ended := drainIterator(test.iter, 20)
			if !ended {
				t.Fatal("iterator did not end")
			}
			if vals != test.wantVals {
				t.Errorf("got %d values; want %d", vals, test.wantVals)
			}
			switch {
			case test.wantError == "" && len(errs) != 0:
				t.Errorf("unexpected errors: %v", errs)
			case test.wantError != "" && len(errs) != 1:
				t.Errorf("got %d errors %v; want exactly one containing %q", len(errs), errs, test.wantError)
			case test.wantError != "" && !strings.Contains(errs[0].Error(), test.wantError):
				t.Errorf("error = %q; want it to contain %q", errs[0], test.wantError)
			}
		})
	}
}

// drainIterator reads it until HasNext is false or limit calls to Next have
// been made. It returns the number of values and the errors read, and whether
// the iterator ended within the limit.
func drainIterator(it traits.Iterator, limit int) (vals int, errs []error, ended bool) {
	for i := 0; i < limit; i++ {
		if it.HasNext() != types.True {
			return vals, errs, true
		}
		v := it.Next()
		if err, ok := v.(*types.Err); ok {
			errs = append(errs, err)
			continue
		}
		vals++
	}
	return vals, errs, false
}

func newJSONIter(r io.Reader) traits.Iterator {
	s := &lazyJSONStream{reader: r, adapter: types.DefaultTypeAdapter}
	return s.Iterator()
}

func newLineIter(r io.Reader) traits.Iterator {
	s := &lazyLineStream{scanner: bufio.NewScanner(r)}
	return s.Iterator()
}

func newCSVIter(r io.Reader) traits.Iterator {
	s := &lazyCSVStream{cr: csv.NewReader(r)}
	return s.Iterator()
}
