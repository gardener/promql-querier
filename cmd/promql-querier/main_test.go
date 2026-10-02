// SPDX-FileCopyrightText: Contributors to the Gardener project
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "0", want: 0},
		{in: "", wantErr: true},
		{in: "1024", want: 1024},
		{in: "1K", want: 1024},
		{in: "1k", want: 1024},
		{in: "512M", want: 512 * 1024 * 1024},
		{in: "512m", want: 512 * 1024 * 1024},
		{in: "1G", want: 1024 * 1024 * 1024},
		{in: "1g", want: 1024 * 1024 * 1024},
		{in: " 1G ", want: 1024 * 1024 * 1024},
		{in: "1KB", wantErr: true},
		{in: "1KiB", wantErr: true},
		{in: "1GB", wantErr: true},
		{in: "1GiB", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "abc", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseBytes(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestWithUnit(t *testing.T) {
	const (
		kib = int64(1024)
		mib = 1024 * kib
		gib = 1024 * mib
		tib = 1024 * gib
	)

	tests := []struct {
		name string
		in   int64
		want string
	}{
		{name: "zero", in: 0, want: "0"},
		{name: "below one KiB stays plain", in: 512, want: "512"},
		{name: "one KiB", in: kib, want: "1K"},
		{name: "two KiB", in: 2 * kib, want: "2K"},
		{name: "one MiB", in: mib, want: "1M"},
		{name: "sixty four MiB", in: 64 * mib, want: "64M"},
		{name: "one GiB", in: gib, want: "1G"},
		{name: "one and a half KiB stays in bytes", in: kib + 512, want: "1536"},
		{name: "one and a half MiB stays in KiB", in: mib + 512*kib, want: "1536K"},
		{name: "one TiB clamps to GiB", in: tib, want: "1024G"},
		{name: "two TiB clamps to GiB", in: 2 * tib, want: "2048G"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, withUnit(tc.in))
		})
	}
}
