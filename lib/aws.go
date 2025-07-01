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
	"bytes"
	"context"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4_creds "github.com/aws/aws-sdk-go-v2/aws/signer/v4" // ¯\_(ツ)_/¯
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

// AWS returns a cel.EnvOption to configure extended functions for
// AWS request signing. In all cases the functions are member overloads
// on a <map<string,dyn>> which is expected to correspond to an HTTP
// request, the returned value is the request with the necessary AWS v4
// signing details added.
//
// # Sign AWS from env
//
// Returns a signed request based on credential details in the relevant
// [AWS environment variables].
//
//	<map<string,dyn>>.sign_aws_from_env(<map<string,dyn>>) -> <map<string,dyn>>
//
// Examples:
//
//	req.sign_aws_from_env({
//		"service": "service",
//		"region": "region",
//		"sign_time": now(),
//		"no_hoist": false,
//		"no_escape": false,
//		"disable_session_token": false,
//	})   // return {"Body": "…
//
// The parameter to sign_aws_from_env corresponds to the [v4_creds.Signer.SignHTTP]
// service, region and signTime parameters, while the three boolean parameters
// correspond to the [v4_creds.SignerOptions] fields; DisableHeaderHoisting,
// DisableURIPathEscaping and DisableSessionToken respectively. The map must
// have all the field listed above and no others, or the call will fail.
//
// This corresponds to using [v4_creds.Signer.SignHTTP] using the result of a call
// to [credentials.NewEnvCredentials].
//
// # Sign AWS from shared credentials
//
// Returns a signed request based on credential details in the relevant
// credentials file, which may be pointed to by the AWS_SHARED_CREDENTIALS_FILE
// and AWS_PROFILE environment variables.
//
//	<map<string,dyn>>.sign_aws_from_shared(<string>, <string>, <map<string,dyn>>) -> <map<string,dyn>>
//
// Examples:
//
//	req.sign_aws_from_shared("filepath", "profile", {"service": "service", … })   // return {"Body": "…
//
// or with AWS_SHARED_CREDENTIALS_FILE set to "filepath" and AWS_PROFILE set to
// "profile":
//
//	req.sign_aws_from_shared("", "", {"service": "service", … })   // return {"Body": "…
//
// The last six parameters are as described for sign_aws_from_env.
//
// This corresponds to using [v4_creds.Signer.SignHTTP] using the result of a call
// to [credentials.NewSharedCredentials].
//
// # Sign AWS from static credentials
//
// Returns a signed request based on credential details provided statically.
//
//	<map<string,dyn>>.sign_aws_from_static(<string>, <string>, <string>, <map<string,dyn>>) -> <map<string,dyn>>
//	<map<string,dyn>>.sign_aws_from_static(<string>, <string>, <string>, <string>, <map<string,dyn>>) -> <map<string,dyn>>
//
// Examples:
//
//	req.sign_aws_from_static("id", "secret", "token", {"service": "service", … })   // return {"Body": "…
//	req.sign_aws_from_static("id", "secret", "token", "source", {"service": "service", … })   // return {"Body": "…
//
// The last six parameters are as described for sign_aws_from_env.
//
// This corresponds to using [v4_creds.Signer.SignHTTP] using the result of a call
// to [credentials.NewStaticCredentials] (nine parameter case) or
// [credentials.NewStaticCredentialsFromCreds] (ten parameter case).
//
// [AWS environment variables]: https://docs.aws.amazon.com/sdkref/latest/guide/settings-reference.html#EVarSettings
func AWS() cel.EnvOption {
	return cel.Lib(awsLib{})
}

type awsLib struct{}

func (awsLib) CompileOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("sign_aws_from_env",
			cel.MemberOverload(
				"map_sign_aws_from_env_map",
				[]*cel.Type{
					mapKV,
					mapStringDyn,
				},
				mapKV,
				cel.FunctionBinding(catch(signFromEnv)),
			),
		),
		cel.Function("sign_aws_from_shared",
			cel.MemberOverload(
				"map_sign_aws_from_shared_map",
				[]*cel.Type{
					mapKV,
					cel.StringType, cel.StringType,
					mapStringDyn,
				},
				mapKV,
				cel.FunctionBinding(catch(signFromShared)),
			),
		),
		cel.Function("sign_aws_from_static",
			cel.MemberOverload(
				"map_sign_aws_from_static_3_map",
				[]*cel.Type{
					mapKV,
					cel.StringType, cel.StringType, cel.StringType,
					mapStringDyn,
				},
				mapKV,
				cel.FunctionBinding(catch(signFromCred)),
			),
			cel.MemberOverload(
				"map_sign_aws_from_static_4_map",
				[]*cel.Type{
					mapKV,
					cel.StringType, cel.StringType, cel.StringType, cel.StringType,
					mapStringDyn,
				},
				mapKV,
				cel.FunctionBinding(catch(signFromCred)),
			),
		),
	}
}

func (awsLib) ProgramOptions() []cel.ProgramOption { return nil }

func signFromEnv(args ...ref.Val) ref.Val {
	const name = "sign_aws_from_env"
	if len(args) != 2 {
		return types.NewErr("no such overload for " + name)
	}
	request, ok := args[0].(traits.Mapper)
	if !ok {
		return types.ValOrErr(request, "no such overload for "+name)
	}
	creds, err := config.NewEnvConfig()
	if err != nil {
		return types.NewErr(err.Error())
	}
	cargs, ok := args[1].(traits.Mapper)
	if !ok {
		return types.NewErr("no such overload for sign_aws_from_creds")
	}
	return sign(request, creds.Credentials, name, cargs)
}

func signFromShared(args ...ref.Val) ref.Val {
	const name = "sign_aws_from_shared"
	if len(args) != 4 {
		return types.NewErr("no such overload for " + name)
	}
	request, ok := args[0].(traits.Mapper)
	if !ok {
		return types.ValOrErr(request, "no such overload for "+name)
	}
	file, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(args[1], "no such overload for "+name)
	}
	profile, ok := args[2].(types.String)
	if !ok {
		return types.ValOrErr(args[2], "no such overload for "+name)
	}

	f := string(file)
	if f == "" {
		f = os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	}
	if f == "" {
		return types.NewErrFromString("neither file nor env var AWS_SHARED_CREDENTIALS_FILE is set")
	}

	p := string(profile)
	if p == "" {
		p = os.Getenv("AWS_PROFILE")
	}
	if p == "" {
		return types.NewErrFromString("neither profile nor env var AWS_PROFILE is set")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithSharedCredentialsFiles([]string{f}),
		config.WithSharedConfigProfile(p),
	)
	if err != nil {
		return types.NewErr(err.Error())
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return types.NewErr(err.Error())
	}

	cargs, ok := args[3].(traits.Mapper)
	if !ok {
		return types.NewErr("no such overload for sign_aws_from_creds")
	}
	return sign(request, creds, name, cargs)
}

func signFromCred(args ...ref.Val) ref.Val {
	const name = "sign_aws_from_creds"
	if len(args) != 5 && len(args) != 6 {
		return types.NewErr("no such overload for " + name)
	}
	request, ok := args[0].(traits.Mapper)
	if !ok {
		return types.ValOrErr(request, "no such overload for "+name)
	}
	id, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(args[1], "no such overload for "+name)
	}
	secret, ok := args[2].(types.String)
	if !ok {
		return types.ValOrErr(args[2], "no such overload for "+name)
	}
	token, ok := args[3].(types.String)
	if !ok {
		return types.ValOrErr(args[3], "no such overload for "+name)
	}
	creds := aws.Credentials{
		AccessKeyID:     string(id),
		SecretAccessKey: string(secret),
		SessionToken:    string(token),
	}
	rest := args[4]
	if len(args) == 6 {
		source, ok := args[4].(types.String)
		if !ok {
			return types.ValOrErr(args[4], "no such overload for sign_aws_from_creds")
		}
		creds.Source = string(source)
		rest = args[5]
	}
	cargs, ok := rest.(traits.Mapper)
	if !ok {
		return types.NewErr("no such overload for sign_aws_from_creds")
	}
	return sign(request, creds, name, cargs)
}

func sign(request ref.Val, creds aws.Credentials, name string, args traits.Mapper) ref.Val {
	if args.Size() != types.Int(6) {
		panic("unexpected number of signing args")
	}
	service, ok := args.Get(types.String("service")).(types.String)
	if !ok {
		return types.ValOrErr(service, "no such overload for "+name)
	}
	region, ok := args.Get(types.String("region")).(types.String)
	if !ok {
		return types.ValOrErr(region, "no such overload for "+name)
	}
	signed, ok := args.Get(types.String("sign_time")).(types.Timestamp)
	if !ok {
		return types.ValOrErr(signed, "no such overload for "+name)
	}
	noHoist, ok := args.Get(types.String("no_hoist")).(types.Bool)
	if !ok {
		return types.ValOrErr(noHoist, "no such overload for "+name)
	}
	noEscape, ok := args.Get(types.String("no_escape")).(types.Bool)
	if !ok {
		return types.ValOrErr(noEscape, "no such overload for "+name)
	}
	disableSessionToken, ok := args.Get(types.String("disable_session_token")).(types.Bool)
	if !ok {
		return types.ValOrErr(disableSessionToken, "no such overload for "+name)
	}
	reqm, err := request.ConvertToNative(reflectMapStringAnyType)
	if err != nil {
		return types.NewErr("%s", err)
	}
	req, err := mapToReq(reqm.(map[string]interface{}))
	if err != nil {
		return types.NewErr("%s", err)
	}

	var (
		body io.ReadSeeker
		buf  bytes.Buffer
	)
	if req.Body != nil {
		io.Copy(&buf, req.Body)
		req.Body.Close()
		body = bytes.NewReader(buf.Bytes())
	}
	v4_creds.NewSigner(func(o *v4_creds.SignerOptions) {
		o.DisableHeaderHoisting = bool(noHoist)
		o.DisableURIPathEscaping = bool(noEscape)
		o.DisableSessionToken = bool(disableSessionToken)
	}).SignHTTP(context.Background(), creds, req, "", string(service), string(region), signed.Time)
	if err != nil {
		return types.NewErr("%s", err)
	}
	var reqBody ref.Val
	if body != nil {
		reqBody = types.Bytes(buf.Bytes())
	}
	reqm, err = reqToMap(req, types.String(req.URL.String()), reqBody, 0)
	if err != nil {
		return types.NewErr("%s", err)
	}
	return types.DefaultTypeAdapter.NativeToValue(reqm)
}
