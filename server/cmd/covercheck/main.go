// covercheck: проверка порогов покрытия сервера. Логика в internal/covercheck.
//
//	go run ./cmd/covercheck coverage.conf coverage.out
package main

import (
	"fmt"
	"os"

	"scibox/server/internal/covercheck"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: covercheck <coverage.conf> <coverage.out>")
		os.Exit(2)
	}
	rules, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer rules.Close()
	profile, err := os.Open(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer profile.Close()
	if err := covercheck.Run(rules, profile, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
