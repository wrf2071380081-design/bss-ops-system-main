package supply

import (
	"strings"
	"unicode/utf8"
)

// validateWriteMetaStorage mirrors operation_log varchar limits in characters,
// rejects PostgreSQL-incompatible text, and never truncates audit evidence.
func validateWriteMetaStorage(meta WriteMeta) error {
	for _, field := range []struct {
		name, value string
		limit       int
	}{
		{"request_id", meta.RequestID, 64}, {"reason", meta.Reason, 500},
	} {
		if strings.TrimSpace(field.value) == "" || !utf8.ValidString(field.value) || strings.ContainsRune(field.value, 0) || utf8.RuneCountInString(field.value) > field.limit {
			return invalid(field.name, "required UTF-8 text exceeds public log storage contract or contains NUL")
		}
	}
	return nil
}
