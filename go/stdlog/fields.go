package stdlog

// Canonical structured field keys for service logs.
// Access-log builders only emit keys listed in AccessLogAllowedKeys.
const (
	FieldService               = "service"
	FieldComponent             = "component"
	FieldRequestID             = "request_id"
	FieldMethod                = "method"
	FieldPath                  = "path"
	FieldRoute                 = "route"
	FieldQuery                 = "query"
	FieldStatus                = "status"
	FieldDurationMS            = "duration_ms"
	FieldResponseSize          = "response_size"
	FieldRemoteAddr            = "remote_addr"
	FieldUserAgent             = "user_agent"
	FieldUserID                = "user_id"
	FieldError                 = "error"
	FieldRequestHeaders        = "request_headers"
	FieldResponseHeaders       = "response_headers"
	FieldRequestBody           = "request_body"
	FieldResponseBody          = "response_body"
	FieldRequestBodyReadError  = "request_body_read_error"
	FieldRequestBodyReadErrMsg = "request_body_read_error_message"
)

// AccessLogMessage is the fixed message for HTTP access-log events.
const AccessLogMessage = "http request completed"

// AccessLogAllowedKeys is the closed set of keys an access-log event may emit.
var AccessLogAllowedKeys = map[string]struct{}{
	FieldService:               {},
	FieldComponent:             {},
	FieldRequestID:             {},
	FieldMethod:                {},
	FieldPath:                  {},
	FieldRoute:                 {},
	FieldQuery:                 {},
	FieldStatus:                {},
	FieldDurationMS:            {},
	FieldResponseSize:          {},
	FieldRemoteAddr:            {},
	FieldUserAgent:             {},
	FieldUserID:                {},
	FieldError:                 {},
	FieldRequestHeaders:        {},
	FieldResponseHeaders:       {},
	FieldRequestBody:           {},
	FieldResponseBody:          {},
	FieldRequestBodyReadError:  {},
	FieldRequestBodyReadErrMsg: {},
}

// IsAccessLogKey reports whether key is allowed on an access-log event.
func IsAccessLogKey(key string) bool {
	_, ok := AccessLogAllowedKeys[key]
	return ok
}

// FilterAccessLogFields keeps only canonical access-log keys (unknown keys dropped).
func FilterAccessLogFields(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if IsAccessLogKey(k) {
			out[k] = v
		}
	}
	return out
}

// Field is a structured key/value pair.
type Field struct {
	Key   string
	Value any
}

// String creates a string field.
func String(key, value string) Field { return Field{Key: key, Value: value} }

// Int creates an int field.
func Int(key string, value int) Field { return Field{Key: key, Value: value} }

// Int64 creates an int64 field.
func Int64(key string, value int64) Field { return Field{Key: key, Value: value} }

// Bool creates a bool field.
func Bool(key string, value bool) Field { return Field{Key: key, Value: value} }

// Err creates an error field under key "error".
func Err(err error) Field { return Field{Key: FieldError, Value: err} }

// Any creates an arbitrary field.
func Any(key string, value any) Field { return Field{Key: key, Value: value} }
