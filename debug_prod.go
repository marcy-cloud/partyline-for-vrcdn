//go:build production

package main

func browserArgs() []string { return nil }

func instanceID() string { return singleInstanceID }
