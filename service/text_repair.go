package service

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var (
	repairHTMLTagRe = regexp.MustCompile(`(?i)<[^>]+>`)
	urlRe           = regexp.MustCompile(`(?i)(?:https?://|www\.|ftp://)[^\s\p{Han}]+`)
	adLinePatterns  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^[\s【\[\(（]*(?:请访问|访问|下载|更多精彩|本书来自|书源|txt80|bbs\.|forum\.|zol\.|bookbao|qidian|douban)[^\n]*$`),
		regexp.MustCompile(`(?i)^[\s\-—=_*#]*(?:广告|推广|赞助)[^\n]*$`),
		regexp.MustCompile(`(?i)^[\s【\[\(（]*(?:www\.|http)[^\n]*$`),
	}
	forumLinePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)powered by Discuz`),
		regexp.MustCompile(`(?i)Comsenz Technology`),
		regexp.MustCompile(`&raquo;|&copy;|&nbsp;|»|©`),
		regexp.MustCompile(`(?i)Oursm Board`),
		regexp.MustCompile(`海岸线之文学天地`),
		regexp.MustCompile(`上一主题\s*\|\s*下一主题`),
		regexp.MustCompile(`^作者:标题:`),
		regexp.MustCompile(`^作者：[\p{Han}A-Za-z·]{1,20}$`),
		regexp.MustCompile(`^积分\s+\d+\s*$`),
		regexp.MustCompile(`^发贴\s+\d+\s*$`),
		regexp.MustCompile(`^注册\s+\d{4}-`),
		regexp.MustCompile(`^状态\s+(在线|离线)`),
		regexp.MustCompile(`^版主\s*$`),
		regexp.MustCompile(`^尊敬的原创者\s*$`),
		regexp.MustCompile(`发表于：`),
		regexp.MustCompile(`^附件:`),
		regexp.MustCompile(`^该附件被下载`),
		regexp.MustCompile(`可打印版本\s*\|`),
		regexp.MustCompile(`^论坛跳转:`),
		regexp.MustCompile(`^>\s`),
		regexp.MustCompile(`^\d{4}-\d{1,2}-\d{1,2}\s+\d{1,2}:\d{2}\s*(AM|PM)?\s*$`),
		regexp.MustCompile(`^\d{4}/\d{2}/\d{2}发表于`),
		regexp.MustCompile(`(?i)退出\s*\|\s*短消息\s*\|\s*控制面板`),
		regexp.MustCompile(`联合征文`),
		regexp.MustCompile(`海岸线联合征文`),
	}
	zeroWidthRe = regexp.MustCompile(`[\x{200B}\x{200C}\x{200D}\x{FEFF}\x{00AD}]`)
)

// DecodeTextBytes 将常见编码转为 UTF-8 文本（支持 UTF-8/UTF-16/GBK/GB18030）
func DecodeTextBytes(raw []byte) (string, string) {
	if len(raw) == 0 {
		return "", "empty"
	}

	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return string(bytes.ReplaceAll(raw[3:], []byte{0x00}, nil)), "utf-8-bom"
	}
	if len(raw) >= 2 {
		if raw[0] == 0xFF && raw[1] == 0xFE {
			return decodeUTF16(raw[2:], true), "utf-16le"
		}
		if raw[0] == 0xFE && raw[1] == 0xFF {
			return decodeUTF16(raw[2:], false), "utf-16be"
		}
	}

	if utf8.Valid(raw) {
		return string(bytes.ReplaceAll(raw, []byte{0x00}, nil)), "utf-8"
	}

	if text, err := transformBytes(raw, simplifiedchinese.GB18030.NewDecoder()); err == nil && utf8.ValidString(text) {
		return text, "gb18030"
	}
	if text, err := transformBytes(raw, simplifiedchinese.GBK.NewDecoder()); err == nil && utf8.ValidString(text) {
		return text, "gbk"
	}

	return strings.ToValidUTF8(string(raw), ""), "utf-8-repaired"
}

func decodeUTF16(b []byte, littleEndian bool) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if littleEndian {
			units = append(units, uint16(b[i])|uint16(b[i+1])<<8)
		} else {
			units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
		}
	}
	return string(utf16.Decode(units))
}

func transformBytes(raw []byte, t transform.Transformer) (string, error) {
	reader := transform.NewReader(bytes.NewReader(raw), t)
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RepairTextContent 修复正文：编码归一、去除非法字符、对齐与剔除垃圾内容
func RepairTextContent(text string) (string, RepairTextStats) {
	stats := RepairTextStats{}
	if text == "" {
		return "", stats
	}

	origLen := len([]rune(text))
	stats.OriginalLines = strings.Count(text, "\n") + 1
	if strings.HasSuffix(text, "\n") {
		stats.OriginalLines--
	}
	if stats.OriginalLines < 1 {
		stats.OriginalLines = 1
	}

	if strings.Contains(text, "\x00") {
		stats.NullBytesRemoved = true
	}
	withoutZW := zeroWidthRe.ReplaceAllString(text, "")
	if withoutZW != text {
		stats.ZeroWidthRemoved = true
	}
	text = withoutZW
	text = strings.ReplaceAll(text, "\x00", "")
	stats.LineEndingsFixed = strings.Contains(text, "\r")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	if repairHTMLTagRe.MatchString(text) {
		stats.HTMLRemoved = true
	}
	text = repairHTMLTagRe.ReplaceAllString(text, "")

	lines := strings.Split(text, "\n")
	lines = stripForumPosterNames(lines)
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = normalizeLineSpaces(line)
		line = strings.TrimRight(line, " \t")
		if isForumLine(line) {
			stats.RemovedForumLines++
			stats.RemovedLines++
			continue
		}
		if shouldDropLine(line) {
			stats.RemovedAdLines++
			stats.RemovedLines++
			continue
		}
		line = decodeBasicHTMLEntities(line)
		line = urlRe.ReplaceAllString(line, "")
		if strings.TrimSpace(line) == "" {
			cleaned = append(cleaned, "")
			continue
		}
		cleaned = append(cleaned, line)
	}

	var headerRemoved, footerRemoved, collapsed int
	cleaned, headerRemoved = stripLeadingForumHeader(cleaned)
	stats.ForumHeaderLines = headerRemoved
	cleaned, collapsed = collapseBlankLines(cleaned)
	stats.CollapsedBlankLines = collapsed
	cleaned = trimEdgeBlankLines(cleaned)
	cleaned, footerRemoved = trimForumFooter(cleaned)
	stats.ForumFooterLines = footerRemoved
	result := strings.Join(cleaned, "\n")
	if result != "" && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}

	stats.OriginalRunes = origLen
	stats.FinalRunes = len([]rune(result))
	stats.FinalLines = strings.Count(result, "\n")
	if result != "" && !strings.HasSuffix(result, "\n") {
		stats.FinalLines++
	}
	if stats.FinalLines == 0 && result != "" {
		stats.FinalLines = 1
	}
	stats.Optimizations = buildOptimizationList(stats)
	return result, stats
}

func normalizeLineSpaces(line string) string {
	var b strings.Builder
	b.Grow(len(line))
	prevSpace := false
	for _, r := range line {
		switch {
		case r == '\u3000' || r == '\u00A0':
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		case unicode.IsSpace(r):
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		case unicode.IsControl(r):
			continue
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func shouldDropLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	for _, re := range adLinePatterns {
		if re.MatchString(trimmed) {
			return true
		}
	}
	if urlRe.MatchString(trimmed) && len([]rune(trimmed)) < 80 {
		return true
	}
	// 仅由符号组成的行
	hasContent := false
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.Han, r) {
			hasContent = true
			break
		}
	}
	return !hasContent && len([]rune(trimmed)) <= 6
}

func isForumLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	for _, candidate := range []string{trimmed, decodeBasicHTMLEntities(trimmed)} {
		if isForumBreadcrumbLine(candidate) {
			return true
		}
		for _, re := range forumLinePatterns {
			if re.MatchString(candidate) {
				return true
			}
		}
	}
	return false
}

func stripLeadingForumHeader(lines []string) ([]string, int) {
	start := 0
	for start < len(lines) {
		trimmed := strings.TrimSpace(lines[start])
		if trimmed == "" {
			start++
			continue
		}
		if isForumLine(lines[start]) {
			start++
			continue
		}
		break
	}
	return lines[start:], start
}

func collapseBlankLines(lines []string) ([]string, int) {
	out := make([]string, 0, len(lines))
	blankRun := 0
	removed := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun > 2 {
				removed++
				continue
			}
			out = append(out, "")
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	return out, removed
}

func trimEdgeBlankLines(lines []string) []string {
	start, end := 0, len(lines)
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

func decodeBasicHTMLEntities(text string) string {
	replacer := strings.NewReplacer(
		"&raquo;", "»",
		"&copy;", "©",
		"&nbsp;", " ",
		"&lt;", "<",
		"&gt;", ">",
		"&amp;", "&",
	)
	return replacer.Replace(text)
}

// stripForumPosterNames 去掉 Discuz 帖主昵称行（昵称下一行是「尊敬的原创者」）
func stripForumPosterNames(lines []string) []string {
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		cur := strings.TrimSpace(lines[i])
		if cur != "" && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "尊敬的原创者" {
			continue
		}
		out = append(out, lines[i])
	}
	return out
}

func trimForumFooter(lines []string) ([]string, int) {
	end := len(lines)
	removed := 0
	for end > 0 {
		trimmed := strings.TrimSpace(lines[end-1])
		if trimmed == "" {
			end--
			continue
		}
		if isForumFooterLine(trimmed) {
			end--
			removed++
			continue
		}
		break
	}
	return lines[:end], removed
}

func isForumFooterLine(line string) bool {
	return isForumLine(line)
}

func isForumBreadcrumbLine(line string) bool {
	if strings.Count(line, " > ") >= 2 {
		return true
	}
	if strings.Count(line, ">") >= 3 && !strings.ContainsAny(line, "。！？；") {
		return true
	}
	return false
}

func buildOptimizationList(s RepairTextStats) []string {
	var list []string
	switch s.Encoding {
	case "utf-8-bom":
		list = append(list, "去除 UTF-8 BOM，输出 UTF-8")
	case "utf-8", "utf-8-repaired", "empty":
		list = append(list, "输出 UTF-8 编码")
	default:
		list = append(list, fmt.Sprintf("编码 %s 转换为 UTF-8", s.Encoding))
	}
	if s.NullBytesRemoved {
		list = append(list, "去除 NUL 字节")
	}
	if s.ZeroWidthRemoved {
		list = append(list, "去除零宽字符")
	}
	if s.LineEndingsFixed {
		list = append(list, "统一换行符为 LF")
	}
	if s.HTMLRemoved {
		list = append(list, "去除 HTML 标签")
	}
	list = append(list, "解码 HTML 实体")
	list = append(list, "对齐正文空格")
	if s.RemovedForumLines > 0 {
		list = append(list, fmt.Sprintf("剔除论坛页眉/导航/页脚 %d 行", s.RemovedForumLines))
	}
	if s.RemovedAdLines > 0 {
		list = append(list, fmt.Sprintf("剔除广告/水印/无效行 %d 行", s.RemovedAdLines))
	}
	if s.ForumHeaderLines > 0 {
		list = append(list, fmt.Sprintf("去除文首论坛残留 %d 行", s.ForumHeaderLines))
	}
	if s.ForumFooterLines > 0 {
		list = append(list, fmt.Sprintf("去除文末论坛残留 %d 行", s.ForumFooterLines))
	}
	if s.CollapsedBlankLines > 0 {
		list = append(list, fmt.Sprintf("压缩多余空行 %d 行", s.CollapsedBlankLines))
	}
	return list
}
