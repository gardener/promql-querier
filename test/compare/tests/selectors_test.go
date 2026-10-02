// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

//go:build compare

package compare_test

import "testing"

func TestSelectors(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "bare metric", query: `container_memory_working_set_bytes`},
		{name: "virtual label filter prod", query: `container_memory_working_set_bytes{env="prod"}`},
		{name: "virtual label filter staging", query: `container_memory_working_set_bytes{env="staging"}`},
		{name: "virtual label filter region", query: `container_memory_working_set_bytes{region="eu"}`},
		{name: "virtual label filter both", query: `container_memory_working_set_bytes{env="prod", region="eu"}`},
		{name: "non virtual label filter", query: `container_memory_working_set_bytes{container="backend"}`},
		{name: "combined filter", query: `container_memory_working_set_bytes{container="backend", env="prod"}`},
		{name: "neq virtual label", query: `container_memory_working_set_bytes{env!="prod"}`},
		{name: "regex virtual label", query: `container_memory_working_set_bytes{env=~"prod|staging"}`},
		{name: "regex neq virtual label", query: `container_memory_working_set_bytes{env!~"prod"}`},
		{name: "selector without metric name", query: `{container="backend"}`},
		{name: "paren expr", query: `(container_memory_working_set_bytes)`},
		{name: "unary negation", query: `-container_memory_working_set_bytes`},
		{name: "scalar", query: `scalar(container_memory_working_set_bytes)`},
		{name: "regex subset virtual label", query: `container_memory_working_set_bytes{env=~"prod"}`},
		{name: "empty virtual label eq", query: `container_memory_working_set_bytes{region=""}`},
		{name: "empty virtual label neq", query: `container_memory_working_set_bytes{region!=""}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.comparator.CompareQuery(t, tc.query, s.harness.EvalTimeMs)
		})
	}
}
