// Command gymrouter is the Gym Router server and its maintenance tools.
package main

import (
	"fmt"
	"os"
)

func usage() {
	fmt.Fprintln(os.Stderr, `usage: gymrouter <command> [flags]

commands:
  suggest-lines   suggest each gym's set of lines from timetable data (run locally)`)
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
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
