package stdlog

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const lokiUserAgent = "auth-sdks-stdlog/1"

// LokiConfig configures the optional Gigapipe/Loki push sink.
// Empty URL disables the sink (WrapLoki returns base unchanged).
type LokiConfig struct {
	URL           string
	Username      string
	Password      string
	Env           string
	Service       string
	Timeout       time.Duration
	BatchSize     int
	FlushInterval time.Duration
	MaxBuffer     int
}

type lokiEntry struct {
	ts     time.Time
	level  string
	kind   string
	msg    string
	fields []Field
}

type lokiSink struct {
	cfg      LokiConfig
	inner    Logger
	client   *http.Client
	pushURL  string
	mu       sync.Mutex
	buf      []lokiEntry
	wg       sync.WaitGroup
	warnOnce sync.Once
	stop     chan struct{}
	stopped  sync.Once
	closed   atomic.Bool
}

type lokiLogger struct {
	base  Logger
	extra []Field
	sink  *lokiSink
}

// WrapLoki tees logs to base and, when URL is set, to POST /loki/api/v1/push.
func WrapLoki(base Logger, cfg LokiConfig) (Logger, error) {
	if base == nil {
		base = Nop()
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return base, nil
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.MaxBuffer <= 0 {
		cfg.MaxBuffer = 1000
	}
	if cfg.FlushInterval < 0 {
		cfg.FlushInterval = 0
	}
	sink := &lokiSink{
		cfg:     cfg,
		inner:   base,
		client:  &http.Client{Timeout: cfg.Timeout},
		pushURL: lokiPushURL(cfg.URL),
		stop:    make(chan struct{}),
	}
	if cfg.FlushInterval > 0 {
		go sink.loop()
	}
	return &lokiLogger{base: base, sink: sink}, nil
}

func lokiPushURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(base, "/loki/api/v1/push") {
		return base
	}
	return base + "/loki/api/v1/push"
}

func (l *lokiLogger) Debug(msg string, fields ...Field) {
	l.base.Debug(msg, fields...)
	l.enqueue("debug", msg, fields)
}
func (l *lokiLogger) Info(msg string, fields ...Field) {
	l.base.Info(msg, fields...)
	l.enqueue("info", msg, fields)
}
func (l *lokiLogger) Warn(msg string, fields ...Field) {
	l.base.Warn(msg, fields...)
	l.enqueue("warn", msg, fields)
}
func (l *lokiLogger) Error(msg string, fields ...Field) {
	l.base.Error(msg, fields...)
	l.enqueue("error", msg, fields)
}
func (l *lokiLogger) Fatal(msg string, fields ...Field) {
	l.enqueue("error", msg, fields)
	_ = l.Sync()
	l.base.Fatal(msg, fields...)
}

func (l *lokiLogger) With(fields ...Field) Logger {
	return &lokiLogger{base: l.base.With(fields...), extra: concatFields(l.extra, fields), sink: l.sink}
}

func (l *lokiLogger) Named(component string) Logger {
	f := String(FieldComponent, component)
	return &lokiLogger{base: l.base.Named(component), extra: concatFields(l.extra, []Field{f}), sink: l.sink}
}

func (l *lokiLogger) Sync() error {
	if l.sink != nil {
		l.sink.flush(true)
	}
	return l.base.Sync()
}

// Close stops the optional flush ticker, flushes remaining entries, and Syncs the base logger.
func (l *lokiLogger) Close() error {
	if l.sink != nil {
		l.sink.shutdown()
	}
	if l.base == nil {
		return nil
	}
	return l.base.Sync()
}

func (l *lokiLogger) enqueue(level, msg string, fields []Field) {
	if l.sink == nil {
		return
	}
	merged := concatFields(l.extra, fields)
	copied := make([]Field, len(merged))
	copy(copied, merged)
	l.sink.enqueue(lokiEntry{
		ts:     time.Now().UTC(),
		level:  level,
		kind:   kindFrom(msg, copied),
		msg:    msg,
		fields: copied,
	})
}

func concatFields(a, b []Field) []Field {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make([]Field, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

func kindFrom(msg string, fields []Field) string {
	for _, f := range fields {
		if f.Key != FieldKind {
			continue
		}
		s, _ := f.Value.(string)
		if s != "" {
			return s
		}
	}
	switch msg {
	case AuditLogMessage:
		return KindAudit
	case AccessLogMessage:
		return KindAccess
	case "http_request_end", "http_request_error", "http_request_start":
		return KindSDK
	default:
		return KindApp
	}
}

func (s *lokiSink) loop() {
	t := time.NewTicker(s.cfg.FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.flush(false)
		}
	}
}

func (s *lokiSink) shutdown() {
	s.stopped.Do(func() {
		s.closed.Store(true)
		close(s.stop)
		s.flush(true)
	})
}

func (s *lokiSink) enqueue(e lokiEntry) {
	if s.closed.Load() {
		return
	}
	var overflow []lokiEntry
	s.mu.Lock()
	for len(s.buf) >= s.cfg.MaxBuffer {
		s.buf = s.buf[1:]
	}
	s.buf = append(s.buf, e)
	if len(s.buf) >= s.cfg.BatchSize {
		overflow = s.buf
		s.buf = nil
	}
	s.mu.Unlock()
	if len(overflow) > 0 {
		s.postAsync(overflow)
	}
}

func (s *lokiSink) flush(wait bool) {
	s.mu.Lock()
	batch := s.buf
	s.buf = nil
	s.mu.Unlock()
	if len(batch) > 0 {
		if wait {
			s.post(batch)
		} else {
			s.postAsync(batch)
		}
	}
	if wait {
		s.wg.Wait()
	}
}

func (s *lokiSink) postAsync(batch []lokiEntry) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.post(batch)
	}()
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

type lokiPushBody struct {
	Streams []lokiStream `json:"streams"`
}

func (s *lokiSink) post(batch []lokiEntry) {
	if len(batch) == 0 {
		return
	}
	grouped := map[string]*lokiStream{}
	var order []string
	for _, e := range batch {
		labels := s.labels(e)
		key := labels["service"] + "|" + labels["env"] + "|" + labels["kind"] + "|" + labels["level"] + "|" + labels["event_type"] + "|" + labels["decision"]
		st, ok := grouped[key]
		if !ok {
			st = &lokiStream{Stream: labels}
			grouped[key] = st
			order = append(order, key)
		}
		ns := e.ts.UnixNano()
		if ns < 0 {
			ns = 0
		}
		st.Values = append(st.Values, []string{
			itoa(ns),
			lineJSON(e.msg, e.level, e.fields),
		})
	}
	body := lokiPushBody{Streams: make([]lokiStream, 0, len(order))}
	for _, k := range order {
		body.Streams = append(body.Streams, *grouped[k])
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, s.pushURL, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", lokiUserAgent)
	if s.cfg.Username != "" || s.cfg.Password != "" {
		req.SetBasicAuth(s.cfg.Username, s.cfg.Password)
	}
	res, err := s.client.Do(req)
	if err != nil {
		s.warnPush("error")
		return
	}
	defer func() { _, _ = io.Copy(io.Discard, res.Body); _ = res.Body.Close() }()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		s.warnPush(res.Status)
	}
}

func (s *lokiSink) warnPush(status string) {
	s.warnOnce.Do(func() {
		if s.inner != nil {
			s.inner.Warn("gigapipe loki push failed", String("status", status))
		}
	})
}

func (s *lokiSink) labels(e lokiEntry) map[string]string {
	out := map[string]string{
		"kind":  e.kind,
		"level": e.level,
	}
	if s.cfg.Env != "" {
		out["env"] = s.cfg.Env
	}
	service := s.cfg.Service
	var eventType, decision string
	for _, f := range e.fields {
		switch f.Key {
		case FieldService:
			if v, ok := f.Value.(string); ok && v != "" {
				service = v
			}
		case FieldEventType:
			if v, ok := f.Value.(string); ok {
				eventType = v
			}
		case FieldDecision:
			if v, ok := f.Value.(string); ok {
				decision = v
			}
		}
	}
	if service != "" {
		out["service"] = service
	}
	if e.kind == KindAudit {
		if eventType != "" {
			out["event_type"] = eventType
		}
		if decision != "" {
			out["decision"] = decision
		}
	}
	return out
}

func lineJSON(msg, level string, fields []Field) string {
	m := map[string]any{"msg": msg, "level": level}
	for _, f := range fields {
		if skipSecretKey(f.Key) {
			continue
		}
		switch v := f.Value.(type) {
		case error:
			if v != nil {
				m[f.Key] = v.Error()
			}
		default:
			m[f.Key] = v
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return `{"msg":` + jsonQuote(msg) + `}`
	}
	return string(b)
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func skipSecretKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	switch k {
	case "password", "token", "access_token", "refresh_token", "authorization",
		"api_key", "x-api-key", "current_password", "new_password", "confirm_password":
		return true
	default:
		return false
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
