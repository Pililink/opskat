package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Claude CLI stream-json 事件解析
// 事件格式为 NDJSON，每行一个 JSON 对象

// claudeRawEvent Claude CLI stream-json 原始事件
type claudeRawEvent struct {
	Type      string          `json:"type"`       // system, assistant, stream_event, result
	Subtype   string          `json:"subtype"`    // init 等
	SessionID string          `json:"session_id"` // system init 时返回
	Event     json.RawMessage `json:"event"`      // stream_event 时的子事件
	Result    string          `json:"result"`     // result 时的最终文本
	// assistant 消息
	Message *claudeAssistantMessage `json:"message"`
}

type claudeAssistantMessage struct {
	Role    string               `json:"role"`
	Content []claudeContentBlock `json:"content"`
}

type claudeContentBlock struct {
	Type      string `json:"type"`         // text, tool_use, thinking, server_tool_use, *_tool_result
	Text      string `json:"text"`         // text/thinking 类型
	Name      string `json:"name"`         // tool_use/server_tool_use 类型
	ID        string `json:"id"`           // tool_use/server_tool_use 类型
	ToolUseID string `json:"tool_use_id"`  // *_tool_result 类型，关联的 tool_use id
}

// pendingToolUse 记录 assistant 消息中的 tool_use，等待 CLI 内部执行后标记完成
type pendingToolUse struct {
	Name string
	ID   string
}

// claudeStreamSubEvent stream_event 内部事件
type claudeStreamSubEvent struct {
	Type         string              `json:"type"` // content_block_start, content_block_delta, content_block_stop, message_start, message_delta, message_stop
	Index        int                 `json:"index"`
	Delta        *claudeDelta        `json:"delta"`
	ContentBlock *claudeContentBlock `json:"content_block"`
}

type claudeDelta struct {
	Type        string `json:"type"`         // text_delta, input_json_delta, thinking_delta
	Text        string `json:"text"`         // text_delta/input_json_delta 时
	PartialJSON string `json:"partial_json"` // input_json_delta API 原始格式
	Thinking    string `json:"thinking"`     // thinking_delta 时
}

// ClaudeEventParser 解析 Claude CLI stream-json 事件
type ClaudeEventParser struct {
	SessionID    string
	currentTools map[int]string    // index → tool name
	toolInputs   map[int]string    // index → accumulated JSON input
	pendingTools []pendingToolUse  // assistant 消息中的 tool_use，等待 CLI 执行后发出 tool_result
	toolIDToName map[string]string // tool_use id → tool name，用于匹配 *_tool_result
}

// NewClaudeEventParser 创建解析器
func NewClaudeEventParser() *ClaudeEventParser {
	return &ClaudeEventParser{
		currentTools: make(map[int]string),
		toolInputs:   make(map[int]string),
		toolIDToName: make(map[string]string),
	}
}

// ParseLine 解析一行 JSON，返回 StreamEvent 和是否完成
// 返回的 events 可能为空（忽略的事件），done 表示对话结束
func (p *ClaudeEventParser) ParseLine(line string) (events []StreamEvent, done bool) {
	if line == "" {
		return nil, false
	}

	var raw claudeRawEvent
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return []StreamEvent{{Type: "error", Error: fmt.Sprintf("解析事件失败: %s", err)}}, false
	}

	switch raw.Type {
	case "system":
		return p.handleSystem(&raw)
	case "stream_event":
		return p.handleStreamEvent(raw.Event)
	case "assistant":
		return p.handleAssistant(&raw)
	case "result":
		// 对话结束前，将所有待执行工具标记为完成
		events := p.flushPendingTools()
		return events, true
	}

	return nil, false
}

func (p *ClaudeEventParser) handleSystem(raw *claudeRawEvent) ([]StreamEvent, bool) {
	if raw.Subtype == "init" && raw.SessionID != "" {
		p.SessionID = raw.SessionID
	}
	return nil, false
}

func (p *ClaudeEventParser) handleStreamEvent(eventData json.RawMessage) ([]StreamEvent, bool) {
	if eventData == nil {
		return nil, false
	}

	var sub claudeStreamSubEvent
	if err := json.Unmarshal(eventData, &sub); err != nil {
		return nil, false
	}

	switch sub.Type {
	case "content_block_start":
		// 新 content block 开始，说明之前的待执行工具已完成
		var events []StreamEvent
		events = append(events, p.flushPendingTools()...)

		if sub.ContentBlock != nil {
			switch {
			case sub.ContentBlock.Type == "tool_use" || sub.ContentBlock.Type == "server_tool_use":
				// 工具调用（客户端或服务端）
				p.currentTools[sub.Index] = sub.ContentBlock.Name
				p.toolInputs[sub.Index] = ""
				if sub.ContentBlock.ID != "" {
					p.toolIDToName[sub.ContentBlock.ID] = sub.ContentBlock.Name
				}
				events = append(events, StreamEvent{
					Type:     "tool_start",
					ToolName: sub.ContentBlock.Name,
				})
			case strings.HasSuffix(sub.ContentBlock.Type, "_tool_result"):
				// 服务端工具结果（如 web_search_tool_result）
				toolName := p.resolveToolName(sub.ContentBlock.ToolUseID)
				events = append(events, StreamEvent{
					Type:     "tool_result",
					ToolName: toolName,
				})
			}
		}

		if len(events) > 0 {
			return events, false
		}

	case "content_block_delta":
		if sub.Delta != nil {
			switch sub.Delta.Type {
			case "text_delta":
				if sub.Delta.Text != "" {
					return []StreamEvent{{
						Type:    "content",
						Content: sub.Delta.Text,
					}}, false
				}
			case "input_json_delta":
				// 累积工具输入 JSON（兼容 text 和 partial_json 字段）
				t := sub.Delta.Text
				if t == "" {
					t = sub.Delta.PartialJSON
				}
				if t != "" {
					p.toolInputs[sub.Index] += t
				}
			case "thinking_delta":
				// 扩展思考内容
				t := sub.Delta.Thinking
				if t == "" {
					t = sub.Delta.Text
				}
				if t != "" {
					return []StreamEvent{{
						Type:    "content",
						Content: t,
					}}, false
				}
			}
		}

	case "content_block_stop":
		if toolName, ok := p.currentTools[sub.Index]; ok {
			// 工具输入累积完成，提取摘要
			input := extractToolInputSummary(toolName, p.toolInputs[sub.Index])
			delete(p.currentTools, sub.Index)
			delete(p.toolInputs, sub.Index)
			if input != "" {
				return []StreamEvent{{
					Type:      "tool_start",
					ToolName:  toolName,
					ToolInput: input,
				}}, false
			}
		}

	case "message_stop":
		// 消息结束，但不一定是对话结束（可能还有 tool 执行后续轮次）
	}

	return nil, false
}

func (p *ClaudeEventParser) handleAssistant(raw *claudeRawEvent) ([]StreamEvent, bool) {
	// assistant 消息包含完整的 content blocks（text 已通过 stream_event delta 发送，不重复）
	// 提取 tool_use 块记录为待执行工具，等 CLI 内部执行完后在下一轮 stream_event 时发出 tool_result
	if raw.Message == nil {
		return nil, false
	}
	for _, block := range raw.Message.Content {
		if block.Type == "tool_use" && block.Name != "" {
			p.pendingTools = append(p.pendingTools, pendingToolUse{
				Name: block.Name,
				ID:   block.ID,
			})
		}
	}
	return nil, false
}

// flushPendingTools 将所有待执行工具作为 tool_result 发出（CLI 已在内部执行完毕）
func (p *ClaudeEventParser) flushPendingTools() []StreamEvent {
	if len(p.pendingTools) == 0 {
		return nil
	}
	var events []StreamEvent
	for _, t := range p.pendingTools {
		events = append(events, StreamEvent{
			Type:     "tool_result",
			ToolName: t.Name,
		})
	}
	p.pendingTools = nil
	return events
}

// resolveToolName 通过 tool_use_id 查找工具名
func (p *ClaudeEventParser) resolveToolName(toolUseID string) string {
	if name, ok := p.toolIDToName[toolUseID]; ok {
		return name
	}
	return "Tool"
}

// extractToolInputSummary 从工具 JSON 输入中提取摘要
func extractToolInputSummary(toolName, inputJSON string) string {
	if inputJSON == "" {
		return ""
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(inputJSON), &args); err != nil {
		return inputJSON
	}
	// 根据工具类型提取关键字段
	switch toolName {
	case "Bash":
		if cmd, ok := args["command"].(string); ok {
			return cmd
		}
	case "Read":
		if p, ok := args["file_path"].(string); ok {
			return p
		}
	case "Write":
		if p, ok := args["file_path"].(string); ok {
			return p
		}
	case "Edit":
		if p, ok := args["file_path"].(string); ok {
			return p
		}
	case "Glob":
		if p, ok := args["pattern"].(string); ok {
			return p
		}
	case "Grep":
		if p, ok := args["pattern"].(string); ok {
			return p
		}
	}
	// 回退：返回整个 JSON
	return inputJSON
}
