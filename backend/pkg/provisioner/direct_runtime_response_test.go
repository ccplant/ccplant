package provisioner

import (
	"io"
	"strings"
	"testing"
)

type shortRuntimeResponseReader struct{ reader io.Reader }

func (r shortRuntimeResponseReader) Read(buffer []byte) (int, error) {
	if len(buffer) > 2 {
		buffer = buffer[:2]
	}
	return r.reader.Read(buffer)
}

func TestReadDirectRuntimeFrameFillsOrdinaryResponsesButNotStreams(t *testing.T) {
	for _, stream := range []bool{false, true} {
		body := shortRuntimeResponseReader{reader: strings.NewReader("abcdef")}
		buffer := make([]byte, 8)
		n, err := readDirectRuntimeFrame(body, buffer, stream)
		if stream {
			if n != 2 || err != nil || string(buffer[:n]) != "ab" {
				t.Fatalf("stream read = %q, %v; want immediate chunk", buffer[:n], err)
			}
		} else if n != 6 || err != io.EOF || string(buffer[:n]) != "abcdef" {
			t.Fatalf("ordinary read = %q, %v; want complete body and EOF", buffer[:n], err)
		}
	}
}

func TestDirectRuntimeFrameBatchCopiesReusedReadBuffer(t *testing.T) {
	batch := &directRuntimeFrameBatch{}
	buffer := []byte("first")
	batch.appendBody(buffer)
	buffer[0] = 'X'
	batch.appendBody(buffer)
	if string(batch.frames[0].Body) != "first" || string(batch.frames[1].Body) != "Xirst" {
		t.Fatalf("frame bodies must be independent copies: %v", batch.frames)
	}
	if batch.frames[0].Sequence != 1 || batch.frames[1].Sequence != 2 || batch.bytes != 10 {
		t.Fatalf("unexpected sequence or byte count: %+v", batch)
	}
}
