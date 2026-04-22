package transfer

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

func UploadFile(client *http.Client, serverURL string, path string, onProgress func(sent, total int64)) (*UploadResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		defer file.Close()
		defer pw.Close()
		defer writer.Close()

		part, err := writer.CreateFormFile("file", filepath.Base(path))
		if err != nil {
			errCh <- err
			_ = pw.CloseWithError(err)
			return
		}
		reader := &progressReader{reader: file, total: info.Size(), onProgress: onProgress}
		if _, err := io.Copy(part, reader); err != nil {
			errCh <- err
			_ = pw.CloseWithError(err)
			return
		}
		errCh <- nil
	}()

	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/files/upload", pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := <-errCh; err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upload failed: %s", string(data))
	}
	var result UploadResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

type progressReader struct {
	reader     io.Reader
	total      int64
	sent       int64
	onProgress func(sent, total int64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.sent += int64(n)
		if r.onProgress != nil {
			r.onProgress(r.sent, r.total)
		}
	}
	return n, err
}
