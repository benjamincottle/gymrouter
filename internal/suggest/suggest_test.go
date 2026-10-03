package suggest

import (
	"testing"

	"github.com/benjamincottle/gymrouter/internal/lines"
)

func TestWindowDepartures(t *testing.T) {
	if n := (Window{Start: 16 * 3600, End: 19 * 3600, Step: 600}).Departures(); n != 19 {
		t.Errorf("departures = %d", n)
	}
	if n := (Window{Start: 10, End: 5, Step: 600}).Departures(); n != 0 {
		t.Errorf("backwards window = %d", n)
	}
}

func TestScoresRankByBestShareAndRecommendRegularLines(t *testing.T) {
	m1, b288, b999 := lines.MustSet("metro M1"), lines.MustSet("bus 288"), lines.MustSet("bus 999")
	key := func(s lines.Set) lines.Key {
		for k := range s {
			return k
		}
		return lines.Key{}
	}
	got := Scores([]WindowResult{
		{Departures: 20, LineSeen: map[lines.Key]int{key(m1): 18, key(b288): 1, key(b999): 1}, LineColor: map[lines.Key]string{key(m1): "168388"}},
		{Departures: 10, LineSeen: map[lines.Key]int{key(b288): 5}},
	})
	if len(got) != 3 || got[0].Line != key(m1) || got[1].Line != key(b288) || got[2].Line != key(b999) {
		t.Fatalf("order: %+v", got)
	}
	if !got[0].Recommended || !got[1].Recommended || got[2].Recommended {
		t.Errorf("recommended: %+v", got)
	}
	if got[0].Color != "168388" || got[1].Share != 0.5 {
		t.Errorf("colour or share: %+v", got)
	}
}
