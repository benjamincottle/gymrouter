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

func TestReplacementBuses(t *testing.T) {
	for name, want := range map[string]string{
		"23T4": "train T4", "3AT4": "train T4", "10T9": "train T9", "12CN": "train CCN", "33BM": "train BMT",
		"27SC": "train SCO", "10HU": "train HUN", "7BM": "train BMT", "10M": "metro M1", "12M": "metro M1", "10M1": "metro M1",
		"1L1": "light-rail L1", "2L4": "light-rail L4",
	} {
		got, ok := Key{ReplacementBus, name}.Replaces()
		if !ok || got.String() != want {
			t.Errorf("%s replaces %v %v, want %s", name, got, ok, want)
		}
	}
	for _, name := range []string{"8", "5B", "1A", "699", "771V8", "12XY"} { // event shuttles, and an unknown code
		if l, ok := (Key{ReplacementBus, name}).Replaces(); ok {
			t.Errorf("%s replaces %v, want nothing", name, l)
		}
	}
	if _, ok := (Key{Bus, "23T4"}).Replaces(); ok {
		t.Error("only replacement buses replace lines")
	}
	if !(Key{ReplacementBus, "12XY"}).UnknownTrackwork() || (Key{ReplacementBus, "5B"}).UnknownTrackwork() ||
		(Key{ReplacementBus, "771V8"}).UnknownTrackwork() || (Key{ReplacementBus, "1L1"}).UnknownTrackwork() ||
		(Key{ReplacementBus, "23T4"}).UnknownTrackwork() || !(Key{Bus, "10M"}).UnknownTrackwork() ||
		(Key{Bus, "288"}).UnknownTrackwork() || (Key{Bus, "160X"}).UnknownTrackwork() {
		t.Error("UnknownTrackwork")
	}
	if got := MustSet("replacement-bus 23T4", "replacement-bus 5B", "bus 392").Chosen().Strings(); !reflect.DeepEqual(got,
		[]string{"bus 392", "replacement-bus 5B", "train T4"}) {
		t.Errorf("Chosen = %v", got)
	}
	if !MustSet("replacement-bus 23T4").Covers(Key{ReplacementBus, "23T4"}) {
		t.Error("a set naming a replacement bus covers it")
	}
	s := MustSet("train T4", "bus 392")
	if !s.Has(714, "23T4") || s.Has(714, "10T9") || s.Has(714, "5B") || !s.Has(2, "T4") {
		t.Error("a set should cover the buses replacing its train lines, and nothing else")
	}
}
