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
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/interpreter"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/oauth2/endpoints"
	"golang.org/x/oauth2/google"
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
	insecure := flag.Bool("insecure", false, "disable TLS verification in the HTTP client")
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
		if len(cfg.XSDs) != 0 {
			xsds := make(map[string]string)
			for name, path := range cfg.XSDs {
				b, err := os.ReadFile(path)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
				xsds[name] = string(b)
			}
			xml, err := lib.XML(nil, xsds)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			libs = append(libs, xml)
		}
		if cfg.Auth != nil {
			switch auth := cfg.Auth; {
			case auth.Basic != nil && auth.OAuth2 != nil:
				fmt.Fprintln(os.Stderr, "configured basic authentication and OAuth2")
				return 2
			case auth.Basic != nil:
				libMap["http"] = lib.HTTP(setClientInsecure(nil, *insecure), nil, auth.Basic)
			case auth.OAuth2 != nil:
				client, err := oAuth2Client(*auth.OAuth2)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
				libMap["http"] = lib.HTTP(setClientInsecure(client, *insecure), nil, nil)
			}
		}
	}
	if libMap["http"] == nil {
		libMap["http"] = lib.HTTP(setClientInsecure(nil, *insecure), nil, nil)
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

// setClientInsecure returns an http.Client that will skip TLS certificate
// verification when insecure is true. If c is nil and insecure is true
// http.DefaultClient and http.DefaultTransport are used and will be mutated.
func setClientInsecure(c *http.Client, insecure bool) *http.Client {
	if !insecure {
		return c
	}
	if c == nil {
		c = http.DefaultClient
	}
	if c.Transport == nil {
		c.Transport = http.DefaultTransport
	}
	t, ok := c.Transport.(*http.Transport)
	if !ok {
		return c
	}
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecure}
	c.Transport = t
	return c
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
		"http":        nil, // This will be populated by Main.
		"limit":       lib.Limit(limitPolicies),
		"strings":     lib.Strings(),
	}

	mimetypes = map[string]interface{}{
		"text/rot13":               func(r io.Reader) io.Reader { return rot13{r} },
		"text/upper":               toUpper,
		"application/gzip":         func(r io.Reader) (io.Reader, error) { return gzip.NewReader(r) },
		"text/csv; header=present": lib.CSVHeader,
		"text/csv; header=absent":  lib.CSVNoHeader,
		"application/x-ndjson":     lib.NDJSON,
		"application/zip":          lib.Zip,
	}

	limitPolicies = map[string]lib.LimitPolicy{
		"okta":  lib.OktaRateLimit,
		"draft": lib.DraftRateLimit,
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
	XSDs    map[string]string      `yaml:"xsd"`
	Auth    *authConfig            `yaml:"auth"`
}

type authConfig struct {
	Basic  *lib.BasicAuth `yaml:"basic"`
	OAuth2 *oAuth2        `yaml:"oauth2"`
}

type oAuth2 struct {
	Provider string `yaml:"provider"`

	ClientID       string     `yaml:"client.id"`
	ClientSecret   *string    `yaml:"client.secret"`
	EndpointParams url.Values `yaml:"endpoint_params"`
	Password       string     `yaml:"password"`
	Scopes         []string   `yaml:"scopes"`
	TokenURL       string     `yaml:"token_url"`
	User           string     `yaml:"user"`

	GoogleCredentialsFile  string `yaml:"google.credentials_file"`
	GoogleCredentialsJSON  string `yaml:"google.credentials_json"`
	GoogleJWTFile          string `yaml:"google.jwt_file"`
	GoogleJWTJSON          string `yaml:"google.jwt_json"`
	GoogleDelegatedAccount string `yaml:"google.delegated_account"`

	AzureTenantID string `yaml:"azure.tenant_id"`
	AzureResource string `yaml:"azure.resource"`
}

func oAuth2Client(cfg oAuth2) (*http.Client, error) {
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{})

	switch prov := strings.ToLower(cfg.Provider); prov {
	case "":
		if cfg.User != "" || cfg.Password != "" {
			var clientSecret string
			if cfg.ClientSecret != nil {
				clientSecret = *cfg.ClientSecret
			}
			oauth2cfg := &oauth2.Config{
				ClientID:     cfg.ClientID,
				ClientSecret: clientSecret,
				Endpoint: oauth2.Endpoint{
					TokenURL:  cfg.TokenURL,
					AuthStyle: oauth2.AuthStyleAutoDetect,
				},
			}
			token, err := oauth2cfg.PasswordCredentialsToken(ctx, cfg.User, cfg.Password)
			if err != nil {
				return nil, fmt.Errorf("oauth2: error loading credentials using user and password: %w", err)
			}
			return oauth2cfg.Client(ctx, token), nil
		}

		fallthrough
	case "azure":
		var token string
		if prov == "azure" {
			if cfg.TokenURL == "" {
				token = endpoints.AzureAD(cfg.AzureTenantID).TokenURL
			}
			if cfg.AzureResource != "" {
				if cfg.EndpointParams == nil {
					cfg.EndpointParams = make(url.Values)
				}
				cfg.EndpointParams.Set("resource", cfg.AzureResource)
			}
		}
		var clientSecret string
		if cfg.ClientSecret != nil {
			clientSecret = *cfg.ClientSecret
		}
		return (&clientcredentials.Config{
			ClientID:       cfg.ClientID,
			ClientSecret:   clientSecret,
			TokenURL:       token,
			Scopes:         cfg.Scopes,
			EndpointParams: cfg.EndpointParams,
		}).Client(ctx), nil

	case "google":
		creds, err := google.FindDefaultCredentials(ctx, cfg.Scopes...)
		if err == nil {
			return nil, fmt.Errorf("oauth2: error loading default credentials: %w", err)
		}
		cfg.GoogleCredentialsJSON = string(creds.JSON)

		if cfg.GoogleJWTFile != "" {
			b, err := os.ReadFile(cfg.GoogleJWTFile)
			if err != nil {
				return nil, err
			}
			cfg.GoogleJWTJSON = string(b)
		}
		if cfg.GoogleJWTJSON != "" {
			if !json.Valid([]byte(cfg.GoogleJWTJSON)) {
				return nil, fmt.Errorf("invalid google jwt: %s", cfg.GoogleJWTJSON)
			}
			googCfg, err := google.JWTConfigFromJSON([]byte(cfg.GoogleJWTJSON), cfg.Scopes...)
			if err != nil {
				return nil, fmt.Errorf("oauth2: error loading jwt credentials: %w", err)
			}
			googCfg.Subject = cfg.GoogleDelegatedAccount
			return googCfg.Client(ctx), nil
		}

		creds, err = google.CredentialsFromJSON(ctx, []byte(cfg.GoogleCredentialsJSON), cfg.Scopes...)
		if err != nil {
			return nil, fmt.Errorf("oauth2: error loading credentials: %w", err)
		}
		return oauth2.NewClient(ctx, creds.TokenSource), nil
	default:
		return nil, errors.New("oauth2: unknown provider")
	}
}
