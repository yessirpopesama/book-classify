package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// BookAnalysis 书籍分析结果
type BookAnalysis struct {
	Classification     string   `json:"classification"`      // 分类号
	ClassificationPath string   `json:"classification_path"` // 最优分类路径
	CategoryLevels     []string `json:"category_levels"`     // 完整类目层级（从一级到最专指）
	LibraryReference   string   `json:"library_reference"`   // 各馆参考分类对照
	Author             string   `json:"author"`              // 书籍作者
	Nationality        string   `json:"nationality"`         // 书籍国籍
}

// DeepSeekClient DeepSeek API客户端
type DeepSeekClient struct {
	apiKey       string
	client       *http.Client
	apiURL       string
	systemPrompt string // 图书馆分类员角色提示词
}

// ChatMessage 聊天消息结构
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 聊天请求结构
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature"`
}

// classifyTemperature 分类为结构化抽取任务，使用较低温度以保证类目路径稳定、可复现
const classifyTemperature = 0.1

// ChatResponse 聊天响应结构
type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// NewDeepSeekClient 创建新的 DeepSeek 客户端。单次请求设置上限，避免异常连接阻塞整个任务。
// systemPrompt 为图书馆分类员角色提示词；为空时回退到内置默认角色。
func NewDeepSeekClient(apiKey, systemPrompt string) *DeepSeekClient {
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = defaultClassifierRole
	}
	return &DeepSeekClient{
		apiKey: apiKey,
		client: &http.Client{
			Timeout: 90 * time.Second,
		},
		apiURL:       "https://api.deepseek.com/v1/chat/completions",
		systemPrompt: systemPrompt,
	}
}

// AnalyzeBook 根据书名和正文第一页（约1000字）内容，分析并返回分类号、作者、国籍
func (c *DeepSeekClient) AnalyzeBook(bookName, bookContent string) (*BookAnalysis, error) {
	return c.AnalyzeBookContext(context.Background(), bookName, bookContent)
}

// AnalyzeBookContext 支持调用方取消正在进行的 API 请求。
func (c *DeepSeekClient) AnalyzeBookContext(ctx context.Context, bookName, bookContent string) (*BookAnalysis, error) {
	systemPrompt := c.systemPrompt
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = defaultClassifierRole
	}

	userContent := fmt.Sprintf("书名：%s\n\n正文第一页：\n%s", bookName, bookContent)
	if bookContent == "" {
		userContent = fmt.Sprintf("书名：%s\n\n（无正文内容，请仅根据书名推断）", bookName)
	}

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent},
	}

	reqBody := ChatRequest{
		Model:       "deepseek-chat",
		Messages:    messages,
		Stream:      false,
		Temperature: classifyTemperature,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API返回错误状态码 %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("API返回空结果")
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if content == "" {
		return nil, fmt.Errorf("API返回内容为空")
	}
	return parseBookAnalysis(content)
}

// parseBookAnalysis 从API响应中解析出分类号、分类路径、层级、作者、国籍
func parseBookAnalysis(content string) (*BookAnalysis, error) {
	result := &BookAnalysis{}
	content = stripModelMarkdown(content)

	// 多行字段：最优分类路径、图书馆参考 可能较长，支持续行
	knownKeys := []string{"分类号", "图书馆参考", "最优分类路径", "完整类目层级", "书籍作者", "书籍国籍"}
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(stripModelMarkdown(lines[i]))
		if line == "" {
			continue
		}
		key, val, ok := splitLabelValue(line)
		if !ok {
			continue
		}
		// 续行：下一行不是新字段开头则拼接到当前值
		for i+1 < len(lines) {
			next := strings.TrimSpace(stripModelMarkdown(lines[i+1]))
			if next == "" {
				i++
				continue
			}
			if isKnownFieldLine(next, knownKeys) {
				break
			}
			val += " " + next
			i++
		}
		val = strings.TrimSpace(val)
		switch key {
		case "分类号":
			result.Classification = val
		case "图书馆参考":
			result.LibraryReference = val
		case "最优分类路径":
			result.ClassificationPath = val
		case "完整类目层级":
			result.CategoryLevels = splitCategoryLevels(val)
		case "书籍作者":
			result.Author = val
		case "书籍国籍":
			result.Nationality = val
		}
	}

	applyParseFallback(content, result)
	normalizeBookAnalysis(result)

	if result.Classification == "" && result.ClassificationPath == "" && result.Author == "" {
		return nil, fmt.Errorf("无法从模型响应中解析分类结果")
	}
	return result, nil
}

func stripModelMarkdown(s string) string {
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '`' && s[len(s)-1] == '`') || (s[0] == '"' && s[len(s)-1] == '"') {
			s = strings.Trim(s, "`\"")
		}
	}
	return strings.TrimSpace(s)
}

func splitLabelValue(line string) (key, val string, ok bool) {
	line = stripModelMarkdown(line)
	for _, sep := range []string{"：", ":"} {
		if idx := strings.Index(line, sep); idx > 0 {
			key = strings.TrimSpace(line[:idx])
			val = strings.TrimSpace(line[idx+len(sep):])
			key = strings.Trim(key, "*# ")
			return key, val, true
		}
	}
	return "", "", false
}

func isKnownFieldLine(line string, keys []string) bool {
	for _, k := range keys {
		if strings.HasPrefix(line, k+"：") || strings.HasPrefix(line, k+":") {
			return true
		}
	}
	return false
}

func applyParseFallback(content string, result *BookAnalysis) {
	fallback := map[string]*string{
		`分类号[：:\s]*([^\n]+)`:    &result.Classification,
		`图书馆参考[：:\s]*([^\n]+)`:  &result.LibraryReference,
		`最优分类路径[：:\s]*([^\n]+)`: &result.ClassificationPath,
		`书籍作者[：:\s]*([^\n]+)`:   &result.Author,
		`书籍国籍[：:\s]*([^\n]+)`:   &result.Nationality,
	}
	for pattern, dest := range fallback {
		if *dest != "" {
			continue
		}
		re := regexp.MustCompile(pattern)
		if m := re.FindStringSubmatch(content); len(m) > 1 {
			*dest = stripModelMarkdown(m[1])
		}
	}
	if len(result.CategoryLevels) == 0 {
		reLevels := regexp.MustCompile(`完整类目层级[：:\s]*([^\n]+)`)
		if m := reLevels.FindStringSubmatch(content); len(m) > 1 {
			result.CategoryLevels = splitCategoryLevels(stripModelMarkdown(m[1]))
		}
	}
}

func normalizeBookAnalysis(a *BookAnalysis) {
	if a.ClassificationPath == "" && len(a.CategoryLevels) > 0 {
		a.ClassificationPath = strings.Join(a.CategoryLevels, " > ")
	}
	if len(a.CategoryLevels) == 0 && a.ClassificationPath != "" {
		a.CategoryLevels = splitCategoryLevels(strings.ReplaceAll(a.ClassificationPath, ">", "|"))
	}
	if a.Classification == "" {
		a.Classification = FinalCodeFromAnalysis(a)
	}
}

func splitCategoryLevels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	raw = strings.ReplaceAll(raw, "｜", "|")
	raw = strings.ReplaceAll(raw, ">", "|")
	parts := strings.Split(raw, "|")
	var levels []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			levels = append(levels, p)
		}
	}
	return levels
}
