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

//go:build go1.21

// Package httplog provides http request and response transaction logging.
package httplog

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// Available indicates logging is supported.
const Available = true

var _ http.RoundTripper = (*LoggingRoundTripper)(nil)

// TraceIDKey is key used to add a trace.id value to the context of HTTP
// requests. The value will be logged by LoggingRoundTripper.
const TraceIDKey = contextKey("trace.id")

type contextKey string

// NewLoggingRoundTripper returns a LoggingRoundTripper that logs requests and
// responses to stderr. Transaction creation is logged to log.
func NewLoggingRoundTripper(next http.RoundTripper, maxBodyLen int) *LoggingRoundTripper {
	return &LoggingRoundTripper{
		transport:  next,
		maxBodyLen: maxBodyLen,
		txLog:      slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		txBaseID:   newID(),
	}
}

// LoggingRoundTripper is an http.RoundTripper that logs requests and responses.
type LoggingRoundTripper struct {
	transport   http.RoundTripper
	maxBodyLen  int           // The maximum length of a body. Longer bodies will be truncated.
	txLog       *slog.Logger  // Destination logger.
	txBaseID    string        // Random value to make transaction IDs unique.
	txIDCounter atomic.Uint64 // Transaction ID counter that is incremented for each request.
}

// RoundTrip implements the http.RoundTripper interface, logging
// the request and response to the underlying logger.
//
// Fields logged in requests:
//
//	url.original
//	url.scheme
//	url.path
//	url.domain
//	url.port
//	url.query
//	http.request
//	user_agent.original
//	http.request.body.content
//	http.request.body.truncated
//	http.request.body.bytes
//	http.request.mime_type
//	event.original (the request without body from httputil.DumpRequestOut)
//
// Fields logged in responses:
//
//	http.response.status_code
//	http.response.body.content
//	http.response.body.truncated
//	http.response.body.bytes
//	http.response.mime_type
//	event.original (the response without body from httputil.DumpResponse)
func (rt *LoggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Create a child logger for this request.
	txID := rt.nextTxID()
	log := rt.txLog.With(
		slog.String("transaction.id", txID),
	)

	if v := req.Context().Value(TraceIDKey); v != nil {
		if traceID, ok := v.(string); ok {
			log = log.With(slog.String("trace.id", traceID))
		}
	}

	req, respParts, errorsMessages := logRequest(log, req, rt.maxBodyLen)

	resp, err := rt.transport.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if resp == nil {
		return resp, err
	}
	respParts = append(respParts,
		slog.Int("http.response.status_code", resp.StatusCode),
	)
	errorsMessages = errorsMessages[:0]
	var body []byte
	resp.Body, body, err = copyBody(resp.Body)
	if err != nil {
		errorsMessages = append(errorsMessages, fmt.Sprintf("failed to read response body: %s", err))
	}
	respParts = append(respParts,
		slog.Any("http.response.body.content", byteString(body[:min(len(body), rt.maxBodyLen)])),
		slog.Bool("http.response.body.truncated", rt.maxBodyLen < len(body)),
		slog.Int("http.response.body.bytes", len(body)),
		slog.String("http.response.mime_type", resp.Header.Get("Content-Type")),
	)
	message, err := httputil.DumpResponse(resp, false)
	if err != nil {
		errorsMessages = append(errorsMessages, fmt.Sprintf("failed to dump response: %s", err))
	} else {
		respParts = append(respParts, slog.Any("event.original", byteString(message)))
	}
	switch len(errorsMessages) {
	case 0:
	case 1:
		respParts = append(respParts, slog.String("error.message", errorsMessages[0]))
	default:
		respParts = append(respParts, slog.Any("error.message", errorsMessages))
	}
	log.LogAttrs(context.Background(), slog.LevelInfo, "HTTP response", respParts...)

	return resp, err
}

func logRequest(log *slog.Logger, req *http.Request, maxBodyLen int, extra ...slog.Attr) (_ *http.Request, parts []slog.Attr, errorsMessages []string) {
	reqParts := append([]slog.Attr{
		slog.String("url.original", req.URL.String()),
		slog.String("url.scheme", req.URL.Scheme),
		slog.String("url.path", req.URL.Path),
		slog.String("url.domain", req.URL.Hostname()),
		slog.String("url.port", req.URL.Port()),
		slog.String("url.query", req.URL.RawQuery),
		slog.String("http.request.method", req.Method),
		slog.String("user_agent.original", req.Header.Get("User-Agent")),
	}, extra...)

	var (
		body []byte
		err  error
	)
	req.Body, body, err = copyBody(req.Body)
	if err != nil {
		errorsMessages = append(errorsMessages, fmt.Sprintf("failed to read request body: %s", err))
	}
	reqParts = append(reqParts,
		slog.Any("http.request.body.content", byteString(body[:min(len(body), maxBodyLen)])),
		slog.Bool("http.request.body.truncated", maxBodyLen < len(body)),
		slog.Int("http.request.body.bytes", len(body)),
		slog.String("http.request.mime_type", req.Header.Get("Content-Type")),
	)
	message, err := httputil.DumpRequestOut(req, false)
	if err != nil {
		errorsMessages = append(errorsMessages, fmt.Sprintf("failed to dump request: %s", err))
	} else {
		reqParts = append(reqParts, slog.Any("event.original", byteString(message)))
	}
	switch len(errorsMessages) {
	case 0:
	case 1:
		reqParts = append(reqParts, slog.String("error.message", errorsMessages[0]))
	default:
		reqParts = append(reqParts, slog.Any("error.message", errorsMessages))
	}
	log.LogAttrs(context.Background(), slog.LevelInfo, "HTTP request", reqParts...)

	return req, reqParts[:0], errorsMessages
}

type byteString []byte

func (b byteString) LogValue() slog.Value {
	if b == nil {
		return slog.StringValue("<nil>")
	}
	return slog.StringValue(string(b))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TxID returns the current transaction.id value. If rt is nil, the empty string is returned.
func (rt *LoggingRoundTripper) TxID() string {
	if rt == nil {
		return ""
	}
	count := rt.txIDCounter.Load()
	return rt.formatTxID(count)
}

// nextTxID returns the next transaction.id value. It increments the internal
// request counter.
func (rt *LoggingRoundTripper) nextTxID() string {
	count := rt.txIDCounter.Add(1)
	return rt.formatTxID(count)
}

func (rt *LoggingRoundTripper) formatTxID(count uint64) string {
	return rt.txBaseID + "-" + strconv.FormatUint(count, 10)
}

// newID returns an ID derived from the current time.
func newID() string {
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], uint64(time.Now().UnixNano()))
	return base32.HexEncoding.WithPadding(base32.NoPadding).EncodeToString(data[:])
}

// copyBody is derived from drainBody in net/http/httputil/dump.go
//
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// copyBody reads all of b to memory and then returns a
// ReadCloser yielding the same bytes, and the bytes themselves.
//
// It returns an error if the initial slurp of all bytes fails.
func copyBody(b io.ReadCloser) (r io.ReadCloser, body []byte, err error) {
	if b == nil || b == http.NoBody {
		// No copying needed. Preserve the magic sentinel meaning of NoBody.
		return http.NoBody, nil, nil
	}
	var buf bytes.Buffer
	if _, err = buf.ReadFrom(b); err != nil {
		return nil, buf.Bytes(), err
	}
	if err = b.Close(); err != nil {
		return nil, buf.Bytes(), err
	}
	return io.NopCloser(&buf), buf.Bytes(), nil
}
