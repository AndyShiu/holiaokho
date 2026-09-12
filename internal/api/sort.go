package api

import (
	"sort"

	"github.com/holiaokho/holiaokho/internal/model"
)

func sortStrings(s []string) { sort.Strings(s) }
func sortAssets(a []*model.Asset) {
	sort.Slice(a, func(i, j int) bool { return a[i].Path < a[j].Path })
}
