package stdlog

// AccessEvent is the strict HTTP access-log payload.
type AccessEvent struct {
	Service              string
	Component            string
	RequestID            string
	Method               string
	Path                 string
	Route                string
	Query                string
	Status               int
	DurationMS           int64
	ResponseSize         int
	RemoteAddr           string
	UserAgent            string
	UserID               string
	RequestHeaders       map[string]any
	ResponseHeaders      map[string]any
	RequestBody          any
	ResponseBody         any
	RequestBodyReadError error
}

// Fields returns only canonical access-log fields (unknown keys never appear).
func (e AccessEvent) Fields() []Field {
	component := e.Component
	if component == "" {
		component = "http"
	}
	raw := map[string]any{
		FieldComponent:    component,
		FieldRequestID:    e.RequestID,
		FieldMethod:       e.Method,
		FieldPath:         e.Path,
		FieldRoute:        e.Route,
		FieldQuery:        e.Query,
		FieldStatus:       e.Status,
		FieldDurationMS:   e.DurationMS,
		FieldResponseSize: e.ResponseSize,
		FieldRemoteAddr:   e.RemoteAddr,
		FieldUserAgent:    e.UserAgent,
	}
	if e.Service != "" {
		raw[FieldService] = e.Service
	}
	if e.UserID != "" {
		raw[FieldUserID] = e.UserID
	}
	if e.RequestHeaders != nil {
		raw[FieldRequestHeaders] = e.RequestHeaders
	}
	if e.ResponseHeaders != nil {
		raw[FieldResponseHeaders] = e.ResponseHeaders
	}
	if e.RequestBody != nil {
		raw[FieldRequestBody] = e.RequestBody
	}
	if e.ResponseBody != nil {
		raw[FieldResponseBody] = e.ResponseBody
	}
	if e.RequestBodyReadError != nil {
		raw[FieldRequestBodyReadError] = true
		raw[FieldRequestBodyReadErrMsg] = e.RequestBodyReadError.Error()
	}
	filtered := FilterAccessLogFields(raw)
	out := make([]Field, 0, len(filtered))
	for k, v := range filtered {
		out = append(out, Field{Key: k, Value: v})
	}
	return out
}
