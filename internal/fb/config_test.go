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

package fb

import (
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

var validateTests = []struct {
	name    string
	cfg     Config
	wantErr string
}{
	// Auth mutual exclusion.
	{
		name: "no_auth",
		cfg:  Config{Resource: &resourceConfig{URL: "http://localhost"}},
	},
	{
		name: "basic_auth_valid",
		cfg: Config{
			Auth:     &authConfig{Basic: &basicAuthConfig{User: "u", Password: "p"}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "two_auth_methods",
		cfg: Config{
			Auth: &authConfig{
				Basic: &basicAuthConfig{User: "u", Password: "p"},
				Token: &tokenAuthConfig{Type: "Bearer", Value: "tok"},
			},
		},
		wantErr: "only one kind of auth can be enabled",
	},
	{
		name: "three_auth_methods",
		cfg: Config{
			Auth: &authConfig{
				Basic:  &basicAuthConfig{User: "u", Password: "p"},
				Token:  &tokenAuthConfig{Type: "Bearer", Value: "tok"},
				Digest: &digestAuthConfig{User: "u", Password: "p"},
			},
		},
		wantErr: "only one kind of auth can be enabled",
	},

	// Basic auth.
	{
		name: "basic_missing_user",
		cfg: Config{
			Auth: &authConfig{Basic: &basicAuthConfig{Password: "p"}},
		},
		wantErr: "both user and password must be set",
	},
	{
		name: "basic_missing_password",
		cfg: Config{
			Auth: &authConfig{Basic: &basicAuthConfig{User: "u"}},
		},
		wantErr: "both user and password must be set",
	},
	{
		name: "basic_both_empty",
		cfg: Config{
			Auth: &authConfig{Basic: &basicAuthConfig{}},
		},
		wantErr: "both user and password must be set",
	},

	// Token auth.
	{
		name: "token_valid",
		cfg: Config{
			Auth:     &authConfig{Token: &tokenAuthConfig{Type: "Bearer", Value: "abc"}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "token_missing_type",
		cfg: Config{
			Auth: &authConfig{Token: &tokenAuthConfig{Value: "abc"}},
		},
		wantErr: "both type and value must be set",
	},
	{
		name: "token_missing_value",
		cfg: Config{
			Auth: &authConfig{Token: &tokenAuthConfig{Type: "Bearer"}},
		},
		wantErr: "both type and value must be set",
	},

	// Digest auth.
	{
		name: "digest_valid",
		cfg: Config{
			Auth:     &authConfig{Digest: &digestAuthConfig{User: "u", Password: "p"}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "digest_missing_user",
		cfg: Config{
			Auth: &authConfig{Digest: &digestAuthConfig{Password: "p"}},
		},
		wantErr: "both user and password must be set",
	},
	{
		name: "digest_missing_password",
		cfg: Config{
			Auth: &authConfig{Digest: &digestAuthConfig{User: "u"}},
		},
		wantErr: "both user and password must be set",
	},

	// File auth.
	{
		name: "file_valid",
		cfg: Config{
			Auth:     &authConfig{File: &fileAuthConfig{Path: "/etc/token"}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "file_valid_with_refresh",
		cfg: Config{
			Auth: &authConfig{File: &fileAuthConfig{
				Path:            "/etc/token",
				RefreshInterval: ptr(5 * time.Second),
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "file_missing_path",
		cfg: Config{
			Auth: &authConfig{File: &fileAuthConfig{}},
		},
		wantErr: "path must be set",
	},
	{
		name: "file_zero_refresh",
		cfg: Config{
			Auth: &authConfig{File: &fileAuthConfig{
				Path:            "/etc/token",
				RefreshInterval: ptr(time.Duration(0)),
			}},
		},
		wantErr: "refresh_interval must be greater than 0",
	},
	{
		name: "file_negative_refresh",
		cfg: Config{
			Auth: &authConfig{File: &fileAuthConfig{
				Path:            "/etc/token",
				RefreshInterval: ptr(-time.Second),
			}},
		},
		wantErr: "refresh_interval must be greater than 0",
	},

	// OAuth2 — default provider.
	{
		name: "oauth2_default_client_credentials",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				TokenURL:     "https://auth.example.com/token",
				ClientID:     "id",
				ClientSecret: ptr("secret"),
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_default_password_grant",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				TokenURL:     "https://auth.example.com/token",
				ClientID:     "id",
				ClientSecret: ptr("secret"),
				User:         "u",
				Password:     "p",
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_default_missing_token_url",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				ClientID:     "id",
				ClientSecret: ptr("secret"),
			}},
		},
		wantErr: "both token_url and client credentials must be provided",
	},
	{
		name: "oauth2_default_user_without_password",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				TokenURL: "https://auth.example.com/token",
				User:     "u",
			}},
		},
		wantErr: "both user and password credentials must be provided",
	},
	{
		name: "oauth2_default_password_without_user",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				TokenURL: "https://auth.example.com/token",
				Password: "p",
			}},
		},
		wantErr: "both user and password credentials must be provided",
	},

	// OAuth2 — azure.
	{
		name: "oauth2_azure_valid_tenant",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:      "azure",
				AzureTenantID: "tenant-id",
				ClientID:      "id",
				ClientSecret:  ptr("secret"),
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_azure_valid_token_url",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:     "azure",
				TokenURL:     "https://login.microsoftonline.com/tenant/oauth2/token",
				ClientID:     "id",
				ClientSecret: ptr("secret"),
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_azure_both_token_and_tenant",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:      "azure",
				TokenURL:      "https://example.com/token",
				AzureTenantID: "tenant-id",
				ClientID:      "id",
				ClientSecret:  ptr("secret"),
			}},
		},
		wantErr: "only one of token_url and tenant_id can be used",
	},
	{
		name: "oauth2_azure_neither_token_nor_tenant",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:     "azure",
				ClientID:     "id",
				ClientSecret: ptr("secret"),
			}},
		},
		wantErr: "at least one of token_url or tenant_id must be provided",
	},
	{
		name: "oauth2_azure_missing_client",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:      "azure",
				AzureTenantID: "tenant-id",
			}},
		},
		wantErr: "client credentials must be provided",
	},

	// OAuth2 — google.
	{
		name: "oauth2_google_credentials_json",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:              "google",
				GoogleCredentialsJSON: `{"type":"service_account"}`,
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_google_credentials_file",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:              "google",
				GoogleCredentialsFile: "/path/to/creds.json",
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_google_jwt_file",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:               "google",
				GoogleJWTFile:          "/path/to/jwt.json",
				GoogleDelegatedAccount: "admin@example.com",
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_google_jwt_json",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:      "google",
				GoogleJWTJSON: `{"type":"service_account"}`,
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_google_no_credentials",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider: "google",
			}},
		},
		wantErr: "no authentication credentials were configured or detected (ADC)",
	},
	{
		name: "oauth2_google_delegated_with_credentials_json",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:               "google",
				GoogleCredentialsJSON:  `{"type":"service_account"}`,
				GoogleDelegatedAccount: "admin@example.com",
			}},
		},
		wantErr: "google.delegated_account can only be provided with a jwt_file",
	},
	{
		name: "oauth2_google_delegated_with_credentials_file",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:               "google",
				GoogleCredentialsFile:  "/path/to/creds.json",
				GoogleDelegatedAccount: "admin@example.com",
			}},
		},
		wantErr: "google.delegated_account can only be provided with a jwt_file",
	},
	{
		name: "oauth2_google_rejects_client_fields",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider: "google",
				TokenURL: "https://example.com/token",
			}},
		},
		wantErr: "none of token_url and client credentials can be used, use google.credentials_file, google.jwt_file, google.credentials_json or ADC instead",
	},

	// OAuth2 — okta.
	{
		name: "oauth2_okta_jwk_json",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:    "okta",
				TokenURL:    "https://dev.okta.com/token",
				ClientID:    "id",
				Scopes:      []string{"openid"},
				OktaJWKJSON: `{"kty":"RSA"}`,
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_okta_jwk_file",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:    "okta",
				TokenURL:    "https://dev.okta.com/token",
				ClientID:    "id",
				Scopes:      []string{"openid"},
				OktaJWKFile: "/path/to/jwk.json",
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_okta_jwk_pem",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:   "okta",
				TokenURL:   "https://dev.okta.com/token",
				ClientID:   "id",
				Scopes:     []string{"openid"},
				OktaJWKPEM: "-----BEGIN PRIVATE KEY-----",
			}},
			Resource: &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name: "oauth2_okta_missing_token_url",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:    "okta",
				ClientID:    "id",
				Scopes:      []string{"openid"},
				OktaJWKJSON: `{"kty":"RSA"}`,
			}},
		},
		wantErr: "okta validation error: token_url, client_id, scopes must be provided",
	},
	{
		name: "oauth2_okta_missing_scopes",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:    "okta",
				TokenURL:    "https://dev.okta.com/token",
				ClientID:    "id",
				OktaJWKJSON: `{"kty":"RSA"}`,
			}},
		},
		wantErr: "okta validation error: token_url, client_id, scopes must be provided",
	},
	{
		name: "oauth2_okta_no_jwk",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider: "okta",
				TokenURL: "https://dev.okta.com/token",
				ClientID: "id",
				Scopes:   []string{"openid"},
			}},
		},
		wantErr: "okta validation error: one of okta.jwk_json, okta.jwk_file or okta.jwk_pem must be provided",
	},
	{
		name: "oauth2_okta_two_jwk",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider:    "okta",
				TokenURL:    "https://dev.okta.com/token",
				ClientID:    "id",
				Scopes:      []string{"openid"},
				OktaJWKJSON: `{"kty":"RSA"}`,
				OktaJWKFile: "/path/to/jwk.json",
			}},
		},
		wantErr: "okta validation error: one of okta.jwk_json, okta.jwk_file or okta.jwk_pem must be provided",
	},

	// Unknown provider.
	{
		name: "oauth2_unknown_provider",
		cfg: Config{
			Auth: &authConfig{OAuth2: &oAuth2Config{
				Provider: "unknown",
			}},
		},
		wantErr: `unknown provider "unknown"`,
	},

	// Resource URL.
	{
		name: "resource_valid",
		cfg:  Config{Resource: &resourceConfig{URL: "http://localhost:8080/api"}},
	},
	{
		name:    "resource_missing_url",
		cfg:     Config{Resource: &resourceConfig{}},
		wantErr: "resource url must be set",
	},

	// Rate limit.
	{
		name: "rate_limit_valid",
		cfg: Config{
			Resource: &resourceConfig{
				URL:       "http://localhost",
				RateLimit: &rateLimit{Limit: ptr(1.0), Burst: ptr(1)},
			},
		},
	},
	{
		name: "rate_limit_zero_limit",
		cfg: Config{
			Resource: &resourceConfig{
				URL:       "http://localhost",
				RateLimit: &rateLimit{Limit: ptr(0.0)},
			},
		},
		wantErr: "limit must be greater than zero",
	},
	{
		name: "rate_limit_negative_limit",
		cfg: Config{
			Resource: &resourceConfig{
				URL:       "http://localhost",
				RateLimit: &rateLimit{Limit: ptr(-1.0)},
			},
		},
		wantErr: "limit must be greater than zero",
	},
	{
		name: "rate_limit_zero_burst_no_limit",
		cfg: Config{
			Resource: &resourceConfig{
				URL:       "http://localhost",
				RateLimit: &rateLimit{Burst: ptr(0)},
			},
		},
		wantErr: "burst must be greater than zero if limit is not specified",
	},

	// Retry.
	{
		name: "retry_valid",
		cfg: Config{
			Resource: &resourceConfig{
				URL: "http://localhost",
				Retry: &retryConfig{
					MaxAttempts: ptr(3),
					WaitMin:     ptr(time.Second),
					WaitMax:     ptr(30 * time.Second),
				},
			},
		},
	},
	{
		name: "retry_zero_max_attempts",
		cfg: Config{
			Resource: &resourceConfig{
				URL:   "http://localhost",
				Retry: &retryConfig{MaxAttempts: ptr(0)},
			},
		},
		wantErr: "max_attempts must be greater than zero",
	},
	{
		name: "retry_negative_max_attempts",
		cfg: Config{
			Resource: &resourceConfig{
				URL:   "http://localhost",
				Retry: &retryConfig{MaxAttempts: ptr(-1)},
			},
		},
		wantErr: "max_attempts must be greater than zero",
	},
	{
		name: "retry_zero_wait_min",
		cfg: Config{
			Resource: &resourceConfig{
				URL:   "http://localhost",
				Retry: &retryConfig{WaitMin: ptr(time.Duration(0))},
			},
		},
		wantErr: "wait_min must be greater than zero",
	},
	{
		name: "retry_zero_wait_max",
		cfg: Config{
			Resource: &resourceConfig{
				URL:   "http://localhost",
				Retry: &retryConfig{WaitMax: ptr(time.Duration(0))},
			},
		},
		wantErr: "wait_max must be greater than zero",
	},

	// Max executions.
	{
		name: "max_executions_valid",
		cfg: Config{
			MaxExecutions: ptr(10),
			Resource:      &resourceConfig{URL: "http://localhost"},
		},
	},
	{
		name:    "max_executions_zero",
		cfg:     Config{MaxExecutions: ptr(0)},
		wantErr: "invalid maximum number of executions: 0 <= 0",
	},
	{
		name:    "max_executions_negative",
		cfg:     Config{MaxExecutions: ptr(-5)},
		wantErr: "invalid maximum number of executions: -5 <= 0",
	},
}

func TestValidate(t *testing.T) {
	for _, tt := range validateTests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Errorf("expected error %q, got nil", tt.wantErr)
			case tt.wantErr != "" && err != nil:
				if got := err.Error(); got != tt.wantErr {
					t.Errorf("error mismatch:\n got: %s\nwant: %s", got, tt.wantErr)
				}
			}
		})
	}
}

var checkStateTests = []struct {
	name    string
	state   map[string]any
	wantErr string
}{
	{
		name:  "no_secret",
		state: map[string]any{"cursor": "abc"},
	},
	{
		name:  "empty_state",
		state: map[string]any{},
	},
	{
		name:    "has_secret",
		state:   map[string]any{"secret": "value"},
		wantErr: `state must not contain a "secret" key: use secret_state instead`,
	},
	{
		name:    "secret_with_other_keys",
		state:   map[string]any{"cursor": "abc", "secret": map[string]any{"token": "x"}},
		wantErr: `state must not contain a "secret" key: use secret_state instead`,
	},
}

func TestCheckState(t *testing.T) {
	for _, tt := range checkStateTests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckState(tt.state)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Errorf("expected error %q, got nil", tt.wantErr)
			case tt.wantErr != "" && err != nil:
				if got := err.Error(); got != tt.wantErr {
					t.Errorf("error mismatch:\n got: %s\nwant: %s", got, tt.wantErr)
				}
			}
		})
	}
}

func TestRC(t *testing.T) {
	secret := "s3cret"
	cfg := Config{
		Globals:     map[string]interface{}{"key": "val"},
		Regexps:     map[string]string{"foo": "bar"},
		MaxBodySize: 1024,
		Auth: &authConfig{
			Basic: &basicAuthConfig{User: "u", Password: "p"},
		},
	}
	got := cfg.RC()
	if got.Auth == nil {
		t.Fatal("expected auth to be set")
	}
	if got.Auth.Basic == nil {
		t.Fatal("expected basic auth to be set")
	}
	if got.Auth.Basic.Username != "u" || got.Auth.Basic.Password != "p" {
		t.Errorf("basic auth mismatch: got %q/%q, want u/p", got.Auth.Basic.Username, got.Auth.Basic.Password)
	}
	if got.MaxBodySize != 1024 {
		t.Errorf("max_body_size mismatch: got %d, want 1024", got.MaxBodySize)
	}

	cfg.Auth = &authConfig{
		Token: &tokenAuthConfig{Type: "Bearer", Value: "tok"},
	}
	got = cfg.RC()
	if got.Auth.Token == nil {
		t.Fatal("expected token auth to be set")
	}
	if got.Auth.Token.Type != "Bearer" || got.Auth.Token.Value != "tok" {
		t.Errorf("token auth mismatch: got %q/%q, want Bearer/tok", got.Auth.Token.Type, got.Auth.Token.Value)
	}

	cfg.Auth = &authConfig{
		OAuth2: &oAuth2Config{
			Provider:     "azure",
			ClientID:     "id",
			ClientSecret: &secret,
			TokenURL:     "https://example.com/token",
		},
	}
	got = cfg.RC()
	if got.Auth.OAuth2 == nil {
		t.Fatal("expected oauth2 to be set")
	}
	if got.Auth.OAuth2.ClientID != "id" {
		t.Errorf("oauth2 client_id mismatch: got %q, want id", got.Auth.OAuth2.ClientID)
	}
	if got.Auth.OAuth2.ClientSecret == nil || *got.Auth.OAuth2.ClientSecret != secret {
		t.Errorf("oauth2 client_secret mismatch")
	}
}
