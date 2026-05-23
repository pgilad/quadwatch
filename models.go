package main

import "time"

type Image struct {
	File       string `json:"file"`
	Image      string `json:"image"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
}

type Update struct {
	File       string        `json:"file"`
	Image      string        `json:"image"`
	Repository string        `json:"repository"`
	CurrentTag string        `json:"currentTag"`
	NewestTag  string        `json:"newestTag"`
	Update     bool          `json:"update"`
	SkipReason string        `json:"skipReason,omitempty"`
	Error      string        `json:"error,omitempty"`
	Duration   time.Duration `json:"-"`
}
