package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Edit struct {
	File  string
	Start int
	End   int
	Old   string
	New   string
}

type pendingWrite struct {
	file     string
	tempPath string
}

func rewriteImageReference(img Image, tag, digest string) (string, error) {
	if img.Source.Original == "" {
		return "", fmt.Errorf("image %s in %s has no editable source", img.Image, img.File)
	}

	prefix, reference := splitImageTransport(img.Source.Original)
	namePart, _, _ := strings.Cut(reference, "@")
	lastSlash := strings.LastIndex(namePart, "/")
	lastColon := strings.LastIndex(namePart, ":")
	if lastColon > lastSlash {
		namePart = namePart[:lastColon]
	}
	if tag == "" {
		return "", fmt.Errorf("image %s in %s has no tag", img.Image, img.File)
	}

	rewritten := prefix + namePart + ":" + tag
	if digest != "" {
		rewritten += "@" + digest
	}
	return rewritten, nil
}

func splitImageTransport(reference string) (string, string) {
	for _, prefix := range []string{"docker://", "docker-daemon:"} {
		if strings.HasPrefix(reference, prefix) {
			return prefix, strings.TrimPrefix(reference, prefix)
		}
	}
	return "", reference
}

func applyEdits(scan imageScan, edits []Edit, dryRun bool, output io.Writer) error {
	if len(edits) == 0 {
		return nil
	}

	grouped := make(map[string][]Edit)
	fileOrder := make([]string, 0)
	for _, edit := range edits {
		if _, found := grouped[edit.File]; !found {
			fileOrder = append(fileOrder, edit.File)
		}
		grouped[edit.File] = append(grouped[edit.File], edit)
	}

	contents := make(map[string][]byte, len(grouped))
	for _, file := range fileOrder {
		scanned, found := scan.Files[file]
		if !found {
			return fmt.Errorf("missing scanned contents for %s", file)
		}
		if !scanned.Mode.IsRegular() {
			return fmt.Errorf("refuse to edit non-regular file %s", file)
		}
		currentInfo, err := os.Lstat(file)
		if err != nil {
			return fmt.Errorf("verify %s: %w", file, err)
		}
		if currentInfo.Mode() != scanned.Mode {
			return fmt.Errorf("%s changed after it was scanned", file)
		}
		current, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("verify %s: %w", file, err)
		}
		if !bytes.Equal(current, scanned.Contents) {
			return fmt.Errorf("%s changed after it was scanned", file)
		}
		updated, err := renderEdits(file, scanned.Contents, grouped[file])
		if err != nil {
			return err
		}
		contents[file] = updated
	}

	if dryRun {
		return outputEdits(output, edits, true)
	}

	pending := make([]pendingWrite, 0, len(fileOrder))
	defer func() {
		for _, write := range pending {
			_ = os.Remove(write.tempPath)
		}
	}()
	for _, file := range fileOrder {
		temp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".quadwatch-*")
		if err != nil {
			return fmt.Errorf("create temporary file for %s: %w", file, err)
		}
		tempPath := temp.Name()
		pending = append(pending, pendingWrite{file: file, tempPath: tempPath})
		if _, err := temp.Write(contents[file]); err != nil {
			_ = temp.Close()
			return fmt.Errorf("write temporary file for %s: %w", file, err)
		}
		mode := scan.Files[file].Mode
		permissions := mode.Perm() | mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky)
		if err := temp.Chmod(permissions); err != nil {
			_ = temp.Close()
			return fmt.Errorf("set permissions on temporary file for %s: %w", file, err)
		}
		if err := temp.Sync(); err != nil {
			_ = temp.Close()
			return fmt.Errorf("sync temporary file for %s: %w", file, err)
		}
		if err := temp.Close(); err != nil {
			return fmt.Errorf("close temporary file for %s: %w", file, err)
		}
	}

	for _, write := range pending {
		if err := os.Rename(write.tempPath, write.file); err != nil {
			return fmt.Errorf("replace %s: %w", write.file, err)
		}
	}
	return outputEdits(output, edits, false)
}

func renderEdits(file string, original []byte, edits []Edit) ([]byte, error) {
	sorted := append([]Edit(nil), edits...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Start > sorted[j].Start
	})
	content := append([]byte(nil), original...)
	lastStart := len(original)
	for _, edit := range sorted {
		if edit.Start < 0 || edit.End < edit.Start || edit.End > len(original) {
			return nil, fmt.Errorf("invalid edit range for %s", file)
		}
		if edit.End > lastStart {
			return nil, fmt.Errorf("overlapping edits for %s", file)
		}
		if string(original[edit.Start:edit.End]) != edit.Old {
			return nil, fmt.Errorf("source text changed for %s", file)
		}
		next := make([]byte, 0, len(content)-(edit.End-edit.Start)+len(edit.New))
		next = append(next, content[:edit.Start]...)
		next = append(next, edit.New...)
		next = append(next, content[edit.End:]...)
		content = next
		lastStart = edit.Start
	}
	return content, nil
}

func outputEdits(output io.Writer, edits []Edit, dryRun bool) error {
	verb := "updated"
	if dryRun {
		verb = "would update"
	}
	for _, edit := range edits {
		if _, err := fmt.Fprintf(output, "%s %s: %s -> %s\n", verb, edit.File, edit.Old, edit.New); err != nil {
			return err
		}
	}
	return nil
}
