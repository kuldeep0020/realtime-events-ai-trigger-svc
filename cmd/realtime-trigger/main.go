// Command realtime-trigger is the multi-mode binary for the Real-time AI
// Trigger Service. It exposes four subcommands:
//
//	serve      Start the HTTP API + Pulsar consumer (production mode)
//	seed       Seed canned responses + reference data into Postgres
//	demo-fire  Fire a browser-channel event sequence against the ingestion svc
//	migrate    Apply Goose database migrations
//
// Each subcommand owns its own FlagSet and exit semantics. Common runtime
// configuration (DSN, log level, etc.) is sourced from environment variables
// per §8.2 of the design doc; flags override env where applicable.
package main

import (
	"flag"
	"fmt"
	"os"
)

const usage = `realtime-trigger — Real-time AI Trigger Service

Usage:
  realtime-trigger <command> [flags]

Commands:
  serve        Start the HTTP API + Pulsar consumer (production mode)
  seed         Seed canned responses and reference data into Postgres
  demo-fire    Fire a browser-channel event sequence for a demo persona
  migrate      Apply Goose database migrations

Run 'realtime-trigger <command> -help' for per-command flags.
`

func main() {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	cmd := flag.Arg(0)
	args := flag.Args()[1:]

	switch cmd {
	case "serve":
		runServe(args)
	case "seed":
		runSeed(args)
	case "demo-fire":
		runDemoFire(args)
	case "migrate":
		runMigrate(args)
	case "-help", "--help", "help":
		flag.Usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", cmd)
		flag.Usage()
		os.Exit(1)
	}
}
