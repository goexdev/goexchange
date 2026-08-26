// Package audit provides admin operation audit logging.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func NewService(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{pool: pool, log: log}
}

type LogEntry struct {
	AdminUserID *uuid.UUID    `json:"admin_user_id,omitempty"`
	AdminEmail  string         `json:"admin_email"`
	Action      string         `json:"action"`
	TargetType  string         `json:"target_type"`
	TargetID    *uuid.UUID     `json:"target_id,omitempty"`
	TargetLabel string         `json:"target_label"`
	Details     map[string]any `json:"details"`
	IP          string         `json:"ip"`
	UserAgent   string         `json:"user_agent"`
	Status      string         `json:"status"`
	ErrorMsg    string         `json:"error_msg"`
}

func (s *Service) Log(ctx context.Context, e LogEntry) {
	if e.Status == "" {
		e.Status = "success"
	}
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	detailsJSON, _ := json.Marshal(e.Details)
	var targetID *uuid.UUID
	if e.TargetID != nil {
		tid := *e.TargetID
		targetID = &tid
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_log (
			admin_user_id, admin_email, action, target_type,
			target_id, target_label, details, ip, user_agent, status, error_msg
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, NULLIF($8, '')::inet, NULLIF($9, ''), $10, NULLIF($11, ''))
	`,
		e.AdminUserID, e.AdminEmail, e.Action, e.TargetType,
		targetID, e.TargetLabel, string(detailsJSON), e.IP, e.UserAgent, e.Status, e.ErrorMsg,
	)
	if err != nil {
		s.log.Error("audit log insert failed", "error", err, "action", e.Action)
	}
}

type QueryFilter struct {
	AdminUserID *uuid.UUID
	Action      string
	TargetType  string
	TargetID    *uuid.UUID
	Since       *time.Time
	Until       *time.Time
	Limit       int
	Offset      int
}

type LogEntryWithMeta struct {
	LogEntry
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) Query(ctx context.Context, f QueryFilter) ([]LogEntryWithMeta, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := `
		SELECT id, admin_user_id, admin_email, action, target_type,
		       target_id, COALESCE(target_label, ''), details,
		       COALESCE(host(ip), '') as ip, COALESCE(user_agent, ''), status, COALESCE(error_msg, ''),
		       created_at
		FROM audit_log
		WHERE 1=1
	`
	args := []interface{}{}
	idx := 1
	if f.AdminUserID != nil {
		q += " AND admin_user_id = $" + strconv.Itoa(idx)
		args = append(args, *f.AdminUserID)
		idx++
	}
	if f.Action != "" {
		q += " AND action = $" + strconv.Itoa(idx)
		args = append(args, f.Action)
		idx++
	}
	if f.TargetType != "" {
		q += " AND target_type = $" + strconv.Itoa(idx)
		args = append(args, f.TargetType)
		idx++
	}
	if f.TargetID != nil {
		q += " AND target_id = $" + strconv.Itoa(idx)
		args = append(args, *f.TargetID)
		idx++
	}
	if f.Since != nil {
		q += " AND created_at >= $" + strconv.Itoa(idx)
		args = append(args, *f.Since)
		idx++
	}
	if f.Until != nil {
		q += " AND created_at < $" + strconv.Itoa(idx)
		args = append(args, *f.Until)
		idx++
	}
	q += " ORDER BY created_at DESC LIMIT $" + strconv.Itoa(idx) + " OFFSET $" + strconv.Itoa(idx+1)
	args = append(args, f.Limit, f.Offset)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LogEntryWithMeta
	for rows.Next() {
		var e LogEntryWithMeta
		var detailsBytes []byte
		if err := rows.Scan(
			&e.ID, &e.AdminUserID, &e.AdminEmail, &e.Action, &e.TargetType,
			&e.TargetID, &e.TargetLabel, &detailsBytes,
			&e.IP, &e.UserAgent, &e.Status, &e.ErrorMsg,
			&e.CreatedAt,
		); err != nil {
			return nil, err
		}
		if len(detailsBytes) > 0 {
			err = json.Unmarshal(detailsBytes, &e.Details)
			if err != nil {
				e.Details = map[string]any{}
			}
		}
		if e.Details == nil {
			e.Details = map[string]any{}
		}
		out = append(out, e)
	}
	return out, nil
}

func FromRequest(r *http.Request) (ip, ua string) {
	ip = r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	ua = r.Header.Get("User-Agent")
	return
}
