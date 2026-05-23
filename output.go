package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
)

func outputImages(images []Image, format string, colors colors) error {
	switch format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(images)
	case "csv":
		w := csv.NewWriter(os.Stdout)
		defer w.Flush()
		if err := w.Write([]string{"quadlet", "image_name", "current_tag"}); err != nil {
			return err
		}
		for _, i := range images {
			if err := w.Write([]string{i.File, i.Repository, i.Tag}); err != nil {
				return err
			}
		}
		return w.Error()
	case "table":
		rows := make([][]string, 0, len(images))
		for _, i := range images {
			rows = append(rows, []string{i.File, i.Repository, i.Tag})
		}
		return writeTable(os.Stdout, []string{"QUADLET", "IMAGE", "TAG"}, rows, colors, func(_ int, col int, text string) string {
			switch col {
			case 0:
				return colors.dim(text)
			case 1:
				return colors.cyan(text)
			case 2:
				return colors.yellow(text)
			default:
				return text
			}
		})
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func outputUpdates(updates []Update, format string, colors colors) error {
	switch format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(updates)
	case "csv":
		w := csv.NewWriter(os.Stdout)
		defer w.Flush()
		if err := w.Write([]string{"quadlet", "image_name", "current_tag", "newest_tag", "update", "error"}); err != nil {
			return err
		}
		for _, u := range updates {
			if err := w.Write([]string{u.File, u.Repository, u.CurrentTag, u.NewestTag, fmt.Sprint(u.Update), u.Error}); err != nil {
				return err
			}
		}
		return w.Error()
	case "table":
		if len(updates) == 0 {
			fmt.Fprintln(os.Stdout, colors.green("No images have an update."))
			return nil
		}
		rows := make([][]string, 0, len(updates))
		for _, u := range updates {
			rows = append(rows, []string{u.File, u.Repository, u.CurrentTag, u.NewestTag, u.Error})
		}
		return writeTable(os.Stdout, []string{"QUADLET", "IMAGE", "CURRENT", "NEWEST", "ERROR"}, rows, colors, func(row int, col int, text string) string {
			u := updates[row]
			switch col {
			case 0:
				return colors.dim(text)
			case 1:
				return colors.cyan(text)
			case 2:
				return colors.yellow(text)
			case 3:
				if u.Error != "" {
					return colors.red(text)
				}
				if u.Update {
					return colors.brightGreen(text)
				}
				return colors.dim(text)
			case 4:
				if u.Error != "" {
					return colors.red(text)
				}
				return colors.dim(text)
			default:
				return text
			}
		})
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}
