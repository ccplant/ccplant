package executorproxy

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	FrameStdin byte = iota
	FrameStdinEOF
	FrameSignal
	FrameStdout
	FrameStderr
	FrameExit
)

type Request struct {
	Args []string `json:"args"`
	Cwd  string   `json:"cwd"`
	Env  []string `json:"env"`
}

type Exit struct {
	Code   int    `json:"code"`
	Signal string `json:"signal,omitempty"`
	Error  string `json:"error,omitempty"`
}

func WriteJSON(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return WriteFrame(w, FrameStdin, data)
}

func ReadJSON(r io.Reader, value any) error {
	kind, data, err := ReadFrame(r)
	if err != nil {
		return err
	}
	if kind != FrameStdin {
		return fmt.Errorf("unexpected request frame %d", kind)
	}
	return json.Unmarshal(data, value)
}

func WriteFrame(w io.Writer, kind byte, data []byte) error {
	header := [5]byte{kind}
	binary.BigEndian.PutUint32(header[1:], uint32(len(data)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func ReadFrame(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[1:])
	if length > 16<<20 {
		return 0, nil, fmt.Errorf("executor frame too large: %d", length)
	}
	data := make([]byte, length)
	_, err := io.ReadFull(r, data)
	return header[0], data, err
}
