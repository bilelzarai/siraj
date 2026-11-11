package messages

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"siraj/internal/db/sqlc"
	"siraj/internal/platform/apperr"
	"siraj/internal/platform/authctx"
)

// MaxUpload is the biggest attachment a message takes: enough for a photo from a
// phone camera, a voice note or a set of notes (MS-02).
const MaxUpload = 10 << 20

// MaxUploadMB is the same number as the screens say it out loud.
const MaxUploadMB = MaxUpload >> 20

// SetUploadsDir tells the service where files land. Without it an attachment is
// refused rather than written somewhere nobody can serve it from.
func (s *Service) SetUploadsDir(dir string) { s.uploads = dir }

// SaveUpload writes one attachment and records it, and says which kind of message it
// makes: a photo, a voice note or a plain file (MS-02).
func (s *Service) SaveUpload(ctx context.Context, who *authctx.Principal, fh *multipart.FileHeader) (uuid.UUID, string, error) {
	if who == nil {
		return uuid.Nil, "", apperr.Unauthorized("Sign in first")
	}
	if s.uploads == "" {
		return uuid.Nil, "", apperr.Conflict("uploads_off", "Attachments are not available")
	}
	if fh.Size <= 0 {
		return uuid.Nil, "", apperr.Invalid(map[string]string{"upload": "that file is empty"})
	}
	// Say the limit out loud: a refused attachment names the size it had to be under.
	if fh.Size > MaxUpload {
		return uuid.Nil, "", apperr.Invalid(map[string]string{
			"upload": fmt.Sprintf("up to %d MB — that one is %.1f MB", MaxUploadMB, float64(fh.Size)/(1<<20)),
		})
	}
	mime := fh.Header.Get("Content-Type")
	kind := kindOf(mime, fh.Filename)

	src, err := fh.Open()
	if err != nil {
		return uuid.Nil, "", err
	}
	defer src.Close()

	// The stored name is random: an uploaded name never decides a path on disk.
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return uuid.Nil, "", err
	}
	key := hex.EncodeToString(raw) + strings.ToLower(filepath.Ext(fh.Filename))
	if err := os.MkdirAll(s.uploads, 0o755); err != nil {
		return uuid.Nil, "", err
	}
	dst, err := os.Create(filepath.Join(s.uploads, key))
	if err != nil {
		return uuid.Nil, "", err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, io.LimitReader(src, MaxUpload)); err != nil {
		return uuid.Nil, "", err
	}

	row, err := s.db.Q.CreateUpload(ctx, sqlc.CreateUploadParams{
		OwnerID:    uuid.NullUUID{UUID: who.ID, Valid: true},
		Kind:       kind,
		Mime:       mime,
		Bytes:      int32(fh.Size),
		StorageKey: key,
		FileName:   text(filepath.Base(fh.Filename)),
	})
	if err != nil {
		return uuid.Nil, "", err
	}
	return row.ID, kind, nil
}

// kindOf reads what was sent: a photo, a voice note, or anything else.
func kindOf(mime, name string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "voice"
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return "image"
	case ".webm", ".ogg", ".mp3", ".m4a", ".wav":
		return "voice"
	}
	return "file"
}
