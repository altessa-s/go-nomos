// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package convert

//go:generate optgen generate --type=options

import (
	"net/http"
	"time"
)

// DefaultRequestTimeout bounds description requests.
const DefaultRequestTimeout = 60 * time.Second

// DefaultMaxResponseBytes bounds the provider response body.
const DefaultMaxResponseBytes int64 = 4 * 1024 * 1024

// DefaultModel identifies the default description model.
const DefaultModel = "claude-sonnet-5"

// DefaultEndpoint is the description service endpoint.
const DefaultEndpoint = "https://api.anthropic.com/v1/messages"

type options struct {
	parseComments    bool
	httpClient       *http.Client
	requestTimeout   time.Duration `optgen:"default=DefaultRequestTimeout"`
	maxResponseBytes int64         `optgen:"default=DefaultMaxResponseBytes" optval:"positive"`
	model            string        `optgen:"default=DefaultModel"`
	endpoint         string        `optgen:"default=DefaultEndpoint"`
}
