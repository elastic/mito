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
	"io"

	"github.com/aws/aws-sdk-go/aws/credentials"
	v4_creds "github.com/aws/aws-sdk-go/aws/signer/v4" // ¯\_(ツ)_/¯
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
// AWS environment variables; AWS_ACCESS_KEY, AWS_ACCESS_KEY_ID,
// AWS_SECRET_ACCESS_KEY and AWS_SECRET_KEY.
//
//	<map<string,dyn>>.sign_aws_from_env(<string>, <string>, <timestamp>, <bool>, <bool>, <bool>) -> <map<string,dyn>>
//
// Examples:
//
//	req.sign_aws_from_env("service", "region", now(), false, false, false)   // return {"Body": "…
//
// The parameters to sign_aws_from_env correspond to the [v4_creds.Signer.Sign]
// service, region and signTime parameters, while the three boolean parameters
// correspond to the [v4_creds.Signer] fields; DisableHeaderHoisting,
// DisableURIPathEscaping and UnsignedPayload respectively.
//
// This corresponds to using [v4_creds.Signer.Sign] using the result of a call
// to [credentials.NewEnvCredentials].
//
// # Sign AWS from shared credentials
//
// Returns a signed request based on credential details in the relevant
// credentials file, which may be pointed to by the AWS_SHARED_CREDENTIALS_FILE
// and AWS_PROFILE environment variables.
//
//	<map<string,dyn>>.sign_aws_from_shared(<string>, <string>, <string>, <string>, <timestamp>, <bool>, <bool>, <bool>) -> <map<string,dyn>>
//
// Examples:
//
//	req.sign_aws_from_shared("filepath", "profile", "service", "region", now(), false, false, false)   // return {"Body": "…
//
// or with AWS_SHARED_CREDENTIALS_FILE set to "filepath" and AWS_PROFILE set to
// "profile":
//
//	req.sign_aws_from_shared("", "", "service", "region", now(), false, false, false)   // return {"Body": "…
//
// The last six parameters are as described for sign_aws_from_env.
//
// This corresponds to using [v4_creds.Signer.Sign] using the result of a call
// to [credentials.NewSharedCredentials].
//
// # Sign AWS from static credentials
//
// Returns a signed request based on credential details provided statically.
//
//	<map<string,dyn>>.sign_aws_from_static(<string>, <string>, <string>, <string>, <string>, <timestamp>, <bool>, <bool>, <bool>) -> <map<string,dyn>>
//	<map<string,dyn>>.sign_aws_from_static(<string>, <string>, <string>, <string>, <string>, <string>, <timestamp>, <bool>, <bool>, <bool>) -> <map<string,dyn>>
//
// Examples:
//
//	req.sign_aws_from_static("id", "secret", "token", "service", "region", now(), false, false, false)   // return {"Body": "…
//	req.sign_aws_from_static("id", "secret", "token", "provider", "service", "region", now(), false, false, false)   // return {"Body": "…
//
// The last six parameters are as described for sign_aws_from_env.
//
// This corresponds to using [v4_creds.Signer.Sign] using the result of a call
// to [credentials.NewStaticCredentials] (nine parameter case) or
// [credentials.NewStaticCredentialsFromCreds] (ten parameter case).
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
					cel.StringType, cel.StringType, cel.TimestampType, cel.BoolType, cel.BoolType, cel.BoolType,
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
					cel.StringType, cel.StringType, cel.TimestampType, cel.BoolType, cel.BoolType, cel.BoolType,
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
					cel.StringType, cel.StringType, cel.TimestampType, cel.BoolType, cel.BoolType, cel.BoolType,
				},
				mapKV,
				cel.FunctionBinding(catch(signFromCred)),
			),
			cel.MemberOverload(
				"map_sign_aws_from_static_4_map",
				[]*cel.Type{
					mapKV,
					cel.StringType, cel.StringType, cel.StringType, cel.StringType,
					cel.StringType, cel.StringType, cel.TimestampType, cel.BoolType, cel.BoolType, cel.BoolType,
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
	if len(args) != 7 {
		return types.NewErr("no such overload for " + name)
	}
	request, ok := args[0].(traits.Mapper)
	if !ok {
		return types.ValOrErr(request, "no such overload for "+name)
	}
	creds := credentials.NewEnvCredentials()

	return sign(request, creds, name, args[1:7]...)
}

func signFromShared(args ...ref.Val) ref.Val {
	const name = "sign_aws_from_shared"
	if len(args) != 9 {
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
	creds := credentials.NewSharedCredentials(string(file), string(profile))

	return sign(request, creds, name, args[3:9]...)
}

func signFromCred(args ...ref.Val) ref.Val {
	const name = "sign_aws_from_creds"
	if len(args) != 10 && len(args) != 11 {
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
	if len(args) == 10 {
		creds := credentials.NewStaticCredentials(string(id), string(secret), string(token))
		return sign(request, creds, name, args[4:10]...)
	}
	provider, ok := args[4].(types.String)
	if !ok {
		return types.ValOrErr(args[2], "no such overload for sign_aws_from_creds")
	}
	creds := credentials.NewStaticCredentialsFromCreds(credentials.Value{
		AccessKeyID:     string(id),
		SecretAccessKey: string(secret),
		SessionToken:    string(token),
		ProviderName:    string(provider),
	})
	return sign(request, creds, name, args[5:11]...)
}

func sign(request ref.Val, creds *credentials.Credentials, name string, args ...ref.Val) ref.Val {
	if len(args) != 6 {
		panic("unexpected number of signing args")
	}
	service, ok := args[0].(types.String)
	if !ok {
		return types.ValOrErr(args[0], "no such overload for "+name)
	}
	region, ok := args[1].(types.String)
	if !ok {
		return types.ValOrErr(args[1], "no such overload for "+name)
	}
	signed, ok := args[2].(types.Timestamp)
	if !ok {
		return types.ValOrErr(args[2], "no such overload for "+name)
	}
	noHoist, ok := args[3].(types.Bool)
	if !ok {
		return types.ValOrErr(args[3], "no such overload for "+name)
	}
	noEscape, ok := args[4].(types.Bool)
	if !ok {
		return types.ValOrErr(args[4], "no such overload for "+name)
	}
	unsignedPayload, ok := args[5].(types.Bool)
	if !ok {
		return types.ValOrErr(args[5], "no such overload for "+name)
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
	_, err = v4_creds.Signer{
		Credentials:            creds,
		DisableHeaderHoisting:  bool(noHoist),
		DisableURIPathEscaping: bool(noEscape),
		UnsignedPayload:        bool(unsignedPayload),
	}.Sign(req, body, string(service), string(region), signed.Time)
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
