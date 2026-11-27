//go:build windows

package main

import "os"

func pathOwner(os.FileInfo) string { return "unknown" }
