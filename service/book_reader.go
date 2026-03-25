package service

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
)

// FirstPageChars 正文第一页约字数
const FirstPageChars = 1000

// ReadBookFirstPages 读取书籍正文（至少 FirstPageChars 字）
func ReadBookFirstPages(filePath string, pages int) (string, bool, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".txt":
		return readTxtContent(filePath)
	case ".pdf":
		return readPDFContent(filePath)
	case ".epub":
		return readEPUBContent(filePath)
	default:
		return "", false, fmt.Errorf("暂不支持的文件格式: %s，支持 .txt、.pdf、.epub", ext)
	}
}

// readTxtContent 读取 txt 直到凑满 FirstPageChars 字
func readTxtContent(filePath string) (string, bool, error) {
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
	return truncate(content), true, nil
}

// readPDFContent 逐页读取 PDF，直到凑满 FirstPageChars 字
func readPDFContent(filePath string) (string, bool, error) {
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
	for pageIndex := 1; pageIndex <= totalPage; pageIndex++ {
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
		if len([]rune(sb.String())) >= FirstPageChars {
			break
		}
	}

	content := sb.String()
	if content == "" {
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
	return truncate(content), true, nil
}

// readEPUBContent 解析 EPUB（zip 内的 XHTML），逐章节提取文本直到凑满 FirstPageChars 字
func readEPUBContent(filePath string) (string, bool, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return "", false, fmt.Errorf("打开EPUB失败: %w", err)
	}
	defer zr.Close()

	// 读取 OPF，按 spine 顺序获取 XHTML 文件列表
	xhtmlFiles := getSpineOrder(zr)
	if len(xhtmlFiles) == 0 {
		// 如果 OPF 解析失败，退回按文件名排序所有 xhtml/html
		xhtmlFiles = getAllXHTML(zr)
	}

	var sb strings.Builder
	for _, name := range xhtmlFiles {
		f := findInZip(zr, name)
		if f == nil {
			continue
		}
		text, err := extractTextFromXHTML(f)
		if err != nil {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(text)
		if len([]rune(sb.String())) >= FirstPageChars {
			break
		}
	}

	content := sb.String()
	if content == "" {
		return "", true, nil
	}
	return truncate(content), true, nil
}

// --- EPUB helpers ---

type opfPackage struct {
	Manifest []opfItem `xml:"manifest>item"`
	Spine    opfSpine  `xml:"spine"`
}
type opfItem struct {
	ID        string `xml:"id,attr"`
	Href      string `xml:"href,attr"`
	MediaType string `xml:"media-type,attr"`
}
type opfSpine struct {
	ItemRefs []opfItemRef `xml:"itemref"`
}
type opfItemRef struct {
	IDRef string `xml:"idref,attr"`
}

type container struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

func getSpineOrder(zr *zip.ReadCloser) []string {
	// 1. 读取 META-INF/container.xml 找到 OPF 路径
	cf := findInZip(zr, "META-INF/container.xml")
	if cf == nil {
		return nil
	}
	cData, err := readZipFile(cf)
	if err != nil {
		return nil
	}
	var ct container
	if err := xml.Unmarshal(cData, &ct); err != nil || len(ct.Rootfiles) == 0 {
		return nil
	}
	opfPath := ct.Rootfiles[0].FullPath
	opfDir := ""
	if idx := strings.LastIndex(opfPath, "/"); idx >= 0 {
		opfDir = opfPath[:idx+1]
	}

	// 2. 读取 OPF
	of := findInZip(zr, opfPath)
	if of == nil {
		return nil
	}
	oData, err := readZipFile(of)
	if err != nil {
		return nil
	}
	var pkg opfPackage
	if err := xml.Unmarshal(oData, &pkg); err != nil {
		return nil
	}

	// 3. 建 id->href 映射
	idMap := map[string]string{}
	for _, item := range pkg.Manifest {
		idMap[item.ID] = item.Href
	}

	// 4. 按 spine 顺序返回完整路径
	var result []string
	for _, ref := range pkg.Spine.ItemRefs {
		href, ok := idMap[ref.IDRef]
		if !ok {
			continue
		}
		result = append(result, opfDir+href)
	}
	return result
}

func getAllXHTML(zr *zip.ReadCloser) []string {
	var names []string
	for _, f := range zr.File {
		lower := strings.ToLower(f.Name)
		if strings.HasSuffix(lower, ".xhtml") || strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm") {
			if !strings.Contains(lower, "toc") && !strings.Contains(lower, "nav") {
				names = append(names, f.Name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func findInZip(zr *zip.ReadCloser, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	// 尝试忽略大小写
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

func extractTextFromXHTML(f *zip.File) (string, error) {
	data, err := readZipFile(f)
	if err != nil {
		return "", err
	}
	text := htmlTagRe.ReplaceAllString(string(data), "")
	// 清理多余空白行
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n"), nil
}

// truncate 截断为约 FirstPageChars 字
func truncate(content string) string {
	runes := []rune(content)
	if len(runes) <= FirstPageChars {
		return content
	}
	return string(runes[:FirstPageChars]) + "\n...(已截断)"
}
