package main

import (
	"fmt"
	"os"
	"strings"
)

type tableCellStyle func(row, col int, text string) string

func writeTable(out *os.File, headers []string, rows [][]string, colors colors, style tableCellStyle) error {
	widths := make([]int, len(headers))
	for col, header := range headers {
		widths[col] = displayLen(header)
	}
	for _, row := range rows {
		for col, cell := range row {
			if col >= len(widths) {
				continue
			}
			if width := displayLen(cell); width > widths[col] {
				widths[col] = width
			}
		}
	}

	if err := writeTableRow(out, headers, widths, colors, true, -1, style); err != nil {
		return err
	}
	for rowIndex, row := range rows {
		if err := writeTableRow(out, row, widths, colors, false, rowIndex, style); err != nil {
			return err
		}
	}
	return nil
}

func writeTableRow(out *os.File, cells []string, widths []int, colors colors, header bool, row int, style tableCellStyle) error {
	for col := range widths {
		if col > 0 {
			if _, err := fmt.Fprint(out, "  "); err != nil {
				return err
			}
		}
		text := ""
		if col < len(cells) {
			text = cells[col]
		}
		if col < len(widths)-1 {
			text = padRight(text, widths[col])
		}
		if header {
			text = colors.header(text)
		} else if style != nil {
			text = style(row, col, text)
		}
		if _, err := fmt.Fprint(out, text); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out)
	return err
}

func displayLen(text string) int {
	return len([]rune(text))
}

func padRight(text string, width int) string {
	length := displayLen(text)
	if length >= width {
		return text
	}
	return text + strings.Repeat(" ", width-length)
}
