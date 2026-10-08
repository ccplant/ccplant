package sessionsettings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateCommandWrapperTemplate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		source  string
		wantErr string
	}{
		{name: "empty"},
		{name: "valid", source: "exec env PROFILE=test {{ .Command }}"},
		{name: "multiline", source: "#!/bin/sh\nset -eu\nexec {{ .Command }}\n"},
		{name: "missing", source: "echo no-command", wantErr: "exactly one"},
		{name: "duplicate", source: "{{ .Command }}; {{ .Command }}", wantErr: "exactly one"},
		{name: "unknown field", source: "{{ .Environment }}", wantErr: "only supports"},
		{name: "conditional", source: "{{ if .Command }}{{ .Command }}{{ end }}", wantErr: "literal text"},
		{name: "pipeline", source: "{{ printf \"%s\" .Command }}", wantErr: "only supports"},
		{name: "nul", source: "{{ .Command }}\x00", wantErr: "NUL"},
		{name: "too large", source: "{{ .Command }}" + strings.Repeat("x", MaxCommandWrapperTemplateSize), wantErr: "exceeds"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateCommandWrapperTemplate(test.source)
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestRenderCommandWrapperShellQuotesArguments(t *testing.T) {
	t.Parallel()
	rendered, err := RenderCommandWrapper("exec env PROFILE=test {{ .Command }}", []string{"agent", "space value", "it's", "$(unsafe)", ""})
	require.NoError(t, err)
	require.Equal(t, `exec env PROFILE=test 'agent' 'space value' 'it'"'"'s' '$(unsafe)' ''`, rendered)
}
