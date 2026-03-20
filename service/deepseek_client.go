package service

import (
	"bytes"
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
	Classification string `json:"classification"` // 分类号
	Author         string `json:"author"`         // 书籍作者
	Nationality    string `json:"nationality"`    // 书籍国籍
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

// NewDeepSeekClient 创建新的DeepSeek客户端
func NewDeepSeekClient(apiKey string) *DeepSeekClient {
	return &DeepSeekClient{
		apiKey: apiKey,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		apiURL: "https://api.deepseek.com/v1/chat/completions",
	}
}

// AnalyzeBook 根据书名和书籍前3页内容，分析并返回分类号、作者、国籍
func (c *DeepSeekClient) AnalyzeBook(bookName, bookContent string) (*BookAnalysis, error) {
	systemPrompt := `你是一个专业的图书分类助手。请根据提供的书名和书籍前3页内容，完成以下任务：
1. 使用中图法第五版进行分类，给出最合适的中国图书分类法分类号（格式如K837.127）
2. 分析并确定书籍作者
3. 分析并确定作者/书籍的国籍

请严格按照以下格式返回，每行一项，不要有多余内容：
分类号：xxx
书籍作者：xxx
书籍国籍：xxx`

	userContent := fmt.Sprintf("书名：%s\n\n书籍前3页内容：\n%s", bookName, bookContent)
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

// parseBookAnalysis 从API响应中解析出分类号、作者、国籍
func parseBookAnalysis(content string) (*BookAnalysis, error) {
	result := &BookAnalysis{}

	// 匹配 "分类号：" 或 "分类号:" 后的内容
	patterns := map[string]*string{
		"分类号":  &result.Classification,
		"书籍作者": &result.Author,
		"书籍国籍": &result.Nationality,
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		for key, dest := range patterns {
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

	// 备用：正则匹配
	if result.Classification == "" || result.Author == "" || result.Nationality == "" {
		reClass := regexp.MustCompile(`分类号[：:]\s*([^\n]+)`)
		reAuthor := regexp.MustCompile(`书籍作者[：:]\s*([^\n]+)`)
		reNation := regexp.MustCompile(`书籍国籍[：:]\s*([^\n]+)`)
		if m := reClass.FindStringSubmatch(content); len(m) > 1 {
			result.Classification = strings.TrimSpace(m[1])
		}
		if m := reAuthor.FindStringSubmatch(content); len(m) > 1 {
			result.Author = strings.TrimSpace(m[1])
		}
		if m := reNation.FindStringSubmatch(content); len(m) > 1 {
			result.Nationality = strings.TrimSpace(m[1])
		}
	}

	return result, nil
}
