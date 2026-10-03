// Command gymrouter is the Gym Router server and its maintenance tools.
package main

import (
	"fmt"
	"os"
	"time"
	_ "time/tzdata" // the container image has no zoneinfo
)

// sydney is the timezone of the TfNSW timetables.
var sydney = func() *time.Location {
	l, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		panic(err)
	}
	return l
}()

func usage() {
	fmt.Fprintln(os.Stderr, `usage: gymrouter <command> [flags]

commands:
  serve           run the server (config file + TFNSW_API_KEY and GYMROUTER_TOKEN from the environment)
  setup-link      print the device setup link (uses server.public_url and GYMROUTER_TOKEN)
  new-token       generate a random access token
  check-config    validate a config file and list gyms, lines and realtime feeds
  suggest-lines   suggest each gym's set of lines from timetable data (run locally)
  plan            plan a trip from the command line (optionally with live data)
  make-fixture    write a trimmed real-data test fixture (GTFS + optional realtime snapshot)`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "setup-link":
		err = setupLink(os.Args[2:])
	case "new-token":
		err = newToken(os.Args[2:])
	case "check-config":
		err = checkConfig(os.Args[2:])
	case "suggest-lines":
		err = suggestLines(os.Args[2:])
	case "plan":
		err = planCmd(os.Args[2:])
	case "make-fixture":
		err = makeFixture(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
