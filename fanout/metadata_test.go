// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/gardener/promql-querier/labels"
)

func TestPlanMatchers(t *testing.T) {
	downstreams := map[string]Downstream{
		"http://prod-eu:9090":    {labels: labels.LabelSet{"env": "prod", "region": "eu"}},
		"http://prod-us:9090":    {labels: labels.LabelSet{"env": "prod", "region": "us"}},
		"http://staging-eu:9090": {labels: labels.LabelSet{"env": "staging", "region": "eu"}},
	}
	labelNames := labels.NewLabelNames("env", "region")

	tests := []struct {
		name           string
		matchers       []string
		want           metadataPlan
		wantErrPattern string
	}{
		{
			name:     "no matchers route to all downstreams to return all items",
			matchers: nil,
			want: metadataPlan{
				"http://prod-eu:9090":    {`{__name__!=""}`},
				"http://prod-us:9090":    {`{__name__!=""}`},
				"http://staging-eu:9090": {`{__name__!=""}`},
			},
		},
		{
			name:     "virtual label narrows downstreams and is stripped",
			matchers: []string{`{env="prod"}`},
			want: metadataPlan{
				"http://prod-eu:9090": {`{__name__!=""}`},
				"http://prod-us:9090": {`{__name__!=""}`},
			},
		},
		{
			name:     "two virtual labels intersect",
			matchers: []string{`{env="prod",region="eu"}`},
			want: metadataPlan{
				"http://prod-eu:9090": {`{__name__!=""}`},
			},
		},
		{
			name:     "real label is forwarded and routes to all",
			matchers: []string{`{job="api"}`},
			want: metadataPlan{
				"http://prod-eu:9090":    {`{job="api"}`},
				"http://prod-us:9090":    {`{job="api"}`},
				"http://staging-eu:9090": {`{job="api"}`},
			},
		},
		{
			name:     "mixed selector splits into route and forward",
			matchers: []string{`{env="prod",job="api"}`},
			want: metadataPlan{
				"http://prod-eu:9090": {`{job="api"}`},
				"http://prod-us:9090": {`{job="api"}`},
			},
		},
		{
			name:     `virtual label with empty-value matcher gets __name__!=""`,
			matchers: []string{`{env="prod",foo=""}`},
			want: metadataPlan{
				"http://prod-eu:9090": {`{foo="",__name__!=""}`},
				"http://prod-us:9090": {`{foo="",__name__!=""}`},
			},
		},
		{
			name: "separate match with disjoint virtual labels union their downstreams",
			matchers: []string{
				`{region="eu",job="api"}`,
				`{region="us",job="web"}`,
			},
			want: metadataPlan{
				"http://prod-eu:9090":    {`{job="api"}`},
				"http://staging-eu:9090": {`{job="api"}`},
				"http://prod-us:9090":    {`{job="web"}`},
			},
		},
		{
			name:           "invalid matcher errors",
			matchers:       []string{`{not a selector`},
			wantErrPattern: `^parsing matcher "\{not a selector": `,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMetadata(nil, downstreams, labelNames, 0)
			plan, err := m.planMatchers(tc.matchers)
			if tc.wantErrPattern != "" {
				require.Regexp(t, tc.wantErrPattern, err.Error())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, plan)
		})
	}
}

type fakeAPI struct {
	promv1.API

	seriesFn      func(ctx context.Context) ([]model.LabelSet, promv1.Warnings, error)
	labelNamesFn  func(ctx context.Context) (model.LabelNames, promv1.Warnings, error)
	labelValuesFn func(ctx context.Context) (model.LabelValues, promv1.Warnings, error)
}

func (f fakeAPI) Series(ctx context.Context, _ []string, _, _ time.Time, _ ...promv1.Option) ([]model.LabelSet, promv1.Warnings, error) {
	return f.seriesFn(ctx)
}

func (f fakeAPI) LabelNames(ctx context.Context, _ []string, _, _ time.Time, _ ...promv1.Option) (model.LabelNames, promv1.Warnings, error) {
	return f.labelNamesFn(ctx)
}

func (f fakeAPI) LabelValues(ctx context.Context, _ string, _ []string, _, _ time.Time, _ ...promv1.Option) (model.LabelValues, promv1.Warnings, error) {
	return f.labelValuesFn(ctx)
}

func testMetadata(clients ...promv1.API) *Metadata {
	downstreams := testDownstreams(clients...)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewMetadata(logger, downstreams, labels.NewLabelNames("region"), 0)
}

func testDownstreams(clients ...promv1.API) map[string]Downstream {
	// Assume at most two clients are passed.
	regions := []string{"eu", "us"}
	downstreams := make(map[string]Downstream)
	for i, c := range clients {
		url := fmt.Sprintf("http://%s:9090", regions[i])
		downstreams[url] = Downstream{client: c, labels: labels.LabelSet{"region": regions[i]}}
	}
	return downstreams
}

// hasSeries is a fakeAPI whose Series returns one series.
func hasSeries() fakeAPI {
	return fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "up"}}, nil, nil
	}}
}

// noSeries is a fakeAPI whose Series returns nothing.
func noSeries() fakeAPI {
	return fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return nil, nil, nil
	}}
}

func TestValidateNonEmptyMatchers(t *testing.T) {
	tests := []struct {
		name           string
		matchers       []string
		wantErrPattern string
	}{
		{
			name:     "no matchers is allowed",
			matchers: nil,
		},
		{
			name:     "single non-empty matcher passes",
			matchers: []string{`{job="api"}`},
		},
		{
			name:     "every matcher non-empty passes",
			matchers: []string{`{job="api"}`, `{job="web"}`},
		},
		{
			name:           "empty selector fails",
			matchers:       []string{`{}`},
			wantErrPattern: `^match\[\] must contain at least one non-empty matcher$`,
		},
		{
			name:           "only empty-value matcher fails",
			matchers:       []string{`{foo=""}`},
			wantErrPattern: `^match\[\] must contain at least one non-empty matcher$`,
		},
		{
			name:           "non-empty then empty fails",
			matchers:       []string{`{job="api"}`, `{}`},
			wantErrPattern: `^match\[\] must contain at least one non-empty matcher$`,
		},
		{
			name:           "empty then non-empty fails",
			matchers:       []string{`{}`, `{job="api"}`},
			wantErrPattern: `^match\[\] must contain at least one non-empty matcher$`,
		},
		{
			name:           "invalid matcher errors",
			matchers:       []string{`{not a selector`},
			wantErrPattern: `^parsing matcher "\{not a selector": `,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNonEmptyMatchers(tc.matchers)
			if tc.wantErrPattern != "" {
				require.Regexp(t, tc.wantErrPattern, err.Error())
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestSeriesErrorsWhenMatchIsNotProvided(t *testing.T) {
	_, _, err := testMetadata().Series(context.Background(), nil, time.Time{}, time.Time{}, 0)
	require.Error(t, err)
	require.Equal(t, "no match[] parameter provided", err.Error())
}

func TestSeriesErrorsWhenMatchIsEmpty(t *testing.T) {
	_, _, err := testMetadata().Series(context.Background(), []string{"{}"}, time.Time{}, time.Time{}, 0)
	require.Error(t, err)
	require.Equal(t, "match[] must contain at least one non-empty matcher", err.Error())
}

func TestSeriesInjectsVirtualLabelsAndDedupes(t *testing.T) {
	shared := model.LabelSet{"__name__": "up", "job": "api"}
	eu := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{shared}, nil, nil
	}}
	us := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{shared}, nil, nil
	}}

	result, _, err := testMetadata(eu, us).Series(context.Background(), []string{`{__name__!=""}`}, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.ElementsMatch(t, []model.LabelSet{
		{"__name__": "up", "job": "api", "region": "eu"},
		{"__name__": "up", "job": "api", "region": "us"},
	}, result)
}

func TestLabelNamesMergesVirtualNames(t *testing.T) {
	eu := fakeAPI{labelNamesFn: func(context.Context) (model.LabelNames, promv1.Warnings, error) {
		return model.LabelNames{"job", "instance"}, nil, nil
	}}
	us := fakeAPI{labelNamesFn: func(context.Context) (model.LabelNames, promv1.Warnings, error) {
		return model.LabelNames{"job"}, nil, nil
	}}

	result, _, err := testMetadata(eu, us).LabelNames(context.Background(), nil, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"instance", "job", "region"}, result)
}

func TestLabelValuesDedupesAcrossDownstreams(t *testing.T) {
	eu := fakeAPI{labelValuesFn: func(context.Context) (model.LabelValues, promv1.Warnings, error) {
		return model.LabelValues{"api", "web"}, nil, nil
	}}
	us := fakeAPI{labelValuesFn: func(context.Context) (model.LabelValues, promv1.Warnings, error) {
		return model.LabelValues{"web", "db"}, nil, nil
	}}

	result, _, err := testMetadata(eu, us).LabelValues(context.Background(), "job", nil, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Equal(t, model.LabelValues{"api", "db", "web"}, result)
}

func TestLabelValuesForVirtualLabelReturnsRoutedDownstreamsWithSeries(t *testing.T) {
	result, _, err := testMetadata(hasSeries(), hasSeries()).LabelValues(context.Background(), "region", nil, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Equal(t, model.LabelValues{"eu", "us"}, result)
}

func TestLabelValuesForVirtualLabelRestrictsToRoutedDownstreams(t *testing.T) {
	result, _, err := testMetadata(hasSeries(), hasSeries()).LabelValues(context.Background(), "region", []string{`{region="eu"}`}, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Equal(t, model.LabelValues{"eu"}, result)
}

func TestLabelValuesForVirtualLabelReturnsNothingWhenMatcherRoutesNowhere(t *testing.T) {
	result, _, err := testMetadata(hasSeries(), hasSeries()).LabelValues(context.Background(), "region", []string{`{region="nonexistent"}`}, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestLabelValuesForVirtualLabelDropsRoutedDownstreamWithNoMatchingSeries(t *testing.T) {
	result, _, err := testMetadata(noSeries(), hasSeries()).LabelValues(context.Background(), "region", []string{`{a="a",a="b"}`}, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Equal(t, model.LabelValues{"us"}, result)
}

func TestLabelValuesForVirtualLabelReturnsNothingWhenNoDownstreamHasSeries(t *testing.T) {
	result, _, err := testMetadata(noSeries(), noSeries()).LabelValues(context.Background(), "region", nil, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestSeriesReturnsPartialResultWhenDownstreamFails(t *testing.T) {
	// A failed downstream must not fail the whole request: autocompletion still
	// gets the series from the healthy downstream.
	eu := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "up", "job": "api"}}, nil, nil
	}}
	boom := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return nil, nil, context.DeadlineExceeded
	}}

	result, _, err := testMetadata(eu, boom).Series(context.Background(), []string{`{__name__!=""}`}, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Equal(t, []model.LabelSet{
		{"__name__": "up", "job": "api", "region": "eu"},
	}, result)
}

func TestSeriesFailsWhenDownstreamReturnsAPIError(t *testing.T) {
	// A *promv1.Error means the downstream understood the request and rejected it,
	// so the whole call fails rather than returning a partial result.
	eu := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "up", "job": "api"}}, nil, nil
	}}
	rejected := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return nil, nil, &promv1.Error{Type: promv1.ErrBadData, Msg: "invalid parameter"}
	}}

	_, _, err := testMetadata(eu, rejected).Series(context.Background(), []string{`{__name__!=""}`}, time.Time{}, time.Time{}, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid parameter")
	require.Regexp(t, `downstream "http://.*:9090": invalid parameter`, err.Error())
}

func TestSeriesWarnsAndContinuesOnTransportError(t *testing.T) {
	// A transport-level error (not a *promv1.Error) drops only the failing
	// downstream.
	eu := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "up", "job": "api"}}, nil, nil
	}}
	boom := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return nil, nil, fmt.Errorf("connection refused")
	}}

	result, warnings, err := testMetadata(eu, boom).Series(context.Background(), []string{`{__name__!=""}`}, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, warnings, 1)
	require.Regexp(t, `^http://.*:9090: inaccurate result due to incomplete data: connection refused$`, warnings[0])
}

func TestSeriesPropagatesWarningFromDownstreams(t *testing.T) {
	eu := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "a"}}, promv1.Warnings{"results truncated due to limit"}, nil
	}}
	us := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "b"}}, promv1.Warnings{"results truncated due to limit"}, nil
	}}

	_, warnings, err := testMetadata(eu, us).Series(context.Background(), []string{`{__name__!=""}`}, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.ElementsMatch(t, promv1.Warnings{
		"http://eu:9090: results truncated due to limit",
		"http://us:9090: results truncated due to limit",
	}, warnings)
}

func TestSeriesTruncatesToLimit(t *testing.T) {
	eu := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "b"}, {"__name__": "d"}}, nil, nil
	}}
	us := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "a"}, {"__name__": "c"}}, nil, nil
	}}

	result, warnings, err := testMetadata(eu, us).Series(context.Background(), []string{`{__name__!=""}`}, time.Time{}, time.Time{}, 2)
	require.NoError(t, err)
	require.Equal(t, []model.LabelSet{{"__name__": "a", "region": "us"}, {"__name__": "b", "region": "eu"}}, result)
	require.Contains(t, warnings, "results truncated due to limit")
}

func TestSeriesNoTruncationWarningWithinLimit(t *testing.T) {
	eu := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "a"}}, nil, nil
	}}
	us := fakeAPI{seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
		return []model.LabelSet{{"__name__": "b"}}, nil, nil
	}}

	_, warnings, err := testMetadata(eu, us).Series(context.Background(), []string{`{__name__!=""}`}, time.Time{}, time.Time{}, 5)
	require.NoError(t, err)
	require.Empty(t, warnings)
}

func TestLabelNamesTruncatesToLimit(t *testing.T) {
	eu := fakeAPI{labelNamesFn: func(context.Context) (model.LabelNames, promv1.Warnings, error) {
		return model.LabelNames{"job", "instance"}, nil, nil
	}}
	us := fakeAPI{labelNamesFn: func(context.Context) (model.LabelNames, promv1.Warnings, error) {
		return model.LabelNames{"job"}, nil, nil
	}}

	result, warnings, err := testMetadata(eu, us).LabelNames(context.Background(), nil, time.Time{}, time.Time{}, 2)
	require.NoError(t, err)
	require.Equal(t, []string{"instance", "job"}, result)
	require.Contains(t, warnings, "results truncated due to limit")
}

func TestLabelValuesTruncatesToLimit(t *testing.T) {
	eu := fakeAPI{labelValuesFn: func(context.Context) (model.LabelValues, promv1.Warnings, error) {
		return model.LabelValues{"api", "web"}, nil, nil
	}}
	us := fakeAPI{labelValuesFn: func(context.Context) (model.LabelValues, promv1.Warnings, error) {
		return model.LabelValues{"web", "db"}, nil, nil
	}}

	result, warnings, err := testMetadata(eu, us).LabelValues(context.Background(), "job", nil, time.Time{}, time.Time{}, 2)
	require.NoError(t, err)
	require.Equal(t, model.LabelValues{"api", "db"}, result)
	require.Contains(t, warnings, "results truncated due to limit")
}

func TestLabelValuesForVirtualLabelTruncatesToLimit(t *testing.T) {
	eu := fakeAPI{
		labelValuesFn: func(context.Context) (model.LabelValues, promv1.Warnings, error) {
			return model.LabelValues{"api", "web"}, nil, nil
		},
		seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
			return []model.LabelSet{{"__name__": "b"}, {"__name__": "d"}}, nil, nil
		},
	}
	us := fakeAPI{
		labelValuesFn: func(context.Context) (model.LabelValues, promv1.Warnings, error) {
			return model.LabelValues{"web", "db"}, nil, nil
		},
		seriesFn: func(context.Context) ([]model.LabelSet, promv1.Warnings, error) {
			return []model.LabelSet{{"__name__": "a"}, {"__name__": "c"}}, nil, nil
		},
	}

	result, warnings, err := testMetadata(eu, us).LabelValues(context.Background(), "region", nil, time.Time{}, time.Time{}, 1)
	require.NoError(t, err)
	require.Equal(t, model.LabelValues{"eu"}, result)
	require.Equal(t, promv1.Warnings{"results truncated due to limit"}, warnings)
}

func TestLabelNamesOmitsVirtualNameWithoutValues(t *testing.T) {
	ds := fakeAPI{labelNamesFn: func(context.Context) (model.LabelNames, promv1.Warnings, error) {
		return model.LabelNames{"job", "instance"}, nil, nil
	}}

	downstreams := map[string]Downstream{}
	downstreams["http://nowhere:9090"] = Downstream{client: ds, labels: labels.LabelSet{"region": ""}}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewMetadata(logger, downstreams, labels.NewLabelNames("region"), 0)

	result, _, err := m.LabelNames(context.Background(), nil, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"instance", "job"}, result)
}

func TestLabelNamesOmitsVirtualNameWhenMatcherRoutesNowhere(t *testing.T) {
	ds := fakeAPI{labelNamesFn: func(context.Context) (model.LabelNames, promv1.Warnings, error) {
		return model.LabelNames{"job", "instance"}, nil, nil
	}}

	result, _, err := testMetadata(ds).LabelNames(context.Background(), []string{`{region="nonexistent"}`}, time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	require.Empty(t, result)
}

func TestWithVirtualLabelSet(t *testing.T) {
	original := model.LabelSet{"__name__": "up", "job": "api"}
	ls := labels.LabelSet{"region": "eu", "env": ""}
	injected := withVirtualLabelSet(original, ls)

	require.Equal(t, model.LabelSet{"__name__": "up", "job": "api", "region": "eu"}, injected)
	require.NotContains(t, injected, model.LabelName("env"))
	require.Equal(t, model.LabelSet{"__name__": "up", "job": "api"}, original)
}
