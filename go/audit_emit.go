package authsdk

import (
	"context"
	"errors"
	"strconv"

	"github.com/hinha/auth-sdks/go/logging"
)

func (c *Client) emitAudit(ctx context.Context, level logging.Level, eventType, decision string, extra ...logging.Field) {
	if c == nil {
		return
	}
	fields := make([]logging.Field, 0, 6+len(extra))
	fields = append(fields,
		logging.String(logging.FieldEventType, eventType),
		logging.String(logging.FieldDecision, decision),
		logging.String(logging.FieldSource, logging.SourceAuthSDKs),
		logging.String(logging.FieldService, c.cfg.ApplicationService),
	)
	fields = append(fields, extra...)
	logging.AuditAt(ctx, c.log, level, fields...)
}

func apiStatus(err error) int {
	var api *APIError
	if errors.As(err, &api) && api != nil {
		return api.StatusCode
	}
	return 0
}

func actorUserID(id *uint) logging.Field {
	if id == nil {
		return logging.String(logging.FieldActorID, "")
	}
	return logging.String(logging.FieldActorID, strconv.FormatUint(uint64(*id), 10))
}

func allowDecision(allowed bool) (string, logging.Level) {
	if allowed {
		return logging.DecisionAllow, logging.LevelInfo
	}
	return logging.DecisionDeny, logging.LevelWarn
}
