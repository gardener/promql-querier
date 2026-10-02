// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package harness

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/gardener/promql-querier/labels"
)

var downstreams = [4]struct {
	name     string
	labels   labels.LabelSet
	seed     int64
	clusters int
}{
	{"prod-eu", labels.LabelSet{"env": "prod", "region": "eu"}, 1, 2},
	{"prod-us", labels.LabelSet{"env": "prod", "region": "us"}, 2, 5},
	{"staging-eu", labels.LabelSet{"env": "staging", "region": "eu"}, 3, 3},
	{"staging-global", labels.LabelSet{"env": "staging"}, 4, 4},
}

func generateSeries(seed int64, index int, clusters int, samples int64) []string {
	rng := rand.New(rand.NewSource(seed))
	var series []string
	for c := range clusters {
		cluster := fmt.Sprintf("cluster-%d%d", index+1, c)
		series = append(
			series,
			generateCounter(rng, "container_cpu_usage_seconds_total", "backend", cluster, samples),
			generateCounter(rng, "container_cpu_usage_seconds_total", "frontend", cluster, samples),
			generateGauge(rng, "container_memory_working_set_bytes", "backend", cluster, samples),
			generateGauge(rng, "container_memory_working_set_bytes", "frontend", cluster, samples),
			generateUnitGauge(rng, "node_load_correlation", "backend", cluster, samples),
			generateUnitGauge(rng, "node_load_correlation", "frontend", cluster, samples),
			generateInfo("kube_pod_info", cluster, samples),
		)
		series = append(series, generateClassicHistogram(rng, "request_duration_seconds", cluster, samples)...)
	}
	return series
}

func generateInfo(name, cluster string, samples int64) string {
	values := make([]string, samples)
	for i := range samples {
		values[i] = "1"
	}
	return fmt.Sprintf(`%s{pod="server",cluster="%s"} %s`, name, cluster, strings.Join(values, " "))
}

func generateCounter(rng *rand.Rand, name, container, cluster string, samples int64) string {
	values := make([]string, samples)
	v := rng.Float64() * 100
	for i := range samples {
		values[i] = strconv.FormatFloat(v, 'f', 2, 64)
		if rng.Intn(1000) == 0 {
			v = rng.Float64() * 50
		} else {
			v += rng.Float64()*20 + 1
		}
	}
	return fmt.Sprintf(`%s{container="%s",cluster="%s"} %s`, name, container, cluster, strings.Join(values, " "))
}

func generateGauge(rng *rand.Rand, name, container, cluster string, samples int64) string {
	values := make([]string, samples)
	for i := range samples {
		values[i] = strconv.FormatFloat(rng.Float64()*100, 'f', 2, 64)
	}
	return fmt.Sprintf(`%s{container="%s",cluster="%s"} %s`, name, container, cluster, strings.Join(values, " "))
}

func generateUnitGauge(rng *rand.Rand, name, container, cluster string, samples int64) string {
	values := make([]string, samples)
	for i := range samples {
		values[i] = strconv.FormatFloat(rng.Float64()*2-1, 'f', 4, 64)
	}
	return fmt.Sprintf(`%s{container="%s",cluster="%s"} %s`, name, container, cluster, strings.Join(values, " "))
}

func generateClassicHistogram(rng *rand.Rand, name, cluster string, samples int64) []string {
	var buckets = []float64{0.1, 0.5, 1, 2.5, 5, 10}

	values := make([][]int, samples)
	counts := make([]int, samples)
	sums := make([]float64, samples)
	for i := range samples {
		values[i] = make([]int, len(buckets))

		var (
			count int
			sum   float64
		)

		for j := range buckets {
			observation := rng.Intn(5)
			count += observation
			sum += float64(observation) * buckets[j]
			values[i][j] = count
		}

		counts[i] = count
		sums[i] = sum
	}

	var series []string
	for i, bucket := range buckets {
		le := fmt.Sprintf("%.1f", bucket)
		isInf := i == len(buckets)-1
		if isInf {
			le = "+Inf"
		}

		seriesVal := make([]string, samples)
		for j := range samples {
			seriesVal[j] = strconv.Itoa(values[j][i])
		}

		series = append(series, fmt.Sprintf(`%s_bucket{cluster="%s",le="%s"} %s`, name, cluster, le, strings.Join(seriesVal, " ")))
	}

	seriesCount := make([]string, samples)
	seriesSum := make([]string, samples)
	for i := range samples {
		seriesCount[i] = strconv.Itoa(counts[i])
		seriesSum[i] = strconv.FormatFloat(sums[i], 'f', 3, 64)
	}

	series = append(series, fmt.Sprintf(`%s_count{cluster="%s"} %s`, name, cluster, strings.Join(seriesCount, " ")))
	series = append(series, fmt.Sprintf(`%s_sum{cluster="%s"} %s`, name, cluster, strings.Join(seriesSum, " ")))

	return series
}
