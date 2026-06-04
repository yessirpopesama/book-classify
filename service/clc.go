package service

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// DefaultCLCIndexPath 中图法第五版扁平索引（类号 -> 类名）
const DefaultCLCIndexPath = "data/clc/clc_index.json"

// PendingClassifyFile 路径未通过中图法校验、需人工分类的结果文件
const PendingClassifyFile = "待进行分类.txt"

// PendingClassifyDir 待人工分类图书存放目录
const PendingClassifyDir = "待分类"

// CLCIndex 中图法类号索引
type CLCIndex struct {
	entries map[string]string
}

// CLCPathSegment 路径中的一级类目
type CLCPathSegment struct {
	Code string
	Name string
}

// CLCValidation 路径校验结果
type CLCValidation struct {
	OK            bool
	Reason        string
	OfficialPath  string   // 由索引生成的标准路径
	OfficialCodes []string // 标准路径各级类号
}

var clcSegmentCodeRe = regexp.MustCompile(`^(\[?[A-Z][A-Z0-9./\-]*\]?)\s*(.*)$`)

// LoadCLCIndex 加载中图法索引 JSON（类号 -> 类名）
func LoadCLCIndex(path string) (*CLCIndex, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultCLCIndexPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取中图法索引失败: %v", err)
	}
	var entries map[string]string
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("解析中图法索引失败: %v", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("中图法索引为空")
	}
	fmt.Printf("已加载中图法索引: %s（%d 条）\n", path, len(entries))
	return &CLCIndex{entries: entries}, nil
}

// Lookup 查询类号对应官方类名
func (idx *CLCIndex) Lookup(code string) (name string, ok bool) {
	code = normalizeCLCCode(code)
	if code == "" {
		return "", false
	}
	if name, ok = idx.entries[code]; ok {
		return name, true
	}
	bracketed := "[" + strings.Trim(code, "[]") + "]"
	if name, ok = idx.entries[bracketed]; ok {
		return name, true
	}
	return "", false
}

// FinalCodeFromAnalysis 从分类号或路径末级提取最终类号
func FinalCodeFromAnalysis(a *BookAnalysis) string {
	if a == nil {
		return ""
	}
	code := normalizeCLCCode(a.Classification)
	segs := parseCLCPathSegments(a.ClassificationPath)
	if len(segs) == 0 && len(a.CategoryLevels) > 0 {
		segs = parseCLCLevels(a.CategoryLevels)
	}
	if len(segs) == 0 {
		return code
	}
	pathFinal := normalizeCLCCode(segs[len(segs)-1].Code)
	if code == "" {
		return pathFinal
	}
	return code
}

// ValidateAnalysis 校验：分类号须在中图法索引中；若提供了路径，则路径末级类号须与分类号一致
func (idx *CLCIndex) ValidateAnalysis(a *BookAnalysis) CLCValidation {
	if a == nil {
		return CLCValidation{Reason: "无分析结果"}
	}
	code := normalizeCLCCode(a.Classification)
	pathFinal := ""
	segs := parseCLCPathSegments(a.ClassificationPath)
	if len(segs) == 0 && len(a.CategoryLevels) > 0 {
		segs = parseCLCLevels(a.CategoryLevels)
	}
	if len(segs) > 0 {
		pathFinal = normalizeCLCCode(segs[len(segs)-1].Code)
	}
	if code == "" && pathFinal != "" {
		code = pathFinal
	}
	if code == "" {
		return CLCValidation{Reason: "分类号为空"}
	}
	if _, ok := idx.Lookup(code); !ok {
		return CLCValidation{Reason: fmt.Sprintf("分类号 %s 不在中图法第五版索引中", code)}
	}
	officialSegs := idx.buildOfficialSegments(code)
	if len(officialSegs) == 0 {
		return CLCValidation{Reason: fmt.Sprintf("无法为中图法类号 %s 构建标准路径", code)}
	}
	officialPath := formatCLCPath(officialSegs)

	if pathFinal != "" && pathFinal != code {
		return CLCValidation{
			Reason:       fmt.Sprintf("分类号 %s 与路径末级类号 %s 不一致", code, pathFinal),
			OfficialPath: officialPath,
		}
	}

	codes := make([]string, len(officialSegs))
	for i, s := range officialSegs {
		codes[i] = s.Code
	}
	return CLCValidation{
		OK:            true,
		OfficialPath:  officialPath,
		OfficialCodes: codes,
	}
}

func (idx *CLCIndex) buildOfficialSegments(code string) []CLCPathSegment {
	code = normalizeCLCCode(code)
	var chain []string
	cur := code
	for cur != "" {
		if _, ok := idx.Lookup(cur); !ok {
			break
		}
		chain = append([]string{cur}, chain...)
		parent := idx.findParentCode(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	if len(chain) == 0 {
		return nil
	}
	segs := make([]CLCPathSegment, len(chain))
	for i, c := range chain {
		name, _ := idx.Lookup(c)
		segs[i] = CLCPathSegment{Code: c, Name: name}
	}
	return segs
}

func (idx *CLCIndex) findParentCode(code string) string {
	best := ""
	bestLen := 0
	for k := range idx.entries {
		if len(k) >= len(code) {
			continue
		}
		if strings.HasPrefix(code, k) && len(k) > bestLen {
			bestLen = len(k)
			best = k
		}
	}
	return normalizeCLCCode(best)
}

func parseCLCPathSegments(path string) []CLCPathSegment {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	parts := strings.Split(path, ">")
	var segs []CLCPathSegment
	for _, p := range parts {
		if seg, ok := parseCLCSegment(strings.TrimSpace(p)); ok {
			segs = append(segs, seg)
		}
	}
	return segs
}

func parseCLCLevels(levels []string) []CLCPathSegment {
	var segs []CLCPathSegment
	for _, lv := range levels {
		if seg, ok := parseCLCSegment(strings.TrimSpace(lv)); ok {
			segs = append(segs, seg)
		}
	}
	return segs
}

func parseCLCSegment(s string) (CLCPathSegment, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return CLCPathSegment{}, false
	}
	m := clcSegmentCodeRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return CLCPathSegment{}, false
	}
	code := normalizeCLCCode(m[1])
	name := strings.TrimSpace(m[2])
	if code == "" {
		return CLCPathSegment{}, false
	}
	return CLCPathSegment{Code: code, Name: name}, true
}

func formatCLCPath(segs []CLCPathSegment) string {
	parts := make([]string, len(segs))
	for i, s := range segs {
		if s.Name != "" {
			parts[i] = s.Code + " " + s.Name
		} else {
			parts[i] = s.Code
		}
	}
	return strings.Join(parts, " > ")
}

func normalizeCLCCode(code string) string {
	code = strings.TrimSpace(code)
	code = strings.Trim(code, "[]")
	return code
}

// ApplyOfficialPath 当模型未返回路径时，用索引中的标准路径补全
func (idx *CLCIndex) ApplyOfficialPath(a *BookAnalysis, v CLCValidation) {
	if a == nil || !v.OK {
		return
	}
	if strings.TrimSpace(a.ClassificationPath) != "" {
		return
	}
	a.ClassificationPath = v.OfficialPath
	segs := idx.buildOfficialSegments(normalizeCLCCode(a.Classification))
	levels := make([]string, len(segs))
	for i, s := range segs {
		levels[i] = s.Code + " " + s.Name
	}
	a.CategoryLevels = levels
}
