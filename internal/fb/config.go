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

// Package fb provides filebeat-compatible configuration validation for
// the mito tool. Validation rules mirror the constraints in the filebeat
// CEL input's config.go and config_auth.go without depending on go-ucfg
// or any elastic-agent-libs package.
//
// Behaviour under -fb is not covered by semver compatibility guarantees.
// Validation rules may change between minor releases to track upstream
// filebeat changes.
package fb

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/elastic/mito/internal/rc"
	"github.com/elastic/mito/lib"
)

// Config is the filebeat-compatible configuration for mito. It contains
// all the fields from rc.Config plus resource, rate limit, and retry
// sections that filebeat's CEL input expects. Auth field names follow
// filebeat conventions (user/password, not username/password).
type Config struct {
	Globals       map[string]interface{} `yaml:"globals"`
	Regexps       map[string]string      `yaml:"regexp"`
	XSDs          map[string]string      `yaml:"xsd"`
	Auth          *authConfig            `yaml:"auth"`
	HTTPHeaders   http.Header            `yaml:"http_headers"`
	MaxBodySize   int64                  `yaml:"max_body_size"`
	MaxExecutions *int                   `yaml:"max_executions"`

	Resource    *resourceConfig `yaml:"resource"`
	SecretState map[string]any  `yaml:"secret_state"`
}

// RC returns the rc.Config subset needed by the mito runtime. Auth is
// converted from filebeat's field names to the lib types mito uses.
func (c *Config) RC() rc.Config {
	cfg := rc.Config{
		Globals:       c.Globals,
		Regexps:       c.Regexps,
		XSDs:          c.XSDs,
		HTTPHeaders:   c.HTTPHeaders,
		MaxBodySize:   c.MaxBodySize,
		MaxExecutions: c.MaxExecutions,
	}
	if c.Auth == nil {
		return cfg
	}
	cfg.Auth = &rc.AuthConfig{}
	if c.Auth.Basic != nil {
		cfg.Auth.Basic = &lib.BasicAuth{
			Username: c.Auth.Basic.User,
			Password: c.Auth.Basic.Password,
		}
	}
	if c.Auth.Token != nil {
		cfg.Auth.Token = &lib.TokenAuth{
			Type:  c.Auth.Token.Type,
			Value: c.Auth.Token.Value,
		}
	}
	if c.Auth.OAuth2 != nil {
		o := c.Auth.OAuth2
		cfg.Auth.OAuth2 = &rc.OAuth2Config{
			Provider:               o.Provider,
			ClientID:               o.ClientID,
			ClientSecret:           o.ClientSecret,
			EndpointParams:         o.EndpointParams,
			Password:               o.Password,
			Scopes:                 o.Scopes,
			TokenURL:               o.TokenURL,
			User:                   o.User,
			GoogleCredentialsFile:  o.GoogleCredentialsFile,
			GoogleCredentialsJSON:  o.GoogleCredentialsJSON,
			GoogleJWTFile:          o.GoogleJWTFile,
			GoogleJWTJSON:          o.GoogleJWTJSON,
			GoogleDelegatedAccount: o.GoogleDelegatedAccount,
			AzureTenantID:          o.AzureTenantID,
			AzureResource:          o.AzureResource,
		}
	}
	return cfg
}

// Validate checks the configuration against filebeat CEL input
// constraints. Error messages match filebeat's where possible.
func (c *Config) Validate() error {
	if c.Auth != nil {
		if err := c.Auth.validate(); err != nil {
			return err
		}
	}
	if c.Resource != nil {
		if err := c.Resource.validate(); err != nil {
			return err
		}
	}
	if c.MaxExecutions != nil && *c.MaxExecutions <= 0 {
		return fmt.Errorf("invalid maximum number of executions: %d <= 0", *c.MaxExecutions)
	}
	return nil
}

// CheckState inspects state data (the -data JSON) and rejects it if
// it contains a "secret" key at the top level.
func CheckState(state map[string]any) error {
	if _, ok := state["secret"]; ok {
		return errors.New(`state must not contain a "secret" key: use secret_state instead`)
	}
	return nil
}

// authConfig mirrors filebeat's authConfig. Field names and YAML tags
// follow filebeat conventions.
type authConfig struct {
	Basic  *basicAuthConfig  `yaml:"basic"`
	Token  *tokenAuthConfig  `yaml:"token"`
	Digest *digestAuthConfig `yaml:"digest"`
	File   *fileAuthConfig   `yaml:"file"`
	OAuth2 *oAuth2Config     `yaml:"oauth2"`
}

func (a *authConfig) validate() error {
	var n int
	if a.Basic != nil {
		n++
	}
	if a.Token != nil {
		n++
	}
	if a.Digest != nil {
		n++
	}
	if a.File != nil {
		n++
	}
	if a.OAuth2 != nil {
		n++
	}
	if n > 1 {
		return errors.New("only one kind of auth can be enabled")
	}
	if a.Basic != nil {
		if err := a.Basic.validate(); err != nil {
			return err
		}
	}
	if a.Token != nil {
		if err := a.Token.validate(); err != nil {
			return err
		}
	}
	if a.Digest != nil {
		if err := a.Digest.validate(); err != nil {
			return err
		}
	}
	if a.File != nil {
		if err := a.File.validate(); err != nil {
			return err
		}
	}
	if a.OAuth2 != nil {
		if err := a.OAuth2.validate(); err != nil {
			return err
		}
	}
	return nil
}

type basicAuthConfig struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

func (b *basicAuthConfig) validate() error {
	if b.User == "" || b.Password == "" {
		return errors.New("both user and password must be set")
	}
	return nil
}

type tokenAuthConfig struct {
	Type  string `yaml:"type"`
	Value string `yaml:"value"`
}

func (t *tokenAuthConfig) validate() error {
	if t.Type == "" || t.Value == "" {
		return errors.New("both type and value must be set")
	}
	return nil
}

type digestAuthConfig struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

func (d *digestAuthConfig) validate() error {
	if d.User == "" || d.Password == "" {
		return errors.New("both user and password must be set")
	}
	return nil
}

type fileAuthConfig struct {
	Path            string         `yaml:"path"`
	RefreshInterval *time.Duration `yaml:"refresh_interval"`
}

func (f *fileAuthConfig) validate() error {
	if f.Path == "" {
		return errors.New("path must be set")
	}
	if f.RefreshInterval != nil && *f.RefreshInterval <= 0 {
		return errors.New("refresh_interval must be greater than 0")
	}
	return nil
}

type oAuth2Config struct {
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

	OktaJWKFile string `yaml:"okta.jwk_file"`
	OktaJWKJSON string `yaml:"okta.jwk_json"`
	OktaJWKPEM  string `yaml:"okta.jwk_pem"`
}

func (o *oAuth2Config) validate() error {
	switch prov := strings.ToLower(o.Provider); prov {
	case "azure":
		return o.validateAzure()
	case "google":
		return o.validateGoogle()
	case "okta":
		return o.validateOkta()
	case "":
		if (o.User != "" && o.Password == "") || (o.User == "" && o.Password != "") {
			return errors.New("both user and password credentials must be provided")
		}
		if o.TokenURL == "" || ((o.ClientID == "" || o.ClientSecret == nil) && (o.User == "" || o.Password == "")) {
			return errors.New("both token_url and client credentials must be provided")
		}
		return nil
	default:
		return fmt.Errorf("unknown provider %q", prov)
	}
}

func (o *oAuth2Config) validateAzure() error {
	if o.TokenURL == "" && o.AzureTenantID == "" {
		return errors.New("at least one of token_url or tenant_id must be provided")
	}
	if o.TokenURL != "" && o.AzureTenantID != "" {
		return errors.New("only one of token_url and tenant_id can be used")
	}
	if o.ClientID == "" || o.ClientSecret == nil {
		return errors.New("client credentials must be provided")
	}
	return nil
}

func (o *oAuth2Config) validateGoogle() error {
	if o.TokenURL != "" || o.ClientID != "" || o.ClientSecret != nil ||
		o.AzureTenantID != "" || o.AzureResource != "" {
		return errors.New("none of token_url and client credentials can be used, use google.credentials_file, google.jwt_file, google.credentials_json or ADC instead")
	}
	if o.GoogleCredentialsJSON != "" {
		if o.GoogleDelegatedAccount != "" {
			return errors.New("google.delegated_account can only be provided with a jwt_file")
		}
		return nil
	}
	if o.GoogleCredentialsFile != "" {
		if o.GoogleDelegatedAccount != "" {
			return errors.New("google.delegated_account can only be provided with a jwt_file")
		}
		return nil
	}
	if o.GoogleJWTFile != "" || o.GoogleJWTJSON != "" {
		return nil
	}
	return errors.New("no authentication credentials were configured or detected (ADC)")
}

func (o *oAuth2Config) validateOkta() error {
	if o.TokenURL == "" || o.ClientID == "" || len(o.Scopes) == 0 {
		return errors.New("okta validation error: token_url, client_id, scopes must be provided")
	}
	var n int
	if o.OktaJWKJSON != "" {
		n++
	}
	if o.OktaJWKFile != "" {
		n++
	}
	if o.OktaJWKPEM != "" {
		n++
	}
	if n != 1 {
		return errors.New("okta validation error: one of okta.jwk_json, okta.jwk_file or okta.jwk_pem must be provided")
	}
	return nil
}

type resourceConfig struct {
	URL       string       `yaml:"url"`
	RateLimit *rateLimit   `yaml:"rate_limit"`
	Retry     *retryConfig `yaml:"retry"`
}

func (r *resourceConfig) validate() error {
	if r.URL == "" {
		return errors.New("resource url must be set")
	}
	if _, err := url.Parse(r.URL); err != nil {
		return fmt.Errorf("resource url is not valid: %w", err)
	}
	if r.RateLimit != nil {
		if err := r.RateLimit.validate(); err != nil {
			return err
		}
	}
	if r.Retry != nil {
		if err := r.Retry.validate(); err != nil {
			return err
		}
	}
	return nil
}

type rateLimit struct {
	Limit *float64 `yaml:"limit"`
	Burst *int     `yaml:"burst"`
}

func (r *rateLimit) validate() error {
	if r.Limit != nil && *r.Limit <= 0 {
		return errors.New("limit must be greater than zero")
	}
	if r.Limit == nil && r.Burst != nil && *r.Burst <= 0 {
		return errors.New("burst must be greater than zero if limit is not specified")
	}
	return nil
}

type retryConfig struct {
	MaxAttempts *int           `yaml:"max_attempts"`
	WaitMin     *time.Duration `yaml:"wait_min"`
	WaitMax     *time.Duration `yaml:"wait_max"`
}

func (r *retryConfig) validate() error {
	switch {
	case r.MaxAttempts != nil && *r.MaxAttempts <= 0:
		return errors.New("max_attempts must be greater than zero")
	case r.WaitMin != nil && *r.WaitMin <= 0:
		return errors.New("wait_min must be greater than zero")
	case r.WaitMax != nil && *r.WaitMax <= 0:
		return errors.New("wait_max must be greater than zero")
	}
	return nil
}
