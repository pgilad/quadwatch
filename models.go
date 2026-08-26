package main

import "time"

type Image struct {
	File       string `json:"file"`
	Image      string `json:"image"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest,omitempty"`
}

type Update struct {
	File         string        `json:"file"`
	Image        string        `json:"image"`
	Repository   string        `json:"repository"`
	CurrentTag   string        `json:"currentTag"`
	NewestTag    string        `json:"newestTag"`
	NewestDigest string        `json:"newestDigest,omitempty"`
	Update       bool          `json:"update"`
	SkipReason   string        `json:"skipReason,omitempty"`
	Error        string        `json:"error,omitempty"`
	Duration     time.Duration `json:"-"`
}
