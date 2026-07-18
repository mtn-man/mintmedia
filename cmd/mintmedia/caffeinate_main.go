package main

import (
	"context"
	"errors"
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
			if errors.Is(err, notify.ErrInhibitUnsupported) {
				fmt.Println(console.ColorizePrefixOut("INFO     caffeinate: sleep inhibition not available on this platform"))
			} else {
				fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("WARNING  caffeinate: %v", err)))
			}
		}
	}
	defer func() {
		cancelCaff()
		if caff == nil {
			return
		}
		if err := caff.Stop(); err != nil {
			fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("WARNING  caffeinate stop: %v", err)))
		}
	}()

	return fn()
}
