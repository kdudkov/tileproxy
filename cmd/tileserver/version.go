package main

import "fmt"

var (
	version = "unknown"
	commit  = ""
	date    = ""
)

func getVersion() string {
	return version
}

func getVersionFull() string {
	return fmt.Sprintf("goatak_server version: %s, commit: %s, built at: %s", version, commit, date)
}
