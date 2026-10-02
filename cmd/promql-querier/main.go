// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

// Command promql-querier runs the distributed PromQL engine with virtual label routing.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/gardener/promql-querier/fanout"
	"github.com/gardener/promql-querier/labels"
	"github.com/gardener/promql-querier/plan"
	"github.com/gardener/promql-querier/server"
)

type config struct {
	Endpoints []endpoint `yaml:"endpoints"`
}

type endpoint struct {
	URL    string            `yaml:"url"`
	Labels map[string]string `yaml:"labels"`
}

func main() {
	var (
		endpointsFile string
		listenAddr    string
		logLevel      string

		metadataMaxFetchConcurrency int

		queryMaxFetchedBytes     string
		queryMaxSamples          int
		queryMaxConcurrency      int
		queryMaxFetchConcurrency int
		queryLookbackDelta       string
	)

	cmd := &cobra.Command{
		Use:   "promql-querier",
		Short: "Distributed PromQL engine with virtual label routing",
		RunE: func(cmd *cobra.Command, _ []string) error {
			level, err := parseLogLevel(logLevel)
			if err != nil {
				return err
			}

			if queryMaxSamples <= 0 {
				return fmt.Errorf("--query.max-samples must be positive")
			}
			if queryMaxConcurrency < 1 {
				return fmt.Errorf("--query.max-concurrency must be positive")
			}
			if queryMaxFetchConcurrency < 0 {
				return fmt.Errorf("--query.max-fetch-concurrency must not be negative")
			}
			lookbackDelta, err := model.ParseDuration(queryLookbackDelta)
			if err != nil {
				return fmt.Errorf("invalid --query.lookback-delta: %w", err)
			}
			if metadataMaxFetchConcurrency < 0 {
				return fmt.Errorf("--metadata.max-fetch-concurrency must not be negative")
			}

			maxFetchedBytes, err := parseBytes(queryMaxFetchedBytes)
			if err != nil {
				return fmt.Errorf("invalid --query.max-fetched-bytes: %w", err)
			}

			data, err := os.ReadFile(endpointsFile)
			if err != nil {
				return fmt.Errorf("reading endpoints file: %w", err)
			}

			var rawConfig config
			if err := yaml.Unmarshal(data, &rawConfig); err != nil {
				return fmt.Errorf("parsing endpoints file: %w", err)
			}

			virtualLabelSets := make(map[string]labels.LabelSet, len(rawConfig.Endpoints))
			for _, ep := range rawConfig.Endpoints {
				virtualLabelSets[ep.URL] = maps.Clone(ep.Labels)
			}

			virtualLabelSets = labels.ExpandLabelSets(virtualLabelSets)
			virtualLabelNames := labels.UnionLabelNames(virtualLabelSets)

			seen := map[string]string{}
			sortedNames := virtualLabelNames.Sorted()
			for _, ep := range rawConfig.Endpoints {
				var parts []string
				for _, name := range sortedNames {
					parts = append(parts, name+"="+virtualLabelSets[ep.URL][name])
				}

				key := strings.Join(parts, ",")
				if prev, ok := seen[key]; ok {
					return fmt.Errorf("endpoint %s: duplicate virtual labels {%s}, same as endpoint %s", ep.URL, key, prev)
				}

				seen[key] = ep.URL
			}

			downstreams, err := fanout.BuildDownstreams(virtualLabelSets, downstreamRequestTimeout)
			if err != nil {
				return err
			}

			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

			engineOpts := fanout.EngineDefaultOpts()
			engineOpts.Logger = logger
			engineOpts.MaxSamples = queryMaxSamples

			processorOpts := fanout.ProcessorDefaultOpts()
			processorOpts.QueryMaxFetchedBytes = maxFetchedBytes
			processorOpts.QueryMaxFetchedSamples = int64(queryMaxSamples)
			processorOpts.QueryMaxFetchConcurrency = queryMaxFetchConcurrency
			processorOpts.QueryLookbackDelta = lookbackDelta

			planner := plan.NewPlanner(virtualLabelSets, virtualLabelNames, plan.WithPlannerLogger(logger))
			processor := fanout.NewProcessor(logger, promql.NewEngine(engineOpts), planner, downstreams, processorOpts)
			metadata := fanout.NewMetadata(logger, downstreams, virtualLabelNames, metadataMaxFetchConcurrency)
			config := server.NewConfig(downstreams, server.WithQueryMaxConcurrency(queryMaxConcurrency))

			srv := server.New(logger, processor, metadata, config)
			httpSrv := &http.Server{
				Addr:              listenAddr,
				Handler:           srv.Handler(),
				ReadHeaderTimeout: serverReadHeaderTimeout,
				ReadTimeout:       serverReadTimeout,
				WriteTimeout:      serverWriteTimeout,
				IdleTimeout:       serverIdleTimeout,
			}

			logger.Info("starting promql-querier", "listen", listenAddr, "endpoints", len(downstreams))
			return runServer(cmd.Context(), logger, httpSrv)
		},
	}

	engineOpts := fanout.EngineDefaultOpts()
	processorOpts := fanout.ProcessorDefaultOpts()

	cmd.Flags().StringVar(&endpointsFile, "config.endpoints-file", "", "path to YAML endpoints config file")
	cmd.Flags().StringVar(&listenAddr, "server.listen-address", ":9090", "HTTP listen address")
	cmd.Flags().StringVar(&logLevel, "log.level", "info", "log level (debug, info, warn, error)")
	cmd.Flags().IntVar(&queryMaxSamples, "query.max-samples", engineOpts.MaxSamples, "max number of samples a query may load, capping both samples fetched from downstreams and samples the local evaluation engine materializes")
	cmd.Flags().StringVar(&queryMaxFetchedBytes, "query.max-fetched-bytes", withUnit(processorOpts.QueryMaxFetchedBytes), "max uncompressed bytes fetched from downstreams per query, e.g. 512M, 1G (0 = unlimited)")
	cmd.Flags().IntVar(&queryMaxConcurrency, "query.max-concurrency", server.DefaultQueryMaxConcurrency, "max number of queries executed concurrently; requests over the cap are rejected with 429")
	cmd.Flags().IntVar(&queryMaxFetchConcurrency, "query.max-fetch-concurrency", processorOpts.QueryMaxFetchConcurrency, "max number of downstream fetches one query may run concurrently (0 = unlimited)")
	cmd.Flags().StringVar(&queryLookbackDelta, "query.lookback-delta", processorOpts.QueryLookbackDelta.String(), "lookback delta sent to downstreams as the lookback_delta query parameter, overriding their own configured value, e.g. 5m, 1d")
	cmd.Flags().IntVar(&metadataMaxFetchConcurrency, "metadata.max-fetch-concurrency", metadataDefaultMaxFetchConcurrency, "max number of downstream fetches one metadata request may run concurrently (0 = unlimited)")
	if err := cmd.MarkFlagRequired("config.endpoints-file"); err != nil {
		panic(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

const (
	// metadataDefaultMaxFetchConcurrency caps how many downstream fetches one metadata
	// request may run concurrently by default. Metadata fan-out hits the same downstream
	// fleet as queries, so the default mirrors the query fetch cap.
	metadataDefaultMaxFetchConcurrency = 100

	// downstreamRequestTimeout bounds a single outbound request to a downstream.
	// It sits above the 2m Prometheus default query timeout so a downstream kills
	// a slow query itself before this deadline fires.
	downstreamRequestTimeout = 3 * time.Minute

	// serverReadHeaderTimeout guards against a client that sends request headers slowly.
	serverReadHeaderTimeout = 10 * time.Second
	// serverReadTimeout guards against a client that sends the request body slowly.
	serverReadTimeout = 5 * time.Minute
	// serverIdleTimeout bounds how long a keep-alive connection may sit idle.
	serverIdleTimeout = 2 * time.Minute
	// serverShutdownTimeout bounds how long a graceful shutdown waits for in-flight
	// requests to drain before the process exits.
	serverShutdownTimeout = 30 * time.Second
	// serverWriteTimeout guards against a client that reads the response slowly.
	serverWriteTimeout = fanout.QueryDefaultTimeout + 2*time.Minute
)

// runServer starts the HTTP server and blocks until ctx is cancelled (a SIGINT or
// SIGTERM), then drains in-flight requests within serverShutdownTimeout. It
// returns nil on a clean shutdown.
func runServer(ctx context.Context, logger *slog.Logger, srv *http.Server) error {
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("shutting down, draining in-flight requests")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "err", err)
			return err
		}
		logger.Info("shutdown complete")
		return nil
	}
}

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (valid: debug, info, warn, error)", s)
	}
}

func parseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0, fmt.Errorf("cannot parse empty string as byte size")
	}

	multiplier := int64(1)
	switch s[len(s)-1] {
	case 'K', 'k':
		multiplier = 1024
		s = s[:len(s)-1]
	case 'M', 'm':
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	case 'G', 'g':
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %q as byte size (use a plain number or a number with K, M, or G suffix)", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("byte size must not be negative")
	}
	return n * multiplier, nil
}

func withUnit(n int64) string {
	units := []string{"", "K", "M", "G"}
	i := 0
	for i < len(units)-1 && n >= 1024 && n%1024 == 0 {
		n /= 1024
		i++
	}
	return fmt.Sprintf("%d%s", n, units[i])
}
