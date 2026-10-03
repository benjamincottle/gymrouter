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
