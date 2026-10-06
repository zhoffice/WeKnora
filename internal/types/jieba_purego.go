//go:build !cgo

package types

import (
	"os"
	"path/filepath"

	"github.com/go-ego/gse"
)

// gseJieba exposes the gojieba methods the code base calls on top of gse, a
// pure-Go segmenter that reads jieba-format dictionaries. Results are close to
// gojieba but not identical (different HMM model and default dictionary).
type gseJieba struct {
	seg gse.Segmenter
}

func newJieba() *gseJieba {
	j := &gseJieba{}
	j.seg.SkipLog = true

	// JIEBA_DICT_DIR keeps working with the same jieba.dict.utf8 / user.dict.utf8
	// files gojieba reads; otherwise use gse's embedded simplified-Chinese
	// dictionary so the binary does not depend on files in the module cache.
	if dictDir := os.Getenv("JIEBA_DICT_DIR"); dictDir != "" {
		dict := filepath.Join(dictDir, "jieba.dict.utf8")
		if user := filepath.Join(dictDir, "user.dict.utf8"); fileExists(user) {
			dict += ", " + user
		}
		if err := j.seg.LoadDict(dict); err == nil {
			return j
		}
		j.seg = gse.Segmenter{SkipLog: true}
	}
	if err := j.seg.LoadDictEmbed("zh_s"); err != nil {
		panic("load embedded gse dictionary: " + err.Error())
	}
	return j
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Cut segments s in accurate mode, like gojieba.Jieba.Cut.
func (j *gseJieba) Cut(s string, hmm bool) []string {
	return j.seg.Cut(s, hmm)
}

// CutForSearch segments s in search-engine mode, like gojieba.Jieba.CutForSearch.
func (j *gseJieba) CutForSearch(s string, hmm bool) []string {
	return j.seg.CutSearch(s, hmm)
}
