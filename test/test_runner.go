package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type TestCase struct {
	ID               int
	Name             string
	Path             string
	RequestBody      string
	ExpectedFormat   string
	ExpectedSemantic string
}

func getLatestProxyLog() (map[string]interface{}, error) {
	data, err := os.ReadFile("test/proxy_debug_logs.jsonl")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		return nil, fmt.Errorf("日志为空")
	}
	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func main() {
	targetURL := "http://127.0.0.1:8188"

	// 设计测试用例矩阵
	testCases := []TestCase{
		{
			ID:   1,
			Name: "OpenAI 基础单轮聊天 (无 max_tokens, 无 system)",
			Path: "/v1/chat/completions",
			RequestBody: `{
				"model": "deepseek-v4-flash",
				"messages": [{"role": "user", "content": "你好，请问你是谁？请简短回答。"}]
			}`,
			ExpectedFormat:   "openai",
			ExpectedSemantic: "chat",
		},
		{
			ID:   2,
			Name: "OpenAI 携带系统提示词 (messages 包含 system)",
			Path: "/v1/chat/completions",
			RequestBody: `{
				"model": "deepseek-v4-flash",
				"messages": [
					{"role": "system", "content": "You are a polite assistant."},
					{"role": "user", "content": "你好，请问你是谁？请简短回答。"}
				]
			}`,
			ExpectedFormat:   "openai",
			ExpectedSemantic: "chat",
		},
		{
			ID:   3,
			Name: "OpenAI 携带 max_tokens 且无 system 消息",
			Path: "/v1/chat/completions",
			RequestBody: `{
				"model": "deepseek-v4-flash",
				"messages": [{"role": "user", "content": "你好，请问你是谁？请简短回答。"}],
				"max_tokens": 4096
			}`,
			ExpectedFormat:   "openai",
			ExpectedSemantic: "chat",
		},
		{
			ID:   4,
			Name: "OpenAI 工具调用请求触发 (声明 tools)",
			Path: "/v1/chat/completions",
			RequestBody: `{
				"model": "deepseek-v4-flash",
				"messages": [{"role": "user", "content": "请读取 test/test.txt 文件的内容。"}],
				"tools": [
					{
						"type": "function",
						"function": {
							"name": "read_file",
							"description": "读取文件",
							"parameters": {
								"type": "object",
								"properties": {
									"path": {"type": "string"}
								},
								"required": ["path"]
							}
						}
					}
				]
			}`,
			ExpectedFormat:   "openai",
			ExpectedSemantic: "chat",
		},
		{
			ID:   5,
			Name: "OpenAI 工具执行结果回传 (包含 role: tool)",
			Path: "/v1/chat/completions",
			RequestBody: `{
				"model": "deepseek-v4-flash",
				"messages": [
					{"role": "user", "content": "请读取 test/test.txt"},
					{
						"role": "assistant",
						"content": "",
						"tool_calls": [{
							"id": "call_mock_123",
							"type": "function",
							"function": {"name": "read_file", "arguments": "{\"path\":\"test/test.txt\"}"}
						}]
					},
					{"role": "tool", "tool_call_id": "call_mock_123", "content": "这是 test.txt 的测试内容"}
				]
			}`,
			ExpectedFormat:   "openai",
			ExpectedSemantic: "tool_result",
		},
		{
			ID:   6,
			Name: "Anthropic 基础单轮聊天 (含 max_tokens, 无 system)",
			Path: "/v1/messages",
			RequestBody: `{
				"model": "deepseek-v4-flash",
				"messages": [{"role": "user", "content": "你好，请问你是谁？请简短回答。"}],
				"max_tokens": 4096
			}`,
			ExpectedFormat:   "anthropic",
			ExpectedSemantic: "chat",
		},
		{
			ID:   7,
			Name: "Anthropic 携带顶层 system 提示词",
			Path: "/v1/messages",
			RequestBody: `{
				"model": "deepseek-v4-flash",
				"system": "You are a helpful assistant.",
				"messages": [{"role": "user", "content": "你好，请问你是谁？请简短回答。"}],
				"max_tokens": 4096
			}`,
			ExpectedFormat:   "anthropic",
			ExpectedSemantic: "chat",
		},
		{
			ID:   8,
			Name: "Anthropic 工具调用结果回传 (type: tool_result)",
			Path: "/v1/messages",
			RequestBody: `{
				"model": "deepseek-v4-flash",
				"messages": [
					{"role": "user", "content": "请读取 test/test.txt"},
					{
						"role": "assistant",
						"content": [{
							"type": "tool_use",
							"id": "call_mock_456",
							"name": "read_file",
							"input": {"path": "test/test.txt"}
						}]
					},
					{
						"role": "user",
						"content": [{
							"type": "tool_result",
							"tool_use_id": "call_mock_456",
							"content": "这是测试内容"
						}]
					}
				],
				"max_tokens": 4096
			}`,
			ExpectedFormat:   "anthropic",
			ExpectedSemantic: "tool_result",
		},
		{
			ID:   9,
			Name: "OpenAI 多轮普通聊天历史请求",
			Path: "/v1/chat/completions",
			RequestBody: `{
				"model": "deepseek-v4-flash",
				"messages": [
					{"role": "user", "content": "你好"},
					{"role": "assistant", "content": "你好！有什么我可以帮你的？"},
					{"role": "user", "content": "请简短自我介绍下。"}
				]
			}`,
			ExpectedFormat:   "openai",
			ExpectedSemantic: "chat",
		},
	}

	fmt.Printf("=== 开始批量集成兼容性测试 (共 %d 个用例) ===\n\n", len(testCases))
	
	failedCount := 0
	passedCount := 0

	for _, tc := range testCases {
		fmt.Printf("[%d/%d] 正在执行: %s\n", tc.ID, len(testCases), tc.Name)

		// 整理 JSON
		var prettyReq bytes.Buffer
		_ = json.Indent(&prettyReq, []byte(tc.RequestBody), "", "  ")

		// 发请求
		reqUrl := targetURL + tc.Path
		resp, err := http.Post(reqUrl, "application/json", bytes.NewReader([]byte(tc.RequestBody)))
		if err != nil {
			fmt.Printf("  -> ❌ 请求发送失败: %v\n\n", err)
			failedCount++
			continue
		}
		
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()

		// 给代理写入日志一些缓冲时间
		time.Sleep(200 * time.Millisecond)

		// 读日志比对
		logEntry, err := getLatestProxyLog()
		if err != nil {
			fmt.Printf("  -> ❌ 无法获取代理的 debug 日志: %v\n\n", err)
			failedCount++
			continue
		}

		actualFormat, _ := logEntry["format"].(string)
		actualSemantic, _ := logEntry["semantic_type"].(string)
		statusCodeVal, _ := logEntry["status_code"].(float64)
		statusCode := int(statusCodeVal)

		formatPass := actualFormat == tc.ExpectedFormat
		semanticPass := actualSemantic == tc.ExpectedSemantic
		statusPass := statusCode == 200

		if formatPass && semanticPass && statusPass {
			fmt.Printf("  ->  测试通过! (Format: %s, Semantic: %s, HTTP Status: 200)\n\n", actualFormat, actualSemantic)
			passedCount++
		} else {
			fmt.Println("  -> ❌ 测试失败！比对明细:")
			if !formatPass {
				fmt.Printf("     * 协议判定错误: 预期 %q, 实际判定为 %q\n", tc.ExpectedFormat, actualFormat)
			}
			if !semanticPass {
				fmt.Printf("     * 语义判定错误: 预期 %q, 实际判定为 %q\n", tc.ExpectedSemantic, actualSemantic)
			}
			if !statusPass {
				fmt.Printf("     * 上游响应异常: 预期 HTTP 200, 实际返回为 HTTP %d\n", statusCode)
			}
			fmt.Println()
			failedCount++
		}
	}

	fmt.Println("==========================================")
	fmt.Printf("测试运行完成。通过: %d, 失败: %d\n", passedCount, failedCount)
	fmt.Println("==========================================")
}
