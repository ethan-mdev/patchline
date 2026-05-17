package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ethan-mdev/dispatch/pkg/publisher"
	localstorage "github.com/ethan-mdev/dispatch/pkg/storage/local"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dispatch:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "publish":
		return publish(args[1:])
	case "help", "-h", "--help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func publish(args []string) error {
	flags := flag.NewFlagSet("publish", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	appID := flags.String("app-id", "", "application id")
	version := flags.String("version", "", "release version")
	channel := flags.String("channel", "beta", "release channel")
	output := flags.String("output", "./release-output", "local publish output directory")
	jsonOut := flags.Bool("json", false, "write machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: dispatch publish [flags] <build-dir>")
	}

	result, err := publisher.Publish(context.Background(), localstorage.New(*output), flags.Arg(0), publisher.Options{
		AppID:   *appID,
		Version: *version,
		Channel: *channel,
	})
	if err != nil {
		return err
	}

	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(result)
	}

	fmt.Fprintf(os.Stdout, "published %s to %s\n", result.Manifest.Version, result.Manifest.Channel)
	fmt.Fprintf(os.Stdout, "files: %d, uploaded: %d, reused: %d\n", len(result.Manifest.Files), result.ObjectsUploaded, result.ObjectsReused)
	return nil
}

func usage() error {
	fmt.Fprintln(os.Stderr, "usage: dispatch <command> [flags]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  publish    publish a build directory to local content-addressed storage")
	return nil
}
