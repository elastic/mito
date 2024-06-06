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

// The mito executable is a CEL program evaluation tool that allows development
// of Filebeat CEL input integrations without the need to run have a running
// stack. Not all feature of the Filebeat input are present, but most behaviors
// that are needed to mimic an integration are available.
//
//	Usage of mito:
//
//	  mito [opts] <src.cel>
//
//	  -cfg string
//	    	path to a YAML file holding run control configuration
//	  -data string
//	    	path to a JSON object holding input (exposed as the label state)
//	  -insecure
//	    	disable TLS verification in the HTTP client
//	  -log_requests
//	    	log request traces to stderr (go1.21+)
//	  -max_executions int
//	    	maximum number of evaluations, or no maximum if -1 (default -1)
//	  -max_log_body int
//	    	maximum length of body logged in request traces (go1.21+) (default 1000)
//	  -use string
//	    	libraries to use (default "all")
//	  -version
//	    	print version and exit
//
// The configuration accepted by mito in the configuration file is described by
// the Go types in the [rc] package, [rc.Config], [rc.AuthConfig] and [rc.OAuth2Config].
package main

import (
	"os"

	"github.com/elastic/mito"
	"github.com/elastic/mito/internal/rc"
)

// Used to allow documentation links to render.
var _ rc.Config

func main() {
	os.Exit(mito.Main())
}
