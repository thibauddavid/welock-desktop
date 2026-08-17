//go:build !screenshots

// The real screenshots command lives in main.go behind the `screenshots` build tag so it
// never compiles into the shipped app. This stub keeps `go build ./...` happy without it.
package main

import "fmt"

func main() {
	fmt.Println("build the screenshot tool with: go build -tags \"screenshots tinygobt\" ./cmd/screenshots")
	fmt.Println("or regenerate all screenshots with: tools/screenshots/capture.sh")
}
