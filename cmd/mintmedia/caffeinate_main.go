package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/notify"
)

var newMainCaffeinate = func() notify.CaffeinateController {
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
