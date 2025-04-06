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

// Package rc provides run control types for the mito tool and tests.
package rc

import (
	"net/http"
	"net/url"

	"github.com/elastic/mito/lib"
)

// Config controls configuration of the mito tool run behavior.
type Config struct {
	// Globals is the set of global variables available to the CEL environment.
	Globals map[string]interface{} `yaml:"globals"`
	// Regexps is a look-up into a table of pre-compiled regular expressions.
	Regexps map[string]string `yaml:"regexp"`
	// XSDs is a look-up into a table of pre-compiled XML document descriptions.
	XSDs map[string]string `yaml:"xsd"`
	// Auth is the authentication configuration for HTTP requests.
	Auth *AuthConfig `yaml:"auth"`
	// HTTPHeaders is the set of headers to include in all HTTP requests.
	// Headers already set by Auth or CEL program evaluation are not
	// overwritten.
	HTTPHeaders http.Header `yaml:"http_headers"`
	// MaxBodySize is the largest response body that will be
	// accepted by an HTTP client. If MaxBodySize is zero there is
	// no limit. Bodies greater than the limit will result in a
	// lib.ErrBodyTooBig error being returned by the request.
	MaxBodySize int64 `yaml:"max_body_size"`
	// MaxExecutions is the maximum number of want_more executions for a single
	// run of mito. This value is overridden by the -max_executions command
	// line flag.
	MaxExecutions *int `yaml:"max_executions"`
}

// AuthConfig controls configuration of HTTP request authentication behavior.
type AuthConfig struct {
	// Basic is a Basic Authentication configuration.
	Basic *lib.BasicAuth `yaml:"basic"`
	// OAuth2 is an OAuth2.0 authentication configuration.
	OAuth2 *OAuth2Config `yaml:"oauth2"`
}

// OAuth2Config controls configuration of HTTP OAuth2.0 request authentication
// behavior.
type OAuth2Config struct {
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
