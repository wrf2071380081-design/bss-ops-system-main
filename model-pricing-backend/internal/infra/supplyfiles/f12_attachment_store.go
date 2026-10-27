package supplyfiles

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"model_bss/internal/domain/supply"
)

var f12AttachmentID = regexp.MustCompile(`^f12:v1:([a-f0-9]{32})$`)

type f12AttachmentManifest struct {
	ID          string `json:"file_id"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Purpose     string `json:"purpose"`
	Name        string `json:"file_name"`
	ContentType string `json:"content_type"`
	UploadedBy  string `json:"uploaded_by"`
	Size        int    `json:"size"`
}

// F12AttachmentFileStore keeps contract and credit documents in a private
// directory. The opaque ID and immutable manifest contain no filesystem path.
type F12AttachmentFileStore struct{ root string }

func NewF12AttachmentFileStore(root string) (*F12AttachmentFileStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("F12 attachment storage root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("F12 attachment storage root must be a real directory")
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, err
	}
	return &F12AttachmentFileStore{root: abs}, nil
}

func (s *F12AttachmentFileStore) Save(ctx context.Context, subjectType, subjectID, purpose, uploadedBy, name string, content []byte) (*supply.F12AttachmentContent, error) {
	if s == nil || s.root == "" {
		return nil, supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validF12FileIdentity(subjectType, subjectID, purpose, uploadedBy) || len(content) == 0 || len(content) > supply.F12AttachmentMaxFileBytes {
		return nil, &supply.FieldError{Path: "file", Reason: "invalid F12 attachment metadata or size"}
	}
	cleanName, contentType, err := validateF12AttachmentFile(name, content)
	if err != nil {
		return nil, err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	ref := "f12:v1:" + hex.EncodeToString(token[:])
	key := hex.EncodeToString(token[:])
	manifest := f12AttachmentManifest{ID: ref, SubjectType: subjectType, SubjectID: subjectID, Purpose: purpose, Name: cleanName, ContentType: contentType, UploadedBy: uploadedBy, Size: len(content)}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	blobPath, manifestPath := filepath.Join(s.root, key+".blob"), filepath.Join(s.root, key+".json")
	if err := writeExclusive(blobPath, content); err != nil {
		_ = os.Remove(blobPath)
		return nil, err
	}
	if err := writeExclusive(manifestPath, encoded); err != nil {
		_ = os.Remove(blobPath)
		_ = os.Remove(manifestPath)
		return nil, err
	}
	return &supply.F12AttachmentContent{F12AttachmentMetadata: f12ManifestMetadata(manifest), Content: append([]byte(nil), content...)}, nil
}

func (s *F12AttachmentFileStore) Open(ctx context.Context, ref string) (*supply.F12AttachmentContent, error) {
	if s == nil || s.root == "" {
		return nil, supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	match := f12AttachmentID.FindStringSubmatch(ref)
	if len(match) != 2 {
		return nil, supply.ErrNotFound
	}
	key := match[1]
	blob, err := readPrivateAttachmentFile(filepath.Join(s.root, key+".blob"), supply.F12AttachmentMaxFileBytes)
	if err != nil {
		return nil, supply.ErrNotFound
	}
	manifestBytes, err := readPrivateAttachmentFile(filepath.Join(s.root, key+".json"), 4096)
	if err != nil {
		return nil, supply.ErrReadContract
	}
	var manifest f12AttachmentManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil || manifest.ID != ref || !validF12FileIdentity(manifest.SubjectType, manifest.SubjectID, manifest.Purpose, manifest.UploadedBy) || manifest.Size != len(blob) {
		return nil, supply.ErrReadContract
	}
	cleanName, contentType, err := validateF12AttachmentFile(manifest.Name, blob)
	if err != nil || cleanName != manifest.Name || contentType != manifest.ContentType || len(blob) == 0 || len(blob) > supply.F12AttachmentMaxFileBytes {
		return nil, supply.ErrReadContract
	}
	return &supply.F12AttachmentContent{F12AttachmentMetadata: f12ManifestMetadata(manifest), Content: blob}, nil
}

func readPrivateAttachmentFile(path string, maxBytes int64) ([]byte, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, errors.New("stored attachment file has invalid type")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(pathInfo, info) || info.Size() < 0 || info.Size() > maxBytes {
		return nil, errors.New("stored attachment file has invalid type or size")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxBytes {
		return nil, errors.New("stored attachment file exceeds size limit")
	}
	return content, nil
}

func (s *F12AttachmentFileStore) Delete(ctx context.Context, ref string) error {
	if s == nil || s.root == "" {
		return supply.ErrDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	match := f12AttachmentID.FindStringSubmatch(ref)
	if len(match) != 2 {
		return supply.ErrNotFound
	}
	key := match[1]
	var errs []error
	for _, suffix := range []string{".blob", ".json"} {
		if err := os.Remove(filepath.Join(s.root, key+suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *F12AttachmentFileStore) Verify(ctx context.Context, ref, subjectType, subjectID, purpose, uploadedBy string) error {
	file, err := s.Open(ctx, ref)
	if err != nil {
		if errors.Is(err, supply.ErrNotFound) {
			return supply.ErrForbidden
		}
		return supply.ErrDependencyUnavailable
	}
	if file.SubjectType != subjectType || file.SubjectID != subjectID || file.Purpose != purpose || uploadedBy != "" && file.UploadedBy != uploadedBy {
		return supply.ErrForbidden
	}
	return nil
}

// ListBefore returns opaque IDs for older-than-cutoff regular blob files,
// including blobs left behind before their manifest was written. Callers must
// verify database references before deleting any returned file.
func (s *F12AttachmentFileStore) ListBefore(ctx context.Context, cutoff time.Time) ([]supply.F12AttachmentMetadata, error) {
	if s == nil || s.root == "" || cutoff.IsZero() {
		return nil, supply.ErrDependencyUnavailable
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, supply.ErrDependencyUnavailable
	}
	files := make([]supply.F12AttachmentMetadata, 0)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".blob") {
			continue
		}
		key := strings.TrimSuffix(name, ".blob")
		ref := "f12:v1:" + key
		if !f12AttachmentID.MatchString(ref) {
			continue
		}
		blobPath := filepath.Join(s.root, name)
		blobInfo, err := os.Lstat(blobPath)
		if err != nil || !blobInfo.Mode().IsRegular() || !blobInfo.ModTime().Before(cutoff) || blobInfo.Size() <= 0 || blobInfo.Size() > supply.F12AttachmentMaxFileBytes {
			continue
		}
		// The database reference check happens in F12AttachmentService. Returning
		// only the opaque ID also lets cleanup remove a crash orphan whose JSON
		// manifest was never completed, while still preserving any referenced blob.
		files = append(files, supply.F12AttachmentMetadata{ID: ref})
	}
	return files, nil
}

func f12ManifestMetadata(value f12AttachmentManifest) supply.F12AttachmentMetadata {
	return supply.F12AttachmentMetadata{ID: value.ID, SubjectType: value.SubjectType, SubjectID: value.SubjectID, Purpose: value.Purpose, Name: value.Name, ContentType: value.ContentType, UploadedBy: value.UploadedBy, Size: value.Size}
}

func validF12FileIdentity(subjectType, subjectID, purpose, actorID string) bool {
	return (subjectType == "SUPPLIER" || subjectType == "CUSTOMER") && positiveBigint(subjectID) && positiveBigint(actorID) && (purpose == "CONTRACT" || purpose == "CREDIT")
}

func positiveBigint(value string) bool {
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0 && strconv.FormatInt(parsed, 10) == value
}

func validateF12AttachmentFile(name string, content []byte) (string, string, error) {
	if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, `/\\`) || len([]rune(name)) > 128 {
		return "", "", &supply.FieldError{Path: "file_name", Reason: "invalid attachment file name"}
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return "", "", &supply.FieldError{Path: "file_name", Reason: "invalid attachment file name"}
		}
	}
	var expected string
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		expected = "application/pdf"
	case ".png":
		expected = "image/png"
	case ".jpg", ".jpeg":
		expected = "image/jpeg"
	default:
		return "", "", &supply.FieldError{Path: "file", Reason: "only PDF, PNG and JPEG are accepted"}
	}
	if len(content) < 8 {
		return "", "", &supply.FieldError{Path: "file", Reason: "invalid attachment content"}
	}
	detected := http.DetectContentType(content[:min(len(content), 512)])
	if !strings.HasPrefix(detected, expected) {
		return "", "", &supply.FieldError{Path: "file", Reason: "file content does not match its extension"}
	}
	return name, expected, nil
}
