// Точка входа сервера SciBox: только сборка окружения, логика в internal/cli.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"scibox/server/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, os.Args[1:], cli.Env{Getenv: os.Getenv, Stdout: os.Stdout, Stderr: os.Stderr})
	stop()
	os.Exit(code)
}
