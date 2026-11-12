package meter

import "testing"

func TestPeak(t *testing.T) {
	meta := map[string]any{
		"lavfi.astats.1.Peak_level": "-12.345",
		"lavfi.astats.2.Peak_level": "-inf",
		"lavfi.astats.3.Peak_level": "nan",
		"lavfi.astats.4.Peak_level": "3.1", // can't be louder than full scale
		"lavfi.astats.5.Peak_level": 7,
	}
	cases := []struct {
		channel int
		want    float64
		ok      bool
	}{
		{1, -12.345, true},
		{2, Silence, true},
		{3, Silence, true},
		{4, 0, true},
		{5, 0, false}, // not a string
		{6, 0, false}, // missing
	}
	for _, c := range cases {
		got, ok := peak(meta, c.channel)
		if got != c.want || ok != c.ok {
			t.Errorf("channel %d = %v, %v; want %v, %v", c.channel, got, ok, c.want, c.ok)
		}
	}
	if _, ok := peak(nil, 1); ok {
		t.Error("no metadata should give no level")
	}
}
