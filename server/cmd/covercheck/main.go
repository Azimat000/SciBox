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
	os.Exit(run(os.Args[1:]))
}

// run отделён от main, чтобы отложенные Close выполнялись до os.Exit.
func run(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: covercheck <coverage.conf> <coverage.out>")
		return 2
	}
	rules, err := os.Open(args[0]) //nolint:gosec // G304/G703: путь задаёт разработчик в scripts/coverage-check, а не посетитель сайта
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = rules.Close() }() // файл только читали
	profile, err := os.Open(args[1])     //nolint:gosec // G304/G703: путь задаёт разработчик в scripts/coverage-check, а не посетитель сайта
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = profile.Close() }() // файл только читали
	if err := covercheck.Run(rules, profile, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
