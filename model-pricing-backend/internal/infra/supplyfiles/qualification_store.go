package supplyfiles

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"model_bss/internal/domain/supply"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const QualificationFileMaxBytes = supply.SupplierQualificationMaxFileBytes

var qualificationFileID = regexp.MustCompile(`^squal:v1:([a-f0-9]{32})$`)

type QualificationFile = supply.SupplierQualificationFile

type qualificationFileMetadata struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
}

// QualificationFileStore is a narrow, private blob adapter for A's supplier
// qualifications. IDs are opaque; filesystem paths are never returned.
type QualificationFileStore struct{ root string }

func NewQualificationFileStore(root string) (*QualificationFileStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("qualification file storage root is required")
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
		return nil, errors.New("qualification file storage root must be a real directory")
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, fmt.Errorf("restrict qualification file storage permissions: %w", err)
	}
	return &QualificationFileStore{root: abs}, nil
}

func (s *QualificationFileStore) Save(ctx context.Context, name string, content []byte) (*QualificationFile, error) {
	if s == nil || s.root == "" {
		return nil, errors.New("qualification file storage is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(content) == 0 || len(content) > QualificationFileMaxBytes {
		return nil, errors.New("qualification file size must be between 1 byte and 2 MiB")
	}
	cleanName, contentType, err := validateQualificationFile(name, content)
	if err != nil {
		return nil, err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	key := hex.EncodeToString(token[:])
	ref := "squal:v1:" + key
	blobPath := filepath.Join(s.root, key+".blob")
	metaPath := filepath.Join(s.root, key+".json")
	if err := writeExclusive(blobPath, content); err != nil {
		_ = os.Remove(blobPath)
		return nil, err
	}
	metadata, err := json.Marshal(qualificationFileMetadata{Name: cleanName, ContentType: contentType})
	if err != nil {
		_ = os.Remove(blobPath)
		return nil, err
	}
	if err := writeExclusive(metaPath, metadata); err != nil {
		_ = os.Remove(blobPath)
		_ = os.Remove(metaPath)
		return nil, err
	}
	return &QualificationFile{ID: ref, Name: cleanName, ContentType: contentType, Size: len(content)}, nil
}

func (s *QualificationFileStore) Open(ctx context.Context, ref string) (*QualificationFile, error) {
	if s == nil || s.root == "" {
		return nil, errors.New("qualification file storage is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	match := qualificationFileID.FindStringSubmatch(ref)
	if len(match) != 2 {
		return nil, errors.New("invalid qualification file reference")
	}
	key := match[1]
	blob, err := readPrivateAttachmentFile(filepath.Join(s.root, key+".blob"), QualificationFileMaxBytes)
	if err != nil {
		return nil, err
	}
	if len(blob) == 0 || len(blob) > QualificationFileMaxBytes {
		return nil, errors.New("stored qualification file has invalid size")
	}
	metadataBytes, err := readPrivateAttachmentFile(filepath.Join(s.root, key+".json"), 4096)
	if err != nil {
		return nil, err
	}
	var metadata qualificationFileMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return nil, errors.New("stored qualification file metadata is invalid")
	}
	cleanName, contentType, err := validateQualificationFile(metadata.Name, blob)
	if err != nil || cleanName != metadata.Name || contentType != metadata.ContentType {
		return nil, errors.New("stored qualification file content does not match its metadata")
	}
	return &QualificationFile{ID: ref, Name: cleanName, ContentType: contentType, Size: len(blob), Content: blob}, nil
}

func (s *QualificationFileStore) Delete(ctx context.Context, ref string) error {
	if s == nil || s.root == "" {
		return errors.New("qualification file storage is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	match := qualificationFileID.FindStringSubmatch(ref)
	if len(match) != 2 {
		return errors.New("invalid qualification file reference")
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

// ListBefore returns opaque references for old, regular blob or metadata files.
// The SQL cleaner must verify that no qualification row references each ID
// before calling Delete.
func (s *QualificationFileStore) ListBefore(ctx context.Context, cutoff time.Time) ([]string, error) {
	if s == nil || s.root == "" || cutoff.IsZero() {
		return nil, errors.New("qualification file storage or cutoff is unavailable")
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		suffix := filepath.Ext(name)
		if suffix != ".blob" && suffix != ".json" {
			continue
		}
		key := strings.TrimSuffix(name, suffix)
		ref := "squal:v1:" + key
		if !qualificationFileID.MatchString(ref) {
			continue
		}
		info, err := os.Lstat(filepath.Join(s.root, name))
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
			continue
		}
		if suffix == ".blob" && (info.Size() <= 0 || info.Size() > QualificationFileMaxBytes) || suffix == ".json" && (info.Size() <= 0 || info.Size() > 4096) {
			continue
		}
		seen[ref] = struct{}{}
	}
	refs := make([]string, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs, nil
}

func writeExclusive(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func validateQualificationFile(name string, content []byte) (string, string, error) {
	if name == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, `/\\`) || len([]rune(name)) > 128 {
		return "", "", errors.New("qualification file name is invalid")
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return "", "", errors.New("qualification file name is invalid")
		}
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return "", "", errors.New("qualification file extension is required")
	}
	var want string
	switch ext {
	case ".pdf":
		want = "application/pdf"
	case ".png":
		want = "image/png"
	case ".jpg", ".jpeg":
		want = "image/jpeg"
	default:
		return "", "", errors.New("only PDF, PNG and JPEG qualification files are accepted")
	}
	if len(content) < 8 {
		return "", "", errors.New("qualification file content is invalid")
	}
	detected := http.DetectContentType(content[:min(len(content), 512)])
	if !strings.HasPrefix(detected, want) {
		return "", "", fmt.Errorf("qualification file content does not match %s", ext)
	}
	return name, want, nil
}
