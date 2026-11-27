package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/logger"
)

type auditSourceContextKey struct{}

func withRequestAuditSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, auditSourceContextKey{}, source)
}

func (s *Server) auditRequest(r *http.Request, event, actor string, fields map[string]any) {
	details := make(map[string]any, len(fields))
	for key, value := range fields {
		if key != "actor_ip" {
			details[key] = value
		}
	}
	source, _ := r.Context().Value(auditSourceContextKey{}).(string)
	if source == "" {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			source = "api"
		} else {
			source = "web"
		}
	}
	details["source"] = source
	category, action, found := strings.Cut(event, ".")
	if !found {
		category, action = event, event
	}
	actorID := auditFieldString(fields["owner_id"])
	actorIP := s.requestIP(r)
	targetType, targetID := "", auditFieldString(fields["id"])
	if actor == "owner" {
		targetType = "owner_token"
		targetID = auditFieldString(fields["token_prefix"])
	}
	if service := auditFieldString(fields["service"]); service != "" {
		targetType, targetID = "mail_service", service
	}
	if auditFieldString(fields["domain"]) != "" && targetID != "" {
		targetType = "mail_domain"
	}
	if strings.HasPrefix(event, "mail.alias.") && targetID != "" {
		targetType = "mail_alias"
	}
	if strings.HasPrefix(event, "mail.user.") && targetID != "" {
		targetType = "mail_user"
	}
	if strings.HasPrefix(event, "mail.forward.") && targetID != "" {
		targetType = "mail_forward"
	}
	success := true
	if value, ok := fields["success"].(bool); ok {
		success = value
	}
	logFields := make(map[string]any, len(fields)+1)
	for key, value := range fields {
		logFields[key] = value
	}
	logFields["actor_ip"] = actorIP
	logFields["source"] = source
	logger.Audit(event, actor, logFields)
	if s.db == nil {
		return
	}
	if err := s.db.AppendAuditEvent(r.Context(), database.AuditEvent{
		Category: category, Action: action, ActorType: actor, ActorID: actorID, ActorIP: actorIP,
		TargetType: targetType, TargetID: targetID, Details: details, Success: success,
	}); err != nil {
		logger.Warnf("could not persist audit event %s: %v", event, err)
	}
}

func auditFieldString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}
