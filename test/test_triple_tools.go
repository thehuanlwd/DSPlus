package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

// ==================== OpenAI 结构定义 ====================

type OpenAITool struct {
	Type     string            `json:"type"`
	Function OpenAIFunctionDef `json:"function"`
}

type OpenAIFunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type OpenAIMessage struct {
	Role             string            `json:"role"`
	Content          interface{}       `json:"content"` // string or array (for multi-modal/tool_result in some format)
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ToolCalls        []OpenAIToolCall  `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
}

type OpenAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function OpenAIFunctionCall `json:"function"`
}

type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type OpenAIRequest struct {
	Model           string                 `json:"model"`
	Messages        []OpenAIMessage        `json:"messages"`
	Tools           []OpenAITool           `json:"tools,omitempty"`
	ReasoningEffort string                 `json:"reasoning_effort,omitempty"`
	Thinking        map[string]interface{} `json:"thinking,omitempty"`
	Stream          bool                   `json:"stream"`
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Choices []OpenAIChoice `json:"choices"`
}

type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// ==================== Anthropic 结构定义 ====================

type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type AnthropicContentPart struct {
	Type       string                 `json:"type"`
	Text       string                 `json:"text,omitempty"`
	ID         string                 `json:"id,omitempty"`           // tool_use
	Name       string                 `json:"name,omitempty"`         // tool_use
	Input      map[string]interface{} `json:"input,omitempty"`        // tool_use
	ToolUseID  string                 `json:"tool_use_id,omitempty"`  // tool_result
	Content    string                 `json:"content,omitempty"`       // tool_result
}

type AnthropicMessage struct {
	Role    string                 `json:"role"`
	Content []AnthropicContentPart `json:"content"`
}

type AnthropicRequest struct {
	Model           string             `json:"model"`
	Messages        []AnthropicMessage `json:"messages"`
	System          string             `json:"system,omitempty"`
	Tools           []AnthropicTool    `json:"tools,omitempty"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
	Thinking        map[string]interface{} `json:"thinking,omitempty"`
	MaxTokens       int                `json:"max_tokens"`
	Stream          bool               `json:"stream"`
}

type AnthropicResponse struct {
	ID         string                 `json:"id"`
	Content    []AnthropicContentPart `json:"content"`
	StopReason string                 `json:"stop_reason"`
}

// ==================== 公共日志辅助结构 ====================

type TurnLog struct {
	Turn     int         `json:"turn"`
	Request  interface{} `json:"request"`
	Response interface{} `json:"response"`
}

// ==================== 工具执行逻辑 ====================

func executeReadFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("读取文件失败: %v", err)
	}
	return string(content)
}

func executeWriteFile(path string, content string) string {
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return fmt.Sprintf("写入文件失败: %v", err)
	}
	return "文件写入成功"
}

func main() {
	format := flag.String("format", "openai", "测试格式: openai 或 anthropic")
	flag.Parse()

	// 1. 重置测试文件 test/test.txt
	_ = os.MkdirAll("test", 0755)
	err := os.WriteFile("test/test.txt", []byte("这是 test.txt 初始内容。\n"), 0644)
	if err != nil {
		fmt.Printf("无法重置 test/test.txt: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("【已重置 test/test.txt 初始内容】")

	var turnLogs []TurnLog

	if *format == "openai" {
		runOpenAITest(&turnLogs)
	} else if *format == "anthropic" {
		runAnthropicTest(&turnLogs)
	} else {
		fmt.Println("未知的格式，请指定 -format openai 或 -format anthropic")
		os.Exit(1)
	}

	// 写入日志文件
	logPath := fmt.Sprintf("test/triple_%s_log.json", *format)
	logJSON, err := json.MarshalIndent(turnLogs, "", "  ")
	if err == nil {
		_ = os.WriteFile(logPath, logJSON, 0644)
		fmt.Printf("\n3次工具链测试完成。完整的请求与响应日志已写入: %s\n", logPath)
	}
}

// ==================== OpenAI 3次工具调用主逻辑 ====================
func runOpenAITest(logs *[]TurnLog) {
	fmt.Println("【开始执行：OpenAI 3次连续工具调用测试】")
	targetURL := "http://127.0.0.1:8188/v1/chat/completions"

	reqBody := OpenAIRequest{
		Model:           "deepseek-v4-flash",
		ReasoningEffort: "high",
		Thinking:        map[string]interface{}{"type": "enabled"},
		Stream:          false,
		Messages: []OpenAIMessage{
			{
				Role:    "user",
				Content: "请先读取 test/test.txt 文件的内容，然后在 test/test.txt 文件中编辑写入 'Hello World'，最后再次读取该文件内容以确认写入成功。",
			},
		},
		Tools: []OpenAITool{
			{
				Type: "function",
				Function: OpenAIFunctionDef{
					Name:        "read_file",
					Description: "读取指定路径的文件内容",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{"type": "string", "description": "文件路径"},
						},
						"required": []string{"path"},
					},
				},
			},
			{
				Type: "function",
				Function: OpenAIFunctionDef{
					Name:        "write_file",
					Description: "向指定路径写入文件内容",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path":    map[string]interface{}{"type": "string", "description": "文件路径"},
							"content": map[string]interface{}{"type": "string", "description": "要写入的内容"},
						},
						"required": []string{"path", "content"},
					},
				},
			},
		},
	}

	currentTurn := 0
	for {
		currentTurn++
		fmt.Printf("\n--- [开始 OpenAI 第 %d 轮对话] ---\n", currentTurn)

		reqJSON, _ := json.MarshalIndent(reqBody, "", "  ")
		fmt.Println("======================= [SEND REQUEST JSON] =======================")
		fmt.Println(string(reqJSON))
		fmt.Println("==================================================================\n")

		resp, err := http.Post(targetURL, "application/json", bytes.NewReader(reqJSON))
		if err != nil {
			fmt.Printf("发送请求失败: %v\n", err)
			break
		}

		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var prettyResp bytes.Buffer
		_ = json.Indent(&prettyResp, respBytes, "", "  ")
		fmt.Println("====================== [RECEIVED RESPONSE JSON] ======================")
		fmt.Println(prettyResp.String())
		fmt.Println("======================================================================")

		// 记录日志
		var reqObj, respObj interface{}
		_ = json.Unmarshal(reqJSON, &reqObj)
		_ = json.Unmarshal(respBytes, &respObj)
		*logs = append(*logs, TurnLog{Turn: currentTurn, Request: reqObj, Response: respObj})

		var respBody OpenAIResponse
		_ = json.Unmarshal(respBytes, &respBody)

		if len(respBody.Choices) == 0 {
			break
		}

		choice := respBody.Choices[0]
		assistantMsg := choice.Message
		assistantMsg.ReasoningContent = "" // 规避 DeepSeek 400 校验
		reqBody.Messages = append(reqBody.Messages, assistantMsg)

		if choice.FinishReason == "tool_calls" && len(choice.Message.ToolCalls) > 0 {
			for _, tc := range choice.Message.ToolCalls {
				var toolResult string
				var args map[string]string
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)

				if tc.Function.Name == "read_file" {
					fmt.Printf("-> 本地执行工具 read_file, 参数: %s\n", tc.Function.Arguments)
					toolResult = executeReadFile(args["path"])
				} else if tc.Function.Name == "write_file" {
					fmt.Printf("-> 本地执行工具 write_file, 参数: %s\n", tc.Function.Arguments)
					toolResult = executeWriteFile(args["path"], args["content"])
				}

				fmt.Printf("-> 执行结果: %s\n", toolResult)

				reqBody.Messages = append(reqBody.Messages, OpenAIMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    toolResult,
				})
			}
		} else {
			fmt.Printf("\n对话自然结束，FinishReason: %s\n", choice.FinishReason)
			break
		}
	}
}

// ==================== Anthropic 3次工具调用主逻辑 ====================
func runAnthropicTest(logs *[]TurnLog) {
	fmt.Println("【开始执行：Anthropic 3次连续工具调用测试】")
	targetURL := "http://127.0.0.1:8188/v1/messages"

	reqBody := AnthropicRequest{
		Model:           "deepseek-v4-flash",
		ReasoningEffort: "high",
		Thinking:        map[string]interface{}{"type": "enabled"},
		MaxTokens:       4096,
		Stream:          false,
		Messages: []AnthropicMessage{
			{
				Role: "user",
				Content: []AnthropicContentPart{
					{
						Type: "text",
						Text: "请先读取 test/test.txt 文件的内容，然后在 test/test.txt 文件中编辑写入 'Hello World'，最后再次读取该文件内容以确认写入成功。",
					},
				},
			},
		},
		Tools: []AnthropicTool{
			{
				Name:        "read_file",
				Description: "读取指定路径的文件内容",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{"type": "string", "description": "文件路径"},
					},
					"required": []string{"path"},
				},
			},
			{
				Name:        "write_file",
				Description: "向指定路径写入文件内容",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path":    map[string]interface{}{"type": "string", "description": "文件路径"},
						"content": map[string]interface{}{"type": "string", "description": "要写入的内容"},
					},
					"required": []string{"path", "content"},
				},
			},
		},
	}

	currentTurn := 0
	for {
		currentTurn++
		fmt.Printf("\n--- [开始 Anthropic 第 %d 轮对话] ---\n", currentTurn)

		reqJSON, _ := json.MarshalIndent(reqBody, "", "  ")
		fmt.Println("======================= [SEND REQUEST JSON] =======================")
		fmt.Println(string(reqJSON))
		fmt.Println("==================================================================\n")

		resp, err := http.Post(targetURL, "application/json", bytes.NewReader(reqJSON))
		if err != nil {
			fmt.Printf("发送请求失败: %v\n", err)
			break
		}

		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var prettyResp bytes.Buffer
		_ = json.Indent(&prettyResp, respBytes, "", "  ")
		fmt.Println("====================== [RECEIVED RESPONSE JSON] ======================")
		fmt.Println(prettyResp.String())
		fmt.Println("======================================================================")

		// 记录日志
		var reqObj, respObj interface{}
		_ = json.Unmarshal(reqJSON, &reqObj)
		_ = json.Unmarshal(respBytes, &respObj)
		*logs = append(*logs, TurnLog{Turn: currentTurn, Request: reqObj, Response: respObj})

		var respBody AnthropicResponse
		_ = json.Unmarshal(respBytes, &respBody)

		if len(respBody.Content) == 0 {
			break
		}

		// 提取模型回复，并构造成历史消息（清除 thinking，保留 tool_use）
		var assistantParts []AnthropicContentPart
		var toolUses []AnthropicContentPart

		for _, part := range respBody.Content {
			if part.Type == "thinking" {
				continue // 规避 DeepSeek 400 校验，不传回上一轮的 thinking
			}
			if part.Type == "text" && part.Text != "" {
				assistantParts = append(assistantParts, part)
			}
			if part.Type == "tool_use" {
				assistantParts = append(assistantParts, part)
				toolUses = append(toolUses, part)
			}
		}

		// 将模型上一轮的 assistant 消息存入历史
		reqBody.Messages = append(reqBody.Messages, AnthropicMessage{
			Role:    "assistant",
			Content: assistantParts,
		})

		if respBody.StopReason == "tool_use" && len(toolUses) > 0 {
			var toolResultParts []AnthropicContentPart

			for _, tu := range toolUses {
				var toolResult string
				pathVal, _ := tu.Input["path"].(string)

				if tu.Name == "read_file" {
					fmt.Printf("-> 本地执行工具 read_file, 参数: path=%s\n", pathVal)
					toolResult = executeReadFile(pathVal)
				} else if tu.Name == "write_file" {
					contentVal, _ := tu.Input["content"].(string)
					fmt.Printf("-> 本地执行工具 write_file, 参数: path=%s, content=%s\n", pathVal, contentVal)
					toolResult = executeWriteFile(pathVal, contentVal)
				}

				fmt.Printf("-> 执行结果: %s\n", toolResult)

				toolResultParts = append(toolResultParts, AnthropicContentPart{
					Type:      "tool_result",
					ToolUseID: tu.ID,
					Content:   toolResult,
				})
			}

			// 将工具执行结果作为 user 消息添加到历史中
			reqBody.Messages = append(reqBody.Messages, AnthropicMessage{
				Role:    "user",
				Content: toolResultParts,
			})
		} else {
			fmt.Printf("\n对话自然结束，StopReason: %s\n", respBody.StopReason)
			break
		}
	}
}
