package main

import (
	"context"
	"os"

	"github.com/gi8lino/screendeck/internal/app"
	"github.com/gi8lino/screendeck/web"
)

var (
	Version = "dev"
	Commit  = "none"
)

// main boots the ScreenDeck application.
func main() {
	ctx := context.Background()
	if err := app.Run(ctx, web.Assets, Version, Commit, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}
