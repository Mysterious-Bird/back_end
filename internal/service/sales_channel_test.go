package service

import "testing"

func TestResolveSaleChannel(t *testing.T) {
	aid := uint64(9)
	cases := []struct {
		pt   uint8
		act  *uint64
		want string
	}{
		{1, nil, "deal"},
		{2, nil, "group"},
		{1, &aid, "activity_deal"},
		{2, &aid, "activity_group"},
		{0, nil, "deal"}, // unknown → deal
	}
	for _, c := range cases {
		ch, _ := ResolveSaleChannel(c.pt, c.act)
		if ch != c.want {
			t.Fatalf("pt=%d act=%v got %s want %s", c.pt, c.act, ch, c.want)
		}
	}
}
