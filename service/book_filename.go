package service

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	filenameAdRe       = regexp.MustCompile(`(?i)(?:www\.|http[s]?://|txt\d+|bbs\.|forum\.|bookbao|qidian|zol\.|douban\.com)[^\s]*`)
	bracketAdRe        = regexp.MustCompile(`[【\[\(（][^\]】）)]{0,30}(?:广告|转载|书源|www\.|http|txt\d+|bbs\.|下载|访问|bookbao|qidian|forum\.)[^\]】）)]*[】\]\)）]`)
	bracketTagRe       = regexp.MustCompile(`【[^】]{0,12}】`)
	poweredByDiscuzRe  = regexp.MustCompile(`(?i)\s*-\s*powered by Discuz!?.*$`)
	chapterRangeInRe   = regexp.MustCompile(`[（(]\s*(\d+)\s*[~～\-－—–]+\s*(\d+)\s*[）)]`)
	chapterRangeTailRe = regexp.MustCompile(`^(.+?)\s*[（(]?\s*(\d+)\s*[~～\-－—–]+\s*(\d+)\s*[）)]?\s*$`)
	invalidNameRe      = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)
	multiSpaceRe       = regexp.MustCompile(`\s+`)
	filenameEmojiRe    = regexp.MustCompile(`[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}]`)
	forumPathDashRe    = regexp.MustCompile(`[^\d]--[^\d]`)
)

var (
	contentTitleRe    = regexp.MustCompile(`([\p{Han}A-Za-z0-9·]+[（(]\s*\d+\s*[~～\-－—–]+\s*\d+\s*[）)])`)
	forumSectionWords = []string{"Board", "board", "合集区", "琅環福地", "文学天地", "海岸线", "Oursm"}
)

// CleanBookFileName 优化书名文件名，去掉广告与非法字符
func CleanBookFileName(name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if ext == "" {
		ext = ".txt"
	}

	base = bracketAdRe.ReplaceAllString(base, "")
	base = bracketTagRe.ReplaceAllString(base, "")
	base = poweredByDiscuzRe.ReplaceAllString(base, "")
	base = extractBookTitleFromFilename(base)
	base = filenameAdRe.ReplaceAllString(base, "")
	base = filenameEmojiRe.ReplaceAllString(base, "")
	base = normalizeChapterRangeInName(base)
	base = invalidNameRe.ReplaceAllString(base, "")
	base = strings.Map(func(r rune) rune {
		if r == 0 || (r < 32 && r != ' ') {
			return -1
		}
		return r
	}, base)

	base = multiSpaceRe.ReplaceAllString(base, " ")
	base = strings.TrimSpace(base)
	base = strings.Trim(base, "._-—")
	if base == "" {
		base = "未命名图书"
	}

	if len([]rune(base)) > 120 {
		runes := []rune(base)
		base = string(runes[:120])
	}

	return base + strings.ToLower(ext)
}

// IsTxtFile 是否为 txt 文本
func IsTxtFile(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".txt")
}

func normalizeChapterRangeInName(s string) string {
	s = chapterRangeInRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := chapterRangeInRe.FindStringSubmatch(m)
		if len(parts) != 3 {
			return m
		}
		return formatChapterRange(parts[1], parts[2])
	})

	if m := chapterRangeTailRe.FindStringSubmatch(s); len(m) == 4 {
		title := strings.TrimSpace(m[1])
		if title != "" && !strings.Contains(title, "（") && !strings.Contains(title, "(") {
			s = title + formatChapterRange(m[2], m[3])
		}
	}
	return s
}

func formatChapterRange(start, end string) string {
	return "（" + padChapterNum(start) + "--" + padChapterNum(end) + "）"
}

func padChapterNum(n string) string {
	n = normalizeDigits(strings.TrimSpace(n))
	if n == "" {
		return "00"
	}
	if len(n) == 1 {
		return "0" + n
	}
	return n
}

func normalizeDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= '０' && r <= '９':
			b.WriteRune('0' + (r - '０'))
		case unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func extractChapterRange(s string) (start, end string, ok bool) {
	if m := chapterRangeInRe.FindStringSubmatch(s); len(m) == 3 {
		return normalizeDigits(m[1]), normalizeDigits(m[2]), true
	}
	if m := chapterRangeTailRe.FindStringSubmatch(strings.TrimSpace(s)); len(m) == 4 {
		return normalizeDigits(m[2]), normalizeDigits(m[3]), true
	}
	return "", "", false
}

func stripChapterRange(s string) string {
	s = chapterRangeInRe.ReplaceAllString(s, "")
	s = chapterRangeTailRe.ReplaceAllString(strings.TrimSpace(s), "$1")
	return strings.TrimSpace(s)
}

// InferBookFileName 根据正文与原始文件名推断修复后的新文件名
func InferBookFileName(content, origName string) string {
	ext := filepath.Ext(origName)
	name := origName
	lower := strings.ToLower(origName)
	if strings.Contains(lower, "discuz") || strings.Contains(origName, "Board") || strings.Contains(origName, "海岸线") {
		if title := extractTitleFromContent(content); title != "" {
			name = mergeTitleWithFilenameRange(title, origName) + ext
		}
	}
	return CleanBookFileName(name)
}

func mergeTitleWithFilenameRange(contentTitle, origName string) string {
	origStart, origEnd, origOK := extractChapterRange(origName)
	if !origOK {
		return contentTitle
	}
	base := stripChapterRange(contentTitle)
	if base == "" {
		base = stripChapterRange(origName)
	}
	if base == "" {
		return contentTitle
	}
	return base + formatChapterRange(origStart, origEnd)
}

// DescribeFilenameChanges 记录文件名优化说明
func DescribeFilenameChanges(origName, newName, content string) []string {
	var notes []string
	if origName == newName {
		notes = append(notes, "文件名无需调整")
		return notes
	}
	lower := strings.ToLower(origName)
	if strings.Contains(lower, "discuz") {
		notes = append(notes, "去除 powered by Discuz 后缀")
	}
	if strings.Contains(origName, "Board") || strings.Contains(origName, "海岸线") {
		notes = append(notes, "从论坛路径中提取书名")
	} else if extractTitleFromContent(content) != "" && (strings.Contains(lower, "discuz") || strings.Contains(origName, "Board")) {
		notes = append(notes, "根据正文标题推断书名")
	}
	if oldRange, newRange := chapterRangeChange(origName, newName); newRange != "" {
		if oldRange == "" {
			notes = append(notes, fmt.Sprintf("补充集数标识：%s", newRange))
		} else if oldRange != newRange {
			notes = append(notes, fmt.Sprintf("集数格式规范化：%s → %s", oldRange, newRange))
		} else {
			notes = append(notes, fmt.Sprintf("集数格式：%s", newRange))
		}
	}
	if bracketAdRe.MatchString(origName) || bracketTagRe.MatchString(origName) || filenameAdRe.MatchString(origName) {
		notes = append(notes, "去除文件名中的广告/站点信息")
	}
	if invalidNameRe.MatchString(origName) || filenameEmojiRe.MatchString(origName) {
		notes = append(notes, "去除非法字符或 emoji")
	}
	notes = append(notes, fmt.Sprintf("重命名为：%s", newName))
	return notes
}

func chapterRangeChange(origName, newName string) (string, string) {
	_, _, origOK := extractChapterRange(origName)
	_, _, newOK := extractChapterRange(newName)
	if !origOK && !newOK {
		return "", ""
	}
	old := chapterRangeInRe.FindString(origName)
	if old == "" && origOK {
		if m := chapterRangeTailRe.FindStringSubmatch(strings.TrimSuffix(origName, filepath.Ext(origName))); len(m) == 4 {
			old = m[2] + "-" + m[3]
		}
	}
	newR := chapterRangeInRe.FindString(newName)
	if newR == "" && newOK {
		if m := chapterRangeTailRe.FindStringSubmatch(strings.TrimSuffix(newName, filepath.Ext(newName))); len(m) == 4 {
			newR = formatChapterRange(m[2], m[3])
		}
	}
	if newR == "" && newOK {
		s, e, _ := extractChapterRange(newName)
		newR = formatChapterRange(s, e)
	}
	return old, newR
}

func extractBookTitleFromFilename(base string) string {
	base = strings.TrimSpace(base)
	if strings.Contains(base, " - ") {
		parts := strings.Split(base, " - ")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(parts[i])
			if candidate == "" || isForumSectionName(candidate) {
				continue
			}
			return candidate
		}
	}
	if strings.Contains(base, "--") && forumPathDashRe.MatchString(base) {
		if idx := strings.LastIndex(base, "--"); idx >= 0 {
			tail := strings.TrimSpace(base[idx+2:])
			if tail != "" && !isForumSectionName(tail) {
				return extractBookTitleFromFilename(tail)
			}
		}
	}
	return base
}

func extractTitleFromContent(content string) string {
	sample := content
	if len(sample) > 5000 {
		sample = sample[:5000]
	}
	matches := contentTitleRe.FindAllString(sample, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		title := strings.TrimSpace(matches[i])
		if title != "" && !isForumSectionName(title) {
			return title
		}
	}
	return ""
}

func isForumSectionName(s string) bool {
	for _, word := range forumSectionWords {
		if strings.Contains(s, word) {
			return true
		}
	}
	return false
}
