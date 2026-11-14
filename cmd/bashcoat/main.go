// Command bashcoat measures C0 line coverage for Bash 5+ scripts.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/noissefnoc/bashcoat/internal/cli"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	quiet := cli.HasQuietFlag(os.Args[1:])
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

	go func() {
		first := true
		for sig := range sigCh {
			if first {
				first = false
				if !quiet {
					fmt.Fprintf(os.Stderr, "\nbashcoat: received signal (%s), restoring target scripts...\n", sig)
				}
				cancel()
			}
		}
	}()

	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	signal.Stop(sigCh)
	close(sigCh)
	os.Exit(code)
}
