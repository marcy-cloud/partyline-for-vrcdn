//go:build !production

package main

import "os"

// browserArgs lets a development build open WebView2's DevTools port, so the
// real window can be driven and screenshotted by a script:
//
//	VRCDN_PLAYER_DEBUG_PORT=9333 bin/dev.exe
//
// Release builds (-tags production) have no such switch.
func browserArgs() []string {
	if port := os.Getenv("VRCDN_PLAYER_DEBUG_PORT"); port != "" {
		return []string{"--remote-debugging-port=" + port}
	}
	return nil
}
