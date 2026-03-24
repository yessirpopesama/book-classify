package service

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/wmentor/epub"
)

// FirstPageChars 正文第一页约字数（用于 API 分析）
const FirstPageChars = 1000

// ReadBookFirstPages 读取书籍正文第一页（约 FirstPageChars 字）
// 支持 .txt、.pdf、.epub；pages 参数已废弃，保留仅为兼容调用方
// 返回: 内容字符串, 是否成功读取, 错误
func ReadBookFirstPages(filePath string, pages int) (string, bool, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".txt":
		return readTxtFirstPage(filePath)
	case ".pdf":
		return readPDFFirstPage(filePath)
	case ".epub":
		return readEPUBFirstPage(filePath)
	default:
		return "", false, fmt.Errorf("暂不支持的文件格式: %s，支持 .txt、.pdf、.epub", ext)
	}
}

// readTxtFirstPage 读取 txt 正文开头约 FirstPageChars 字
func readTxtFirstPage(filePath string) (string, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", false, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	var sb strings.Builder
	scanner := bufio.NewScanner(file)
	var runeCount int
	for scanner.Scan() {
		line := scanner.Text()
		if sb.Len() > 0 {
			sb.WriteByte('\n')
			runeCount++
		}
		sb.WriteString(line)
		runeCount += len([]rune(line))
		if runeCount >= FirstPageChars {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", false, fmt.Errorf("读取文件失败: %w", err)
	}

	content := sb.String()
	if content == "" {
		return "", true, nil
	}
	return truncateFirstPage(content), true, nil
}

// readPDFFirstPage 读取 PDF 第一页，再截断至约 FirstPageChars 字
func readPDFFirstPage(filePath string) (string, bool, error) {
	f, r, err := pdf.Open(filePath)
	if err != nil {
		return "", false, fmt.Errorf("打开PDF失败: %w", err)
	}
	defer f.Close()

	totalPage := r.NumPage()
	if totalPage == 0 {
		return "", true, nil
	}

	var sb strings.Builder
	for pageIndex := 1; pageIndex <= 1 && pageIndex <= totalPage; pageIndex++ {
		p := r.Page(pageIndex)
		rows, err := p.GetTextByRow()
		if err != nil {
			continue
		}
		for _, row := range rows {
			for _, word := range row.Content {
				sb.WriteString(word.S)
			}
			sb.WriteString("\n")
		}
	}

	content := sb.String()
	if content == "" {
		// 回退：使用 GetPlainText 获取全文
		b, err := r.GetPlainText()
		if err != nil {
			return "", false, fmt.Errorf("提取PDF文本失败: %w", err)
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, b); err != nil {
			return "", false, fmt.Errorf("读取PDF文本失败: %w", err)
		}
		content = buf.String()
	}
	if content == "" {
		return "", true, nil
	}
	return truncateFirstPage(content), true, nil
}

// readEPUBFirstPage 读取 EPUB 正文开头约 FirstPageChars 字
func readEPUBFirstPage(filePath string) (string, bool, error) {
	var buf bytes.Buffer
	if err := epub.ToTxt(filePath, &buf); err != nil {
		return "", false, fmt.Errorf("打开EPUB失败: %w", err)
	}
	content := buf.String()
	if content == "" {
		return "", true, nil
	}
	return truncateFirstPage(content), true, nil
}

// truncateFirstPage 截断为约第一页（FirstPageChars 字）
func truncateFirstPage(content string) string {
	runes := []rune(content)
	if len(runes) <= FirstPageChars {
		return content
	}
	return string(runes[:FirstPageChars]) + "\n...(已截断)"
}
