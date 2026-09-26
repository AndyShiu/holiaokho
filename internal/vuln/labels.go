package vuln

import (
	"sort"
	"strings"
)

// Labels for the reports people read (PDF, Excel). CSV and JSON keep fixed
// English keys: they are read by programs, which need names that do not
// change with whoever exported them.
//
// The PDF embeds a font cut down to exactly the characters used here plus
// Latin text. TestPDFFontsCoverLabels fails when a label gains a character
// the fonts lack; scripts/pdf-fonts.sh regenerates them.
var labels = map[string]map[string]string{
	"en": {
		"lastUsed":    "Last used",
		"notInFilter": "not in this report", "partial": "This report is filtered and does not show every finding.",
		"title": "Vulnerability report", "generated": "Generated", "source": "Source", "version": "Holiaokho",
		"filters": "Filters", "none": "none", "coverage": "Coverage",
		"scanned": "Packages checked", "pending": "Waiting to be checked", "notCovered": "Not covered by OSV",
		"excluded": "In repositories with scanning off", "lastScan": "Last checked",
		"coverageNote": "Packages OSV has no data for are not in this report. That does not make them safe.",
		"summary":      "Summary", "packages": "Packages affected", "vulnerabilities": "Vulnerabilities",
		"package": "Package", "pkgVersion": "Version", "repositories": "Repositories", "severity": "Severity",
		"upgradeTo": "Upgrade to", "fixesAll": "Fixes all", "yes": "yes", "noFixFor": "no fix yet for some",
		"vulnerability": "Vulnerability", "aliases": "Also known as", "cvss": "CVSS", "vulnSummary": "Summary",
		"fixedIn": "Fixed in", "fixTo": "Fix from this version", "published": "Published", "firstSeen": "First seen",
		"link": "Details", "sheetPackages": "Packages", "sheetFindings": "Vulnerabilities", "page": "Page",
		"empty":    "No known vulnerabilities in the packages checked.",
		"CRITICAL": "Critical", "HIGH": "High", "MODERATE": "Moderate", "LOW": "Low", "UNKNOWN": "Unrated",
	},
	"zh-TW": {
		"lastUsed":    "最後使用",
		"notInFilter": "不在此報告範圍", "partial": "這份報告套用了篩選條件，並未列出所有漏洞。",
		"title": "漏洞報告", "generated": "產生時間", "source": "資料來源", "version": "好料庫",
		"filters": "篩選條件", "none": "無", "coverage": "檢查範圍",
		"scanned": "已檢查的套件", "pending": "等待檢查", "notCovered": "OSV 不支援",
		"excluded": "位於已關閉掃描的 repository", "lastScan": "上次檢查",
		"coverageNote": "OSV 沒有資料的套件不在這份報告中，這不代表它們是安全的。",
		"summary":      "摘要", "packages": "受影響的套件", "vulnerabilities": "漏洞",
		"package": "套件", "pkgVersion": "版本", "repositories": "所在 repository", "severity": "嚴重度",
		"upgradeTo": "建議升級到", "fixesAll": "可修復全部", "yes": "是", "noFixFor": "部分漏洞尚無修復版本",
		"vulnerability": "漏洞", "aliases": "其他編號", "cvss": "CVSS", "vulnSummary": "摘要",
		"fixedIn": "修復版本", "fixTo": "從目前版本修復", "published": "公開日期", "firstSeen": "發現時間",
		"link": "詳細資料", "sheetPackages": "套件", "sheetFindings": "漏洞明細", "page": "頁",
		"empty":    "已檢查的套件中沒有已知漏洞。",
		"CRITICAL": "嚴重", "HIGH": "高", "MODERATE": "中", "LOW": "低", "UNKNOWN": "未評級",
	},
	"zh-CN": {
		"lastUsed":    "最后使用",
		"notInFilter": "不在此报告范围", "partial": "这份报告应用了筛选条件，并未列出所有漏洞。",
		"title": "漏洞报告", "generated": "生成时间", "source": "数据来源", "version": "好料库",
		"filters": "筛选条件", "none": "无", "coverage": "检查范围",
		"scanned": "已检查的包", "pending": "等待检查", "notCovered": "OSV 不支持",
		"excluded": "位于已关闭扫描的仓库", "lastScan": "上次检查",
		"coverageNote": "OSV 没有数据的包不在这份报告中，这不代表它们是安全的。",
		"summary":      "摘要", "packages": "受影响的包", "vulnerabilities": "漏洞",
		"package": "包", "pkgVersion": "版本", "repositories": "所在仓库", "severity": "严重度",
		"upgradeTo": "建议升级到", "fixesAll": "可修复全部", "yes": "是", "noFixFor": "部分漏洞尚无修复版本",
		"vulnerability": "漏洞", "aliases": "其他编号", "cvss": "CVSS", "vulnSummary": "摘要",
		"fixedIn": "修复版本", "fixTo": "从当前版本修复", "published": "公开日期", "firstSeen": "发现时间",
		"link": "详细信息", "sheetPackages": "包", "sheetFindings": "漏洞明细", "page": "页",
		"empty":    "已检查的包中没有已知漏洞。",
		"CRITICAL": "严重", "HIGH": "高", "MODERATE": "中", "LOW": "低", "UNKNOWN": "未评级",
	},
	"ja": {
		"lastUsed":    "最終利用",
		"notInFilter": "このレポートの対象外", "partial": "このレポートは絞り込まれており、すべての検出結果を示していません。",
		"title": "脆弱性レポート", "generated": "作成日時", "source": "データ提供", "version": "Holiaokho",
		"filters": "絞り込み条件", "none": "なし", "coverage": "チェック範囲",
		"scanned": "チェック済みパッケージ", "pending": "チェック待ち", "notCovered": "OSV の対象外",
		"excluded": "スキャン無効のリポジトリ内", "lastScan": "最終チェック",
		"coverageNote": "OSV にデータがないパッケージはこのレポートに含まれません。安全という意味ではありません。",
		"summary":      "概要", "packages": "影響を受けるパッケージ", "vulnerabilities": "脆弱性",
		"package": "パッケージ", "pkgVersion": "バージョン", "repositories": "リポジトリ", "severity": "深刻度",
		"upgradeTo": "推奨アップグレード先", "fixesAll": "すべて修正", "yes": "はい", "noFixFor": "一部は修正版なし",
		"vulnerability": "脆弱性", "aliases": "別名", "cvss": "CVSS", "vulnSummary": "概要",
		"fixedIn": "修正バージョン", "fixTo": "このバージョンからの修正", "published": "公開日", "firstSeen": "検出日時",
		"link": "詳細", "sheetPackages": "パッケージ", "sheetFindings": "脆弱性一覧", "page": "ページ",
		"empty":    "チェック済みのパッケージに既知の脆弱性はありません。",
		"CRITICAL": "緊急", "HIGH": "高", "MODERATE": "中", "LOW": "低", "UNKNOWN": "未評価",
	},
	"ko": {
		"lastUsed":    "마지막 사용",
		"notInFilter": "이 보고서 범위 밖", "partial": "필터가 적용된 보고서로, 모든 취약점을 보여 주지 않습니다.",
		"title": "취약점 보고서", "generated": "생성 시각", "source": "데이터 출처", "version": "Holiaokho",
		"filters": "필터", "none": "없음", "coverage": "확인 범위",
		"scanned": "확인된 패키지", "pending": "확인 대기", "notCovered": "OSV 대상 아님",
		"excluded": "스캔이 꺼진 리포지터리", "lastScan": "마지막 확인",
		"coverageNote": "OSV에 데이터가 없는 패키지는 이 보고서에 없습니다. 안전하다는 뜻은 아닙니다.",
		"summary":      "요약", "packages": "영향받는 패키지", "vulnerabilities": "취약점",
		"package": "패키지", "pkgVersion": "버전", "repositories": "리포지터리", "severity": "심각도",
		"upgradeTo": "권장 업그레이드", "fixesAll": "모두 수정", "yes": "예", "noFixFor": "일부는 수정 버전 없음",
		"vulnerability": "취약점", "aliases": "다른 ID", "cvss": "CVSS", "vulnSummary": "요약",
		"fixedIn": "수정 버전", "fixTo": "현재 버전에서 수정", "published": "공개일", "firstSeen": "발견 시각",
		"link": "자세히", "sheetPackages": "패키지", "sheetFindings": "취약점 목록", "page": "페이지",
		"empty":    "확인된 패키지에 알려진 취약점이 없습니다.",
		"CRITICAL": "심각", "HIGH": "높음", "MODERATE": "보통", "LOW": "낮음", "UNKNOWN": "등급 없음",
	},
}

// Lang maps a UI language to one the reports have, English otherwise.
func Lang(l string) string {
	l = strings.ToLower(l)
	switch {
	case strings.HasPrefix(l, "zh-cn"), strings.HasPrefix(l, "zh-hans"), l == "zh-sg":
		return "zh-CN"
	case strings.HasPrefix(l, "zh"):
		return "zh-TW"
	case strings.HasPrefix(l, "ja"):
		return "ja"
	case strings.HasPrefix(l, "ko"):
		return "ko"
	}
	return "en"
}

func label(lang, key string) string {
	if v, ok := labels[lang][key]; ok {
		return v
	}
	return labels["en"][key]
}

// LabelRunes lists the non-Latin characters a language's labels use, for
// cutting the PDF fonts down to size.
func LabelRunes(lang string) string {
	set := map[rune]bool{}
	for _, v := range labels[lang] {
		for _, r := range v {
			if r > 0x24F { // beyond Latin Extended-B, which the fonts keep whole
				set[r] = true
			}
		}
	}
	out := make([]rune, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return string(out)
}
