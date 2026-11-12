package player

import (
	"net/http"
	"time"
)

// HTTPOffline reports whether the stream at an HTTP URL is offline: the
// server answers that it is not there. Any other outcome, including not
// reaching the server at all, counts as "not known to be offline", so a
// broken check never keeps a live stream from playing.
func HTTPOffline(url string) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	// A live stream's body never ends; the status line is all we want.
	resp.Body.Close()
	return resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized
}
