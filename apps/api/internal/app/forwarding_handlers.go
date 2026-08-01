package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type ForwardingVerifiedEmail struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Verified  bool      `json:"verified"`
	CreatedAt time.Time `json:"createdAt"`
}

type MailboxForwardingRule struct {
	MailboxID   string `json:"mailboxId"`
	TargetEmail string `json:"targetEmail"`
}

type ForwardingSettings struct {
	VerifiedEmails     []ForwardingVerifiedEmail `json:"verifiedEmails"`
	AccountTargetEmail string                    `json:"accountTargetEmail"`
	MailboxRules       []MailboxForwardingRule   `json:"mailboxRules"`
}

func (a *App) handleForwardingSettings(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) handleAddForwardingVerifiedEmail(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	email := normalizeEmail(req.Email)
	if email == "" || !strings.Contains(email, "@") {
		badRequest(w, errors.New("邮箱地址无效"))
		return
	}
	if owns, err := a.userOwnsMailboxAddress(r.Context(), user.ID, email); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to check mailbox")
		return
	} else if owns {
		badRequest(w, errors.New("不能把当前账号邮箱作为转发验证邮箱"))
		return
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	id := newID("fwd")
	_, err := a.db.ExecContext(r.Context(), `INSERT INTO forwarding_verified_emails(id,user_id,email,verified,created_at,updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(user_id,email) DO UPDATE SET verified=1,updated_at=excluded.updated_at`,
		id, user.ID, email, 1, now, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save verified email")
		return
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusCreated, settings)
}

func (a *App) handleDeleteForwardingVerifiedEmail(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		respondError(w, http.StatusNotFound, "verified email not found")
		return
	}
	var email string
	if err := a.db.QueryRowContext(r.Context(), `SELECT email FROM forwarding_verified_emails WHERE id=? AND user_id=?`, id, user.ID).Scan(&email); err != nil {
		respondError(w, http.StatusNotFound, "verified email not found")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()
	now := a.now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM forwarding_verified_emails WHERE id=? AND user_id=?`, id, user.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete verified email")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE account_forwarding_settings SET target_email='',updated_at=? WHERE user_id=? AND target_email=?`, now, user.ID, email); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update account forwarding")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM mailbox_forwarding_settings
		WHERE target_email=? AND mailbox_id IN (SELECT id FROM mailboxes WHERE user_id=?)`, email, user.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update mailbox forwarding")
		return
	}
	if err := tx.Commit(); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save forwarding settings")
		return
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) handleUpdateAccountForwarding(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var req struct {
		TargetEmail string `json:"targetEmail"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	target, err := a.cleanForwardingTarget(r.Context(), user.ID, req.TargetEmail)
	if err != nil {
		badRequest(w, err)
		return
	}
	now := a.now().UTC().Format(time.RFC3339Nano)
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO account_forwarding_settings(user_id,target_email,updated_at)
		VALUES(?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET target_email=excluded.target_email,updated_at=excluded.updated_at`,
		user.ID, target, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save account forwarding")
		return
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) handleUpdateMailboxForwarding(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	mailboxID := strings.TrimSpace(chi.URLParam(r, "id"))
	if mailboxID == "" {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	if ok, err := a.userOwnsMailboxID(r.Context(), user.ID, mailboxID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to check mailbox")
		return
	} else if !ok {
		respondError(w, http.StatusNotFound, "mailbox not found")
		return
	}
	var req struct {
		TargetEmail string `json:"targetEmail"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, err)
		return
	}
	target, err := a.cleanForwardingTarget(r.Context(), user.ID, req.TargetEmail)
	if err != nil {
		badRequest(w, err)
		return
	}
	if target == "" {
		if _, err := a.db.ExecContext(r.Context(), `DELETE FROM mailbox_forwarding_settings WHERE mailbox_id=?`, mailboxID); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save mailbox forwarding")
			return
		}
	} else {
		now := a.now().UTC().Format(time.RFC3339Nano)
		if _, err := a.db.ExecContext(r.Context(), `INSERT INTO mailbox_forwarding_settings(mailbox_id,target_email,updated_at)
			VALUES(?,?,?)
			ON CONFLICT(mailbox_id) DO UPDATE SET target_email=excluded.target_email,updated_at=excluded.updated_at`,
			mailboxID, target, now); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to save mailbox forwarding")
			return
		}
	}
	settings, err := a.forwardingSettings(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load forwarding settings")
		return
	}
	respondJSON(w, http.StatusOK, settings)
}

func (a *App) forwardingSettings(ctx context.Context, userID string) (ForwardingSettings, error) {
	settings := ForwardingSettings{
		VerifiedEmails: []ForwardingVerifiedEmail{},
		MailboxRules:   []MailboxForwardingRule{},
	}
	rows, err := a.db.QueryContext(ctx, `SELECT id,email,verified,created_at FROM forwarding_verified_emails WHERE user_id=? ORDER BY created_at DESC,email`, userID)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ForwardingVerifiedEmail
		var verified int
		var created string
		if err := rows.Scan(&item.ID, &item.Email, &verified, &created); err != nil {
			return settings, err
		}
		item.Verified = intBool(verified)
		item.CreatedAt = parseTime(created)
		settings.VerifiedEmails = append(settings.VerifiedEmails, item)
	}
	if err := rows.Err(); err != nil {
		return settings, err
	}
	err = a.db.QueryRowContext(ctx, `SELECT target_email FROM account_forwarding_settings WHERE user_id=?`, userID).Scan(&settings.AccountTargetEmail)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return settings, err
	}
	rows, err = a.db.QueryContext(ctx, `SELECT mfs.mailbox_id,mfs.target_email
		FROM mailbox_forwarding_settings mfs
		JOIN mailboxes mb ON mb.id=mfs.mailbox_id
		WHERE mb.user_id=? AND mfs.target_email<>''
		ORDER BY mb.address`, userID)
	if err != nil {
		return settings, err
	}
	defer rows.Close()
	for rows.Next() {
		var item MailboxForwardingRule
		if err := rows.Scan(&item.MailboxID, &item.TargetEmail); err != nil {
			return settings, err
		}
		settings.MailboxRules = append(settings.MailboxRules, item)
	}
	return settings, rows.Err()
}

func (a *App) cleanForwardingTarget(ctx context.Context, userID, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") {
		return "", nil
	}
	target := normalizeEmail(value)
	if target == "" || !strings.Contains(target, "@") {
		return "", errors.New("转发邮箱无效")
	}
	ok, err := a.forwardingEmailVerified(ctx, userID, target)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("请先添加验证邮箱")
	}
	return target, nil
}

func (a *App) forwardingEmailVerified(ctx context.Context, userID, email string) (bool, error) {
	var count int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM forwarding_verified_emails WHERE user_id=? AND email=? AND verified=1`, userID, normalizeEmail(email)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *App) userOwnsMailboxID(ctx context.Context, userID, mailboxID string) (bool, error) {
	var count int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM mailboxes WHERE id=? AND user_id=? AND status='active'`, mailboxID, userID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *App) userOwnsMailboxAddress(ctx context.Context, userID, address string) (bool, error) {
	var count int
	err := a.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM mailboxes WHERE user_id=? AND address=? AND status='active'`, userID, normalizeEmail(address)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
