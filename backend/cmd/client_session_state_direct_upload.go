package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionstate"
	closeutils "github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

func uploadSessionStateDirect(ctx context.Context, request directUploadRequest) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	go func() {
		if err := writer.CloseWithError(sessionstate.Pack(writer, request.agentType, request.acpID, request.home, request.cwd)); err != nil {
			return
		}
	}()
	jobs := make(chan directUploadJob, 2)
	var wg sync.WaitGroup
	progress := &directUploadProgress{}
	for range 2 {
		wg.Add(1)
		worker := directUploadWorker{ctx: ctx, request: request, jobs: jobs, progress: progress, cancel: cancel, wg: &wg}
		go runDirectUploadWorker(worker)
	}
	if err := queueDirectUploadJobs(ctx, reader, request.upload.PartSize, jobs); err != nil {
		progress.recordPart(0, "", err, cancel)
	}
	close(jobs)
	wg.Wait()
	parts, err := progress.result()
	if err != nil {
		return abortFailedDirectUpload(request, err)
	}
	if len(parts) == 0 {
		return abortFailedDirectUpload(request, fmt.Errorf("empty snapshot"))
	}
	return completeDirectUpload(request, parts)
}

func runDirectUploadWorker(worker directUploadWorker) {
	defer worker.wg.Done()
	for job := range worker.jobs {
		if worker.ctx.Err() != nil {
			continue
		}
		request := worker.request
		part := directPartRequest{client: request.client, proxy: request.proxy, token: request.token, sessionID: request.sessionID, uploadID: request.upload.UploadID, number: job.number, data: job.data}
		etag, err := uploadDirectPart(worker.ctx, part)
		worker.progress.recordPart(job.number, etag, err, worker.cancel)
	}
}

func queueDirectUploadJobs(ctx context.Context, reader io.Reader, partSize int64, jobs chan<- directUploadJob) error {
	for number := int32(1); ; number++ {
		buf := make([]byte, partSize)
		n, err := io.ReadFull(reader, buf)
		if n > 0 {
			select {
			case jobs <- directUploadJob{number: number, data: buf[:n]}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func abortFailedDirectUpload(request directUploadRequest, cause error) error {
	abortDirectUpload(request.client, request.proxy, request.token, request.sessionID, request.upload.UploadID)
	return cause
}

func completeDirectUpload(request directUploadRequest, parts []uploadedPart) error {
	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	payload, err := json.Marshal(map[string]any{"parts": parts})
	if err != nil {
		return fmt.Errorf("encode completed upload: %w", err)
	}
	req, err := internalRequest(http.MethodPost, request.proxy+"/internal/session-state/"+request.sessionID+"/uploads/"+request.upload.UploadID+"/complete", request.token, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := request.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { closeutils.SafeCloseResponseBody(resp.Body) }()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("complete upload HTTP %d", resp.StatusCode)
	}
	return nil
}

func uploadDirectPart(ctx context.Context, part directPartRequest) (string, error) {
	req, err := internalRequest(http.MethodGet, part.proxy+"/internal/session-state/"+part.sessionID+"/uploads/"+part.uploadID+"/parts/"+strconv.Itoa(int(part.number)), part.token, nil)
	if err != nil {
		return "", err
	}
	resp, err := part.client.Do(req)
	if err != nil {
		return "", err
	}
	var signed struct {
		URL string `json:"url"`
	}
	if resp.StatusCode == http.StatusOK {
		err = json.NewDecoder(resp.Body).Decode(&signed)
	} else {
		err = fmt.Errorf("presign part HTTP %d", resp.StatusCode)
	}
	closeutils.SafeCloseResponseBody(resp.Body)
	if err != nil {
		return "", err
	}
	put, err := http.NewRequestWithContext(ctx, http.MethodPut, signed.URL, bytes.NewReader(part.data))
	if err != nil {
		return "", err
	}
	put.ContentLength = int64(len(part.data))
	result, err := objectStorageClient(signed.URL, part.client.Timeout).Do(put)
	if err != nil {
		return "", err
	}
	defer func() { closeutils.SafeCloseResponseBody(result.Body) }()
	if result.StatusCode/100 != 2 {
		return "", fmt.Errorf("upload part HTTP %d", result.StatusCode)
	}
	return result.Header.Get("ETag"), nil
}
