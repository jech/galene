// Command extract-i18n regenerates the English source catalog
// (static/locales/en.json) from the strings marked for translation in the
// static frontend sources.
//
// By default, keys that are no longer present in the sources are removed.
// Pass -prune=false to keep them.
//
// Usage:
//
//	go run ./tools/extract-i18n [-prune=false]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	tRegexp    = regexp.MustCompile(`(?:^|[^.\w])(?:t|msg)\(\s*("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')`)
	dataRegexp = regexp.MustCompile(`data-i18n(?:-[a-zA-Z0-9-]+)?\s*=\s*"([^"]*)"`)
)

// pluralSuffixes are the CLDR plural suffixes used by the i18next v4 format.
var pluralSuffixes = []string{"_zero", "_one", "_two", "_few", "_many", "_other"}

const (
	staticRoot = "static"
	outfile    = "static/locales/en.json"
)

// hasPluralSibling reports whether existing contains a plural form of key.
func hasPluralSibling(existing map[string]string, key string) bool {
	for _, suffix := range pluralSuffixes {
		if _, ok := existing[key+suffix]; ok {
			return true
		}
	}
	return false
}

// unquoteJS converts a JavaScript string literal (including its quotes) to
// its value.
func unquoteJS(s string) (string, error) {
	if len(s) < 2 {
		return "", fmt.Errorf("bad literal %q", s)
	}
	if s[0] == '"' {
		return strconv.Unquote(s)
	}
	inner := s[1 : len(s)-1]
	inner = strings.ReplaceAll(inner, `\'`, `'`)
	inner = strings.ReplaceAll(inner, `\\`, `\`)
	return inner, nil
}

func collect(keys map[string]bool, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(data)

	for _, m := range tRegexp.FindAllStringSubmatch(s, -1) {
		k, err := unquoteJS(m[1])
		if err == nil && k != "" {
			keys[k] = true
		}
	}
	for _, m := range dataRegexp.FindAllStringSubmatch(s, -1) {
		if m[1] != "" {
			keys[m[1]] = true
		}
	}
	return nil
}

func main() {
	prune := flag.Bool("prune", true,
		"remove keys that are no longer present in the sources")
	flag.Parse()

	keys := map[string]bool{}

	err := filepath.WalkDir(staticRoot,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "third-party", "example", "locales":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".js") &&
				!strings.HasSuffix(path, ".html") {
				return nil
			}
			return collect(keys, path)
		})
	if err != nil {
		fmt.Fprintln(os.Stderr, "extract-i18n:", err)
		os.Exit(1)
	}

	existing := map[string]string{}
	if data, err := os.ReadFile(outfile); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			fmt.Fprintln(os.Stderr, "extract-i18n:", err)
			os.Exit(1)
		}
	}

	// A key is kept when it is referenced in the sources, either directly
	// or as a plural form of a referenced key.
	keep := map[string]bool{}
	for k := range keys {
		keep[k] = true
		for _, suffix := range pluralSuffixes {
			if _, ok := existing[k+suffix]; ok {
				keep[k+suffix] = true
			}
		}
	}

	added := 0
	for k := range keys {
		if _, ok := existing[k]; ok {
			continue
		}
		if hasPluralSibling(existing, k) {
			continue
		}
		existing[k] = k
		added++
	}

	removed := 0
	if *prune {
		for k := range existing {
			if !keep[k] {
				delete(existing, k)
				removed++
			}
		}
	}

	list := make([]string, 0, len(existing))
	for k := range existing {
		list = append(list, k)
	}
	sort.Strings(list)

	// Rebuild in sorted order; encoding/json sorts map keys, but we build
	// an ordered structure to be explicit.
	ordered := make([]byte, 0, 1024)
	ordered = append(ordered, '{')
	for i, k := range list {
		v, err := json.Marshal(existing[k])
		if err != nil {
			fmt.Fprintln(os.Stderr, "extract-i18n:", err)
			os.Exit(1)
		}
		kb, _ := json.Marshal(k)
		if i > 0 {
			ordered = append(ordered, ',')
		}
		ordered = append(ordered, '\n', ' ', ' ', ' ', ' ')
		ordered = append(ordered, kb...)
		ordered = append(ordered, ':', ' ')
		ordered = append(ordered, v...)
	}
	if len(list) > 0 {
		ordered = append(ordered, '\n')
	}
	ordered = append(ordered, '}', '\n')

	if err := os.MkdirAll(filepath.Dir(outfile), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "extract-i18n:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outfile, ordered, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "extract-i18n:", err)
		os.Exit(1)
	}

	fmt.Printf("extract-i18n: %d keys, %d added, %d removed\n",
		len(existing), added, removed)
}
