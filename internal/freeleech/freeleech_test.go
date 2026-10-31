package freeleech

import "testing"

func boolPtr(value bool) *bool        { return &value }
func floatPtr(value float64) *float64 { return &value }

func TestResolveThreeStates(t *testing.T) {
	tests := []struct {
		name   string
		free   *bool
		known  *bool
		factor *float64
		want   State
	}{
		{"nothing stated", nil, nil, nil, State{}},
		{"free flag", boolPtr(true), nil, nil, State{Free: true, Known: true}},
		{"paid flag", boolPtr(false), nil, nil, State{Known: true}},
		{"factor zero", nil, nil, floatPtr(0), State{Free: true, Known: true}},
		{"factor one", nil, nil, floatPtr(1), State{Known: true}},
		{"fractional factor is not free", nil, nil, floatPtr(0.5), State{Known: true}},
		{"unknown wins over factor", nil, boolPtr(false), floatPtr(0), State{}},
		{"known with free flag", boolPtr(true), boolPtr(true), nil, State{Free: true, Known: true}},
		{"known with paid flag", boolPtr(false), boolPtr(true), nil, State{Known: true}},
		{"known with factor", nil, boolPtr(true), floatPtr(0), State{Free: true, Known: true}},
		{"known without a value", nil, boolPtr(true), nil, State{Known: true}},
		{"explicit free wins over paid factor", boolPtr(true), nil, floatPtr(1), State{Free: true, Known: true}},
		{"explicit paid wins over free factor", boolPtr(false), nil, floatPtr(0), State{Known: true}},
	}
	for _, test := range tests {
		if got := Resolve(test.free, test.known, test.factor); got != test.want {
			t.Errorf("%s: Resolve = %+v, want %+v", test.name, got, test.want)
		}
	}
}

func TestParseFlag(t *testing.T) {
	cases := []struct {
		raw   string
		value bool
		ok    bool
	}{
		{"1", true, true}, {"true", true, true}, {"yes", true, true},
		{"on", true, true}, {"free", true, true}, {"FREELEECH", true, true},
		{"0", false, true}, {"false", false, true}, {"no", false, true},
		{"off", false, true}, {"paid", false, true}, {"maybe", false, false},
	}
	for _, test := range cases {
		value, ok := ParseFlag(test.raw)
		if value != test.value || ok != test.ok {
			t.Errorf("ParseFlag(%q) = (%v, %v), want (%v, %v)", test.raw, value, ok, test.value, test.ok)
		}
	}
}
