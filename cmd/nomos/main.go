// Copyright 2021-2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/altessa-s/go-nomos/cmd/nomos/commands"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := commands.RunContext(ctx, os.Args[1:])
	stop()
	if err != nil {
		os.Exit(1)
	}
}
