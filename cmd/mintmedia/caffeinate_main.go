package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Mtn-Man/mintmedia/internal/console"
	"github.com/Mtn-Man/mintmedia/internal/notify"
)

type mainCaffeinateController interface {
	Start(context.Context) error
	Stop() error
}

var newMainCaffeinate = func() mainCaffeinateController {
	return notify.NewCaffeinate()
}

func withCaffeinate(fn func() error) error {
	caffCtx, cancelCaff := context.WithCancel(context.Background())
	caff := newMainCaffeinate()
	if caff != nil {
		if err := caff.Start(caffCtx); err != nil {
			fmt.Fprintln(os.Stderr, console.ColorizePrefix(fmt.Sprintf("WARNING  caffeinate: %v", err)))
		}
	}
	defer func() {
		cancelCaff()
		if caff == nil {
			return
		}
		if err := caff.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, console.ColorizePrefix(fmt.Sprintf("WARNING  caffeinate stop: %v", err)))
		}
	}()

	return fn()
}
