package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const RepairReportFile = "修复报告.txt"
const RepairReportJSONFile = "repair_report.json"

// RepairTextStats 单文件正文修复统计
type RepairTextStats struct {
	Encoding            string   `json:"encoding"`
	OutputEncoding      string   `json:"output_encoding"`
	OriginalBytes       int      `json:"original_bytes"`
	FinalBytes          int      `json:"final_bytes"`
	OriginalRunes       int      `json:"original_runes"`
	FinalRunes          int      `json:"final_runes"`
	OriginalLines       int      `json:"original_lines"`
	FinalLines          int      `json:"final_lines"`
	RemovedLines        int      `json:"removed_lines"`
	RemovedForumLines   int      `json:"removed_forum_lines"`
	RemovedAdLines      int      `json:"removed_ad_lines"`
	CollapsedBlankLines int      `json:"collapsed_blank_lines"`
	ForumHeaderLines    int      `json:"forum_header_lines"`
	ForumFooterLines    int      `json:"forum_footer_lines"`
	NullBytesRemoved    bool     `json:"null_bytes_removed"`
	ZeroWidthRemoved    bool     `json:"zero_width_removed"`
	HTMLRemoved         bool     `json:"html_removed"`
	LineEndingsFixed    bool     `json:"line_endings_fixed"`
	Optimizations       []string `json:"optimizations"`
}

// RepairResultItem 单本书修复结果
type RepairResultItem struct {
	OriginalName    string          `json:"original_name"`
	NewName         string          `json:"new_name"`
	Stats           RepairTextStats `json:"stats"`
	FilenameChanges []string        `json:"filename_changes"`
	Error           string          `json:"error,omitempty"`
}

// RepairReport 修复任务报告
type RepairReport struct {
	GeneratedAt string             `json:"generated_at"`
	Success     int                `json:"success"`
	Failed      int                `json:"failed"`
	Total       int                `json:"total"`
	Items       []RepairResultItem `json:"items"`
}

// RepairProgressFunc 修复进度回调
type RepairProgressFunc func(done, total int, fileName string)

// ListTxtFiles 递归列出目录下 txt 文件
func ListTxtFiles(sourceDir string) ([]string, error) {
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		return nil, err
	}

	absSourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, err
	}

	excluded := map[string]bool{
		RepairReportFile:     true,
		RepairReportJSONFile: true,
		"结果.txt":           true,
		"待进行分类.txt":        true,
		".DS_Store":        true,
	}

	var paths []string
	err = filepath.Walk(absSourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if excluded[info.Name()] || !IsTxtFile(info.Name()) {
			return nil
		}
		rel, err := filepath.Rel(absSourceDir, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	return paths, err
}

// RepairBooks 修复上传目录中的 txt 图书，输出到 resultsDir
func RepairBooks(sourceDir, resultsDir string, onProgress ...RepairProgressFunc) error {
	var progress RepairProgressFunc
	if len(onProgress) > 0 {
		progress = onProgress[0]
	}

	filePaths, err := ListTxtFiles(sourceDir)
	if err != nil {
		return fmt.Errorf("扫描文件失败: %v", err)
	}
	if len(filePaths) == 0 {
		return fmt.Errorf("未找到 txt 文件")
	}

	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %v", err)
	}

	var results []RepairResultItem
	success := 0
	failed := 0

	for i, relPath := range filePaths {
		origName := filepath.Base(relPath)
		if progress != nil {
			progress(i, len(filePaths), origName)
		}

		srcPath := filepath.Join(sourceDir, relPath)
		item := RepairResultItem{OriginalName: origName}

		raw, err := os.ReadFile(srcPath)
		if err != nil {
			item.Error = err.Error()
			results = append(results, item)
			failed++
			continue
		}

		text, encoding := DecodeTextBytes(raw)
		repaired, stats := RepairTextContent(text)
		stats.Encoding = encoding
		stats.OutputEncoding = "utf-8"
		stats.OriginalBytes = len(raw)
		stats.FinalBytes = len([]byte(repaired))

		newName := InferBookFileName(repaired, origName)
		item.NewName = newName
		item.Stats = stats
		item.FilenameChanges = DescribeFilenameChanges(origName, newName, repaired)

		destPath := filepath.Join(resultsDir, newName)
		destPath = uniquePath(destPath)

		if err := os.WriteFile(destPath, []byte(repaired), 0644); err != nil {
			item.Error = err.Error()
			results = append(results, item)
			failed++
			continue
		}

		results = append(results, item)
		success++
	}

	if err := writeRepairReport(resultsDir, results, success, failed); err != nil {
		return err
	}
	if err := writeRepairReportJSON(resultsDir, results, success, failed); err != nil {
		return err
	}

	fmt.Printf("修复完成：成功 %d，失败 %d\n", success, failed)
	return nil
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func writeRepairReport(resultsDir string, results []RepairResultItem, success, failed int) error {
	path := filepath.Join(resultsDir, RepairReportFile)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "图书内容修复报告\n")
	fmt.Fprintf(f, "生成时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "成功: %d\t失败: %d\t总计: %d\n\n", success, failed, len(results))

	for i, r := range results {
		if i > 0 {
			fmt.Fprintln(f, strings.Repeat("=", 80))
		}
		writeRepairReportItem(f, i+1, r)
	}
	return nil
}

func writeRepairReportItem(f *os.File, index int, r RepairResultItem) {
	status := "成功"
	if r.Error != "" {
		status = r.Error
	}
	fmt.Fprintf(f, "[%d] %s\n", index, r.OriginalName)
	fmt.Fprintf(f, "状态: %s\n", status)
	fmt.Fprintln(f, strings.Repeat("-", 80))

	if r.Error != "" && r.NewName == "" {
		return
	}

	fmt.Fprintln(f, "【文件名优化】")
	fmt.Fprintf(f, "  原文件名: %s\n", r.OriginalName)
	fmt.Fprintf(f, "  新文件名: %s\n", r.NewName)
	for _, note := range r.FilenameChanges {
		fmt.Fprintf(f, "  · %s\n", note)
	}

	s := r.Stats
	fmt.Fprintln(f, "【编码与格式】")
	fmt.Fprintf(f, "  检测编码: %s\n", displayEncoding(s.Encoding))
	fmt.Fprintf(f, "  输出编码: %s\n", displayEncoding(s.OutputEncoding))
	fmt.Fprintf(f, "  原文件大小: %s\n", formatBytes(s.OriginalBytes))
	fmt.Fprintf(f, "  修复后大小: %s\n", formatBytes(s.FinalBytes))
	if s.NullBytesRemoved {
		fmt.Fprintln(f, "  · 已去除 NUL 字节")
	}
	if s.ZeroWidthRemoved {
		fmt.Fprintln(f, "  · 已去除零宽字符")
	}
	if s.LineEndingsFixed {
		fmt.Fprintln(f, "  · 已统一换行符为 LF")
	}
	if s.HTMLRemoved {
		fmt.Fprintln(f, "  · 已去除 HTML 标签")
	}

	fmt.Fprintln(f, "【正文优化】")
	fmt.Fprintf(f, "  原文字数: %d 字\n", s.OriginalRunes)
	fmt.Fprintf(f, "  修复后字数: %d 字\n", s.FinalRunes)
	fmt.Fprintf(f, "  原行数: %d 行\n", s.OriginalLines)
	fmt.Fprintf(f, "  修复后行数: %d 行\n", s.FinalLines)
	fmt.Fprintf(f, "  合计剔除: %d 行\n", s.RemovedLines+s.CollapsedBlankLines+s.ForumHeaderLines+s.ForumFooterLines)
	if s.RemovedForumLines > 0 {
		fmt.Fprintf(f, "    - 论坛页眉/导航/页脚: %d 行\n", s.RemovedForumLines)
	}
	if s.RemovedAdLines > 0 {
		fmt.Fprintf(f, "    - 广告/水印/无效行: %d 行\n", s.RemovedAdLines)
	}
	if s.ForumHeaderLines > 0 {
		fmt.Fprintf(f, "    - 文首论坛残留: %d 行\n", s.ForumHeaderLines)
	}
	if s.ForumFooterLines > 0 {
		fmt.Fprintf(f, "    - 文末论坛残留: %d 行\n", s.ForumFooterLines)
	}
	if s.CollapsedBlankLines > 0 {
		fmt.Fprintf(f, "    - 压缩多余空行: %d 行\n", s.CollapsedBlankLines)
	}

	if len(s.Optimizations) > 0 {
		fmt.Fprintln(f, "【优化项清单】")
		for _, opt := range s.Optimizations {
			fmt.Fprintf(f, "  ✓ %s\n", opt)
		}
	}
	fmt.Fprintln(f)
}

func writeRepairReportJSON(resultsDir string, results []RepairResultItem, success, failed int) error {
	report := RepairReport{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Success:     success,
		Failed:      failed,
		Total:       len(results),
		Items:       results,
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(resultsDir, RepairReportJSONFile), data, 0644)
}

// LoadRepairReport 读取 JSON 修复报告
func LoadRepairReport(resultsDir string) (*RepairReport, error) {
	data, err := os.ReadFile(filepath.Join(resultsDir, RepairReportJSONFile))
	if err != nil {
		return nil, err
	}
	var report RepairReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func displayEncoding(enc string) string {
	if enc == "" {
		return "未知"
	}
	return enc
}

func formatBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.2f MB", float64(n)/(1024*1024))
}
