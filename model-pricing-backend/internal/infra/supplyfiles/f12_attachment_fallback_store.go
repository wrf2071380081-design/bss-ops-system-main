package supplyfiles

import (
	"context"
	"errors"

	"model_bss/internal/domain/supply"
)

// F12AttachmentFallbackStore 让"新写入走对象存储、老文件仍可从本地读"成为可能。
//
// 背景（2026-11-26）：切到 COS 时，历史附件还在本地目录里（f12:v1:<key> 引用不变）。
// 若直接换实现，老文件会 404。这里 Save/Delete 走主存储，Open/Verify 先主后备，
// 于是切换可以零停机，迁移可以慢慢做。
type F12AttachmentFallbackStore struct {
	Primary  supply.F12AttachmentBlobStore
	Fallback supply.F12AttachmentBlobStore
}

func (s *F12AttachmentFallbackStore) Save(ctx context.Context, subjectType, subjectID, purpose, uploadedBy, name string, content []byte) (*supply.F12AttachmentContent, error) {
	if s == nil || s.Primary == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	return s.Primary.Save(ctx, subjectType, subjectID, purpose, uploadedBy, name, content)
}

func (s *F12AttachmentFallbackStore) Open(ctx context.Context, ref string) (*supply.F12AttachmentContent, error) {
	if s == nil || s.Primary == nil {
		return nil, supply.ErrDependencyUnavailable
	}
	content, err := s.Primary.Open(ctx, ref)
	if err == nil || s.Fallback == nil || !isNotFound(err) {
		return content, err
	}
	return s.Fallback.Open(ctx, ref)
}

func (s *F12AttachmentFallbackStore) Delete(ctx context.Context, ref string) error {
	if s == nil || s.Primary == nil {
		return supply.ErrDependencyUnavailable
	}
	primaryErr := s.Primary.Delete(ctx, ref)
	if s.Fallback != nil {
		// 两边都删：历史上可能只在本地存在，也可能已迁到 COS。
		_ = s.Fallback.Delete(ctx, ref)
	}
	if primaryErr != nil && !isNotFound(primaryErr) {
		return primaryErr
	}
	return nil
}

func (s *F12AttachmentFallbackStore) Verify(ctx context.Context, ref, subjectType, subjectID, purpose, uploadedBy string) error {
	if s == nil || s.Primary == nil {
		return supply.ErrDependencyUnavailable
	}
	err := s.Primary.Verify(ctx, ref, subjectType, subjectID, purpose, uploadedBy)
	if err == nil || s.Fallback == nil || !isNotFound(err) {
		return err
	}
	return s.Fallback.Verify(ctx, ref, subjectType, subjectID, purpose, uploadedBy)
}

func isNotFound(err error) bool {
	return errors.Is(err, supply.ErrNotFound)
}
