package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/ethan-mdev/patchline/pkg/client"
	"github.com/ethan-mdev/patchline/pkg/config"
	"github.com/ethan-mdev/patchline/pkg/publisher"
	"github.com/ethan-mdev/patchline/pkg/releaseops"
	"github.com/ethan-mdev/patchline/pkg/signing"
	"github.com/ethan-mdev/patchline/pkg/storage"
	localstorage "github.com/ethan-mdev/patchline/pkg/storage/local"
	s3storage "github.com/ethan-mdev/patchline/pkg/storage/s3"
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
	case "doctor":
		return doctor(args[1:])
	case "gc":
		return gc(args[1:])
	case "keygen":
		return keygen(args[1:])
	case "promote":
		return move(args[1:], "promote")
	case "publish":
		return publish(args[1:])
	case "rollback":
		return move(args[1:], "rollback")
	case "verify":
		return verify(args[1:])
	case "help", "-h", "--help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func publish(args []string) error {
	flags := flag.NewFlagSet("publish", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "patchline.yaml", "config file path")
	appID := flags.String("app-id", "", "application id")
	version := flags.String("version", "", "release version")
	channel := flags.String("channel", "", "release channel")
	output := flags.String("output", "", "local publish output directory")
	signingKey := flags.String("signing-key", "", "Ed25519 private signing key path")
	unsignedDev := flags.Bool("unsigned-dev", false, "publish without signing; development only")
	jsonOut := flags.Bool("json", false, "write machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: patchline publish [flags] <build-dir>")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	resolveCommon(cfg, appID, channel, output, signingKey, nil)

	signer, err := signerFromPath(*signingKey)
	if err != nil {
		return err
	}
	backend, err := backendFromConfig(cfg, *output)
	if err != nil {
		return err
	}
	result, err := publisher.Publish(context.Background(), backend, flags.Arg(0), publisher.Options{
		AppID:       *appID,
		Version:     *version,
		Channel:     defaultString(*channel, "beta"),
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
	configPath := flags.String("config", "patchline.yaml", "config file path")
	appID := flags.String("app-id", "", "application id")
	channel := flags.String("channel", "", "release channel")
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
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	resolveCommon(cfg, appID, channel, nil, nil, publicKeyPath)
	if *baseURL == "" {
		*baseURL = cfg.BaseURL
	}
	if *installDir == "" {
		*installDir = cfg.InstallDir
	}

	verifier, err := verifierFromPath(*publicKeyPath)
	if err != nil {
		return err
	}
	c, err := client.New(client.Config{
		AppID:               *appID,
		Channel:             defaultString(*channel, "beta"),
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

func verify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "patchline.yaml", "config file path")
	version := flags.String("version", "", "release version to verify")
	channel := flags.String("channel", "", "channel to verify")
	output := flags.String("output", "", "local release output directory")
	publicKeyPath := flags.String("public-key", "", "Ed25519 public verification key path")
	unsignedDev := flags.Bool("unsigned-dev", false, "allow unsigned manifests; development only")
	jsonOut := flags.Bool("json", false, "write machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: patchline verify [flags]")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	resolveCommon(cfg, nil, channel, output, nil, publicKeyPath)
	if *version == "" && *channel == "" {
		*channel = defaultString(cfg.Channel, "beta")
	}
	backend, err := backendFromConfig(cfg, *output)
	if err != nil {
		return err
	}
	verifier, err := verifierFromPath(*publicKeyPath)
	if err != nil {
		return err
	}
	result, err := releaseops.Verify(context.Background(), releaseops.VerifyOptions{
		Backend:          backend,
		Version:          *version,
		Channel:          *channel,
		Verifier:         verifier,
		AllowUnsignedDev: *unsignedDev,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	fmt.Fprintf(os.Stdout, "verified %s from %s\n", result.Manifest.Version, result.Manifest.Channel)
	fmt.Fprintf(os.Stdout, "objects checked: %d\n", result.ObjectsChecked)
	return nil
}

func move(args []string, action string) error {
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "patchline.yaml", "config file path")
	version := flags.String("version", "", "release version")
	channel := flags.String("channel", "", "target channel")
	output := flags.String("output", "", "local release output directory")
	signingKey := flags.String("signing-key", "", "Ed25519 private signing key path")
	unsignedDev := flags.Bool("unsigned-dev", false, "write unsigned channel manifest; development only")
	jsonOut := flags.Bool("json", false, "write machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: patchline %s [flags]", action)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	resolveCommon(cfg, nil, channel, output, signingKey, nil)
	if *version == "" {
		return fmt.Errorf("version is required")
	}
	if *channel == "" {
		*channel = defaultString(cfg.Channel, "stable")
	}
	backend, err := backendFromConfig(cfg, *output)
	if err != nil {
		return err
	}
	signer, err := signerFromPath(*signingKey)
	if err != nil {
		return err
	}
	opts := releaseops.MoveOptions{
		Backend:     backend,
		Version:     *version,
		Channel:     *channel,
		Signer:      signer,
		UnsignedDev: *unsignedDev,
	}
	var result *releaseops.MoveResult
	if action == "rollback" {
		result, err = releaseops.Rollback(context.Background(), opts)
	} else {
		result, err = releaseops.Promote(context.Background(), opts)
	}
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if action == "rollback" {
		fmt.Fprintf(os.Stdout, "rolled back %s to %s\n", result.Manifest.Channel, result.Manifest.Version)
	} else {
		fmt.Fprintf(os.Stdout, "promoted %s to %s\n", result.Manifest.Version, result.Manifest.Channel)
	}
	fmt.Fprintf(os.Stdout, "release_sequence: %d\n", result.Manifest.ReleaseSequence)
	return nil
}

func gc(args []string) error {
	flags := flag.NewFlagSet("gc", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "patchline.yaml", "config file path")
	output := flags.String("output", "", "local release output directory")
	dryRun := flags.Bool("dry-run", false, "show deletions without removing objects")
	jsonOut := flags.Bool("json", false, "write machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: patchline gc [flags]")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	resolveCommon(cfg, nil, nil, output, nil, nil)
	backend, err := backendFromConfig(cfg, *output)
	if err != nil {
		return err
	}
	result, err := releaseops.GC(context.Background(), releaseops.GCOptions{Backend: backend, DryRun: *dryRun})
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if *dryRun {
		fmt.Fprintf(os.Stdout, "would delete %d unreferenced objects\n", len(result.Deleted))
	} else {
		fmt.Fprintf(os.Stdout, "deleted %d unreferenced objects\n", len(result.Deleted))
	}
	fmt.Fprintf(os.Stdout, "kept %d objects referenced by manifests\n", len(result.Kept))
	return nil
}

func doctor(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "patchline.yaml", "config file path")
	output := flags.String("output", "", "local release output directory")
	signingKey := flags.String("signing-key", "", "Ed25519 private signing key path")
	publicKeyPath := flags.String("public-key", "", "Ed25519 public verification key path")
	jsonOut := flags.Bool("json", false, "write machine-readable output")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: patchline doctor [flags]")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	resolveCommon(cfg, nil, nil, output, signingKey, publicKeyPath)
	backend, err := backendFromConfig(cfg, *output)
	if err != nil {
		return err
	}
	result, err := releaseops.Doctor(context.Background(), releaseops.DoctorOptions{
		Backend:        backend,
		SigningKeyPath: *signingKey,
		PublicKeyPath:  *publicKeyPath,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if result.OK {
		fmt.Fprintln(os.Stdout, "doctor ok")
	} else {
		fmt.Fprintln(os.Stdout, "doctor found problems")
	}
	for _, check := range result.Checks {
		fmt.Fprintf(os.Stdout, "ok: %s\n", check)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(os.Stdout, "warn: %s\n", warning)
	}
	if !result.OK {
		return fmt.Errorf("doctor failed")
	}
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
	fmt.Fprintln(os.Stderr, "  doctor     check config, keys, and storage health")
	fmt.Fprintln(os.Stderr, "  gc         delete unreferenced content-addressed objects")
	fmt.Fprintln(os.Stderr, "  keygen     generate an Ed25519 signing keypair")
	fmt.Fprintln(os.Stderr, "  promote    move a channel to an existing release version")
	fmt.Fprintln(os.Stderr, "  publish    publish a build directory to content-addressed storage")
	fmt.Fprintln(os.Stderr, "  rollback   move a channel back to an existing release version")
	fmt.Fprintln(os.Stderr, "  verify     verify a release or channel manifest and objects")
	return nil
}

func backendFromConfig(cfg *config.Config, output string) (storage.Backend, error) {
	backendType := "local"
	if cfg != nil && cfg.Backend.Type != "" {
		backendType = cfg.Backend.Type
	}
	path := output
	if path == "" && cfg != nil {
		path = cfg.Backend.Path
	}
	if path == "" {
		path = "./release-output"
	}
	switch backendType {
	case "local":
		return localstorage.New(path), nil
	case "s3", "s3-compatible":
		if cfg == nil {
			return nil, fmt.Errorf("s3 backend requires config")
		}
		return s3storage.New(context.Background(), s3storage.Config{
			Bucket:         cfg.Backend.Bucket,
			Region:         cfg.Backend.Region,
			Endpoint:       cfg.Backend.Endpoint,
			Prefix:         cfg.Backend.Prefix,
			ForcePathStyle: cfg.Backend.ForcePathStyle,
		})
	default:
		return nil, fmt.Errorf("unsupported backend type %q", backendType)
	}
}

func resolveCommon(cfg *config.Config, appID *string, channel *string, output *string, signingKey *string, publicKey *string) {
	if cfg == nil {
		return
	}
	if appID != nil && *appID == "" {
		*appID = cfg.AppID
	}
	if channel != nil && *channel == "" {
		*channel = cfg.Channel
	}
	if output != nil && *output == "" {
		*output = cfg.Backend.Path
	}
	if signingKey != nil && *signingKey == "" {
		*signingKey = cfg.SigningKey
	}
	if publicKey != nil && *publicKey == "" {
		*publicKey = cfg.PublicKey
	}
}

func signerFromPath(path string) (publisher.PayloadSigner, error) {
	if path == "" {
		return nil, nil
	}
	privateKey, err := signing.ReadPrivateKey(path)
	if err != nil {
		return nil, err
	}
	return signing.NewSigner(privateKey)
}

func verifierFromPath(path string) (client.ManifestVerifier, error) {
	if path == "" {
		return nil, nil
	}
	publicKey, err := signing.ReadPublicKey(path)
	if err != nil {
		return nil, err
	}
	return signing.NewVerifier(publicKey)
}

func defaultString(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
