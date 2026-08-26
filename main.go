package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	switch cmd {
	case "images":
		if err := runImages(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "fetch":
		if err := runFetch(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "version", "--version":
		if err := runVersion(); err != nil {
			fatal(err)
		}
	case "self-update":
		if err := runSelfUpdate(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "uninstall":
		if err := runUninstall(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fatal(fmt.Errorf("unknown command: %s", cmd))
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `quadwatch finds container image updates in Quadlet files.

Usage:
  quadwatch images      [--format json|csv|table] [--color auto|always|never] PATH
  quadwatch fetch       [--format json|csv|table] [--color auto|always|never] [--all] [--check-digest-pinned] [--config PATH] [--progress] [--resolve-digests] PATH
  quadwatch self-update
  quadwatch uninstall
  quadwatch version

Commands:
  images       List detected images and current tags
  fetch        List images with updates and newest compatible remote tags
  self-update  Update quadwatch to the latest GitHub release
  uninstall    Remove the current quadwatch binary
  version      Print the quadwatch version

Options:
  --all          Show all images, including those without updates
  --check-digest-pinned
                 Check digest-pinned images for newer tags
  --color MODE   Colorize human-readable output: auto, always, never (default auto)
  --config PATH  YAML config file (defaults to ./quadwatch.yaml, then XDG config)
  --progress     Show remote lookup progress on stderr
  --resolve-digests
                 Resolve the top-level digest for each newer tag
`)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
