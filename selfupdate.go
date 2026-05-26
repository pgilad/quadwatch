package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	repoSlug           = "pgilad/quadwatch"
	installURL         = "https://raw.githubusercontent.com/pgilad/quadwatch/main/scripts/install.sh"
	versionDev         = "dev"
	httpRequestTimeout = 30 * time.Second
)

var httpClient = &http.Client{Timeout: httpRequestTimeout}

var version = versionDev

func runVersion() error {
	_, err := fmt.Fprintln(os.Stdout, version)
	return err
}

func runSelfUpdate(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("self-update does not accept arguments")
	}

	latest, err := latestReleaseTag()
	if err != nil {
		return err
	}
	if version != versionDev && version == latest {
		_, err := fmt.Fprintf(os.Stdout, "quadwatch is already up to date (%s)\n", version)
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	installDir := filepath.Dir(exe)

	if _, err := fmt.Fprintf(os.Stderr, "Updating quadwatch from %s to %s\n", version, latest); err != nil {
		return err
	}
	installer, err := downloadInstaller()
	if err != nil {
		return err
	}

	cmd := exec.Command("sh", "-s", "--", "--version", latest, "--dir", installDir)
	cmd.Stdin = strings.NewReader(installer)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

func runUninstall(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("uninstall does not accept arguments")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.Remove(exe); err != nil {
		return err
	}
	_, err = fmt.Fprintf(os.Stdout, "removed %s\n", exe)
	return err
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

func latestReleaseTag() (string, error) {
	url := "https://api.github.com/repos/" + repoSlug + "/releases/latest"
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("latest release response did not include tag_name")
	}
	return release.TagName, nil
}

func downloadInstaller() (string, error) {
	resp, err := httpClient.Get(installURL)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GET %s: %s", installURL, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
