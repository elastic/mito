package main

import (
	"flag"
	"log"
	"os"

	"github.com/goccy/go-yaml"
	"golang.org/x/tools/txtar"
)

type config struct {
	Globals map[string]interface{} `yaml:"globals,omitempty"`
	Regexps map[string]string      `yaml:"regexp,omitempty"`
	WASM    map[string]wasmModule  `yaml:"wasm,omitempty"`
	XSDs    map[string]string      `yaml:"xsd,omitempty"`
	Auth    map[string]any         `yaml:"auth,omitempty"`
}
type wasmModule struct {
	Funcs       []string `yaml:"funcs,omitempty"`
	Environment string   `yaml:"env"`
	Object      string   `yaml:"obj"` // base64 encoded bytes
}

func main() {
	tgt := flag.String("test", "", "specify the test file to rewrite")
	lib := flag.String("lib", "", "specify the wasm lib to rewrite")
	wasm := flag.String("wasm", "", "specify the compiled and base64 encoded wasm object")
	flag.Parse()
	if *tgt == "" || *lib == "" || *wasm == "" {
		flag.Usage()
		os.Exit(2)
	}
	ar, err := txtar.ParseFile(*tgt)
	if err != nil {
		log.Fatal(err)
	}
	var (
		i int
		f txtar.File
	)
	for i, f = range ar.Files {
		if f.Name == "cfg.yaml" {
			break
		}
	}

	var cfg config
	err = yaml.Unmarshal(f.Data, &cfg)
	if err != nil {
		log.Fatal(err)
	}

	obj := cfg.WASM[*lib]
	b, err := os.ReadFile(*wasm)
	if err != nil {
		log.Fatal(err)
	}
	obj.Object = string(b)
	cfg.WASM[*lib] = obj
	b, err = yaml.Marshal(cfg)
	if err != nil {
		log.Fatal(err)
	}
	ar.Files[i].Data = b
	err = os.WriteFile(*tgt, txtar.Format(ar), 0o664)
	if err != nil {
		log.Fatal(err)
	}
}
