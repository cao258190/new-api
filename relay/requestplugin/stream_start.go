package requestplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/requestpatch"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// streamStart belongs to the logical request, not an upstream attempt. The
// writer sits below reply checks and above report capture, so it preserves the
// checks and reports the exact frames the client received, across retries.
type streamStart struct {
	c          *gin.Context
	info       *relaycommon.RelayInfo
	writer     *streamStartWriter
	timing     string
	delay      time.Duration
	skipped    bool
	id         string
	created    int64
	sent       bool
	sequence   int
	upstreamID string
	outputItem bool
	itemID     string
	itemDone   bool
}

// streamStartBody restores the byte used to arbitrate the delayed start and
// owns cancellation of the upstream request until the handler closes its body.
type streamStartBody struct {
	io.ReadCloser
	first  []byte
	err    error
	cancel context.CancelFunc
	ready  chan struct{}
}

func (b *streamStartBody) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	<-b.ready
	if len(b.first) > 0 {
		n := copy(data, b.first)
		b.first = b.first[n:]
		return n, nil
	}
	if b.err != nil {
		err := b.err
		b.err = nil
		return 0, err
	}
	return b.ReadCloser.Read(data)
}

func (b *streamStartBody) Close() error {
	b.cancel()
	err := b.ReadCloser.Close()
	<-b.ready
	return err
}

type streamStartWriter struct {
	gin.ResponseWriter
	state   *streamStart
	mu      sync.Mutex
	pending []byte
	err     error
}

func streamStartEndpoint(info *relaycommon.RelayInfo) bool {
	return info.IsStream && info.ClientWs == nil && ((info.RelayFormat == types.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeChatCompletions) ||
		(info.RelayFormat == types.RelayFormatOpenAIResponses && info.RelayMode == relayconstant.RelayModeResponses))
}

func (p *Plan) prepareStreamStart(c *gin.Context, info *relaycommon.RelayInfo) {
	if p.stream != nil || p.socket || p.submission != nil || !streamStartEndpoint(info) {
		return
	}
	s := &streamStart{c: c, info: info}
	s.writer = &streamStartWriter{ResponseWriter: c.Writer, state: s}
	p.stream, c.Writer = s, s.writer
}

func (p *Plan) attachStreamStart(c *gin.Context, info *relaycommon.RelayInfo, effect *requestpatch.Effect) {
	// A fresh attempt cannot inherit a policy from a channel that failed. If a
	// start already went out, the same writer must still normalize its output.
	info.RequestPluginStreamStart = nil
	if p.stream == nil || p.socket || p.submission != nil || !streamStartEndpoint(info) {
		return
	}
	s := p.stream
	s.info, s.timing, s.delay = info, "", 0
	if effect != nil {
		s.timing, s.delay = effect.Timing, time.Duration(effect.DelayMS)*time.Millisecond
		s.outputItem = effect.OutputItem
	}
	if !c.GetBool("request_plugin_stream_payload_sent") {
		s.upstreamID = ""
	}
	if s.timing == "" || s.sent || s.skipped {
		return
	}
	info.RequestPluginStreamStart = s.send
}

// send keeps all client writes on its caller's path. Only the upstream request
// and its first body byte run concurrently with the timer, so response/reply
// writers, headers and request completion never race a timer goroutine.
func (s *streamStart) send(req *http.Request, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if s.sent || s.skipped || s.timing != "before_upstream" || s.delay == 0 {
		if err := s.start(nil); err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		}
		resp, err := send(req)
		if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
			if startErr := s.start(resp); startErr != nil {
				_ = resp.Body.Close()
				return nil, types.NewError(startErr, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
			}
		}
		return resp, err
	}
	type result struct {
		response  *http.Response
		body      *streamStartBody
		err       error
		streaming bool
	}
	ctx, cancel := context.WithCancel(req.Context())
	results := make(chan result, 1)
	go func(results chan<- result) {
		var got result
		delivered := false
		defer func() {
			if recovered := recover(); recovered != nil {
				got.err = fmt.Errorf("upstream start request panic: %v", recovered)
				if delivered {
					got.body.err = got.err
				}
			}
			if !delivered {
				results <- got
			}
			if got.body != nil {
				close(got.body.ready)
			}
		}()
		got.response, got.err = send(req.WithContext(ctx))
		if got.err != nil || got.response == nil || got.response.Body == nil {
			return
		}
		got.body = &streamStartBody{ReadCloser: got.response.Body, cancel: cancel, ready: make(chan struct{})}
		got.response.Body = got.body
		mediaType := strings.ToLower(strings.TrimSpace(got.response.Header.Get("Content-Type")))
		got.streaming = got.response.StatusCode == http.StatusOK && (mediaType == "" || strings.HasPrefix(mediaType, "text/event-stream"))
		results <- got
		delivered = true
		if got.streaming {
			first := make([]byte, 1)
			n, err := io.ReadFull(got.body.ReadCloser, first)
			got.body.first, got.body.err = first[:n], err
		}
	}(results)
	timer := time.NewTimer(s.delay)
	defer timer.Stop()
	timerC, done := timer.C, ctx.Done()
	var first <-chan struct{}
	var got result
	var startErr error
wait:
	for {
		select {
		case got = <-results:
			results = nil
			if startErr != nil || got.err != nil || !got.streaming || s.sent {
				break wait
			}
			first = got.body.ready
		case <-first:
			s.skipped = len(got.body.first) > 0
			break wait
		case <-done:
			startErr = ctx.Err()
			done = nil
			timer.Stop()
			timerC = nil
			cancel()
			if results == nil {
				break wait
			}
		case <-timerC:
			timerC = nil
			// Give already available headers and body bytes priority at the
			// deadline, without waiting for a slow or failing upstream.
			select {
			case got = <-results:
				results = nil
				if got.err != nil || !got.streaming {
					break wait
				}
				first = got.body.ready
			default:
			}
			select {
			case <-first:
				s.skipped = len(got.body.first) > 0
				break wait
			default:
			}
			if startErr = s.start(nil); startErr != nil {
				cancel()
				done = nil
			}
			if results == nil {
				break wait
			}
		}
	}
	if startErr != nil || got.err != nil || got.response == nil || got.response.Body == nil {
		cancel()
		if got.response != nil && got.response.Body != nil {
			_ = got.response.Body.Close()
		}
		if startErr != nil {
			return nil, types.NewError(startErr, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		}
		return got.response, got.err
	}
	return got.response, nil
}

// start is called only after request checks and signing. A nil response marks
// the before_upstream policy (immediate or delayed); otherwise HTTP headers arrived.
func (s *streamStart) start(resp *http.Response) error {
	if s.sent || s.skipped || s.timing == "" || (resp == nil) != (s.timing == "before_upstream") {
		return nil
	}
	if err := s.c.Request.Context().Err(); err != nil {
		return err
	}
	if resp != nil {
		mediaType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
		if mediaType != "" && !strings.HasPrefix(mediaType, "text/event-stream") {
			return nil
		}
	}
	if s.c.Writer.Written() && !strings.HasPrefix(s.c.Writer.Header().Get("Content-Type"), "text/event-stream") {
		return nil
	}
	s.created = time.Now().Unix()
	s.id = "chatcmpl-" + common.GetUUID()
	var event any
	if s.info.RelayFormat == types.RelayFormatOpenAIResponses {
		s.id = service.ResponseAliasPrefix + common.GetUUID()
		event = gin.H{"type": "response.created", "sequence_number": 0,
			"response": gin.H{"id": s.id, "object": "response", "created_at": s.created, "status": "in_progress",
				"error": nil, "incomplete_details": nil, "model": s.info.OriginModelName,
				"output": []any{}, "usage": nil, "metadata": gin.H{}}}
	} else {
		event = gin.H{"id": s.id, "object": "chat.completion.chunk", "created": s.created, "model": s.info.OriginModelName,
			"choices": []any{gin.H{"index": 0, "delta": gin.H{"role": "assistant", "content": ""}, "finish_reason": nil}}}
	}
	raw, err := common.Marshal(event)
	if err != nil {
		return err
	}
	frame := "data: " + string(raw) + "\n\n"
	sequence := 1
	if s.info.RelayFormat == types.RelayFormatOpenAIResponses {
		frame = "event: response.created\n" + frame
		if s.outputItem {
			s.itemID = service.ResponseStartItemPrefix + common.GetUUID()
			raw, err = common.Marshal(gin.H{"type": "response.output_item.added", "sequence_number": sequence,
				"output_index": 0, "item": gin.H{"id": s.itemID, "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}})
			if err != nil {
				return err
			}
			frame += "event: response.output_item.added\ndata: " + string(raw) + "\n\n"
			sequence++
		}
	}
	if _, err = s.c.Writer.WriteString(frame); err != nil {
		return err
	}
	s.c.Writer.Flush()
	if err = s.writer.FlushError(); err != nil {
		return err
	}
	s.writer.mu.Lock()
	s.sent, s.sequence = true, sequence
	s.writer.mu.Unlock()
	s.info.ClientFirstResponseTime, s.info.StreamStartTiming = time.Now(), s.timing
	return nil
}

func (w *streamStartWriter) Unwrap() http.ResponseWriter          { return w.ResponseWriter }
func (w *streamStartWriter) WriteString(text string) (int, error) { return w.Write([]byte(text)) }

func (w *streamStartWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	if !w.state.sent {
		return w.ResponseWriter.Write(data)
	}
	// Use the same bound as the relay scanner. Writes may split event/data
	// lines arbitrarily, including in the middle of a UTF-8 character.
	count := len(data)
	limit := 128 << 20
	if maxMB := constant.StreamScannerMaxBufferMB; maxMB > 0 {
		limit = maxMB << 20
	}
	for len(data) > 0 {
		n := bytes.IndexByte(data, '\n')
		if n < 0 {
			n = len(data) - 1
		}
		n++
		if len(w.pending)+n > limit {
			w.err = errors.New("stream start event exceeds the stream buffer limit")
			return 0, w.err
		}
		w.pending = append(w.pending, data[:n]...)
		data = data[n:]
		if bytes.HasSuffix(w.pending, []byte("\n\n")) || bytes.HasSuffix(w.pending, []byte("\r\n\r\n")) {
			w.err = w.frame(w.pending)
			w.pending = w.pending[:0]
			if w.err != nil {
				return 0, w.err
			}
		}
	}
	return count, nil
}

// FinishStreamStart surfaces normalization/storage errors that a response parser
// recorded only as a stream failure, and leaves the writer ready for an SSE error.
func FinishStreamStart(c *gin.Context) *types.NewAPIError {
	p := planOf(c)
	if p == nil || p.stream == nil {
		return nil
	}
	w := p.stream.writer
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.err
	if err == nil && len(bytes.TrimSpace(w.pending)) > 0 {
		err = errors.New("incomplete event in response stream")
	}
	w.err, w.pending = nil, nil
	if err == nil {
		return nil
	}
	return types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

// StreamStartFailed closes an already started Responses lifecycle. Other
// protocols retain their normal stream error encoding in the controller.
func StreamStartFailed(c *gin.Context, apiErr *types.NewAPIError) bool {
	p := planOf(c)
	if p == nil || p.stream == nil || !p.stream.sent || p.stream.info.RelayFormat != types.RelayFormatOpenAIResponses {
		return false
	}
	s := p.stream
	e := apiErr.ToOpenAIError(common.UpstreamAddresses(c)...)
	raw, err := common.Marshal(gin.H{"type": "response.failed", "response": gin.H{
		"id": s.id, "object": "response", "created_at": s.created, "status": "failed", "output": []any{},
		"model": s.info.OriginModelName, "error": gin.H{"code": e.Code, "message": e.Message}, "usage": nil}})
	if err == nil {
		_, _ = c.Writer.WriteString("event: response.failed\ndata: " + string(raw) + "\n\n")
		c.Writer.Flush()
	}
	return true
}

func (w *streamStartWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 && w.err == nil {
		w.ResponseWriter.Flush()
	}
}

func (w *streamStartWriter) FlushError() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// frame preserves upstream payloads and item identity. A requested empty item
// takes index zero; real indices shift together and terminal snapshots include it.
func (w *streamStartWriter) frame(frame []byte) error {
	s := w.state
	var data []byte
	for line := range bytes.Lines(frame) {
		if value, ok := bytes.CutPrefix(bytes.TrimRight(line, "\r\n"), []byte("data:")); ok {
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, bytes.TrimPrefix(value, []byte(" "))...)
		}
	}
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		_, err := w.ResponseWriter.Write(frame)
		return err
	}
	if !gjson.ValidBytes(data) {
		return errors.New("invalid JSON in response stream")
	}
	root := gjson.ParseBytes(data)
	var err error
	if s.info.RelayFormat == types.RelayFormatOpenAIResponses {
		if upstream := root.Get("response.id").Str; upstream != "" && upstream != s.id {
			if s.upstreamID != "" && upstream != s.upstreamID {
				return errors.New("upstream response identity changed during the stream")
			}
			terminal := false
			switch root.Get("type").Str {
			case "response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled", "response.canceled":
				terminal = true
			}
			if s.upstreamID == "" || terminal {
				if err = service.RememberResponseAlias(s.c, s.info, s.id, upstream); err != nil {
					return err
				}
				s.upstreamID = upstream
			}
			data, err = sjson.SetBytes(data, "response.id", s.id)
			if err != nil {
				return err
			}
		}
		if root.Get("type").Str == "response.created" {
			return nil
		}
		if root.Get("response_id").Exists() {
			data, err = sjson.SetBytes(data, "response_id", s.id)
			if err != nil {
				return err
			}
		}
		if s.itemID != "" {
			kind := root.Get("type").Str
			failed := kind == "response.failed" || kind == "response.cancelled" || kind == "response.canceled"
			terminal := failed || kind == "response.completed" || kind == "response.done" || kind == "response.incomplete"
			item := gin.H{"id": s.itemID, "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}
			// Keep pre-output failures retryable downstream. The empty added item
			// alone is semantic TTFT, but its done event can commit an attempt.
			if !s.itemDone && !failed && (root.Get("output_index").Exists() || terminal) {
				item["status"] = "completed"
				done, marshalErr := common.Marshal(gin.H{"type": "response.output_item.done", "sequence_number": s.sequence, "output_index": 0, "item": item})
				if marshalErr != nil {
					return marshalErr
				}
				if _, err = fmt.Fprintf(w.ResponseWriter, "event: response.output_item.done\ndata: %s\n\n", done); err != nil {
					return err
				}
				s.sequence++
				s.itemDone = true
			}
			if s.itemDone {
				item["status"] = "completed"
			} else if failed {
				item["status"] = "incomplete"
			}
			if index := root.Get("output_index"); index.Exists() {
				if index.Type != gjson.Number || index.Num < 0 || index.Num >= 1<<53 || index.Num != float64(index.Int()) {
					return errors.New("invalid output index in response stream")
				}
				data, err = sjson.SetBytes(data, "output_index", index.Int()+1)
				if err != nil {
					return err
				}
			}
			if output := root.Get("response.output"); output.IsArray() {
				items := []any{item}
				for _, upstreamItem := range output.Array() {
					items = append(items, json.RawMessage(upstreamItem.Raw))
				}
				data, err = sjson.SetBytes(data, "response.output", items)
				if err != nil {
					return err
				}
			}
		}
		data, err = sjson.SetBytes(data, "sequence_number", s.sequence)
		s.sequence++
	} else {
		if root.Get("choices").IsArray() {
			choices := root.Get("choices").Array()
			if len(choices) == 1 && choices[0].Get("index").Int() == 0 && choices[0].Get("delta.role").Str == "assistant" &&
				choices[0].Get("finish_reason").Type == gjson.Null && root.Get("usage").Type == gjson.Null {
				empty := true
				for name := range root.Map() {
					switch name {
					case "id", "object", "created", "model", "choices", "usage", "system_fingerprint", "service_tier":
					default:
						empty = false
					}
				}
				for name := range choices[0].Map() {
					switch name {
					case "index", "delta", "finish_reason":
					default:
						empty = false
					}
				}
				for name, value := range choices[0].Get("delta").Map() {
					if name != "role" && (name != "content" || value.Type != gjson.String || value.Str != "") {
						empty = false
					}
				}
				if empty {
					return nil
				}
			}
			// Remove only the duplicated assistant role. Retain nonempty text,
			// tools, finish reasons, usage and additional provider fields.
			for i, choice := range choices {
				if choice.Get("index").Int() == 0 && choice.Get("delta.role").Str == "assistant" {
					data, err = sjson.DeleteBytes(data, fmt.Sprintf("choices.%d.delta.role", i))
					if err != nil {
						return err
					}
				}
			}
		}
		if root.Get("id").Exists() {
			data, err = sjson.SetBytes(data, "id", s.id)
			if err != nil {
				return err
			}
		}
		if root.Get("created").Exists() {
			data, err = sjson.SetBytes(data, "created", s.created)
		}
	}
	if err != nil {
		return err
	}
	// After actual upstream data was published, retrying would duplicate text.
	s.c.Set("request_plugin_stream_payload_sent", true)
	var output bytes.Buffer
	inserted := false
	for line := range bytes.Lines(frame) {
		if bytes.HasPrefix(line, []byte("data:")) {
			if !inserted {
				output.WriteString("data: ")
				output.Write(data)
				output.WriteByte('\n')
				inserted = true
			}
		} else {
			output.Write(line)
		}
	}
	_, err = w.ResponseWriter.Write(output.Bytes())
	return err
}
