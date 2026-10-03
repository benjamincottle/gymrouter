package lines

import (
	"reflect"
	"testing"
)

func TestParseAndString(t *testing.T) {
	k, err := Parse("  bus 288 ")
	if err != nil || k != (Key{Bus, "288"}) || k.String() != "bus 288" {
		t.Fatalf("Parse: %v %v", k, err)
	}
	if k, _ := Parse("light-rail L4"); k.Mode != LightRail {
		t.Errorf("light rail: %v", k)
	}
	for _, bad := range []string{"", "bus", "tram 1", "288"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestModeDistinguishesSameShortName(t *testing.T) {
	if Of(700, "286") == Of(712, "286") {
		t.Error("bus 286 and school bus 286 must be different lines")
	}
	if ModeOf(2) != Train || ModeOf(401) != Metro || ModeOf(900) != LightRail || ModeOf(714) != ReplacementBus {
		t.Error("TfNSW route types mis-mapped")
	}
}

func TestSetHelpers(t *testing.T) {
	a := MustSet("bus 288", "train T9")
	b := MustSet("bus 52", "metro M1")
	u := Union(a, b)
	if !u.Has(700, "52") || !u.Has(2, "T9") || u.Has(700, "T9") {
		t.Errorf("union/has: %v", u)
	}
	want := []string{"bus 52", "bus 288", "metro M1", "train T9"}
	if got := u.Strings(); !reflect.DeepEqual(got, want) {
		t.Errorf("Strings() = %v, want %v", got, want)
	}
	if _, err := ParseSet("bus 1", "nope"); err == nil {
		t.Error("ParseSet should fail on an invalid key")
	}
}
