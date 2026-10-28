package supply

import "context"

const F12AttachmentMaxFileBytes = 2 * 1024 * 1024

type F12AttachmentMetadata struct {
	ID          string `json:"file_id"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Purpose     string `json:"purpose"`
	Name        string `json:"file_name"`
	ContentType string `json:"content_type"`
	UploadedBy  string `json:"uploaded_by"`
	Size        int    `json:"size"`
}

type F12AttachmentContent struct {
	F12AttachmentMetadata
	Content []byte `json:"-"`
}

// F12AttachmentChain identifies the immutable record chain that may reuse an
// already-linked attachment. Empty ContractKey means a new contract chain.
type F12AttachmentChain struct {
	Kind      string
	ContractKey string
	Direction string
	Currency  string
}

type F12AttachmentUploadCommand struct {
	SubjectType     string
	SubjectID       string
	Purpose         string
	SourceFlowID    string
	SourceModuleKey string
	ExpectedFlowRevision uint64
	FileName        string
	Content         []byte
}

// F12AttachmentBlobStore is the private byte store. Subject ownership and
// purpose metadata are immutable and verified on every use.
type F12AttachmentBlobStore interface {
	Save(context.Context, string, string, string, string, string, []byte) (*F12AttachmentContent, error)
	Open(context.Context, string) (*F12AttachmentContent, error)
	Delete(context.Context, string) error
	Verify(context.Context, string, string, string, string, string) error
}

type F12AttachmentService interface {
	Upload(context.Context, WriteMeta, F12AttachmentUploadCommand) (*F12AttachmentMetadata, error)
	Download(context.Context, string, string, string, string, string) (*F12AttachmentContent, error)
}
