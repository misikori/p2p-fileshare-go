package main

import (
	"p2p-fileshare/tracker"
)

func main() {
	tracker.RunServer(":8080")
}
