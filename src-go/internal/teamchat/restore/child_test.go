package restore

import "testing"

func TestCheckArgsWithNoPortAndNoTokenAreTheLiveServer(t *testing.T) {
	for _, c := range []struct {
		a    CheckArgs
		live bool
	}{
		{CheckArgs{}, true},
		{CheckArgs{Port: 8449}, false},
		{CheckArgs{Token: "t"}, false},
		{CheckArgs{Port: 8449, Token: "t"}, false},
	} {
		if got := c.a.Live(); got != c.live {
			t.Errorf("%+v Live = %v", c.a, got)
		}
	}
}
