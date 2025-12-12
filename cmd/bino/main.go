package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"

	"bino.bi/bino/internal/cli"
	"bino.bi/bino/internal/logx"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	ctx = logx.WithRunID(ctx, uuid.NewString())

	app := cli.New()

	if err := app.Execute(ctx); err != nil {
		handleCtx := app.Context()
		if handleCtx == nil {
			handleCtx = ctx
		}
		msg, code := cli.FormatError(handleCtx, err)
		fmt.Fprintf(os.Stderr, "bino: %s\n", msg)
		os.Exit(code)
	}
}
