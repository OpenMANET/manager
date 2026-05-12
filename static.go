package main

import "embed"

// StaticFS embeds the static/ directory into the binary.
//
//go:embed static/*
var StaticFS embed.FS
