package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
)

const forwardingHeaderName = "X-LanQin-Forwarded-By"

func (a *App) processInboundForwarding(ctx context.Context, messageID, mailboxID string, raw []byte) {
	target, userID, mailboxAddress, err := a.inboundForwardingTarget(ctx, mailboxID)
	if err != nil {
		a.log.Warn("failed to load forwarding target", "message", messageID, "mailbox", mailboxID, "error", err)
		return
	}
	if target == "" || userID == "" || mailboxAddress == "" {
		return
	}
	if normalizeEmail(target) == normalizeEmail(mailboxAddress) {
		return
	}
	if len(raw) == 0 {
		raw, err = a.forwardingRawMessage(ctx, messageID)
		if err != nil {
			a.log.Warn("failed to load raw message for forwarding", "message", messageID, "error", err)
			return
		}
	}
	if hasForwardingHeader(raw) {
		a.log.Warn("skip forwarding message that already has LanQin forwarding header", "message", messageID, "mailbox", mailboxID)
		return
	}
	forwarded := addForwardingHeaders(raw, mailboxAddress, a.cfg.PublicHostname)
	var rfcMessageID string
	_ = a.db.QueryRowContext(ctx, `SELECT message_id FROM messages WHERE id=?`, messageID).Scan(&rfcMessageID)
	if strings.TrimSpace(rfcMessageID) == "" {
		rfcMessageID = messageID
	}
	queueID, err := a.enqueueSend(ctx, sendQueueInput{
		UserID:        userID,
		MailboxID:     mailboxID,
		SentMessageID: messageID,
		MessageID:     rfcMessageID,
		Source:        sendSourceForwarding,
		MailFrom:      mailboxAddress,
		HeaderFrom:    mailboxAddress,
		Recipients:    []string{target},
		MIMEBytes:     forwarded,
		Now:           a.now().UTC(),
	})
	if err != nil {
		a.log.Warn("failed to enqueue inbound forwarding", "message", messageID, "mailbox", mailboxID, "target", target, "error", err)
		return
	}
	if queueID == "" {
		a.log.Warn("forwarding target configured but SMTP sending is not configured", "message", messageID, "mailbox", mailboxID, "target", target)
	}
}

func (a *App) inboundForwardingTarget(ctx context.Context, mailboxID string) (targetEmail, userID, mailboxAddress string, err error) {
	var mailboxTarget, accountTarget string
	err = a.db.QueryRowContext(ctx, `SELECT mb.user_id,mb.address,COALESCE(mfs.target_email,''),COALESCE(afs.target_email,'')
		FROM mailboxes mb
		LEFT JOIN mailbox_forwarding_settings mfs ON mfs.mailbox_id=mb.id
		LEFT JOIN account_forwarding_settings afs ON afs.user_id=mb.user_id
		WHERE mb.id=? AND mb.status='active'`, mailboxID).Scan(&userID, &mailboxAddress, &mailboxTarget, &accountTarget)
	if err != nil {
		return "", "", "", err
	}
	target := normalizeEmail(mailboxTarget)
	if target == "" {
		target = normalizeEmail(accountTarget)
	}
	if target == "" {
		return "", userID, mailboxAddress, nil
	}
	verified, err := a.forwardingEmailVerified(ctx, userID, target)
	if err != nil {
		return "", "", "", err
	}
	if !verified {
		return "", userID, mailboxAddress, nil
	}
	return target, userID, mailboxAddress, nil
}

func (a *App) forwardingRawMessage(ctx context.Context, messageID string) ([]byte, error) {
	msg, err := a.storedMessageByID(ctx, messageID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(msg.RawPath) != "" {
		if ok, err := a.pathIsUnderMaildirRoot(msg.RawPath); err == nil && ok {
			if raw, err := os.ReadFile(msg.RawPath); err == nil {
				return raw, nil
			}
		}
	}
	attachments, err := a.attachmentInputsForMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	return BuildMIME(MIMEMessage{
		From:        msg.From,
		FromName:    msg.FromName,
		To:          msg.To,
		CC:          msg.CC,
		BCC:         msg.BCC,
		Subject:     msg.Subject,
		Text:        msg.BodyText,
		HTML:        msg.BodyHTML,
		MessageID:   msg.MessageID,
		Date:        messageDate(msg),
		Attachments: attachments,
	})
}

func hasForwardingHeader(raw []byte) bool {
	header := raw
	if idx := bytes.Index(raw, []byte("\r\n\r\n")); idx >= 0 {
		header = raw[:idx]
	} else if idx := bytes.Index(raw, []byte("\n\n")); idx >= 0 {
		header = raw[:idx]
	}
	return strings.Contains(strings.ToLower(string(header)), strings.ToLower(forwardingHeaderName)+":")
}

func addForwardingHeaders(raw []byte, mailboxAddress, hostname string) []byte {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = "lanqin.local"
	}
	header := fmt.Sprintf("%s: %s\r\nX-LanQin-Forwarded-For: %s\r\n", forwardingHeaderName, hostname, normalizeEmail(mailboxAddress))
	if idx := bytes.Index(raw, []byte("\r\n\r\n")); idx >= 0 {
		out := make([]byte, 0, len(raw)+len(header))
		out = append(out, raw[:idx]...)
		out = append(out, []byte("\r\n"+header)...)
		out = append(out, raw[idx+2:]...)
		return out
	}
	if idx := bytes.Index(raw, []byte("\n\n")); idx >= 0 {
		lfHeader := strings.ReplaceAll(header, "\r\n", "\n")
		out := make([]byte, 0, len(raw)+len(lfHeader))
		out = append(out, raw[:idx]...)
		out = append(out, []byte("\n"+lfHeader)...)
		out = append(out, raw[idx+1:]...)
		return out
	}
	return append([]byte(header+"\r\n"), raw...)
}
