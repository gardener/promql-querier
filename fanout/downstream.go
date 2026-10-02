// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"

	"github.com/gardener/promql-querier/labels"
)

// Downstream bundles the clients used to reach a single downstream endpoint.
type Downstream struct {
	client  promv1.API
	querier *streamingQuerier
	labels  labels.LabelSet
}

// BuildDownstreams creates one client per downstream.
func BuildDownstreams(downstreamLabels map[string]labels.LabelSet, timeout time.Duration) (map[string]Downstream, error) {
	downstreams := make(map[string]Downstream, len(downstreamLabels))
	for url, ls := range downstreamLabels {
		httpClient := &http.Client{
			Transport: &FetchedBytesTransport{Base: api.DefaultRoundTripper, URL: url},
			Timeout:   timeout,
		}
		client, err := api.NewClient(api.Config{Address: url, Client: httpClient})
		if err != nil {
			return nil, fmt.Errorf("creating client for endpoint %s: %w", url, err)
		}
		downstreams[url] = Downstream{
			client:  promv1.NewAPI(client),
			querier: &streamingQuerier{client: httpClient, baseURL: url},
			labels:  ls,
		}
	}
	return downstreams, nil
}

// LabelSet returns the label set that identifies this downstream.
func (d Downstream) LabelSet() labels.LabelSet {
	return d.labels
}
