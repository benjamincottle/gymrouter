package api

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPIsWhatTheProxySaw(t *testing.T) {
	cases := []struct {
		xff   []string
		trust bool
		want  string
	}{
		{nil, true, "10.0.0.9"},
		{[]string{"203.0.113.7"}, true, "203.0.113.7"},
		{[]string{"1.2.3.4, 203.0.113.7"}, true, "203.0.113.7"}, // the client claimed 1.2.3.4
		{[]string{"1.2.3.4", "203.0.113.7"}, true, "203.0.113.7"},
		{[]string{"203.0.113.7"}, false, "10.0.0.9"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "10.0.0.9:5555"
		for _, v := range c.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := clientIP(r, c.trust); got != c.want {
			t.Errorf("%q trust=%v: got %s, want %s", c.xff, c.trust, got, c.want)
		}
	}
}
