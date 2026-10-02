// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package harness

import (
	"context"
	"fmt"

	"github.com/prometheus/common/promslog"
	"github.com/prometheus/prometheus/model/labels"
	promparser "github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/tsdb"
)

type series struct {
	labels labels.Labels
	values []promparser.SequenceValue
	mint   int64
}

// WriteBlock writes a TSDB block into dir from the given Prometheus series
// descriptions, using maxt as the timestamp of each series' last sample and
// stepSec as the spacing between samples. virtualLabels are added to every
// series. It is intended for building fixture data in tests.
func WriteBlock(dir string, maxt int64, stepMs int64, virtualLabels map[string]string, seriesDescs ...string) error {
	parsed := make([]series, len(seriesDescs))
	parser := promparser.NewParser(promparser.Options{})
	for i, desc := range seriesDescs {
		labels, values, err := parser.ParseSeriesDesc(desc)
		if err != nil {
			return fmt.Errorf("failed to parse series description %q: %w", desc, err)
		}

		parsed[i] = series{
			labels: labels,
			values: values,
			mint:   maxt - int64(len(values)-1)*stepMs,
		}
	}

	mint := maxt
	for _, ps := range parsed {
		if ps.mint < mint {
			mint = ps.mint
		}
	}

	writer, err := tsdb.NewBlockWriter(promslog.NewNopLogger(), dir, maxt-mint)
	if err != nil {
		return fmt.Errorf("failed to create block writer: %w", err)
	}
	defer closeQuietly(writer)

	ctx := context.Background()
	appender := writer.Appender(ctx)
	for _, ps := range parsed {
		b := labels.NewBuilder(ps.labels)
		for name, value := range virtualLabels {
			b.Set(name, value)
		}

		lbls := b.Labels()
		ts := ps.mint
		for _, value := range ps.values {
			if _, err := appender.Append(0, lbls, ts, value.Value); err != nil {
				_ = appender.Rollback()
				return fmt.Errorf("failed to append sample: %w", err)
			}
			ts += stepMs
		}
	}

	if err := appender.Commit(); err != nil {
		return fmt.Errorf("failed to commit appender: %w", err)
	}

	if _, err := writer.Flush(ctx); err != nil {
		return fmt.Errorf("failed to flush block: %w", err)
	}

	return nil
}
