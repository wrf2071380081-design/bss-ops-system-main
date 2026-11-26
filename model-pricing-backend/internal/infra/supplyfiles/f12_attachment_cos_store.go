package supplyfiles

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"model_bss/internal/domain/supply"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// F12AttachmentCOSStore 把合同/授信附件放腾讯云 COS，语义与本地文件实现完全一致：
// 同一个 f12:v1:<32hex> 引用、同样的 <key>.blob / <key>.json 对象名、同样的校验。
// 这样从 fs 迁到 cos 时，数据库里的 file_id 不需要改，迁移脚本只要按 key 上传对象即可。
type F12AttachmentCOSStore struct {
	client *cos.Client
	bucket string
}

// NewF12AttachmentCOSStore 创建 COS 存储。bucket 形如 cos-bucket-asset-bss-1254396208。
func NewF12AttachmentCOSStore(secretID, secretKey, region, bucket string) (*F12AttachmentCOSStore, error) {
	if strings.TrimSpace(secretID) == "" || strings.TrimSpace(secretKey) == "" ||
		strings.TrimSpace(region) == "" || strings.TrimSpace(bucket) == "" {
		return nil, errors.New("F12 COS storage requires secret id, secret key, region and bucket")
	}
	bucketURL, err := url.Parse("https://" + strings.TrimSpace(bucket) + ".cos." + strings.TrimSpace(region) + ".myqcloud.com")
	if err != nil {
		return nil, err
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{SecretID: strings.TrimSpace(secretID), SecretKey: strings.TrimSpace(secretKey)},
	})
	return &F12AttachmentCOSStore{client: client, bucket: strings.TrimSpace(bucket)}, nil
}

func (s *F12AttachmentCOSStore) Save(ctx context.Context, subjectType, subjectID, purpose, uploadedBy, name string, content []byte) (*supply.F12AttachmentContent, error) {
	if s == nil || s.client == nil {
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
	// 先写 blob 再写清单：清单不存在时 Open 会判 ReadContract，不会读到半截内容。
	if _, err := s.client.Object.Put(ctx, key+".blob", strings.NewReader(string(content)), nil); err != nil {
		return nil, err
	}
	if _, err := s.client.Object.Put(ctx, key+".json", strings.NewReader(string(encoded)), nil); err != nil {
		_, _ = s.client.Object.Delete(ctx, key+".blob", nil)
		return nil, err
	}
	return &supply.F12AttachmentContent{F12AttachmentMetadata: f12ManifestMetadata(manifest), Content: append([]byte(nil), content...)}, nil
}

func (s *F12AttachmentCOSStore) Open(ctx context.Context, ref string) (*supply.F12AttachmentContent, error) {
	if s == nil || s.client == nil {
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
	blob, err := s.getObject(ctx, key+".blob", supply.F12AttachmentMaxFileBytes)
	if err != nil {
		return nil, supply.ErrNotFound
	}
	manifestBytes, err := s.getObject(ctx, key+".json", 4096)
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

func (s *F12AttachmentCOSStore) Delete(ctx context.Context, ref string) error {
	if s == nil || s.client == nil {
		return supply.ErrDependencyUnavailable
	}
	match := f12AttachmentID.FindStringSubmatch(ref)
	if len(match) != 2 {
		return supply.ErrNotFound
	}
	key := match[1]
	for _, suffix := range []string{".blob", ".json"} {
		if _, err := s.client.Object.Delete(ctx, key+suffix, nil); err != nil && !isCOSNotFound(err) {
			return err
		}
	}
	return nil
}

// Verify 校验这条引用确实属于该主体/用途/上传人，供审批链核对，不返回内容。
func (s *F12AttachmentCOSStore) Verify(ctx context.Context, ref, subjectType, subjectID, purpose, uploadedBy string) error {
	if s == nil || s.client == nil {
		return supply.ErrDependencyUnavailable
	}
	match := f12AttachmentID.FindStringSubmatch(ref)
	if len(match) != 2 {
		return supply.ErrNotFound
	}
	raw, err := s.getObject(ctx, match[1]+".json", 4096)
	if err != nil {
		return supply.ErrNotFound
	}
	var manifest f12AttachmentManifest
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.ID != ref {
		return supply.ErrReadContract
	}
	if manifest.SubjectType != subjectType || manifest.SubjectID != subjectID || manifest.Purpose != purpose || manifest.UploadedBy != uploadedBy {
		return supply.ErrReadContract
	}
	return nil
}

func (s *F12AttachmentCOSStore) getObject(ctx context.Context, key string, maxBytes int64) ([]byte, error) {
	resp, err := s.client.Object.Get(ctx, key, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	content := make([]byte, 0, 4096)
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > maxBytes {
				return nil, errors.New("stored object exceeds size limit")
			}
			content = append(content, buf[:n]...)
		}
		if readErr != nil {
			if errors.Is(readErr, http.ErrBodyReadAfterClose) || readErr.Error() == "EOF" {
				break
			}
			if strings.Contains(readErr.Error(), "EOF") {
				break
			}
			return nil, readErr
		}
	}
	return content, nil
}

func isCOSNotFound(err error) bool {
	var cosErr *cos.ErrorResponse
	if errors.As(err, &cosErr) {
		return cosErr.Response != nil && cosErr.Response.StatusCode == http.StatusNotFound
	}
	return false
}
