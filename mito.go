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
// The majority of the logic resides in the the lib package.
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
	runtimedebug "runtime/debug"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/ext"
	"github.com/google/cel-go/interpreter"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/oauth2/endpoints"
	"golang.org/x/oauth2/google"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/elastic/mito/internal/fb"
	"github.com/elastic/mito/internal/httplog"
	"github.com/elastic/mito/internal/rc"
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
	maxExecutions := flag.Int("max_executions", -1, "maximum number of evaluations, or no maximum if -1")
	cfgPath := flag.String("cfg", "", "path to a YAML file holding run control configuration (see pkg.go.dev/github.com/elastic/mito/cmd/mito)")
	insecure := flag.Bool("insecure", false, "disable TLS verification in the HTTP client")
	logTrace := flag.Bool("log_requests", false, "log request traces to stderr (go1.21+)")
	maxTraceBody := flag.Int("max_log_body", 1000, "maximum length of body logged in request traces (go1.21+)")
	maxConcurrency := flag.Int("max_concurrency", 3, "maximum concurrency in parallel macro")
	globalConcurrency := flag.Int("global_concurrency", 0, "global concurrency limit across all parallel calls (0 = no limit)")
	fold := flag.Bool("fold", false, "apply constant folding optimisation")
	dumpState := flag.String("dump", "", "dump eval state ('always' or 'error')")
	coverage := flag.String("coverage", "", "file to write an execution coverage report to (prefix if multiple executions are run)")
	fbMode := flag.Bool("fb", false, "validate configuration against filebeat CEL input constraints (not covered by semver compatibility guarantees; validation rules track upstream filebeat changes)")
	version := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *version {
		return printVersion()
	}
	if len(flag.Args()) != 1 {
		flag.Usage()
		return 2
	}

	libs := []cel.EnvOption{
		cel.OptionalTypes(cel.OptionalTypesVersion(lib.OptionalTypesVersion)),
		ext.TwoVarComprehensions(ext.TwoVarComprehensionsVersion(lib.OptionalTypesVersion)),
	}
	ctx := context.Background()
	limit := rate.NewLimiter(1, 1)
	var secretState map[string]any
	if *cfgPath != "" {
		f, err := os.Open(*cfgPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		defer f.Close()

		var cfg Config
		if *fbMode {
			dec := yaml.NewDecoder(f)
			var fbCfg fb.Config
			err = dec.Decode(&fbCfg)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			if err := fbCfg.Validate(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			cfg = fbCfg.RC()
			secretState = fbCfg.SecretState
		} else {
			dec := yaml.NewDecoder(f)
			err = dec.Decode(&cfg)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
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
			libMap["xml"] = xml
		}
		var client *http.Client
		httpOptions := lib.HTTPOptions{
			Limiter:     limit,
			Headers:     cfg.HTTPHeaders,
			MaxBodySize: cfg.MaxBodySize,
		}
		if cfg.Auth != nil {
			switch auth := cfg.Auth; {
			case authsCount(auth) > 1:
				fmt.Fprintln(os.Stderr, "configured more than one authentication method")
				return 2
			case auth.Basic != nil:
				httpOptions.BasicAuth = auth.Basic
			case auth.Token != nil:
				httpOptions.TokenAuth = auth.Token
			case auth.OAuth2 != nil:
				client, err = oAuth2Client(*auth.OAuth2)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
			}
		}
		if client != nil || !httpOptions.IsZero() {
			libMap["http"] = lib.HTTPWithContextOpts(ctx, traceReqs(setClientInsecure(client, *insecure), *logTrace, *maxTraceBody), httpOptions)
		}
		if *maxExecutions == -1 && cfg.MaxExecutions != nil {
			*maxExecutions = *cfg.MaxExecutions
		}

	}
	if libMap["http"] == nil {
		libMap["http"] = lib.HTTPWithContextOpts(ctx, traceReqs(setClientInsecure(nil, *insecure), *logTrace, *maxTraceBody), lib.HTTPOptions{Limiter: limit})
	}
	libMap["limit"] = lib.LimitWithApply(limitPolicies, func(m map[string]any, h http.Header) map[string]any {
		handleRateLimit(m, h, limit)
		return m
	})
	if libMap["xml"] == nil {
		var err error
		libMap["xml"], err = lib.XML(nil, nil)
		if err != nil {
			return 2
		}
	}
	libMap["emit"] = lib.Emit(func() lib.Emitter { return stderrEmitter{} })
	libMap["parallel"] = lib.Parallel(*maxConcurrency, *globalConcurrency)
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
			fmt.Fprintf(os.Stderr, "failed reading JSON data from %q: %s", *data, err)
			return 2
		}
		err = json.Unmarshal(b, &input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed parsing JSON data from %q: %s", *data, err)
			return 2
		}
		if *fbMode {
			if state, ok := input.(map[string]any); ok {
				if err := fb.CheckState(state); err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 2
				}
				if len(secretState) != 0 {
					state["secret"] = secretState
					input = state
				}
			}
		}
		if *maxExecutions > 0 {
			// Only provide remaining_executions if we have set a limit.
			input = map[string]interface{}{
				root:                   input,
				"remaining_executions": *maxExecutions - 1,
			}
		} else {
			input = map[string]interface{}{root: input}
		}
	}

	var cov lib.Coverage
	budget := *maxExecutions - 1
	for n := int(0); *maxExecutions < 0 || n < *maxExecutions; n++ {
		res, val, dump, c, err := eval(string(b), root, input, *fold, *dumpState != "", *coverage != "", libs...)
		if err := cov.Merge(c); err != nil {
			fmt.Fprintf(os.Stderr, "internal error merging coverage: %v\n", err)
			return 1
		}
		if *dumpState == "always" {
			fmt.Fprint(os.Stderr, dump)
		}
		if err != nil {
			if *dumpState == "error" {
				fmt.Fprint(os.Stderr, dump)
			}
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println(res)

		// Check if we want more. This can happen when we have a map
		// and the map has a true boolean field, want_more.
		state, ok := val.(map[string]any)
		if !ok {
			break
		}
		if more, _ := state["want_more"].(bool); !more {
			break
		}
		if budget > 0 {
			budget--
			input = map[string]any{
				"state":                val,
				"remaining_executions": budget,
			}
		} else {
			input = map[string]any{
				"state": val,
			}
		}
	}
	if *coverage != "" {
		f, err := os.Create(*coverage)
		if err != nil {
			fmt.Fprintf(os.Stderr, "internal error opening coverage file: %v\n", err)
			return 1
		}
		defer func() {
			f.Sync()
			f.Close()
		}()
		_, err = f.WriteString(cov.String() + "\n")
		if err != nil {
			fmt.Fprintf(os.Stderr, "internal error writing coverage file: %v\n", err)
			return 1
		}
	}
	return 0
}

func handleRateLimit(rateLimit map[string]interface{}, header http.Header, limiter *rate.Limiter) (waitUntil time.Time) {
	if _, ok := rateLimit["error"]; ok {
		// The error field should be a string, but we won't quibble here.
		return waitUntil
	}

	limit, ok := getLimit("rate", rateLimit)
	if !ok {
		return waitUntil
	}

	var burst int
	b := rateLimit["burst"]
	switch b := b.(type) {
	case int:
		burst = b
	case int64:
		burst = int(b)
	case float64:
		burst = int(b)
	default:
	}
	if burst < 1 {
		// Make sure we can make at least one new request, even if we fail
		// to get a non-zero rate.Limit. We could set to zero for the case
		// that limit=rate.Inf, but that detail is not important.
		burst = 1
	}

	// Process reset if we need to wait until reset to avoid a request against a zero quota.
	if limit <= 0 {
		w, ok := rateLimit["reset"]
		if ok {
			switch w := w.(type) {
			case time.Time:
				waitUntil = w
				next, ok := getLimit("next", rateLimit)
				if !ok {
					return waitUntil
				}
				limiter.SetLimitAt(waitUntil, next)
				limiter.SetBurstAt(waitUntil, burst)
			case string:
				t, err := time.Parse(time.RFC3339, w)
				if err != nil {
					return waitUntil
				}
				waitUntil = t
				next, ok := getLimit("next", rateLimit)
				if !ok {
					return waitUntil
				}
				limiter.SetLimitAt(waitUntil, next)
				limiter.SetBurstAt(waitUntil, burst)
			default:
			}
		}
		return waitUntil
	}

	limiter.SetLimit(limit)
	limiter.SetBurst(burst)
	return waitUntil
}

func getLimit(which string, rateLimit map[string]interface{}) (limit rate.Limit, ok bool) {
	r, ok := rateLimit[which]
	if !ok {
		return limit, false
	}
	switch r := r.(type) {
	case rate.Limit:
		limit = r
	case int:
		limit = rate.Limit(r)
	case int64:
		limit = rate.Limit(r)
	case float64:
		limit = rate.Limit(r)
	case string:
		if !strings.EqualFold(strings.TrimPrefix(r, "+"), "inf") && !strings.EqualFold(strings.TrimPrefix(r, "+"), "infinity") {
			return limit, false
		}
		limit = rate.Inf
	default:
	}
	return limit, true
}

func authsCount(auth *rc.AuthConfig) int {
	var n int
	if auth.Basic != nil {
		n++
	}
	if auth.Token != nil {
		n++
	}
	if auth.OAuth2 != nil {
		n++
	}
	return n
}

func printVersion() int {
	bi, ok := runtimedebug.ReadBuildInfo()
	if !ok {
		fmt.Fprintln(os.Stderr, "no build info")
		return 1
	}
	var revision, modified string
	for _, bs := range bi.Settings {
		switch bs.Key {
		case "vcs.revision":
			revision = bs.Value
		case "vcs.modified":
			modified = bs.Value
		}
	}
	if revision == "" {
		fmt.Println(bi.Main.Version)
		return 0
	}
	switch modified {
	case "true":
		fmt.Println(bi.Main.Version, revision, "(modified)")
	case "false":
		fmt.Println(bi.Main.Version, revision)
	default:
		// This should never happen.
		fmt.Println(bi.Main.Version, revision, modified)
	}
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

// traceReqs wraps c with a request trace logger that logs HTTP requests and
// their responses to stderr. If c is nil and trace is true
// http.DefaultClient and http.DefaultTransport are used and will be mutated.
func traceReqs(c *http.Client, trace bool, max int) *http.Client {
	if !trace {
		return c
	}
	if c == nil {
		c = http.DefaultClient
	}
	if c.Transport == nil {
		c.Transport = http.DefaultTransport
	}
	c.Transport = httplog.NewLoggingRoundTripper(c.Transport, max)
	return c
}

var (
	libMap = map[string]cel.EnvOption{
		"aws":         lib.AWS(),
		"collections": lib.Collections(),
		"crypto":      lib.Crypto(),
		"json":        lib.JSON(nil),
		"time":        lib.Time(),
		"try":         lib.Try(),
		"debug":       lib.Debug(debug),
		"file":        lib.File(mimetypes),
		"mime":        lib.MIME(mimetypes),
		"http":        nil, // This will be populated by Main.
		"limit":       nil, // This will be populated by Main.
		"strings":     lib.Strings(),
		"printf":      lib.Printf(),
		"xml":         nil, // This will be populated by Main.
		"stream":      lib.Stream(),
		"csv":         lib.CSV(),
		"lines":       lib.Lines(),
		"parallel":    nil, // This will be populated by Main.
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

func debug(tag string, value any) {
	level := "DEBUG"
	if _, ok := value.(error); ok {
		level = "ERROR"
	}
	fmt.Fprintf(os.Stderr, "%s: logging %q: %v\n", level, tag, value)
}

func eval(src, root string, input interface{}, fold, details, coverage bool, libs ...cel.EnvOption) (string, any, *lib.Dump, *lib.Coverage, error) {
	prg, ast, cov, err := compile(src, root, fold, details, coverage, libs...)
	if err != nil {
		return "", nil, nil, nil, fmt.Errorf("failed program instantiation: %v", err)
	}
	res, val, det, err := run(prg, ast, false, input)
	var dump *lib.Dump
	if details {
		dump = lib.NewDump(ast, det)
	}
	return res, val, dump, cov, err
}

func compile(src, root string, fold, details, coverage bool, libs ...cel.EnvOption) (cel.Program, *cel.Ast, *lib.Coverage, error) {
	opts := append([]cel.EnvOption{
		cel.VariableDecls(
			decls.NewVariable(root, types.DynType),
			decls.NewVariable("remaining_executions", types.IntType),
		),
	}, libs...)
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create env: %v", err)
	}

	ast, iss := env.Compile(src)
	if iss.Err() != nil {
		return nil, nil, nil, fmt.Errorf("failed compilation: %v", iss.Err())
	}

	if fold {
		folder, err := cel.NewConstantFoldingOptimizer()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed folding optimization: %v", err)
		}
		opt, err := cel.NewStaticOptimizer(folder)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to make new static optimizer: %v", err)
		}
		ast, iss = opt.Optimize(env, ast)
		if iss.Err() != nil {
			return nil, nil, nil, fmt.Errorf("failed optimization: %v", iss.Err())
		}
	}

	var cov *lib.Coverage
	var progOpts []cel.ProgramOption
	if coverage {
		cov = lib.NewCoverage(ast)
		progOpts = []cel.ProgramOption{cov.ProgramOption()}
	}
	if details {
		progOpts = append(progOpts, cel.EvalOptions(cel.OptTrackState))
	}
	prg, err := env.Program(ast, progOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed program instantiation: %v", err)
	}
	return prg, ast, cov, nil
}

func run(prg cel.Program, ast *cel.Ast, fast bool, input interface{}) (string, any, *cel.EvalDetails, error) {
	if input == nil {
		input = interpreter.EmptyActivation()
	}
	out, det, err := prg.Eval(input)
	if err != nil {
		return "", nil, det, fmt.Errorf("failed eval: %v", lib.DecoratedError{AST: ast, Err: err})
	}

	v, err := out.ConvertToNative(reflect.TypeOf(&structpb.Value{}))
	if err != nil {
		return "", nil, det, fmt.Errorf("failed proto conversion: %v", err)
	}
	val := v.(*structpb.Value).AsInterface()
	if fast {
		b, err := protojson.MarshalOptions{}.Marshal(v.(proto.Message))
		if err != nil {
			return "", nil, det, fmt.Errorf("failed native conversion: %v", err)
		}
		return string(b), val, det, nil
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "\t")
	err = enc.Encode(val)
	return strings.TrimRight(buf.String(), "\n"), val, det, err
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

type (
	Config     = rc.Config
	AuthConfig = rc.AuthConfig
	OAuth2     = rc.OAuth2Config
)

func oAuth2Client(cfg OAuth2) (*http.Client, error) {
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
		token := cfg.TokenURL
		if prov == "azure" {
			if token == "" {
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

// stderrEmitter prints emitted events to stderr for the mito CLI.
type stderrEmitter struct{}

func (stderrEmitter) Emit(value, cursor any) error {
	if cursor != nil {
		fmt.Fprintf(os.Stderr, "EMIT: %v (cursor: %v)\n", value, cursor)
	} else {
		fmt.Fprintf(os.Stderr, "EMIT: %v\n", value)
	}
	return nil
}
