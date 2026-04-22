package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
)

func DownloadFile(client *http.Client, serverURL string, relayID string, destPath string, expectedSHA256 string, onProgress func(done, total int64)) error {
	resp, err := client.Get(serverURL + "/api/files/" + relayID)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed: %s", string(data))
	}
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()
	hasher := sha256.New()
	buf := make([]byte, 256*1024)
	var done int64
	total := resp.ContentLength
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, writeErr := out.Write(chunk); writeErr != nil {
				return writeErr
			}
			_, _ = hasher.Write(chunk)
			done += int64(n)
			if onProgress != nil {
				onProgress(done, total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if expectedSHA256 != "" {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if actual != expectedSHA256 {
			return fmt.Errorf("sha256 mismatch")
		}
	}
	return nil
}
