package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 模拟本地执行的工具
func executeWriteTool(tmpDir string, args map[string]interface{}) string {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return "error: missing path"
	}
	fullPath := filepath.Join(tmpDir, path)
	err := os.WriteFile(fullPath, []byte(content), 0644)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return "success"
}

func executeReadTool(tmpDir string, args map[string]interface{}) string {
	path, _ := args["path"].(string)
	if path == "" {
		return "error: missing path"
	}
	fullPath := filepath.Join(tmpDir, path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return string(data)
}

func executeEditTool(tmpDir string, args map[string]interface{}) string {
	path, _ := args["path"].(string)
	target, _ := args["target"].(string)
	replacement, _ := args["replacement"].(string)
	if path == "" {
		return "error: missing path"
	}
	fullPath := filepath.Join(tmpDir, path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, target) {
		return fmt.Sprintf("error: target %q not found in file", target)
	}
	newContent := strings.ReplaceAll(content, target, replacement)
	err = os.WriteFile(fullPath, []byte(newContent), 0644)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return "success"
}

// 模拟 Upstream Server 的处理器
type mockUpstreamHandler struct {
	t *testing.T
}

func (h *mockUpstreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	r.Body.Close()

	var req map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}

	stream, _ := req["stream"].(bool)
	isAnthropic := strings.Contains(r.URL.Path, "/messages")

	if isAnthropic {
		h.handleAnthropic(w, req, stream)
	} else {
		h.handleOpenAI(w, req, stream)
	}
}

func (h *mockUpstreamHandler) handleOpenAI(w http.ResponseWriter, req map[string]interface{}, stream bool) {
	messages, _ := req["messages"].([]interface{})
	if len(messages) == 0 {
		http.Error(w, "empty messages", 400)
		return
	}

	lastMsg := messages[len(messages)-1].(map[string]interface{})
	role, _ := lastMsg["role"].(string)

	// 根据消息历史判断轮次
	round := 1
	if role == "tool" {
		name, _ := lastMsg["name"].(string)
		if name == "write" {
			round = 2
		} else if name == "read" {
			round = 3
		} else if name == "edit" {
			round = 4
		}
	}

	var responseContent string
	var toolCalls []map[string]interface{}

	switch round {
	case 1:
		responseContent = "好的，我要先写入测试文件。"
		toolCalls = []map[string]interface{}{
			{
				"id":   "call_write_openai",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "write",
					"arguments": `{"path":"test_openai.txt","content":"Line 1\nLine 2"}`,
				},
			},
		}
	case 2:
		responseContent = "写入成功。接下来我要读取这个文件。"
		toolCalls = []map[string]interface{}{
			{
				"id":   "call_read_openai",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "read",
					"arguments": `{"path":"test_openai.txt"}`,
				},
			},
		}
	case 3:
		responseContent = "读取成功。现在我要修改文件。"
		toolCalls = []map[string]interface{}{
			{
				"id":   "call_edit_openai",
				"type": "function",
				"function": map[string]interface{}{
					"name":      "edit",
					"arguments": `{"path":"test_openai.txt","target":"Line 2","replacement":"Line 2 Modified"}`,
				},
			},
		}
	case 4:
		responseContent = "修改成功，工具自动测试流程已全部通过！"
	}

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// 逐步输出 content
		if responseContent != "" {
			chunk := map[string]interface{}{
				"choices": []interface{}{
					map[string]interface{}{
						"delta": map[string]interface{}{
							"role":    "assistant",
							"content": responseContent,
						},
					},
				},
			}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(b))
		}

		// 输出 tool_calls (如果是 1-3 轮)
		if len(toolCalls) > 0 {
			for i, tc := range toolCalls {
				fn := tc["function"].(map[string]interface{})
				args := fn["arguments"].(string)
				// 分段发送参数
				chunk1 := map[string]interface{}{
					"choices": []interface{}{
						map[string]interface{}{
							"delta": map[string]interface{}{
								"tool_calls": []interface{}{
									map[string]interface{}{
										"index": i,
										"id":    tc["id"],
										"type":  "function",
										"function": map[string]interface{}{
											"name":      fn["name"],
											"arguments": args[:len(args)/2],
										},
									},
								},
							},
						},
					},
				}
				b1, _ := json.Marshal(chunk1)
				fmt.Fprintf(w, "data: %s\n\n", string(b1))

				chunk2 := map[string]interface{}{
					"choices": []interface{}{
						map[string]interface{}{
							"delta": map[string]interface{}{
								"tool_calls": []interface{}{
									map[string]interface{}{
										"index": i,
										"function": map[string]interface{}{
											"arguments": args[len(args)/2:],
										},
									},
								},
							},
						},
					},
				}
				b2, _ := json.Marshal(chunk2)
				fmt.Fprintf(w, "data: %s\n\n", string(b2))
			}
		}

		// 输出结束
		finishReason := "stop"
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		}
		endChunk := map[string]interface{}{
			"choices": []interface{}{
				map[string]interface{}{
					"delta":         map[string]interface{}{},
					"finish_reason": finishReason,
				},
			},
		}
		be, _ := json.Marshal(endChunk)
		fmt.Fprintf(w, "data: %s\n\n", string(be))
		fmt.Fprint(w, "data: [DONE]\n\n")
	} else {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"id":      "chatcmpl-mock",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "deepseek-v4-pro",
			"choices": []interface{}{
				map[string]interface{}{
					"index": 0,
					"message": map[string]interface{}{
						"role":       "assistant",
						"content":    responseContent,
						"tool_calls": toolCalls,
					},
					"finish_reason": condStr(len(toolCalls) > 0, "tool_calls", "stop"),
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}
}

func (h *mockUpstreamHandler) handleAnthropic(w http.ResponseWriter, req map[string]interface{}, stream bool) {
	messages, _ := req["messages"].([]interface{})
	if len(messages) == 0 {
		http.Error(w, "empty messages", 400)
		return
	}

	lastMsg := messages[len(messages)-1].(map[string]interface{})

	// 判断 Anthropic 轮次
	round := 1
	cArr, ok := lastMsg["content"].([]interface{})
	if ok && len(cArr) > 0 {
		firstPart, ok := cArr[0].(map[string]interface{})
		if ok {
			pType, _ := firstPart["type"].(string)
			if pType == "tool_result" {
				toolName, _ := firstPart["tool_use_id"].(string) // 我们把 id 设为 write, read, edit 即可
				if strings.Contains(toolName, "write") {
					round = 2
				} else if strings.Contains(toolName, "read") {
					round = 3
				} else if strings.Contains(toolName, "edit") {
					round = 4
				}
			}
		}
	}

	var responseContent string
	var toolUse map[string]interface{}

	switch round {
	case 1:
		responseContent = "好的，使用 Anthropic，我需要先调用 write 工具写入测试文件。"
		toolUse = map[string]interface{}{
			"type": "tool_use",
			"id":   "toolu_write_anthropic",
			"name": "write",
			"input": map[string]interface{}{
				"path":    "test_anthropic.txt",
				"content": "Anthropic Line 1\nAnthropic Line 2",
			},
		}
	case 2:
		responseContent = "写入成功。接下来我要读取这个文件。"
		toolUse = map[string]interface{}{
			"type": "tool_use",
			"id":   "toolu_read_anthropic",
			"name": "read",
			"input": map[string]interface{}{
				"path": "test_anthropic.txt",
			},
		}
	case 3:
		responseContent = "读取成功。现在我要修改文件。"
		toolUse = map[string]interface{}{
			"type": "tool_use",
			"id":   "toolu_edit_anthropic",
			"name": "edit",
			"input": map[string]interface{}{
				"path":        "test_anthropic.txt",
				"target":      "Anthropic Line 2",
				"replacement": "Anthropic Line 2 Modified",
			},
		}
	case 4:
		responseContent = "修改成功，Anthropic 工具自动测试流程已全数通过！"
	}

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Anthropic SSE structure
		fmt.Fprintf(w, "data: %s\n\n", `{"type":"message_start","message":{"id":"msg_mock","type":"message","role":"assistant","content":[],"model":"deepseek-v4-pro"}}`)

		// Text content block
		if responseContent != "" {
			fmt.Fprintf(w, "data: %s\n\n", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
			chunk := map[string]interface{}{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": responseContent,
				},
			}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			fmt.Fprintf(w, "data: %s\n\n", `{"type":"content_block_stop","index":0}`)
		}

		// Tool use block
		if toolUse != nil {
			idx := 1
			if responseContent == "" {
				idx = 0
			}
			// content_block_start for tool_use
			startChunk := map[string]interface{}{
				"type":  "content_block_start",
				"index": idx,
				"content_block": map[string]interface{}{
					"type":  "tool_use",
					"id":    toolUse["id"],
					"name":  toolUse["name"],
					"input": map[string]interface{}{},
				},
			}
			bs, _ := json.Marshal(startChunk)
			fmt.Fprintf(w, "data: %s\n\n", string(bs))

			// content_block_delta for tool input
			inputBytes, _ := json.Marshal(toolUse["input"])
			inputStr := string(inputBytes)
			deltaChunk1 := map[string]interface{}{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]interface{}{
					"type":         "input_json_delta",
					"partial_json": inputStr[:len(inputStr)/2],
				},
			}
			bd1, _ := json.Marshal(deltaChunk1)
			fmt.Fprintf(w, "data: %s\n\n", string(bd1))

			deltaChunk2 := map[string]interface{}{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]interface{}{
					"type":         "input_json_delta",
					"partial_json": inputStr[len(inputStr)/2:],
				},
			}
			bd2, _ := json.Marshal(deltaChunk2)
			fmt.Fprintf(w, "data: %s\n\n", string(bd2))

			fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, idx))
		}

		stopReason := "end_turn"
		if toolUse != nil {
			stopReason = "tool_use"
		}
		fmt.Fprintf(w, "data: %s\n\n", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":"%s"}}`, stopReason))
		fmt.Fprintf(w, "data: %s\n\n", `{"type":"message_stop"}`)
	} else {
		w.Header().Set("Content-Type", "application/json")
		var content []interface{}
		if responseContent != "" {
			content = append(content, map[string]interface{}{
				"type": "text",
				"text": responseContent,
			})
		}
		if toolUse != nil {
			content = append(content, toolUse)
		}
		resp := map[string]interface{}{
			"id":          "msg_mock",
			"type":        "message",
			"role":        "assistant",
			"content":     content,
			"model":       "deepseek-v4-pro",
			"stop_reason": condStr(toolUse != nil, "tool_use", "end_turn"),
		}
		json.NewEncoder(w).Encode(resp)
	}
}

// 运行自动工具链测试
func runAutoToolHarness(t *testing.T, isStream bool, isAnthropic bool) {
	tmpDir, err := os.MkdirTemp("", "dsplus_test_harness")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. 启动 Mock Upstream Server
	mockUpstream := &http.Server{
		Addr:    "127.0.0.1:8189",
		Handler: &mockUpstreamHandler{t: t},
	}
	go func() {
		mockUpstream.ListenAndServe()
	}()
	defer mockUpstream.Close()
	time.Sleep(100 * time.Millisecond) // 等待启动

	// 2. 备份和修改 config.json 写入 8188 端口
	origConfigBytes, configErr := os.ReadFile("config.json")
	if configErr == nil {
		defer func() {
			os.WriteFile("config.json", origConfigBytes, 0644)
		}()
	}

	testConfig := DefaultConfig()
	testConfig.Port = 8188
	testConfig.OpenAIUpstream = "http://127.0.0.1:8189"
	testConfig.AnthropicUpstream = "http://127.0.0.1:8189"
	testConfig.AnalysisEnabled = true
	testConfig.AnalysisPersistence = true

	configBytes, _ := json.MarshalIndent(testConfig, "", "  ")
	os.WriteFile("config.json", configBytes, 0644)

	// 3. 内存中启动 Gateway Proxy 监听 8188
	initTrace()
	safeTestConfig := NewSafeConfig(testConfig)
	svc := InitAnalysisService(safeTestConfig)
	// 清空原有的内存和磁盘 sessions，防止历史污染
	svc.lock.Lock()
	svc.sessions = make(map[string]*ConversationSession)
	svc.lock.Unlock()
	os.RemoveAll(svc.logDir)
	os.MkdirAll(svc.logDir, 0755)

	logger := NewLogger(100)
	proxy := NewProxyServer(safeTestConfig, logger, svc)

	gatewayServer := &http.Server{
		Addr:    "127.0.0.1:8188",
		Handler: proxy,
	}
	go func() {
		gatewayServer.ListenAndServe()
	}()
	defer gatewayServer.Close()
	time.Sleep(100 * time.Millisecond)

	// 4. 模拟客户端发起多轮交互
	client := &http.Client{}
	var messages []interface{}

	if isAnthropic {
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": "请测试工具读取、修改和写入功能",
		})
	} else {
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": "请测试工具读取、修改和写入功能",
		})
	}

	url := "http://127.0.0.1:8188/v1/chat/completions"
	if isAnthropic {
		url = "http://127.0.0.1:8188/v1/messages"
	}

	for round := 1; round <= 4; round++ {
		// 构造请求
		reqBodyMap := map[string]interface{}{
			"model":    "deepseek-v4-pro",
			"messages": messages,
			"stream":   isStream,
		}
		if isAnthropic {
			reqBodyMap["max_tokens"] = 1000
		}
		reqBodyBytes, _ := json.Marshal(reqBodyMap)

		req, err := http.NewRequest("POST", url, bytes.NewReader(reqBodyBytes))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed on round %d: %v", round, err)
		}

		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("round %d returned status %d: %s", round, resp.StatusCode, string(b))
		}

		// 解析响应，如果是流式，需要自己读取流
		var finalAssistantText string
		var finalToolCalls []map[string]interface{}

		if isStream {
			scanner := bufio.NewScanner(resp.Body)
			var toolBuilder map[string]interface{}
			var anthropicInputBuilder strings.Builder

			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "data: ") {
					dataStr := strings.TrimPrefix(line, "data: ")
					if dataStr == "[DONE]" {
						continue
					}
					var chunk map[string]interface{}
					if json.Unmarshal([]byte(dataStr), &chunk) != nil {
						continue
					}

					if isAnthropic {
						cType, _ := chunk["type"].(string)
						if cType == "content_block_start" {
							if cb, ok := chunk["content_block"].(map[string]interface{}); ok {
								if cb["type"] == "tool_use" {
									toolBuilder = map[string]interface{}{
										"id":   cb["id"],
										"name": cb["name"],
									}
								}
							}
						} else if cType == "content_block_delta" {
							if delta, ok := chunk["delta"].(map[string]interface{}); ok {
								if tDelta, ok := delta["text"].(string); ok {
									finalAssistantText += tDelta
								}
								if pJson, ok := delta["partial_json"].(string); ok {
									anthropicInputBuilder.WriteString(pJson)
								}
							}
						}
					} else {
						// OpenAI stream
						choices, _ := chunk["choices"].([]interface{})
						if len(choices) > 0 {
							c := choices[0].(map[string]interface{})
							if delta, ok := c["delta"].(map[string]interface{}); ok {
								if content, ok := delta["content"].(string); ok {
									finalAssistantText += content
								}
								if tcs, ok := delta["tool_calls"].([]interface{}); ok && len(tcs) > 0 {
									tcm := tcs[0].(map[string]interface{})
									if toolBuilder == nil {
										toolBuilder = map[string]interface{}{
											"id":   tcm["id"],
											"name": tcm["function"].(map[string]interface{})["name"],
										}
									}
									if fn, ok := tcm["function"].(map[string]interface{}); ok {
										if args, ok := fn["arguments"].(string); ok {
											if _, exists := toolBuilder["arguments"]; !exists {
												toolBuilder["arguments"] = ""
											}
											toolBuilder["arguments"] = toolBuilder["arguments"].(string) + args
										}
									}
								}
							}
						}
					}
				}
			}
			resp.Body.Close()

			if toolBuilder != nil {
				if isAnthropic {
					var inputMap map[string]interface{}
					json.Unmarshal([]byte(anthropicInputBuilder.String()), &inputMap)
					toolBuilder["input"] = inputMap
				}
				finalToolCalls = append(finalToolCalls, toolBuilder)
			}
		} else {
			// 非流式
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var respMap map[string]interface{}
			json.Unmarshal(bodyBytes, &respMap)

			if isAnthropic {
				content, _ := respMap["content"].([]interface{})
				for _, partVal := range content {
					part := partVal.(map[string]interface{})
					pType, _ := part["type"].(string)
					if pType == "text" {
						finalAssistantText = part["text"].(string)
					} else if pType == "tool_use" {
						finalToolCalls = append(finalToolCalls, part)
					}
				}
			} else {
				choices, _ := respMap["choices"].([]interface{})
				if len(choices) > 0 {
					c := choices[0].(map[string]interface{})
					msg := c["message"].(map[string]interface{})
					finalAssistantText, _ = msg["content"].(string)
					if tcs, ok := msg["tool_calls"].([]interface{}); ok {
						for _, tcVal := range tcs {
							tc := tcVal.(map[string]interface{})
							fn := tc["function"].(map[string]interface{})
							var argsMap map[string]interface{}
							json.Unmarshal([]byte(fn["arguments"].(string)), &argsMap)
							finalToolCalls = append(finalToolCalls, map[string]interface{}{
								"id":    tc["id"],
								"name":  fn["name"],
								"input": argsMap,
							})
						}
					}
				}
			}
		}

		// 处理轮次逻辑并追加历史
		if len(finalToolCalls) > 0 {
			tc := finalToolCalls[0]
			tcId, _ := tc["id"].(string)
			tcName, _ := tc["name"].(string)

			// 解析 arguments/input 并执行
			var args map[string]interface{}
			if isAnthropic {
				args, _ = tc["input"].(map[string]interface{})
			} else {
				if argsStr, ok := tc["arguments"].(string); ok {
					json.Unmarshal([]byte(argsStr), &args)
				} else if inputMap, ok := tc["input"].(map[string]interface{}); ok {
					args = inputMap
				}
			}

			// 执行本地 mock 工具
			var result string
			switch tcName {
			case "write":
				result = executeWriteTool(tmpDir, args)
			case "read":
				result = executeReadTool(tmpDir, args)
			case "edit":
				result = executeEditTool(tmpDir, args)
			}

			// 追加 assistant 响应和 tool 返回到消息历史
			if isAnthropic {
				messages = append(messages, map[string]interface{}{
					"role": "assistant",
					"content": []interface{}{
						map[string]interface{}{
							"type": "text",
							"text": finalAssistantText,
						},
						map[string]interface{}{
							"type":  "tool_use",
							"id":    tcId,
							"name":  tcName,
							"input": args,
						},
					},
				})

				messages = append(messages, map[string]interface{}{
					"role": "user",
					"content": []interface{}{
						map[string]interface{}{
							"type":        "tool_result",
							"tool_use_id": tcId,
							"content":     result,
						},
					},
				})
			} else {
				argsStrBytes, _ := json.Marshal(args)
				messages = append(messages, map[string]interface{}{
					"role":    "assistant",
					"content": finalAssistantText,
					"tool_calls": []interface{}{
						map[string]interface{}{
							"id":   tcId,
							"type": "function",
							"function": map[string]interface{}{
								"name":      tcName,
								"arguments": string(argsStrBytes),
							},
						},
					},
				})

				messages = append(messages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": tcId,
					"name":         tcName,
					"content":      result,
				})
			}
		} else {
			// 没有工具调用，到达最终文本
			if isAnthropic {
				messages = append(messages, map[string]interface{}{
					"role":    "assistant",
					"content": finalAssistantText,
				})
			} else {
				messages = append(messages, map[string]interface{}{
					"role":    "assistant",
					"content": finalAssistantText,
				})
			}
			break
		}
	}

	// 延迟等待数据刷新写盘
	time.Sleep(300 * time.Millisecond)

	// 5. 验证是否生成了正确的 sessions 数据 (直接从内存中读取)
	svc.lock.RLock()
	maxHistLen := 0
	for _, sess := range svc.sessions {
		if n := len(sess.getFullChatHistory()); n > maxHistLen {
			maxHistLen = n
		}
	}
	svc.lock.RUnlock()

	t.Logf("found max chat_history length: %d", maxHistLen)

	if maxHistLen < 3 {
		t.Fatalf("expected chat_history to have at least 3 messages (user, assistant, tool), got %d", maxHistLen)
	}

	// 确保没有被 count_tokens 等垃圾请求污染
	logFiles, _ := os.ReadDir(svc.logDir)
	for _, file := range logFiles {
		if filepath.Ext(file.Name()) == ".jsonl" {
			fileBytes, _ := os.ReadFile(filepath.Join(svc.logDir, file.Name()))
			lines := strings.Split(string(fileBytes), "\n")
			for _, line := range lines {
				if strings.Contains(line, "count_tokens") {
					t.Fatalf("found spurious endpoint in analysis log: %s", line)
				}
			}
		}
	}
}

func TestAutoToolHarness_OpenAI_NonStream(t *testing.T) {
	runAutoToolHarness(t, false, false)
}

func TestAutoToolHarness_OpenAI_Stream(t *testing.T) {
	runAutoToolHarness(t, true, false)
}

func TestAutoToolHarness_Anthropic_NonStream(t *testing.T) {
	runAutoToolHarness(t, false, true)
}

func TestAutoToolHarness_Anthropic_Stream(t *testing.T) {
	runAutoToolHarness(t, true, true)
}
