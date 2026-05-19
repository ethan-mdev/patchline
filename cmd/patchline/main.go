package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ethan-mdev/patchline/pkg/client"
	"github.com/ethan-mdev/patchline/pkg/publisher"
	"github.com/ethan-mdev/patchline/pkg/signing"
	localstorage "github.com/ethan-mdev/patchline/pkg/storage/local"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "patchline:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usage()
	}

	switch args[0] {
	case "apply":
		return apply(args[1:])
	case "keygen":
		return keygen(args[1:])
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
	signingKey := flags.String("signing-key", "", "Ed25519 private signing key path")
	unsignedDev := flags.Bool("unsigned-dev", false, "publish without signing; development only")
	jsonOut := flags.Bool("json", false, "write machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: patchline publish [flags] <build-dir>")
	}

	var signer publisher.ManifestSigner
	if *signingKey != "" {
		privateKey, err := signing.ReadPrivateKey(*signingKey)
		if err != nil {
			return err
		}
		signer, err = signing.NewSigner(privateKey)
		if err != nil {
			return err
		}
	}

	result, err := publisher.Publish(context.Background(), localstorage.New(*output), flags.Arg(0), publisher.Options{
		AppID:       *appID,
		Version:     *version,
		Channel:     *channel,
		Signer:      signer,
		UnsignedDev: *unsignedDev,
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

func apply(args []string) error {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	appID := flags.String("app-id", "", "application id")
	channel := flags.String("channel", "beta", "release channel")
	baseURL := flags.String("base-url", "", "release base URL")
	installDir := flags.String("install-dir", "", "application install directory")
	lastSequence := flags.Int64("last-sequence", 0, "last accepted release sequence")
	publicKeyPath := flags.String("public-key", "", "Ed25519 public verification key path")
	unsignedDev := flags.Bool("unsigned-dev", false, "allow unsigned manifests; development only")
	jsonOut := flags.Bool("json", false, "write machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: patchline apply [flags]")
	}

	var verifier client.ManifestVerifier
	if *publicKeyPath != "" {
		publicKey, err := signing.ReadPublicKey(*publicKeyPath)
		if err != nil {
			return err
		}
		verifier, err = signing.NewVerifier(publicKey)
		if err != nil {
			return err
		}
	}

	c, err := client.New(client.Config{
		AppID:               *appID,
		Channel:             *channel,
		BaseURL:             *baseURL,
		InstallDir:          *installDir,
		LastReleaseSequence: *lastSequence,
		ManifestVerifier:    verifier,
		AllowUnsignedDev:    *unsignedDev,
	})
	if err != nil {
		return err
	}

	ctx := context.Background()
	m, err := c.FetchChannelManifest(ctx)
	if err != nil {
		return err
	}
	plan, err := c.Plan(ctx, m)
	if err != nil {
		return err
	}
	if err := c.Apply(ctx, plan); err != nil {
		return err
	}

	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	fmt.Fprintf(os.Stdout, "applied %s from %s\n", plan.Manifest.Version, plan.Manifest.Channel)
	fmt.Fprintf(os.Stdout, "files: %d, bytes: %d\n", len(plan.Files), plan.TotalBytes)
	return nil
}

func keygen(args []string) error {
	flags := flag.NewFlagSet("keygen", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	privateOut := flags.String("private-out", "patchline.key", "private signing key output path")
	publicOut := flags.String("public-out", "patchline.pub", "public verification key output path")
	jsonOut := flags.Bool("json", false, "write machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: patchline keygen [flags]")
	}

	pair, err := signing.GenerateKeyPair()
	if err != nil {
		return err
	}
	if err := signing.WriteKeyPair(*privateOut, *publicOut, pair); err != nil {
		return err
	}

	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(map[string]string{
			"private_key": *privateOut,
			"public_key":  *publicOut,
			"key_id":      pair.KeyID,
		})
	}
	fmt.Fprintf(os.Stdout, "wrote private key: %s\n", *privateOut)
	fmt.Fprintf(os.Stdout, "wrote public key: %s\n", *publicOut)
	fmt.Fprintf(os.Stdout, "key id: %s\n", pair.KeyID)
	return nil
}

func usage() error {
	fmt.Fprintln(os.Stderr, "usage: patchline <command> [flags]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  apply      apply updates from a static Patchline release")
	fmt.Fprintln(os.Stderr, "  keygen     generate an Ed25519 signing keypair")
	fmt.Fprintln(os.Stderr, "  publish    publish a build directory to local content-addressed storage")
	return nil
}
