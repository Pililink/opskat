package ai

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestClaudeEventParser(t *testing.T) {
	Convey("Claude stream-json 事件解析", t, func() {
		parser := NewClaudeEventParser()

		Convey("解析 system init 事件", func() {
			events, done := parser.ParseLine(`{"type":"system","subtype":"init","session_id":"abc-123"}`)
			So(events, ShouldBeEmpty)
			So(done, ShouldBeFalse)
			So(parser.SessionID, ShouldEqual, "abc-123")
		})

		Convey("解析 text_delta 事件", func() {
			events, done := parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}}`)
			So(done, ShouldBeFalse)
			So(events, ShouldHaveLength, 1)
			So(events[0].Type, ShouldEqual, "content")
			So(events[0].Content, ShouldEqual, "Hello")
		})

		Convey("解析 tool_use 开始事件", func() {
			events, done := parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","name":"Bash","id":"tool_1"}}}`)
			So(done, ShouldBeFalse)
			So(events, ShouldHaveLength, 1)
			So(events[0].Type, ShouldEqual, "tool_start")
			So(events[0].ToolName, ShouldEqual, "Bash")
		})

		Convey("解析 input_json_delta 累积工具输入", func() {
			// 开始 tool_use
			parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","name":"Bash","id":"tool_1"}}}`)

			// 输入 delta
			events, _ := parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","text":"{\"command\":\"ls -la\"}"}}}`)
			So(events, ShouldBeEmpty) // delta 不直接产生事件

			// content_block_stop → 发送带 input 的 tool_start
			events, done := parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`)
			So(done, ShouldBeFalse)
			So(events, ShouldHaveLength, 1)
			So(events[0].Type, ShouldEqual, "tool_start")
			So(events[0].ToolName, ShouldEqual, "Bash")
			So(events[0].ToolInput, ShouldEqual, "ls -la")
		})

		Convey("解析 result 事件标记完成", func() {
			events, done := parser.ParseLine(`{"type":"result","result":"分析完成","session_id":"abc-123"}`)
			So(events, ShouldBeEmpty)
			So(done, ShouldBeTrue)
		})

		Convey("assistant 消息仅含 text 不产生事件", func() {
			events, done := parser.ParseLine(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"服务器分析结果"}]}}`)
			So(done, ShouldBeFalse)
			So(events, ShouldBeEmpty)
		})

		Convey("assistant 消息中的 tool_use 记录为待执行", func() {
			// assistant 包含 tool_use
			events, done := parser.ParseLine(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"tool_1"}]}}`)
			So(done, ShouldBeFalse)
			So(events, ShouldBeEmpty)
			So(parser.pendingTools, ShouldHaveLength, 1)
			So(parser.pendingTools[0].Name, ShouldEqual, "Bash")
		})

		Convey("下一轮 content_block_start 时发出 tool_result", func() {
			// 模拟完整的工具调用流程：tool_use → assistant → CLI 执行 → 新 content block
			parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","name":"Bash","id":"tool_1"}}}`)
			parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","text":"{\"command\":\"ls\"}"}}}`)
			parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`)

			// assistant 消息，记录待执行工具
			parser.ParseLine(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"tool_1"}]}}`)
			So(parser.pendingTools, ShouldHaveLength, 1)

			// CLI 内部执行完毕，新一轮 stream_event 开始 → 应先发出 tool_result
			events, done := parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text"}}}`)
			So(done, ShouldBeFalse)
			So(events, ShouldHaveLength, 1)
			So(events[0].Type, ShouldEqual, "tool_result")
			So(events[0].ToolName, ShouldEqual, "Bash")
			So(parser.pendingTools, ShouldBeEmpty)
		})

		Convey("多个工具调用依次发出 tool_result", func() {
			// assistant 包含两个 tool_use
			parser.ParseLine(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"t1"},{"type":"tool_use","name":"Read","id":"t2"}]}}`)
			So(parser.pendingTools, ShouldHaveLength, 2)

			// 下一轮 content block 开始
			events, _ := parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text"}}}`)
			So(events, ShouldHaveLength, 2)
			So(events[0].Type, ShouldEqual, "tool_result")
			So(events[0].ToolName, ShouldEqual, "Bash")
			So(events[1].Type, ShouldEqual, "tool_result")
			So(events[1].ToolName, ShouldEqual, "Read")
		})

		Convey("result 事件前刷新待执行工具", func() {
			parser.ParseLine(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","id":"t1"}]}}`)
			So(parser.pendingTools, ShouldHaveLength, 1)

			events, done := parser.ParseLine(`{"type":"result","result":"done"}`)
			So(done, ShouldBeTrue)
			So(events, ShouldHaveLength, 1)
			So(events[0].Type, ShouldEqual, "tool_result")
			So(events[0].ToolName, ShouldEqual, "Bash")
		})

		Convey("server_tool_use 作为 tool_start 处理", func() {
			events, done := parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","name":"web_search","id":"srv_1"}}}`)
			So(done, ShouldBeFalse)
			So(events, ShouldHaveLength, 1)
			So(events[0].Type, ShouldEqual, "tool_start")
			So(events[0].ToolName, ShouldEqual, "web_search")
			// 应记录 ID 映射
			So(parser.toolIDToName["srv_1"], ShouldEqual, "web_search")
		})

		Convey("*_tool_result 发出 tool_result 并匹配工具名", func() {
			// 先注册 server_tool_use
			parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","name":"web_search","id":"srv_1"}}}`)
			parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`)

			// 收到对应的 tool_result
			events, done := parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_1"}}}`)
			So(done, ShouldBeFalse)
			So(events, ShouldHaveLength, 1)
			So(events[0].Type, ShouldEqual, "tool_result")
			So(events[0].ToolName, ShouldEqual, "web_search")
		})

		Convey("thinking_delta 作为 content 发送", func() {
			events, done := parser.ParseLine(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}}`)
			So(done, ShouldBeFalse)
			So(events, ShouldHaveLength, 1)
			So(events[0].Type, ShouldEqual, "content")
			So(events[0].Content, ShouldEqual, "Let me think...")
		})

		Convey("空行不报错", func() {
			events, done := parser.ParseLine("")
			So(events, ShouldBeEmpty)
			So(done, ShouldBeFalse)
		})

		Convey("无效 JSON 返回错误事件", func() {
			events, done := parser.ParseLine("not json")
			So(done, ShouldBeFalse)
			So(events, ShouldHaveLength, 1)
			So(events[0].Type, ShouldEqual, "error")
		})
	})
}
