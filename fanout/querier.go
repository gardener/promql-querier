// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package fanout

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	promlabels "github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/gardener/promql-querier/labels"
)

const timeMillisPrecision = 3

// streamingQuerier queries a single downstream Prometheus over HTTP and decodes
// the JSON result array element by element rather than buffering the whole body.
type streamingQuerier struct {
	client  *http.Client
	baseURL string
}

// query runs an instant query. A zero timestamp omits the time parameter, matching the
// prometheus client. The virtual labels are injected onto every decoded series
// as its labels are built.
func (q *streamingQuerier) query(ctx context.Context, expr string, ts time.Time, lookbackDelta model.Duration, labelSet labels.LabelSet) ([]promql.Series, promv1.Warnings, error) {
	form := url.Values{}
	form.Set("query", expr)
	if !ts.IsZero() {
		form.Set("time", formatTimeParam(ts))
	}
	form.Set("lookback_delta", lookbackDelta.String())
	return q.do(ctx, "/api/v1/query", form, labelSet)
}

// queryRange runs a range query.
func (q *streamingQuerier) queryRange(ctx context.Context, expr string, rng promv1.Range, lookbackDelta model.Duration, labelSet labels.LabelSet) ([]promql.Series, promv1.Warnings, error) {
	form := url.Values{}
	form.Set("query", expr)
	form.Set("start", formatTimeParam(rng.Start))
	form.Set("end", formatTimeParam(rng.End))
	form.Set("step", strconv.FormatFloat(rng.Step.Seconds(), 'f', -1, 64))
	form.Set("lookback_delta", lookbackDelta.String())
	return q.do(ctx, "/api/v1/query_range", form, labelSet)
}

// formatTimeParam matches the prometheus client's time encoding (unix seconds
// with a fractional part), so requests stay byte compatible with a real
// Prometheus.
func formatTimeParam(t time.Time) string {
	return strconv.FormatFloat(float64(t.Unix())+float64(t.Nanosecond())/1e9, 'f', -1, 64)
}

func (q *streamingQuerier) do(ctx context.Context, path string, form url.Values, labelSet labels.LabelSet) ([]promql.Series, promv1.Warnings, error) {
	endpoint := strings.TrimRight(q.baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}

	// Do not set "Accept-Encoding: gzip", but rely on the base http.Transport to handle compression by default.
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := q.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer closeQuietly(resp.Body)

	return parseResponse(resp.Body, fetchedSamplesLimitFromContext(ctx), labelSet)
}

// closeQuietly closes c, discarding any error. Use it in defers where the close
// error is genuinely uninteresting, such as a fully consumed response body on
// the read side.
func closeQuietly(c io.Closer) {
	_ = c.Close()
}

// parseResponse parses the response body. A non-nil samplesLimit is charged for
// each decoded sample and aborts once breached.
func parseResponse(body io.Reader, samplesLimit *Limit, labelSet labels.LabelSet) ([]promql.Series, promv1.Warnings, error) {
	dec := json.NewDecoder(body)
	if err := expectDelim(dec, '{'); err != nil {
		return nil, nil, err
	}

	var (
		status   string
		errorMsg string
		warnings []string
		series   []promql.Series
	)

	for dec.More() {
		key, err := expectString(dec)
		if err != nil {
			return nil, nil, err
		}
		switch key {
		case "status":
			if err := dec.Decode(&status); err != nil {
				return nil, nil, err
			}
		case "error":
			if err := dec.Decode(&errorMsg); err != nil {
				return nil, nil, err
			}
		case "warnings", "infos":
			if err := dec.Decode(&warnings); err != nil {
				return nil, nil, err
			}
		case "data":
			// The "data" field should only appear on successful responses.
			series, err = parseData(dec, samplesLimit, labelSet)
			if err != nil {
				return nil, nil, err
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, nil, err
			}
		}
	}

	if status == "error" {
		return nil, promv1.Warnings(warnings), &promv1.Error{
			Type: promv1.ErrorType("downstream_error"),
			Msg:  errorMsg,
		}
	}

	return series, promv1.Warnings(warnings), nil
}

// parseData parses the inner "data" object. The result type is ignored
// because it will be inferred in nested functions.
func parseData(dec *json.Decoder, samplesLimit *Limit, labelSet labels.LabelSet) ([]promql.Series, error) {
	if err := expectDelim(dec, '{'); err != nil {
		return nil, err
	}
	var (
		series []promql.Series
		err    error
	)
	for dec.More() {
		key, keyErr := expectString(dec)
		if keyErr != nil {
			return nil, keyErr
		}
		switch key {
		case "result":
			series, err = parseResult(dec, samplesLimit, labelSet)
			if err != nil {
				return nil, err
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, err
			}
		}
	}
	if err := expectDelim(dec, '}'); err != nil {
		return nil, err
	}
	return series, nil
}

// parseResult parses the result array into []promql.Series. Each element is
// decoded into a reused json.RawMessage. The element's shape is inferred from
// which sample field it carries. The builder and scratch pair slice are reused
// across elements.
func parseResult(dec *json.Decoder, samplesLimit *Limit, labelSet labels.LabelSet) ([]promql.Series, error) {
	if err := expectDelim(dec, '['); err != nil {
		return nil, err
	}

	var (
		series     []promql.Series
		labelPairs []labelPair
		elem       json.RawMessage

		builder = promlabels.NewScratchBuilder(0)
	)

	for dec.More() {
		// Reuse the same buffer
		elem = elem[:0]
		if err := dec.Decode(&elem); err != nil {
			return nil, err
		}

		spans, err := elementSpans(elem)
		if err != nil {
			return nil, err
		}

		builder.Reset()
		lbls, err := parseLabels(spans, &builder, &labelPairs, labelSet)
		if err != nil {
			return nil, err
		}

		floats, err := parseValues(spans)
		if err != nil {
			return nil, err
		}

		if samplesLimit != nil {
			if err := samplesLimit.Add(int64(len(floats))); err != nil {
				return nil, err
			}
		}

		series = append(series, promql.Series{Metric: lbls, Floats: floats})
	}

	if err := expectDelim(dec, ']'); err != nil {
		return nil, err
	}

	return series, nil
}

func parseLabels(spans elementFieldSpans, builder *promlabels.ScratchBuilder, labelPairs *[]labelPair, labelSet labels.LabelSet) (promlabels.Labels, error) {
	p := parser{&cursor{buf: spans.metric}}
	return p.labels(builder, labelPairs, labelSet)
}

func parseValues(spans elementFieldSpans) ([]promql.FPoint, error) {
	if spans.isMatrix() {
		p := parser{&cursor{buf: spans.values}}
		return p.fpoints()
	}

	p := parser{&cursor{buf: spans.value}}
	point, err := p.fpoint()
	if err != nil {
		return nil, err
	}

	return []promql.FPoint{point}, nil
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("expected %q, got %v", want, tok)
	}
	return nil
}

func expectString(dec *json.Decoder) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}
	key, ok := tok.(string)
	if !ok {
		return "", fmt.Errorf("expected string, got %v", tok)
	}
	return key, nil
}

// elementFieldSpans holds byte spans sliced out of one result element: the
// metric and value or values fields.
type elementFieldSpans struct {
	metric []byte
	value  []byte
	values []byte
}

func (e elementFieldSpans) isMatrix() bool {
	return e.values != nil
}

// elementSpans slices the series out of one result element, returning byte spans
// rather than decoding into a struct. The element type is inferred.
// - Vector: [ { "metric": {...}, "value": [ts, "value"] }, ... ]
// - Matrix: [ { "metric": {...}, "values": [ [ts, "value"], ... ] }, ... ]
func elementSpans(elem []byte) (elementFieldSpans, error) {
	var fields elementFieldSpans
	s := cursor{buf: elem}
	if err := s.past('{'); err != nil {
		return fields, fmt.Errorf("expected result element object: %w", err)
	}

	for {
		s.skipSpace()
		if !s.done() && s.peek() == '}' {
			break
		}

		key, err := s.spanString()
		if err != nil {
			return fields, err
		}

		if err := s.past(':'); err != nil {
			return fields, fmt.Errorf("after element key %q: %w", key, err)
		}

		value, err := s.spanValue()
		if err != nil {
			return fields, err
		}

		switch string(key) {
		case "metric":
			fields.metric = value
		case "value":
			fields.value = value
		case "values":
			fields.values = value
		case "histogram", "histograms":
			return fields, fmt.Errorf("histogram result type is not supported")
		}

		s.skipSpace()
		if !s.done() && s.peek() == ',' {
			s.advance()
		}
	}

	if fields.metric == nil {
		return fields, fmt.Errorf("result element missing metric")
	}
	if fields.value == nil && fields.values == nil {
		return fields, fmt.Errorf(`result element has neither "value" nor "values"`)
	}
	if fields.value != nil && fields.values != nil {
		return fields, fmt.Errorf(`result element has both "value" and "values"`)
	}

	return fields, nil
}

// cursor is a forward byte cursor for byte inspection, whitespace skipping, and
// spans. It never allocates. It returns byte spans into the buffer it was given.
type cursor struct {
	buf []byte
	at  int
}

func (s *cursor) done() bool {
	return s.at >= len(s.buf)
}

func (s *cursor) peek() byte {
	return s.buf[s.at]
}

func (s *cursor) advance() {
	s.at++
}

// skip moves the cursor forward n bytes.
func (s *cursor) skip(n int) {
	s.at += n
}

// skipSpace advances past JSON whitespace.
func (s *cursor) skipSpace() {
	for !s.done() {
		switch s.buf[s.at] {
		case ' ', '\t', '\n', '\r':
			s.advance()
		default:
			return
		}
	}
}

// past skips whitespace then requires the next byte to be c, advancing past it.
func (s *cursor) past(c byte) error {
	s.skipSpace()
	if s.done() {
		return fmt.Errorf("expected %q at offset %d, got end of input", c, s.at)
	}
	if s.buf[s.at] != c {
		return fmt.Errorf("expected %q at offset %d, got %q", c, s.at, s.buf[s.at])
	}
	s.advance()
	return nil
}

// spanString reads a quoted string and returns the raw bytes between the quotes,
// advancing past the closing quote.
func (s *cursor) spanString() ([]byte, error) {
	s.skipSpace()
	if s.done() || s.peek() != '"' {
		return nil, fmt.Errorf("expected string at offset %d", s.at)
	}
	s.advance()
	start := s.at
	for !s.done() {
		switch s.buf[s.at] {
		case '\\':
			s.skip(2)
			continue
		case '"':
			span := s.buf[start:s.at]
			s.advance()
			return span, nil
		}
		s.advance()
	}
	return nil, fmt.Errorf("unterminated string in fragment")
}

// spanValue advances past one JSON value (object, array, string, number,
// literal), tracking state until the value is closed.
func (s *cursor) spanValue() ([]byte, error) {
	s.skipSpace()
	if s.done() {
		return nil, fmt.Errorf("unexpected end of fragment")
	}

	start := s.at

	switch s.peek() {
	case '"':
		_, err := s.spanString()
		if err != nil {
			return nil, err
		}
	case '{', '[':
		depth := 1
		s.advance()

		for !s.done() {
			switch s.buf[s.at] {
			case '"':
				// Advance for the whole string to avoid braces or brackets inside
				// close the value too early.
				if _, err := s.spanString(); err != nil {
					return nil, err
				}
			case '{', '[':
				depth++
				s.advance()
			case '}', ']':
				depth--
				s.advance()
				if depth == 0 {
					return s.buf[start:s.at], nil
				}
			default:
				s.advance()
			}
		}
		return nil, fmt.Errorf("unterminated container in fragment")
	default:
		for !s.done() {
			switch s.buf[s.at] {
			case ',', '}', ']', ' ', '\t', '\n', '\r':
				return s.buf[start:s.at], nil
			}
			s.advance()
		}
	}

	return s.buf[start:s.at], nil
}

// parser parses Prometheus result values (sample pairs, timestamps, quoted floats)
// on top of a cursor. While cursor handles byte movement and JSON grammar, parser
// adds the Prometheus-specific parsing.
type parser struct {
	*cursor
}

// string reads a quoted string and returns it as a copied Go string. It
// parses escapes with strconv.Unquote only when a backslash is present.
func (d *parser) string() (string, error) {
	d.skipSpace()
	if d.done() || d.peek() != '"' {
		return "", fmt.Errorf("expected string at offset %d", d.at)
	}

	// start is the opening quote
	start := d.at

	d.advance()
	escaped := false
	for !d.done() {
		switch d.buf[d.at] {
		case '\\':
			escaped = true
			d.skip(2)
		case '"':
			// end is the closing quote
			end := d.at

			d.advance()
			if escaped {
				// Unquote requires the surrounding quotes to be present.
				return strconv.Unquote(string(d.buf[start : end+1]))
			}
			return string(d.buf[start+1 : end]), nil
		default:
			d.advance()
		}
	}

	return "", fmt.Errorf("unterminated string in fragment")
}

// labels parses the raw metric object into sorted labels, injecting the virtual
// labels. The builder and scratch pair slice are reused to avoid allocating per
// series.
func (d *parser) labels(builder *promlabels.ScratchBuilder, labelPairs *[]labelPair, labelSet labels.LabelSet) (promlabels.Labels, error) {
	for name, value := range labelSet {
		if value != "" {
			builder.Add(name, value)
		}
	}

	// [:0] resets length but keeps the backing array, so it is reused across series.
	*labelPairs = (*labelPairs)[:0]
	err := d.labelPairs(labelPairs)
	if err != nil {
		return promlabels.EmptyLabels(), err
	}

	for _, p := range *labelPairs {
		builder.Add(p.name, p.value)
	}

	builder.Sort()
	return builder.Labels(), nil
}

type labelPair struct {
	name  string
	value string
}

// labelPairs walks a flat JSON dictionary of strings-to-strings and appends each
// (key, value) pair to buffer, reusing its backing array.
func (d *parser) labelPairs(buf *[]labelPair) error {
	if err := d.past('{'); err != nil {
		return fmt.Errorf("expected metric object: %w", err)
	}

	d.skipSpace()
	if !d.done() && d.peek() == '}' {
		return nil
	}

	for {
		name, err := d.string()
		if err != nil {
			return err
		}

		if err := d.past(':'); err != nil {
			return fmt.Errorf("after label name %q: %w", name, err)
		}

		value, err := d.string()
		if err != nil {
			return err
		}

		*buf = append(*buf, labelPair{name: name, value: value})
		d.skipSpace()
		if d.done() {
			return fmt.Errorf("unterminated metric object")
		}

		switch d.peek() {
		case ',':
			d.advance()
		case '}':
			return nil
		default:
			return fmt.Errorf("expected ',' or '}' in metric object at offset %d", d.at)
		}
	}
}

// fpoint reads one [timestamp, "value"] pair into a single promql.FPoint,
// leaving the cursor past its closing bracket.
func (d *parser) fpoint() (promql.FPoint, error) {
	if err := d.past('['); err != nil {
		return promql.FPoint{}, err
	}

	// Decode the timestamp
	ts, err := d.timestampMillis()
	if err != nil {
		return promql.FPoint{}, err
	}
	if err := d.past(','); err != nil {
		return promql.FPoint{}, fmt.Errorf("in sample pair: %w", err)
	}

	// ParseFloat also accepts the special tokens NaN, Inf, and -Inf that
	// Prometheus encodes sample values with.
	str, err := d.spanString()
	if err != nil {
		return promql.FPoint{}, err
	}
	value, err := strconv.ParseFloat(string(str), 64)
	if err != nil {
		return promql.FPoint{}, err
	}

	if err := d.past(']'); err != nil {
		return promql.FPoint{}, err
	}

	return promql.FPoint{T: ts, F: value}, nil
}

// fpoints scans a Prometheus values array of [timestamp, "value"] pairs
// into a single []promql.FPoint.
func (d *parser) fpoints() ([]promql.FPoint, error) {
	if err := d.past('['); err != nil {
		return nil, err
	}

	d.skipSpace()
	if !d.done() && d.peek() == ']' {
		d.advance()
		return nil, nil
	}

	var floats []promql.FPoint
loop:
	for {
		point, err := d.fpoint()
		if err != nil {
			return nil, err
		}

		floats = append(floats, point)
		d.skipSpace()
		if d.done() {
			return nil, fmt.Errorf("unterminated values array")
		}

		switch d.peek() {
		case ',':
			d.advance()
		case ']':
			d.advance()

			// Break out and return the slice after the loop rather than from inside
			// it: the compiler elides append allocations when the grown slice leaves
			// the loop at a single return point.
			break loop
		default:
			return nil, fmt.Errorf("expected ',' or ']' in values array at offset %d", d.at)
		}
	}

	return floats, nil
}

// timestampMillis reads the raw number timestamp as unix seconds and returns it in
// milliseconds.
func (d *parser) timestampMillis() (int64, error) {
	isNumber := func(b byte) bool {
		return b >= '0' && b <= '9'
	}

	str, err := d.spanValue()
	if err != nil {
		return 0, err
	}

	var (
		ts   int64
		i    int
		sign int64 = 1
	)

	if i < len(str) && str[i] == '-' {
		sign = -1
		i++
	}

	if i >= len(str) {
		return 0, fmt.Errorf("expected timestamp digits, got %q", str)
	}

	for ; i < len(str) && str[i] != '.'; i++ {
		if !isNumber(str[i]) {
			return 0, fmt.Errorf("invalid timestamp %q", str)
		}
		ts = ts*10 + int64(str[i]-'0')
	}

	i++
	for range timeMillisPrecision {
		add := int64(0)
		if i < len(str) {
			if !isNumber(str[i]) {
				return 0, fmt.Errorf("invalid timestamp %q", str)
			}
			add = int64(str[i] - '0')
			i++
		}
		ts = ts*10 + add
	}

	// Any bytes left past the millisecond precision must still be digits, so a
	// malformed value should fail loud.
	for ; i < len(str); i++ {
		if !isNumber(str[i]) {
			return 0, fmt.Errorf("invalid timestamp %q", str)
		}
	}

	return ts * sign, nil
}
