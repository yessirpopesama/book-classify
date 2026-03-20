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

// LinesPerPage 每页大约行数（中文书籍通常25-35行/页）
const LinesPerPage = 35

// PDFPagesToRead PDF 读取页数
const PDFPagesToRead = 3

// MaxContentChars 发送给API的最大字符数（避免超出token限制）
const MaxContentChars = 12000

// ReadBookFirstPages 读取书籍前N页内容
// 支持 .txt、.pdf、.epub，均为前N页
// 返回: 内容字符串, 是否成功读取, 错误
func ReadBookFirstPages(filePath string, pages int) (string, bool, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".txt":
		return readTxtFirstPages(filePath, pages)
	case ".pdf":
		return readPDFFirstPages(filePath, PDFPagesToRead)
	case ".epub":
		return readEPUBFirstPages(filePath, pages)
	default:
		return "", false, fmt.Errorf("暂不支持的文件格式: %s，支持 .txt、.pdf、.epub", ext)
	}
}

// readTxtFirstPages 读取 txt 前 N 页
func readTxtFirstPages(filePath string, pages int) (string, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", false, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	linesToRead := LinesPerPage * pages
	var lines []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() && len(lines) < linesToRead {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return "", false, fmt.Errorf("读取文件失败: %w", err)
	}

	if len(lines) == 0 {
		return "", true, nil
	}

	content := strings.Join(lines, "\n")
	return limitContent(content), true, nil
}

// readPDFFirstPages 读取 PDF 前 N 页
func readPDFFirstPages(filePath string, pages int) (string, bool, error) {
	f, r, err := pdf.Open(filePath)
	if err != nil {
		return "", false, fmt.Errorf("打开PDF失败: %w", err)
	}
	defer f.Close()

	totalPage := r.NumPage()
	if totalPage == 0 {
		return "", true, nil
	}

	// 尝试按页提取
	var sb strings.Builder
	pagesToRead := pages
	if totalPage < pagesToRead {
		pagesToRead = totalPage
	}

	for pageIndex := 1; pageIndex <= pagesToRead; pageIndex++ {
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
	return limitContent(content), true, nil
}

// readEPUBFirstPages 读取 EPUB 前 N 页内容（EPUB 无页概念，按字符量近似前 N 页）
func readEPUBFirstPages(filePath string, pages int) (string, bool, error) {
	var buf bytes.Buffer
	if err := epub.ToTxt(filePath, &buf); err != nil {
		return "", false, fmt.Errorf("打开EPUB失败: %w", err)
	}
	content := buf.String()
	if content == "" {
		return "", true, nil
	}
	return limitContent(content), true, nil
}

// limitContent 限制内容长度
func limitContent(content string) string {
	runes := []rune(content)
	if len(runes) > MaxContentChars {
		return string(runes[:MaxContentChars]) + "\n...(内容已截断)"
	}
	return content
}
