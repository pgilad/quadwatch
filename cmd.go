package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
)

func runImages(args []string) error {
	fs := flag.NewFlagSet("images", flag.ExitOnError)
	format := fs.String("format", "table", "output format: json, csv, table")
	colorMode := fs.String("color", colorAuto, "color output: auto, always, never")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := pathArg(fs)
	if err != nil {
		return err
	}
	images, err := scanImages(path)
	if err != nil {
		return err
	}
	colors, err := newColors(*colorMode, os.Stdout)
	if err != nil {
		return err
	}
	return outputImages(images, *format, colors)
}

func runFetch(args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	format := fs.String("format", "table", "output format: json, csv, table")
	colorMode := fs.String("color", colorAuto, "color output: auto, always, never")
	all := fs.Bool("all", false, "show all images, including those without updates")
	configPath := fs.String("config", "", "YAML config file")
	progress := fs.Bool("progress", false, "show remote lookup progress on stderr")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := pathArg(fs)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	images, err := scanImages(path)
	if err != nil {
		return err
	}
	stdoutColors, err := newColors(*colorMode, os.Stdout)
	if err != nil {
		return err
	}
	stderrColors, err := newColors(*colorMode, os.Stderr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	updates := fetchUpdates(ctx, images, cfg, *progress, stderrColors)
	if !*all {
		updates = updatesWithAvailableUpdates(updates)
	}
	return outputUpdates(updates, *format, stdoutColors)
}
func pathArg(fs *flag.FlagSet) (string, error) {
	switch fs.NArg() {
	case 0:
		return ".", nil
	case 1:
		return fs.Arg(0), nil
	default:
		return "", fmt.Errorf("too many arguments: %s", strings.Join(fs.Args()[1:], " "))
	}
}
