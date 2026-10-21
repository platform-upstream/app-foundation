package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"strings"
)

// ErrIncomplete means a stream ended before the model finished; the partial Response is returned
// with it.
var ErrIncomplete = errors.New("llm: the reply stream ended early")

const (
	maxToolCalls     = 64
	maxToolArguments = 1 << 20
)

// EventKind tells what a stream Event carries.
type EventKind int

const (
	// EventText is a piece of the answer.
	EventText EventKind = iota
	// EventReasoning is a piece of the thinking.
	EventReasoning
	// EventToolCall marks a tool-call fragment; the assembled calls are in the Response.
	EventToolCall
)

// Event is one piece of a streamed reply, in arrival order.
type Event struct {
	Kind EventKind
	Text string
}

// Stream sends the request as a stream and calls onEvent for each piece as it arrives. It returns
// the assembled Response when the stream ends; with an error (ErrIncomplete, ErrTruncated, a
// cancelled ctx) it still returns what arrived. A backend that answers with a plain JSON reply is
// read as one.
func (c *Client) Stream(ctx context.Context, req Request, onEvent func(Event)) (Response, error) {
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	httpResp, err := c.send(ctx, req, true)
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()
	if mediaType, _, _ := mime.ParseMediaType(httpResp.Header.Get("Content-Type")); mediaType == "application/json" {
		data, err := c.readAll(httpResp.Body)
		if err != nil {
			return Response{}, err
		}
		resp, err := decode(data)
		if resp.Reasoning != "" {
			onEvent(Event{Kind: EventReasoning, Text: resp.Reasoning})
		}
		if resp.Content != "" {
			onEvent(Event{Kind: EventText, Text: resp.Content})
		}
		return resp, err
	}

	var (
		resp      Response
		content   strings.Builder
		reasoning strings.Builder
		splitter  thinkingSplitter
		calls     []*ToolCall
		done      bool
		read      int64
	)
	emit := func(kind EventKind, text string) {
		if text == "" {
			return
		}
		if kind == EventReasoning {
			reasoning.WriteString(text)
		} else {
			content.WriteString(text)
		}
		onEvent(Event{Kind: kind, Text: text})
	}
	assemble := func() Response {
		splitter.add("", true, emit)
		resp.Content = strings.TrimSpace(content.String())
		resp.Reasoning = strings.TrimSpace(reasoning.String())
		resp.ToolCalls = nil
		for _, call := range calls {
			if call != nil {
				resp.ToolCalls = append(resp.ToolCalls, *call)
			}
		}
		return resp
	}
	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if read += int64(len(line)); read > c.cfg.MaxResponseBytes*4 {
			return assemble(), errors.New("llm: reply stream is too large")
		}
		payload, ok := strings.CutPrefix(string(line), "data:")
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "[DONE]" {
			done = true
			break
		}
		var chunk struct {
			Error   json.RawMessage `json:"error"`
			Usage   *wireUsage      `json:"usage"`
			Timings json.RawMessage `json:"timings"`
			Choices []struct {
				Delta struct {
					Content          string         `json:"content"`
					ReasoningContent string         `json:"reasoning_content"`
					Reasoning        string         `json:"reasoning"`
					ToolCalls        []wireToolCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return assemble(), &Error{Message: errorMessage([]byte(`{"error":` + string(chunk.Error) + `}`))}
		}
		if chunk.Usage != nil {
			resp.Usage = chunk.Usage.usage()
		}
		if len(chunk.Timings) > 0 && string(chunk.Timings) != "null" {
			resp.Timings = chunk.Timings
		}
		for _, choice := range chunk.Choices {
			emit(EventReasoning, choice.Delta.ReasoningContent+choice.Delta.Reasoning)
			splitter.add(choice.Delta.Content, false, emit)
			for _, delta := range choice.Delta.ToolCalls {
				if delta.Index < 0 || delta.Index >= maxToolCalls {
					return assemble(), fmt.Errorf("llm: tool call index %d out of range", delta.Index)
				}
				for len(calls) <= delta.Index {
					calls = append(calls, nil)
				}
				call := calls[delta.Index]
				if call == nil {
					call = &ToolCall{}
					calls[delta.Index] = call
				}
				if delta.ID != "" {
					call.ID = delta.ID
				}
				call.Name += delta.Function.Name
				call.Arguments += delta.Function.Arguments
				if len(call.Arguments) > maxToolArguments {
					return assemble(), errors.New("llm: tool call arguments are too large")
				}
				onEvent(Event{Kind: EventToolCall})
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				resp.FinishReason = *choice.FinishReason
			}
		}
	}
	result := assemble()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("%w: %v", ErrIncomplete, err)
	}
	if !done && result.FinishReason == "" {
		return result, ErrIncomplete
	}
	return finish(result)
}

// thinkingSplitter routes <think>…</think> text in streamed content to reasoning, also when a
// tag is split across chunks.
type thinkingSplitter struct {
	pending  string
	thinking bool
}

func (s *thinkingSplitter) add(text string, final bool, emit func(EventKind, string)) {
	s.pending += text
	for s.pending != "" {
		marker, kind := "<think>", EventText
		if s.thinking {
			marker, kind = "</think>", EventReasoning
		}
		if pos := strings.Index(s.pending, marker); pos >= 0 {
			emit(kind, s.pending[:pos])
			s.pending = s.pending[pos+len(marker):]
			s.thinking = !s.thinking
			continue
		}
		keep := 0
		if !final {
			for i := 1; i < len(marker) && i <= len(s.pending); i++ {
				if strings.HasSuffix(s.pending, marker[:i]) {
					keep = i
				}
			}
		}
		emit(kind, s.pending[:len(s.pending)-keep])
		s.pending = s.pending[len(s.pending)-keep:]
		break
	}
}
