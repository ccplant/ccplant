package utils

import (
	"errors"
	"io"
	"net/http"
	"testing"
)

type trackedHTTPBody struct {
	closes int
	err    error
}

func (b *trackedHTTPBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *trackedHTTPBody) Close() error             { b.closes++; return b.err }

func TestSafeCloseResponseBodyCapturesOriginalDeferredBody(t *testing.T) {
	original, replacement := &trackedHTTPBody{}, &trackedHTTPBody{}
	func() {
		response := &http.Response{Body: original}
		defer SafeCloseResponseBody(response.Body)
		response.Body = replacement
	}()
	if original.closes != 1 || replacement.closes != 0 {
		t.Fatal("deferred cleanup must close the original body only")
	}
}

func TestSafeCloseResponseBodyHandlesNilAndCloseFailure(t *testing.T) {
	SafeCloseResponseBody(nil)
	body := &trackedHTTPBody{err: errors.New("test cleanup failure")}
	SafeCloseResponseBody(body)
	if body.closes != 1 {
		t.Fatalf("body closed %d times, want once", body.closes)
	}
}
