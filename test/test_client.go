package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// 定义工具的结构体以符合 OpenAI / DeepSeek 规范
type Tool struct {
	Type     string        `json:"type"`
	Function FunctionProto `json:"function"`
}

type FunctionProto struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type Message struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	ReasoningContent string            `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function ToolCallFunction   `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type RequestBody struct {
	Model           string                 `json:"model"`
	Messages        []Message              `json:"messages"`
	Tools           []Tool                 `json:"tools,omitempty"`
	ReasoningEffort string                 `json:"reasoning_effort,omitempty"`
	Thinking        map[string]interface{} `json:"thinking,omitempty"`
	Stream          bool                   `json:"stream"`
	MaxTokens       int                    `json:"max_tokens,omitempty"`
}

type ResponseBody struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type TurnLog struct {
	Turn     int         `json:"turn"`
	Request  interface{} `json:"request"`
	Response interface{} `json:"response"`
}

func executeReadFile(argsStr string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return fmt.Sprintf("解析参数失败: %v", err)
	}
	content, err := os.ReadFile(args.Path)
	if err != nil {
		return fmt.Sprintf("读取文件失败: %v", err)
	}
	return string(content)
}

func executeWriteFile(argsStr string) string {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return fmt.Sprintf("解析参数失败: %v", err)
	}
	if err := os.WriteFile(args.Path, []byte(args.Content), 0644); err != nil {
		return fmt.Sprintf("编辑文件失败: %v", err)
	}
	return "文件写入成功"
}

func main() {
	mode := flag.String("mode", "chat", "运行模式: chat (普通聊天) 或 tool (工具调用)")
	stream := flag.Bool("stream", true, "是否以流式模式请求")
	flag.Parse()

	// 目标代理地址
	targetURL := "http://127.0.0.1:8188/v1/chat/completions"

	// 初始化 test.txt，如果文件夹不存在则创建
	_ = os.MkdirAll("test", 0755)
	if *mode == "tool" {
		err := os.WriteFile("test/test.txt", []byte("这是 test.txt 的初始测试内容。\n"), 0644)
		if err != nil {
			fmt.Printf("无法创建 test/test.txt: %v\n", err)
		} else {
			fmt.Println("【已重置 test/test.txt 文件】")
		}
	}

	var reqBody RequestBody
	reqBody.Model = "deepseek-v4-flash"
	reqBody.ReasoningEffort = "high"
	reqBody.Thinking = map[string]interface{}{"type": "enabled"}
	reqBody.Stream = *stream
	reqBody.MaxTokens = 4096

	var tools []Tool
	if *mode == "tool" {
		fmt.Println("【当前模式：工具调用模拟 AI Agent 编程】")
		reqBody.Messages = []Message{
			{
				Role:    "user",
				Content: "请先读取 test/test.txt 文件的内容，然后在 test/test.txt 文件中编辑写入 'Hello DeepSeek'。",
			},
		}

		tools = []Tool{
			{
				Type: "function",
				Function: FunctionProto{
					Name:        "read_file",
					Description: "读取指定路径的文件内容",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{
								"type":        "string",
								"description": "文件绝对路径或相对路径",
							},
						},
						"required": []string{"path"},
					},
				},
			},
			{
				Type: "function",
				Function: FunctionProto{
					Name:        "write_file",
					Description: "向指定路径写入文件内容",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{
								"type":        "string",
								"description": "文件绝对路径或相对路径",
							},
							"content": map[string]interface{}{
								"type":        "string",
								"description": "要写入的文件内容",
							},
						},
						"required": []string{"path", "content"},
					},
				},
			},
		}
		reqBody.Tools = tools
	} else {
		fmt.Println("【当前模式：最基础的聊天】")
		reqBody.Messages = []Message{
			{
				Role:    "user",
				Content: "你好，请问你是谁？请简短回答。",
			},
		}
	}

	var turnLogs []TurnLog
	currentTurn := 0

	for {
		currentTurn++
		fmt.Printf("\n--- [开始第 %d 轮对话请求] ---\n", currentTurn)

		// 1. 序列化请求体
		reqJSON, err := json.MarshalIndent(reqBody, "", "  ")
		if err != nil {
			fmt.Printf("序列化请求体失败: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("\n======================= [SEND REQUEST JSON] =======================")
		fmt.Println(string(reqJSON))
		fmt.Println("==================================================================\n")

		// 2. 发送请求
		resp, err := http.Post(targetURL, "application/json", bytes.NewReader(reqJSON))
		if err != nil {
			fmt.Printf("发送请求失败，请确保 DSPlus 代理在 8188 端口运行: %v\n", err)
			os.Exit(1)
		}

		var respBodyBytes []byte
		var isJSONResp bool
		var streamChoice *Choice

		if reqBody.Stream {
			isJSONResp = true
			fmt.Println("\n======================= [STREAMING RESPONSES] =======================")
			
			var assistantText strings.Builder
			var reasoningText strings.Builder
			var toolCallsMap = make(map[int]*ToolCall)

			reader := bufio.NewReader(resp.Body)
			for {
				lineBytes, err := reader.ReadBytes('\n')
				if err != nil {
					break
				}
				line := strings.TrimSpace(string(lineBytes))
				if line == "" {
					continue
				}
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if dataStr == "[DONE]" {
					break
				}

				var chunk map[string]interface{}
				if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
					continue
				}

				choices, _ := chunk["choices"].([]interface{})
				if len(choices) > 0 {
					cMap, _ := choices[0].(map[string]interface{})
					if fr, ok := cMap["finish_reason"].(string); ok && fr != "" {
						if streamChoice == nil {
							streamChoice = &Choice{}
						}
						streamChoice.FinishReason = fr
					}
					if delta, ok := cMap["delta"].(map[string]interface{}); ok {
						if content, ok := delta["content"].(string); ok && content != "" {
							assistantText.WriteString(content)
							fmt.Print(content)
						}
						if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
							reasoningText.WriteString(reasoning)
						}
						if tCalls, ok := delta["tool_calls"].([]interface{}); ok {
							for _, tcVal := range tCalls {
								tcMap, _ := tcVal.(map[string]interface{})
								idxVal, _ := tcMap["index"].(float64)
								idx := int(idxVal)
								
								tc, exists := toolCallsMap[idx]
								if !exists {
									tc = &ToolCall{}
									toolCallsMap[idx] = tc
								}
								if id, ok := tcMap["id"].(string); ok {
									tc.ID = id
								}
								if tcType, ok := tcMap["type"].(string); ok {
									tc.Type = tcType
								}
								if fnMap, ok := tcMap["function"].(map[string]interface{}); ok {
									if name, ok := fnMap["name"].(string); ok {
										tc.Function.Name = name
									}
									if args, ok := fnMap["arguments"].(string); ok {
										tc.Function.Arguments += args
									}
								}
							}
						}
					}
				}
			}
			resp.Body.Close()
			fmt.Println("\n=====================================================================")

			var finalToolCalls []ToolCall
			for i := 0; i < len(toolCallsMap); i++ {
				if tc, ok := toolCallsMap[i]; ok {
					finalToolCalls = append(finalToolCalls, *tc)
				}
			}

			streamChoice = &Choice{
				FinishReason: streamChoice.FinishReason,
				Message: Message{
					Role:             "assistant",
					Content:          assistantText.String(),
					ReasoningContent: reasoningText.String(),
					ToolCalls:        finalToolCalls,
				},
			}

			mockResp := ResponseBody{
				ID:      "mock-stream-id",
				Object:  "chat.completion",
				Choices: []Choice{*streamChoice},
			}
			respBodyBytes, _ = json.Marshal(mockResp)
		} else {
			// 3. 读取响应体 (非流模式)
			var err error
			respBodyBytes, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				fmt.Printf("读取响应体失败: %v\n", err)
				os.Exit(1)
			}

			// 4. 美化并打印响应 JSON
			var prettyResp bytes.Buffer
			isJSONResp = true
			if err := json.Indent(&prettyResp, respBodyBytes, "", "  "); err != nil {
				isJSONResp = false
				fmt.Println("\n======================= [RESPONSE RAW BODY] =======================")
				fmt.Println(string(respBodyBytes))
				fmt.Println("==================================================================")
			} else {
				fmt.Println("\n====================== [RECEIVED RESPONSE JSON] ======================")
				fmt.Println(prettyResp.String())
				fmt.Println("======================================================================")
			}
		}

		// 保存当前 turn 的日志
		logTurn := TurnLog{
			Turn: currentTurn,
		}
		var reqBodyObj interface{}
		_ = json.Unmarshal(reqJSON, &reqBodyObj)
		logTurn.Request = reqBodyObj

		if isJSONResp {
			var respBodyObj interface{}
			_ = json.Unmarshal(respBodyBytes, &respBodyObj)
			logTurn.Response = respBodyObj
		} else {
			logTurn.Response = string(respBodyBytes)
		}
		turnLogs = append(turnLogs, logTurn)

		// 如果不是合法的 JSON 响应，直接退出
		if !isJSONResp {
			fmt.Println("收到非 JSON 响应，退出循环")
			break
		}

		// 解析响应，决定下一步
		var respBody ResponseBody
		if err := json.Unmarshal(respBodyBytes, &respBody); err != nil {
			fmt.Printf("反序列化响应体失败: %v\n", err)
			break
		}

		if len(respBody.Choices) == 0 {
			fmt.Println("响应中无 choices，结束")
			break
		}

		choice := respBody.Choices[0]
		
		// 将模型本轮的回复存入历史消息（供下一轮使用）
		assistantMsg := choice.Message
		// 规范要求：回传历史消息时需剥离 reasoning_content 以免 DeepSeek 报 400 错
		assistantMsg.ReasoningContent = ""
		reqBody.Messages = append(reqBody.Messages, assistantMsg)

		if choice.FinishReason == "tool_calls" && len(choice.Message.ToolCalls) > 0 {
			fmt.Printf("\n[模型发起了工具调用，共 %d 个]\n", len(choice.Message.ToolCalls))
			
			// 依次执行工具并向历史追加 tool 回复
			for _, tc := range choice.Message.ToolCalls {
				var toolResult string
				if tc.Function.Name == "read_file" {
					fmt.Printf("-> 执行工具 read_file, 参数: %s\n", tc.Function.Arguments)
					toolResult = executeReadFile(tc.Function.Arguments)
				} else if tc.Function.Name == "write_file" {
					fmt.Printf("-> 执行工具 write_file, 参数: %s\n", tc.Function.Arguments)
					toolResult = executeWriteFile(tc.Function.Arguments)
				} else {
					toolResult = fmt.Sprintf("未知的工具: %s", tc.Function.Name)
				}

				fmt.Printf("-> 执行结果: %s\n", toolResult)

				// 追加 tool 消息到历史中
				toolMsg := Message{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    toolResult,
				}
				reqBody.Messages = append(reqBody.Messages, toolMsg)
			}
			
			// 对于后续多轮，由于我们已经将 tool 的返回信息加到 messages 中了，需要继续循环发送
			// 注意：有工具回传时，根据官方规范需要继续携带 tools 定义
			reqBody.Tools = tools
		} else {
			// 如果是 stop 或其他 finish_reason，则退出 Agent 循环
			fmt.Printf("\n对话自然结束，FinishReason: %s\n", choice.FinishReason)
			break
		}
	}

	// 5. 写入最终日志文件
	logFilePath := "test/chat_log.json"
	if *mode == "tool" {
		logFilePath = "test/tool_log.json"
	}

	logJSON, err := json.MarshalIndent(turnLogs, "", "  ")
	if err == nil {
		if err := os.WriteFile(logFilePath, logJSON, 0644); err != nil {
			fmt.Printf("写入日志文件失败: %v\n", err)
		} else {
			fmt.Printf("\n完整的请求与响应日志已写入: %s\n", logFilePath)
		}
	} else {
		fmt.Printf("序列化日志失败: %v\n", err)
	}
}
