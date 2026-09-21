package obs

import (
	"strings"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// hopSemconvAttrs is the Tempo service-graph set written at span start.
// Custom labels (component / operation / peer) stay on End for metrics search.
func hopSemconvAttrs(hop Hop) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 3)
	if hop.Component != hopComponentHTTPServer && hop.Peer != hopStatusUnknown {
		attrs = append(attrs, semconv.PeerService(hop.Peer))
	}
	switch hop.Component {
	case "redis":
		attrs = append(attrs, attribute.String("db.system", "redis"))
	case "db":
		attrs = append(attrs, attribute.String("db.system", dbSystemFromPeer(hop.Peer)))
	case hopComponentHTTPServer:
		method, route := splitHTTPOperation(hop.Operation)
		if method != "" {
			attrs = append(attrs, semconv.HTTPRequestMethodKey.String(method))
		}
		if route != "" {
			attrs = append(attrs, semconv.HTTPRoute(route))
		}
	}
	return attrs
}

func dbSystemFromPeer(peer string) string {
	switch strings.ToLower(peer) {
	case "postgres", "postgresql":
		return "postgresql"
	case "mysql":
		return "mysql"
	case "sqlite":
		return "sqlite"
	default:
		return "other_sql"
	}
}

func splitHTTPOperation(op string) (method, route string) {
	method, route, ok := strings.Cut(strings.TrimSpace(op), " ")
	if !ok {
		return op, ""
	}
	return method, strings.TrimSpace(route)
}
