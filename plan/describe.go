// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package plan

import "time"

// Description is a JSON-serializable summary of a Plan, including its
// execution mode and the description of every partition.
type Description struct {
	Expr       string                 `json:"expr"`
	IsRange    bool                   `json:"is_range"`
	Time       time.Time              `json:"time,omitempty"`
	Start      time.Time              `json:"start,omitempty"`
	End        time.Time              `json:"end,omitempty"`
	Step       time.Duration          `json:"step,omitempty"`
	Partitions []PartitionDescription `json:"partitions"`
}

// PartitionDescription is a JSON-serializable summary of a single Partition,
// including its downstream expression and the downstreams it targets.
type PartitionDescription struct {
	Expr           string        `json:"expr"`
	DownstreamExpr string        `json:"downstream_expr"`
	Downstreams    []string      `json:"downstreams"`
	IsRange        bool          `json:"is_range"`
	Time           time.Time     `json:"time,omitempty"`
	Start          time.Time     `json:"start,omitempty"`
	End            time.Time     `json:"end,omitempty"`
	Step           time.Duration `json:"step,omitempty"`
}

// Describe returns a JSON-serializable description of the plan and all of its
// partitions.
func (p *Plan) Describe() Description {
	desc := Description{
		Expr:       p.expr.String(),
		IsRange:    p.IsRange(),
		Time:       p.Time(),
		Start:      p.Start(),
		End:        p.End(),
		Step:       p.Step(),
		Partitions: []PartitionDescription{},
	}
	for _, part := range p.partitions {
		desc.Partitions = append(desc.Partitions, PartitionDescription{
			Expr:           part.Expr().String(),
			DownstreamExpr: part.DownstreamExpr().String(),
			Downstreams:    part.Downstreams(),
			IsRange:        part.IsRange(),
			Time:           part.Time(),
			Start:          part.Start(),
			End:            part.End(),
			Step:           part.Step(),
		})
	}
	return desc
}
