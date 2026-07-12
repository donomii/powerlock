package main

import (
	"bytes"
	"fmt"
	"go/format"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if err := checkFormatting("."); err != nil {
		log.Fatal(err)
	}
}

func checkFormatting(root string) error {
	unformatted := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("inspect %s: %w", path, walkErr)
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read Go source %s: %w", path, err)
		}
		formatted, err := format.Source(source)
		if err != nil {
			return fmt.Errorf("format Go source %s: %w", path, err)
		}
		if !bytes.Equal(source, formatted) {
			absolutePath, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("resolve absolute path for %s: %w", path, err)
			}
			unformatted = append(unformatted, absolutePath)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(unformatted) > 0 {
		sort.Strings(unformatted)
		return fmt.Errorf("Go source requires formatting:\n%s", strings.Join(unformatted, "\n"))
	}
	return nil
}
