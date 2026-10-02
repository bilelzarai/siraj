package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/bilelzarai/siraj/internal/models"
	"github.com/bilelzarai/siraj/internal/repository"
)

var (
	ErrUploadsOff     = errors.New("uploads are not configured")
	ErrUploadTooLarge = errors.New("file too large")
	ErrUploadType     = errors.New("file type not accepted")
	ErrUploadEmpty    = errors.New("file is empty")
)

// acceptedUploads maps a sniffed media type to the kind of thing it is.
//
// Sniffed, never taken from the request: Content-Type is whatever the client
// felt like saying, and a script announced as a photograph is exactly the
// upload this list exists to refuse. Nothing executable is on it, which is why
// SVG is missing — it is a document that can carry script, not a picture.
var acceptedUploads = map[string]string{
	"image/jpeg":      models.AttachmentImage,
	"image/png":       models.AttachmentImage,
	"image/gif":       models.AttachmentImage,
	"image/webp":      models.AttachmentImage,
	"audio/webm":      models.AttachmentAudio,
	"video/webm":      models.AttachmentAudio, // what MediaRecorder labels a voice note
	"audio/ogg":       models.AttachmentAudio,
	"audio/mpeg":      models.AttachmentAudio,
	"audio/mp4":       models.AttachmentAudio,
	"application/pdf": models.AttachmentFile,
	"text/plain":      models.AttachmentFile,
}

// Uploads stores what people send and hands back a row describing it.
type Uploads struct {
	repo  *repository.Repo
	dir   string
	limit int64
}

func NewUploads(repo *repository.Repo, dir string, limit int64) *Uploads {
	return &Uploads{repo: repo, dir: dir, limit: limit}
}

// Enabled reports whether there is anywhere to put bytes.
func (u *Uploads) Enabled() bool { return u != nil && u.dir != "" }

// StoreRecording is Store for a voice note, which arrives with a length the
// bytes themselves do not carry: a WebM stream from MediaRecorder has no
// duration in its header, so the only party that ever knows how long the
// recording ran is the browser that made it.
//
// The claim is only believed of something that turns out to be audio. A length
// attached to a PDF is a client saying something about a file that cannot have
// one, and it is dropped rather than stored.
func (u *Uploads) StoreRecording(ctx context.Context, ownerID uuid.UUID, name string, src io.Reader, durationMS int) (*models.Attachment, error) {
	return u.store(ctx, ownerID, name, src, durationMS)
}

// Store reads the upload, decides what it is from its own first bytes, writes
// it, and records it.
//
// The read is capped at one byte over the limit so an oversized file is
// refused rather than buffered: without the cap the only thing standing
// between the process and its memory is the honesty of Content-Length.
func (u *Uploads) Store(ctx context.Context, ownerID uuid.UUID, name string, src io.Reader) (*models.Attachment, error) {
	return u.store(ctx, ownerID, name, src, 0)
}

func (u *Uploads) store(ctx context.Context, ownerID uuid.UUID, name string, src io.Reader, durationMS int) (*models.Attachment, error) {
	if !u.Enabled() {
		return nil, ErrUploadsOff
	}

	head := make([]byte, 512)
	n, err := io.ReadFull(src, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	head = head[:n]
	if len(head) == 0 {
		return nil, ErrUploadEmpty
	}

	mime := sniff(head, name)
	kind, ok := acceptedUploads[mime]
	if !ok {
		return nil, ErrUploadType
	}

	id := uuid.New()
	path, err := u.pathFor(id)
	if err != nil {
		return nil, err
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	written, err := io.Copy(file, io.MultiReader(
		strings.NewReader(string(head)),
		io.LimitReader(src, u.limit+1-int64(len(head))),
	))
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	if written > u.limit {
		os.Remove(path)
		return nil, ErrUploadTooLarge
	}

	if kind != models.AttachmentAudio || durationMS < 0 {
		durationMS = 0
	}
	att := &models.Attachment{
		ID: id, OwnerID: ownerID, Kind: kind, Mime: mime,
		Name: cleanName(name, kind), Bytes: written, DurationMS: durationMS,
	}
	if err := u.repo.CreateAttachment(ctx, att); err != nil {
		os.Remove(path)
		return nil, err
	}
	return att, nil
}

// Open reads stored bytes back.
func (u *Uploads) Open(id uuid.UUID) (*os.File, error) {
	if !u.Enabled() {
		return nil, ErrUploadsOff
	}
	path, err := u.pathFor(id)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// pathFor shards on the first two characters of the id, so a directory does
// not end up holding every file anybody ever sent.
func (u *Uploads) pathFor(id uuid.UUID) (string, error) {
	name := id.String()
	dir := filepath.Join(u.dir, name[:2])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// sniff decides the media type from the bytes themselves, falling back to the
// name only where sniffing is known to be blind.
func sniff(head []byte, name string) string {
	mime := http.DetectContentType(head)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	// A WebM voice note and a WebM video are the same container, and Go's
	// sniffer answers "video/webm" for both. The extension is the only thing
	// that distinguishes them, and both are on the accepted list anyway.
	if mime == "application/octet-stream" {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".webm":
			return "audio/webm"
		case ".ogg", ".oga":
			return "audio/ogg"
		case ".mp3":
			return "audio/mpeg"
		case ".m4a":
			return "audio/mp4"
		}
	}
	return mime
}

// cleanName keeps something recognisable to show and nothing that could be
// read as a path.
func cleanName(name, kind string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = fmt.Sprintf("%s-upload", kind)
	}
	if runes := []rune(name); len(runes) > 120 {
		name = string(runes[:120])
	}
	return name
}
