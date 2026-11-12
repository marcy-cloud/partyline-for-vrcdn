package player

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone":
			http.NotFound(w, r)
		case "/denied":
			w.WriteHeader(http.StatusUnauthorized)
		case "/broken":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.Write([]byte("stream data"))
		}
	}))
	defer srv.Close()

	cases := map[string]bool{"/live": false, "/gone": true, "/denied": true, "/broken": false}
	for path, want := range cases {
		if got := HTTPOffline(srv.URL + path); got != want {
			t.Errorf("%s: offline = %v, want %v", path, got, want)
		}
	}
	// A server that can't be reached says nothing about the stream.
	srv.Close()
	if HTTPOffline(srv.URL + "/live") {
		t.Error("an unreachable server was taken as the stream being offline")
	}
}
