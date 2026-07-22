package main

import (
	"fmt"
	"os"

	"github.com/mtn-man/mintmedia/internal/console"
	"github.com/mtn-man/mintmedia/internal/notify"
)

var newMainCaffeinate = func() notify.CaffeinateController {
	return notify.NewCaffeinate()
}

// cliCaffeinateHooks builds the notify.CaffeinateHooks shared by every CLI
// one-shot path (--process, --process-drop): plain console output, matching
// the wording used before notify.StartCaffeinate existed.
func cliCaffeinateHooks() notify.CaffeinateHooks {
	return notify.CaffeinateHooks{
		OnUnsupported: func() {
			fmt.Println(console.ColorizePrefixOut("INFO     caffeinate: sleep inhibition not available on this platform"))
		},
		OnStartWarn: func(err error) {
			fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("WARNING  caffeinate: %v", err)))
		},
		OnStopWarn: func(err error) {
			fmt.Fprintln(os.Stderr, console.ColorizePrefixErr(fmt.Sprintf("WARNING  caffeinate stop: %v", err)))
		},
	}
}
