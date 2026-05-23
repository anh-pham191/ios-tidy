// internal/apps/sort_test.go
package apps

import (
	"reflect"
	"testing"
)

func TestSort(t *testing.T) {
	cases := []struct {
		name string
		in   []App
		want []App
	}{
		{
			name: "empty input stays empty",
			in:   []App{},
			want: []App{},
		},
		{
			name: "single app passes through",
			in:   []App{{BundleID: "a", DynamicBytes: 5, StaticBytes: 10}},
			want: []App{{BundleID: "a", DynamicBytes: 5, StaticBytes: 10}},
		},
		{
			name: "descending total bytes",
			in: []App{
				{BundleID: "small", DynamicBytes: 1, StaticBytes: 1},
				{BundleID: "big", DynamicBytes: 100, StaticBytes: 100},
				{BundleID: "mid", DynamicBytes: 10, StaticBytes: 10},
			},
			want: []App{
				{BundleID: "big", DynamicBytes: 100, StaticBytes: 100},
				{BundleID: "mid", DynamicBytes: 10, StaticBytes: 10},
				{BundleID: "small", DynamicBytes: 1, StaticBytes: 1},
			},
		},
		{
			name: "equal totals broken by bundle id ascending",
			in: []App{
				{BundleID: "com.zeta", DynamicBytes: 50, StaticBytes: 50},
				{BundleID: "com.alpha", DynamicBytes: 50, StaticBytes: 50},
				{BundleID: "com.mike", DynamicBytes: 50, StaticBytes: 50},
			},
			want: []App{
				{BundleID: "com.alpha", DynamicBytes: 50, StaticBytes: 50},
				{BundleID: "com.mike", DynamicBytes: 50, StaticBytes: 50},
				{BundleID: "com.zeta", DynamicBytes: 50, StaticBytes: 50},
			},
		},
		{
			name: "mixed zero sizes still sort stably by bundle id",
			in: []App{
				{BundleID: "b", DynamicBytes: 0, StaticBytes: 0},
				{BundleID: "a", DynamicBytes: 0, StaticBytes: 0},
				{BundleID: "c", DynamicBytes: 0, StaticBytes: 5},
			},
			want: []App{
				{BundleID: "c", DynamicBytes: 0, StaticBytes: 5},
				{BundleID: "a", DynamicBytes: 0, StaticBytes: 0},
				{BundleID: "b", DynamicBytes: 0, StaticBytes: 0},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := make([]App, len(c.in))
			copy(in, c.in)
			Sort(in)
			if !reflect.DeepEqual(in, c.want) {
				t.Fatalf("Sort produced %+v, want %+v", in, c.want)
			}
		})
	}
}

func TestLimit(t *testing.T) {
	apps := []App{
		{BundleID: "a"}, {BundleID: "b"}, {BundleID: "c"},
	}
	cases := []struct {
		name string
		n    int
		want []App
	}{
		{"zero returns all", 0, apps},
		{"negative returns all", -3, apps},
		{"larger than slice returns all", 99, apps},
		{"smaller truncates", 2, apps[:2]},
		{"exactly len returns all", 3, apps},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Limit(append([]App(nil), apps...), c.n)
			if len(got) != len(c.want) {
				t.Fatalf("len(Limit(_, %d)) = %d, want %d", c.n, len(got), len(c.want))
			}
			for i := range c.want {
				if got[i].BundleID != c.want[i].BundleID {
					t.Fatalf("Limit(_, %d)[%d] = %q, want %q", c.n, i, got[i].BundleID, c.want[i].BundleID)
				}
			}
		})
	}
}

func TestLimit_emptyInput(t *testing.T) {
	got := Limit(nil, 5)
	if len(got) != 0 {
		t.Fatalf("Limit(nil, 5) = %v, want empty", got)
	}
}
