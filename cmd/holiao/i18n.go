package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//go:embed locales/*.json
var localeFS embed.FS

var messages map[string]string

// initLocale picks the message table from HOLIAO_LANG, then LANG/LC_ALL.
// Supported: en, zh-TW, zh-CN, ja, ko (missing tables fall back to en).
func initLocale() {
	lang := os.Getenv("HOLIAO_LANG")
	if lang == "" {
		lang = os.Getenv("LC_ALL")
	}
	if lang == "" {
		lang = os.Getenv("LANG")
	}
	lang = strings.ReplaceAll(strings.SplitN(lang, ".", 2)[0], "_", "-")
	load := func(name string) map[string]string {
		b, err := localeFS.ReadFile("locales/" + name + ".json")
		if err != nil {
			return nil
		}
		var m map[string]string
		json.Unmarshal(b, &m)
		return m
	}
	messages = load("en")
	for _, cand := range []string{lang, strings.SplitN(lang, "-", 2)[0]} {
		if cand == "" || cand == "en" {
			continue
		}
		if m := load(cand); m != nil {
			for k, v := range m {
				messages[k] = v
			}
			break
		}
	}
}

// T formats a localised message by key.
func T(key string, args ...any) string {
	s, ok := messages[key]
	if !ok {
		s = key
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}
