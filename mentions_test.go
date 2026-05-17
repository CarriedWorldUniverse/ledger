package ledger

import (
	"reflect"
	"sort"
	"testing"
)

func TestParseMentions(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"hello @anvil and @plumb!", []string{"anvil", "plumb"}},
		{"no mentions here", nil},
		{"email like a@b.com shouldn't match", nil},
		{"case @ANvil should be lowered", []string{"anvil"}},
		{"@shadow @shadow dedup", []string{"shadow"}},
	}
	for _, c := range cases {
		got := ParseMentions(c.in)
		sort.Strings(got)
		sort.Strings(c.want)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseMentions(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
