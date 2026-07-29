package main

import (
	"context"
	"os"

	"github.com/imythu/dpull/cmd"
)

func main() {
	os.Exit(cmd.Execute(context.Background()))
}
