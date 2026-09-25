// Command pdffonts prints, per report language, the characters its labels
// use beyond Latin. scripts/pdf-fonts.sh feeds this to the font subsetter.
package main

import (
	"fmt"

	"github.com/holiaokho/holiaokho/internal/vuln"
)

func main() {
	for _, l := range []string{"zh-TW", "zh-CN", "ja", "ko"} {
		fmt.Printf("%s\t%s\n", l, vuln.LabelRunes(l))
	}
}
