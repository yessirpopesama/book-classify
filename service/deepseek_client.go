package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// BookAnalysis 书籍分析结果
type BookAnalysis struct {
	Classification   string   `json:"classification"`    // 分类号
	ClassificationPath string `json:"classification_path"` // 最优分类路径
	CategoryLevels   []string `json:"category_levels"`   // 完整类目层级（从一级到最专指）
	LibraryReference string   `json:"library_reference"` // 各馆参考分类对照
	Author           string   `json:"author"`            // 书籍作者
	Nationality      string   `json:"nationality"`       // 书籍国籍
}

// DeepSeekClient DeepSeek API客户端
type DeepSeekClient struct {
	apiKey string
	client *http.Client
	apiURL string
}

// ChatMessage 聊天消息结构
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 聊天请求结构
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ChatResponse 聊天响应结构
type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// NewDeepSeekClient 创建新的 DeepSeek 客户端（不设 HTTP 超时，避免批量分类时被中断）。
func NewDeepSeekClient(apiKey string) *DeepSeekClient {
	return &DeepSeekClient{
		apiKey: apiKey,
		client: &http.Client{
			Timeout: 0,
		},
		apiURL: "https://api.deepseek.com/v1/chat/completions",
	}
}

// AnalyzeBook 根据书名和正文第一页（约1000字）内容，分析并返回分类号、作者、国籍
func (c *DeepSeekClient) AnalyzeBook(bookName, bookContent string) (*BookAnalysis, error) {
	systemPrompt := `你是一名资深图书管理员，精通《中国图书馆分类法》（中图法）第五版（最新版）及其分类规则。请根据提供的书名和正文第一页（约1000字）内容，完成以下任务：

1. 分类号：以正文内容为主要依据（书名仅作辅助），按中图法第五版选取最贴切的基本类号；需要时可加复分号（如 I242.1、K837.127、R2-52）。
2. 图书馆参考：参照全国主要图书馆对同类文献的中图法著录惯例（如国家图书馆、上海图书馆、北京大学图书馆、CALIS 联合目录、浙江图书馆、广东省立中山图书馆等），列出 2～4 个可能的分类号及所属层级差异，说明各馆取舍依据。
3. 最优分类路径：综合各馆实践与文献内容，选定最专指且符合中图法第五版规则的最优类目，给出从一级类到最终类目的完整路径（用 " > " 连接各级，含类号与类名，如 I 文学 > I2 中国文学 > I242 散文、随笔 > I242.1 作品综合集）。
4. 完整类目层级：列出最优路径上每一级类目，用 " | " 分隔（与最优分类路径层级一一对应）。
5. 书籍作者：从正文或书名中识别主要作者（编者、译者不作为作者，除非无明确作者）。
6. 书籍国籍：给出作者所属国家或地区（如中国、美国）。

请严格按照以下格式返回，每行一项，不要有多余内容：
分类号：xxx
图书馆参考：xxx
最优分类路径：xxx
完整类目层级：xxx
书籍作者：xxx
书籍国籍：xxx`

	userContent := fmt.Sprintf("书名：%s\n\n正文第一页：\n%s", bookName, bookContent)
	if bookContent == "" {
		userContent = fmt.Sprintf("书名：%s\n\n（无正文内容，请仅根据书名推断）", bookName)
	}

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent},
	}

	reqBody := ChatRequest{
		Model:    "deepseek-chat",
		Messages: messages,
		Stream:   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	req, err := http.NewRequest("POST", c.apiURL, bytes.NewBuffer(jsonData))
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
	return parseBookAnalysis(content)
}

// parseBookAnalysis 从API响应中解析出分类号、分类路径、层级、作者、国籍
func parseBookAnalysis(content string) (*BookAnalysis, error) {
	result := &BookAnalysis{}

	stringFields := map[string]*string{
		"分类号":    &result.Classification,
		"图书馆参考":  &result.LibraryReference,
		"最优分类路径": &result.ClassificationPath,
		"书籍作者":   &result.Author,
		"书籍国籍":   &result.Nationality,
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "完整类目层级：") || strings.HasPrefix(line, "完整类目层级:") {
			sep := "："
			if strings.Contains(line, ":") && !strings.Contains(line, "：") {
				sep = ":"
			}
			parts := strings.SplitN(line, sep, 2)
			if len(parts) == 2 {
				result.CategoryLevels = splitCategoryLevels(strings.TrimSpace(parts[1]))
			}
			continue
		}
		for key, dest := range stringFields {
			if strings.HasPrefix(line, key+"：") || strings.HasPrefix(line, key+":") {
				sep := "："
				if strings.Contains(line, ":") && !strings.Contains(line, "：") {
					sep = ":"
				}
				parts := strings.SplitN(line, sep, 2)
				if len(parts) == 2 {
					*dest = strings.TrimSpace(parts[1])
				}
				break
			}
		}
	}

	fallback := map[string]*string{
		`分类号[：:]\s*([^\n]+)`:    &result.Classification,
		`图书馆参考[：:]\s*([^\n]+)`:  &result.LibraryReference,
		`最优分类路径[：:]\s*([^\n]+)`: &result.ClassificationPath,
		`书籍作者[：:]\s*([^\n]+)`:   &result.Author,
		`书籍国籍[：:]\s*([^\n]+)`:   &result.Nationality,
	}
	for pattern, dest := range fallback {
		if *dest != "" {
			continue
		}
		re := regexp.MustCompile(pattern)
		if m := re.FindStringSubmatch(content); len(m) > 1 {
			*dest = strings.TrimSpace(m[1])
		}
	}
	if len(result.CategoryLevels) == 0 {
		reLevels := regexp.MustCompile(`完整类目层级[：:]\s*([^\n]+)`)
		if m := reLevels.FindStringSubmatch(content); len(m) > 1 {
			result.CategoryLevels = splitCategoryLevels(strings.TrimSpace(m[1]))
		}
	}

	if result.ClassificationPath == "" && len(result.CategoryLevels) > 0 {
		result.ClassificationPath = strings.Join(result.CategoryLevels, " > ")
	}
	if len(result.CategoryLevels) == 0 && result.ClassificationPath != "" {
		result.CategoryLevels = splitCategoryLevels(strings.ReplaceAll(result.ClassificationPath, ">", "|"))
	}

	return result, nil
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
