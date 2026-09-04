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

type editAction string

const (
	editActionUpdate editAction = "update"
	editActionPin    editAction = "pin"
)

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

func applyEdits(scan imageScan, edits []Edit, dryRun bool, action editAction, output io.Writer) error {
	if len(edits) == 0 {
		return outputEdits(output, edits, dryRun, action)
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
		return outputEdits(output, edits, true, action)
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
	return outputEdits(output, edits, false, action)
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

func outputEdits(output io.Writer, edits []Edit, dryRun bool, action editAction) error {
	if len(edits) == 0 {
		_, err := fmt.Fprintln(output, "No changes.")
		return err
	}

	grouped := make(map[string][]Edit)
	fileOrder := make([]string, 0)
	for _, edit := range edits {
		if _, found := grouped[edit.File]; !found {
			fileOrder = append(fileOrder, edit.File)
		}
		grouped[edit.File] = append(grouped[edit.File], edit)
	}

	verb := "Updated"
	if action == editActionPin {
		verb = "Pinned"
	}
	if dryRun {
		verb = "Would " + string(action)
	}
	if _, err := fmt.Fprintf(output, "%s %d %s in %d %s:\n\n", verb, len(edits), plural(len(edits), "image", "images"), len(fileOrder), plural(len(fileOrder), "file", "files")); err != nil {
		return err
	}

	for index, file := range fileOrder {
		if index > 0 {
			if _, err := fmt.Fprintln(output); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(output, "%s:\n", file); err != nil {
			return err
		}
		for _, edit := range grouped[file] {
			if err := outputEditDetails(output, edit); err != nil {
				return err
			}
		}
	}
	return nil
}

type imageReferenceDetails struct {
	name   string
	tag    string
	digest string
}

func outputEditDetails(output io.Writer, edit Edit) error {
	oldReference := describeImageReference(edit.Old)
	newReference := describeImageReference(edit.New)
	name := oldReference.name
	if oldReference.name != newReference.name {
		name += " → " + newReference.name
	}
	if _, err := fmt.Fprintf(output, "  %s\n", name); err != nil {
		return err
	}
	if oldReference.tag != newReference.tag {
		if _, err := fmt.Fprintf(output, "    tag     %s → %s\n", oldReference.tag, newReference.tag); err != nil {
			return err
		}
	}
	if oldReference.digest != newReference.digest {
		if _, err := fmt.Fprintf(output, "    digest  %s → %s\n", displayDigest(oldReference.digest), displayDigest(newReference.digest)); err != nil {
			return err
		}
	}
	return nil
}

func describeImageReference(reference string) imageReferenceDetails {
	_, reference = splitImageTransport(reference)
	nameAndTag, digest, _ := strings.Cut(reference, "@")
	name := nameAndTag
	tag := "latest"
	lastSlash := strings.LastIndex(nameAndTag, "/")
	if lastColon := strings.LastIndex(nameAndTag, ":"); lastColon > lastSlash {
		name = nameAndTag[:lastColon]
		tag = nameAndTag[lastColon+1:]
	}
	return imageReferenceDetails{name: name, tag: tag, digest: digest}
}

func displayDigest(digest string) string {
	if digest == "" {
		return "unpinned"
	}
	algorithm, encoded, found := strings.Cut(digest, ":")
	if !found || len(encoded) <= 12 {
		return digest
	}
	return algorithm + ":" + encoded[:12] + "…"
}

func plural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
