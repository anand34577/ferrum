package api

import (
	"reflect"
	"testing"

	"ferrum/internal/pve"
)

func TestSplitTagsHandlesBothSeparatorsAndDedupes(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"prod,web", []string{"prod", "web"}},
		{"prod;web", []string{"prod", "web"}},
		{"prod, web ; prod", []string{"prod", "web"}}, // dupe + whitespace collapse
		{"  ", []string{}},
	}
	for _, c := range cases {
		got := splitTags(c.raw)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitTags(%q) = %#v, want %#v", c.raw, got, c.want)
		}
	}
}

func TestAggregateTagCountsAcrossConnectionsSortedByCountThenName(t *testing.T) {
	byConn := [][]pve.ClusterResource{
		{
			{Type: "qemu", Tags: "prod;web"},
			{Type: "lxc", Tags: "prod"},
			{Type: "node"}, // non-guest rows must be ignored
			{Type: "storage", Tags: "prod"},
		},
		{
			{Type: "qemu", Tags: "prod,db"},
		},
	}
	got := aggregateTagCounts(byConn)
	want := []tagCount{
		{Tag: "prod", Count: 3},
		{Tag: "db", Count: 1},
		{Tag: "web", Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aggregateTagCounts = %#v, want %#v", got, want)
	}
}
