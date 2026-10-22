package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/imythu/dpull/cmd"
)

func main() {
	// Cancel on Ctrl-C so unfinished downloads are released for resuming.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cmd.Execute(ctx)
	stop()
	os.Exit(code)
}
