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
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter/functions"
	"golang.org/x/time/rate"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// Limit returns a cel.EnvOption to configure extended functions for interpreting
// request rate limit policies.
//
// It takes a mapping of policy names to policy interpreters to allow implementing
// specific rate limit policies. The map returned by the policy functions should
// have a "rate" field with type rate.Limit or string with the value "inf", and a
// "burst" field with type int. The semantics of "rate" and "burst" are described
// in the documentation for the golang.org/x/time/rate package.
//
// The map may have other fields that can be logged. If a field named "error"
// exists it should be a string with an error message indicating the result can
// not be used.
//
// Rate Limit
//
// rate_limit returns <map<string,dyn>> interpreted through the registered rate
// limit policy or with a generalised policy constructor:
//
//     rate_limit(<map<string,dyn>>, <string>) -> <map<string,dyn>>
//     rate_limit(<map<string,dyn>>, <string>, <bool>, <bool>, <duration>, <int>) -> <map<string,dyn>>
//
// In the second form the parameters are the header, the prefix for the rate limit
// header keys, whether the keys are canonically formatted MIME header keys,
// whether the reset header is a delta as opposed to a timestamp, the duration
// of the quota window, and the burst rate. rate_limit in the second form will
// never set a burst rate to zero.
//
// In all cases if any of the three rate limit headers is missing the rate_limit
// call returns a map with only the headers written. This should be considered an
// error condition.
//
// Examples:
//
//     rate_limit(h, 'okta')
//     rate_limit(h, 'draft')
//
//     // Similar semantics to the octa policy.
//     rate_limit(h, 'X-Rate-Limit', true, false, duration('1s'), 1)
//
//     // Similar semantics to the draft policy in the simplest case.
//     rate_limit(h, 'Rate-Limit', true, true, duration('1s'), 1)
//
//     // Non-canonical keys.
//     rate_limit(h, 'X-RateLimit', false, false, duration('1s'), 1)
//
func Limit(policy map[string]LimitPolicy) cel.EnvOption {
	return cel.Lib(limitLib{policies: policy})
}

type LimitPolicy func(header http.Header) map[string]interface{}

type limitLib struct {
	policies map[string]LimitPolicy
}

func (limitLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Declarations(
			decls.NewFunction("rate_limit",
				decls.NewOverload(
					"map_dyn_rate_limit_string",
					[]*expr.Type{decls.NewMapType(decls.String, decls.Dyn), decls.String},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
			decls.NewFunction("rate_limit",
				decls.NewOverload(
					"map_dyn_rate_limit_string_bool_bool_duration_int",
					[]*expr.Type{decls.NewMapType(decls.String, decls.Dyn), decls.String, decls.Bool, decls.Bool, decls.Duration, decls.Int},
					decls.NewMapType(decls.String, decls.Dyn),
				),
			),
		),
	}
}

func (l limitLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.Functions(
			&functions.Overload{
				Operator: "map_dyn_rate_limit_string",
				Binary:   l.translatePolicy,
			},
			&functions.Overload{
				Operator: "map_dyn_rate_limit_string_bool_bool_duration_int",
				Function: translatePolicy,
			},
		),
	}
}

func (l limitLib) translatePolicy(arg0, arg1 ref.Val) ref.Val {
	headers, ok := arg0.(traits.Mapper)
	if !ok {
		return types.ValOrErr(headers, "no such overload for headers: %s", arg0.Type())
	}
	policy, ok := arg1.(types.String)
	if !ok {
		return types.ValOrErr(policy, "no such overload for policy: %s", arg1.Type())
	}
	translate, ok := l.policies[string(policy)]
	if !ok {
		return types.NewErr("unknown policy: %q", policy)
	}
	if translate == nil {
		return types.NewErr("policy is nil: %q", policy)
	}
	h, err := mapStrings(headers)
	if err != nil {
		return types.NewErr("%s", err)
	}
	return types.DefaultTypeAdapter.NativeToValue(translate(h))
}

func mapStrings(val ref.Val) (map[string][]string, error) {
	iface := val.Value()
	switch iface := iface.(type) {
	case http.Header:
		return iface, nil
	case url.Values:
		return iface, nil
	case map[string][]string:
		return iface, nil
	case map[ref.Val]ref.Val:
		val := types.DefaultTypeAdapter.NativeToValue(iface)
		v, err := val.ConvertToNative(reflectMapStringStringSliceType)
		if err != nil {
			return nil, err
		}
		return v.(map[string][]string), nil
	case ref.Val:
		v, err := iface.ConvertToNative(reflectMapStringStringSliceType)
		if err != nil {
			return nil, err
		}
		return v.(map[string][]string), nil
	default:
		return nil, fmt.Errorf("invalid type: %T", iface)
	}
}

// OktaRateLimit implements the Okta rate limit policy translation.
// It should be handed to the Limit lib with
//
//  Limit(map[string]lib.LimitPolicy{
//  	"octa": lib.OktaRateLimit,
//  })
//
// It will then be able to be used in a limit call.
//
// Example:
//
//     rate_limit(h, 'octa')
//
//     might return:
//
//     {
//         "burst": 1,
//         "headers": "X-Rate-Limit-Limit=\"600\" X-Rate-Limit-Remaining=\"598\" X-Rate-Limit-Reset=\"1650094960\"",
//         "rate": 0.9975873271836141
//     },
//
// See https://developer.okta.com/docs/reference/rl-best-practices/
func OktaRateLimit(h http.Header) map[string]interface{} {
	limit := h.Get("X-Rate-Limit-Limit")
	remaining := h.Get("X-Rate-Limit-Remaining")
	reset := h.Get("X-Rate-Limit-Reset")
	if limit == "" || remaining == "" || reset == "" {
		return map[string]interface{}{
			"headers": fmt.Sprintf("X-Rate-Limit-Limit=%q X-Rate-Limit-Remaining=%q X-Rate-Limit-Reset=%q",
				limit, remaining, reset),
		}
	}
	rem, err := strconv.ParseFloat(remaining, 64)
	if err != nil {
		return map[string]interface{}{
			"headers": fmt.Sprintf("X-Rate-Limit-Limit=%q X-Rate-Limit-Remaining=%q X-Rate-Limit-Reset=%q",
				limit, remaining, reset),
			"error": err.Error(),
		}
	}
	rst, err := strconv.ParseInt(reset, 10, 64)
	if err != nil {
		return map[string]interface{}{
			"headers": fmt.Sprintf("X-Rate-Limit-Limit=%q X-Rate-Limit-Remaining=%q X-Rate-Limit-Reset=%q",
				limit, remaining, reset),
			"error": err.Error(),
		}
	}
	per := time.Until(time.Unix(rst, 0)).Seconds()
	return map[string]interface{}{
		"headers": fmt.Sprintf("X-Rate-Limit-Limit=%q X-Rate-Limit-Remaining=%q X-Rate-Limit-Reset=%q",
			limit, remaining, reset),
		"rate":  rate.Limit(rem / per),
		"burst": 1, // Be conservative here; the docs don't describe burst rates.
	}
}

// DraftRateLimit implements the draft rate limit policy translation.
// It should be handed to the Limit lib with
//
//  Limit(map[string]lib.LimitPolicy{
//  	"draft": lib.DraftRateLimit,
//  })
//
// It will then be able to be used in a limit call.
//
// Example:
//
//     rate_limit(h, 'draft')
//
//     might return something like:
//
//     {
//         "burst": 1,
//         "headers": "Rate-Limit-Limit=\"5000\" Rate-Limit-Remaining=\"100\" Rate-Limit-Reset=\"Sat, 16 Apr 2022 07:48:40 GMT\"",
//         "rate": 0.16689431007474315
//     }
//
//     or
//
//     {
//         "burst": 1000,
//         "headers": "Rate-Limit-Limit=\"12, 12;window=1; burst=1000;policy=\\\"leaky bucket\\\"\" Rate-Limit-Remaining=\"100\" Rate-Limit-Reset=\"Sat, 16 Apr 2022 07:48:40 GMT\"",
//         "rate": 100
//     }
//
// See https://tools.ietf.org/id/draft-polli-ratelimit-headers-00.html
func DraftRateLimit(h http.Header) map[string]interface{} {
	limit := h.Get("Rate-Limit-Limit")
	remaining := h.Get("Rate-Limit-Remaining")
	reset := h.Get("Rate-Limit-Reset")
	if limit == "" || remaining == "" || reset == "" {
		return map[string]interface{}{
			"headers": fmt.Sprintf("Rate-Limit-Limit=%q Rate-Limit-Remaining=%q Rate-Limit-Reset=%q",
				limit, remaining, reset),
		}
	}
	rem, err := strconv.ParseFloat(remaining, 64)
	if err != nil {
		return map[string]interface{}{
			"headers": fmt.Sprintf("Rate-Limit-Limit=%q Rate-Limit-Remaining=%q Rate-Limit-Reset=%q",
				limit, remaining, reset),
			"error": err.Error(),
		}
	}
	var per float64
	if d, err := strconv.ParseFloat(reset, 64); err == nil {
		per = d
	} else if t, err := time.Parse(http.TimeFormat, reset); err == nil {
		per = time.Until(t).Seconds()
	} else if t, err := time.Parse(time.RFC1123, reset); err == nil {
		per = time.Until(t).Seconds()
	} else {
		return map[string]interface{}{
			"headers": fmt.Sprintf("Rate-Limit-Limit=%q Rate-Limit-Remaining=%q Rate-Limit-Reset=%q",
				limit, remaining, reset),
			"error": fmt.Sprintf("could not parse %q as number or timestamp", reset),
		}
	}
	burst := 1

	// Examine quota policies.
	limFields := strings.Split(limit, ",")
	quota, err := strconv.Atoi(limFields[0])
	if err != nil {
		return map[string]interface{}{
			"headers": fmt.Sprintf("Rate-Limit-Limit=%q Rate-Limit-Remaining=%q Rate-Limit-Reset=%q",
				limit, remaining, reset),
			"error": err.Error(),
		}
	}
	for _, f := range limFields[1:] {
		p := policy(strings.TrimSpace(f))
		q, err := p.quota()
		if err != nil {
			return map[string]interface{}{
				"headers": fmt.Sprintf("Rate-Limit-Limit=%q Rate-Limit-Remaining=%q Rate-Limit-Reset=%q",
					limit, remaining, reset),
				"error": err.Error(),
			}
		}
		if q > quota {
			break
		}
		w, b, err := p.details(q)
		if err != nil {
			return map[string]interface{}{
				"headers": fmt.Sprintf("Rate-Limit-Limit=%q Rate-Limit-Remaining=%q Rate-Limit-Reset=%q",
					limit, remaining, reset),
				"error": err.Error(),
			}
		}
		if w >= 0 {
			per = float64(w)
		}
		if b >= 0 {
			burst = b
		}
	}
	return map[string]interface{}{
		"headers": fmt.Sprintf("Rate-Limit-Limit=%q Rate-Limit-Remaining=%q Rate-Limit-Reset=%q",
			limit, remaining, reset),
		"rate":  rate.Limit(rem / per),
		"burst": burst,
	}
}

type policy string

func (p policy) quota() (int, error) {
	idx := strings.Index(string(p), ";")
	if idx < 0 {
		return 0, fmt.Errorf("invalid policy: %q", p)
	}
	return strconv.Atoi(string(p[:idx]))
}

func (p policy) details(q int) (window, burst int, err error) {
	window = -1
	burst = -1
	for _, f := range strings.Split(string(p), ";") {
		f := strings.TrimSpace(f)
		switch {
		case strings.HasPrefix(f, "window="):
			window, err = strconv.Atoi(strings.TrimPrefix(f, "window="))
			if err != nil {
				return window, burst, err
			}
		case strings.HasPrefix(f, "burst="):
			burst, err = strconv.Atoi(strings.TrimPrefix(f, "burst="))
			if err != nil {
				return window, burst, err
			}
		}
	}
	return window, burst, nil
}

func translatePolicy(args ...ref.Val) ref.Val {
	if len(args) != 6 {
		return types.NewErr("no such overload")
	}
	headers, ok := args[0].(traits.Mapper)
	if !ok {
		return types.ValOrErr(headers, "no such overload for headers: %s", args[0].Type())
	}
	h, err := mapStrings(headers)
	if err != nil {
		return types.NewErr("%s", err)
	}
	prefix, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(prefix, "no such overload for prefix: %s", args[1].Type())
	}
	canonical, ok := args[2].(types.Bool)
	if !ok {
		return types.ValOrErr(canonical, "no such overload for canonical: %s", args[1].Type())
	}
	delta, ok := args[3].(types.Bool)
	if !ok {
		return types.ValOrErr(delta, "no such overload for delta: %s", args[2].Type())
	}
	window, ok := args[4].(types.Duration)
	if !ok {
		return types.ValOrErr(window, "no such overload for window: %s", args[3].Type())
	}
	burst, ok := args[5].(types.Int)
	if !ok {
		return types.ValOrErr(burst, "no such overload for burst: %s", args[4].Type())
	}
	p := limitPolicy(h, string(prefix), bool(canonical), bool(delta), window.Duration, int(burst))
	return types.DefaultTypeAdapter.NativeToValue(p)
}

func limitPolicy(h http.Header, prefix string, canonical, delta bool, window time.Duration, burst int) map[string]interface{} {
	get := getNonCanonical
	if canonical {
		get = http.Header.Get
	}
	limitKey := prefix + "-Limit"
	limit := get(h, limitKey)
	remainingKey := prefix + "-Remaining"
	remaining := get(h, remainingKey)
	resetKey := prefix + "-Reset"
	reset := get(h, resetKey)
	m := map[string]interface{}{
		"headers": fmt.Sprintf("%s=%q %s=%q %s=%q",
			limitKey, limit, remainingKey, remaining, resetKey, reset),
	}
	if limit == "" || remaining == "" || reset == "" {
		return m
	}
	rem, err := strconv.ParseFloat(remaining, 64)
	if err != nil {
		m["error"] = err.Error()
		return m
	}

	var per float64
	if d, err := strconv.ParseInt(reset, 10, 64); err == nil {
		if delta {
			per = float64(d)
		} else {
			per = time.Until(time.Unix(d, 0)).Seconds()
		}
	} else if t, err := time.Parse(http.TimeFormat, reset); err == nil {
		per = time.Until(t).Seconds()
	} else if t, err := time.Parse(time.RFC1123, reset); err == nil {
		per = time.Until(t).Seconds()
	} else {
		m["error"] = fmt.Sprintf("could not parse %q as number or timestamp", reset)
		return m
	}
	per *= window.Seconds()

	m["rate"] = rate.Limit(rem / per)
	if burst < 1 {
		burst = 1
	}
	m["burst"] = burst
	return m
}

func getNonCanonical(h http.Header, k string) string {
	if h == nil {
		return ""
	}
	v := h[k]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
