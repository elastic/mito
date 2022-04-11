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

// Package mito provides the logic for a main function and test infrastructure
// for a CEL-based message stream processor.
//
// This repository is a design sketch. The majority of the logic resides in the
// the lib package.
package mito

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/interpreter"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/elastic/mito/lib"
)

const root = "state"

func Main() int {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage of %s:

  %[1]s [opts] <src.cel>

`, os.Args[0])
		flag.PrintDefaults()
	}
	use := flag.String("use", "all", "libraries to use")
	data := flag.String("data", "", "path to a JSON object holding input (exposed as the label "+root+")")
	cfgPath := flag.String("cfg", "", "path to a YAML file holding configuration for global vars and regular expressions")
	flag.Parse()
	if len(flag.Args()) != 1 {
		flag.Usage()
		return 2
	}

	var libs []cel.EnvOption
	if *cfgPath != "" {
		f, err := os.Open(*cfgPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		defer f.Close()
		dec := yaml.NewDecoder(f)
		var cfg config
		err = dec.Decode(&cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		if len(cfg.Globals) != 0 {
			libs = append(libs, lib.Globals(cfg.Globals))
		}
		if len(cfg.Regexps) != 0 {
			regexps := make(map[string]*regexp.Regexp)
			for name, expr := range cfg.Regexps {
				re, err := regexp.Compile(expr)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
				regexps[name] = re
			}
			libs = append(libs, lib.Regexp(regexps))
		}
	}
	if *use == "all" {
		for _, l := range libMap {
			libs = append(libs, l)
		}
	} else {
		for _, u := range strings.Split(*use, ",") {
			l, ok := libMap[u]
			if !ok {
				fmt.Fprintf(os.Stderr, "no lib %q\n", u)
				return 2
			}
			libs = append(libs, l)
		}
	}
	b, err := os.ReadFile(flag.Args()[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	var input interface{}
	if *data != "" {
		b, err := os.ReadFile(*data)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		err = json.Unmarshal(b, &input)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		input = map[string]interface{}{root: input}
	}

	res, err := eval(string(b), root, input, libs...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(res)
	return 0
}

var (
	libMap = map[string]cel.EnvOption{
		"collections": lib.Collections(),
		"crypto":      lib.Crypto(),
		"json":        lib.JSON(nil),
		"time":        lib.Time(),
		"try":         lib.Try(),
		"file":        lib.File(mimetypes),
		"mime":        lib.MIME(mimetypes),
		"http":        lib.HTTP(nil, nil),
	}

	mimetypes = map[string]interface{}{
		"text/rot13":           func(r io.Reader) io.Reader { return rot13{r} },
		"text/upper":           toUpper,
		"application/gzip":     func(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) },
		"application/x-ndjson": lib.NDJSON,
	}
)

func eval(src, root string, input interface{}, libs ...cel.EnvOption) (string, error) {
	opts := append([]cel.EnvOption{
		cel.Declarations(decls.NewVar(root, decls.Dyn)),
	}, libs...)
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return "", fmt.Errorf("failed to create env: %v", err)
	}

	ast, iss := env.Compile(src)
	if iss.Err() != nil {
		return "", fmt.Errorf("failed compilation: %v", iss.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		return "", fmt.Errorf("failed program instantiation: %v", err)
	}

	if input == nil {
		input = interpreter.EmptyActivation()
	}
	out, _, err := prg.Eval(input)
	if err != nil {
		return "", fmt.Errorf("failed eval: %v", err)
	}

	v, err := out.ConvertToNative(reflect.TypeOf(&structpb.Value{}))
	if err != nil {
		return "", fmt.Errorf("failed proto conversion: %v", err)
	}
	b, err := protojson.MarshalOptions{Indent: "\t"}.Marshal(v.(proto.Message))
	if err != nil {
		return "", fmt.Errorf("failed native conversion: %v", err)
	}
	var res interface{}
	err = json.Unmarshal(b, &res)
	if err != nil {
		return "", fmt.Errorf("failed json conversion: %v", err)
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "\t")
	err = enc.Encode(res)
	return strings.TrimRight(buf.String(), "\n"), err
}

// rot13 is provided for testing purposes.
type rot13 struct {
	r io.Reader
}

func (r rot13) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	for i, b := range p[:n] {
		var base byte
		switch {
		case 'A' <= b && b <= 'Z':
			base = 'A'
		case 'a' <= b && b <= 'z':
			base = 'a'
		default:
			continue
		}
		p[i] = ((b - base + 13) % 26) + base
	}
	return n, err
}

func toUpper(p []byte) {
	for i, b := range p {
		if 'a' <= b && b <= 'z' {
			p[i] &^= 'a' - 'A'
		}
	}
}

type config struct {
	Globals map[string]interface{} `yaml:"globals"`
	Regexps map[string]string      `yaml:"regexp"`
}
