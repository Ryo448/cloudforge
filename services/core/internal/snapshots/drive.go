package snapshots

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const driveFields = "id,name,mimeType,parents,createdTime,size,md5Checksum,sha256Checksum,appProperties,trashed"
const folderMIME = "application/vnd.google-apps.folder"

type driveFile struct {
	ID      string            `json:"id,omitempty"`
	Name    string            `json:"name"`
	MIME    string            `json:"mimeType,omitempty"`
	Parents []string          `json:"parents,omitempty"`
	Created time.Time         `json:"createdTime,omitempty"`
	Size    int64             `json:"size,string,omitempty"`
	MD5     string            `json:"md5Checksum,omitempty"`
	SHA256  string            `json:"sha256Checksum,omitempty"`
	Props   map[string]string `json:"appProperties"`
	Trashed bool              `json:"trashed,omitempty"`
}

var driveID = regexp.MustCompile("^[A-Za-z0-9_-]{1,150}$")
var snapshotName = regexp.MustCompile("^snapshot_[0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}-[0-9]{2}\\.(dump|sql\\.gz|archive\\.gz|rdb)$")

func (d *drive) request(ctx context.Context, id, method, address string, body []byte, headers map[string]string) (*http.Response, error) {
	var rejected string
	for attempt := 0; attempt < 2; attempt++ {
		token, err := d.accessToken(ctx, id, rejected)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
		if err != nil {
			return nil, errDriveNetwork
		}
		req.Header.Set("Authorization", "Bearer "+token)
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, err := d.client.Do(req)
		if err != nil {
			return nil, errDriveNetwork
		}
		if resp.StatusCode != 401 || attempt == 1 {
			return resp, nil
		}
		_ = resp.Body.Close()
		rejected = token
	}
	return nil, errRevoked
}

func decodeDrive(resp *http.Response, out any) error {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return googleHTTPError(resp)
	}
	if out != nil && json.NewDecoder(io.LimitReader(resp.Body, 16*1024*1024)).Decode(out) != nil {
		return errors.New("o Google Drive retornou metadados inválidos")
	}
	return nil
}

func qvalue(s string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(s) + "'"
}

func property(key, value string) string {
	return "appProperties has { key=" + qvalue(key) + " and value=" + qvalue(value) + " }"
}

func (d *drive) list(ctx context.Context, accountID, query string) ([]driveFile, error) {
	var result []driveFile
	token := ""
	for {
		pageCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		params := url.Values{"q": {"trashed = false and " + query}, "fields": {"nextPageToken,files(" + driveFields + ")"},
			"spaces": {"drive"}, "pageSize": {"1000"}, "pageToken": {token}}
		resp, err := d.request(pageCtx, accountID, "GET", d.api+"/files?"+params.Encode(), nil, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		var page struct {
			Files []driveFile `json:"files"`
			Next  string      `json:"nextPageToken"`
		}
		err = decodeDrive(resp, &page)
		cancel()
		if err != nil {
			return nil, err
		}
		result = append(result, page.Files...)
		if page.Next == "" {
			return result, nil
		}
		if page.Next == token {
			return nil, errors.New("paginação inválida do Google Drive")
		}
		token = page.Next
	}
}

func (d *drive) get(ctx context.Context, accountID, id string) (driveFile, bool, error) {
	var item driveFile
	if !driveID.MatchString(id) {
		return item, false, errors.New("identificador do Drive inválido")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := d.request(ctx, accountID, "GET", d.api+"/files/"+id+"?fields="+driveFields, nil, nil)
	if err != nil {
		return item, false, err
	}
	if resp.StatusCode == 404 {
		_ = resp.Body.Close()
		return item, false, nil
	}
	err = decodeDrive(resp, &item)
	return item, err == nil && !item.Trashed, err
}

func (d *drive) createFolder(ctx context.Context, accountID, name, parent string, props map[string]string) (driveFile, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body := map[string]any{"name": name, "mimeType": folderMIME, "appProperties": props}
	if parent != "" {
		body["parents"] = []string{parent}
	}
	data, _ := json.Marshal(body)
	resp, err := d.request(ctx, accountID, "POST", d.api+"/files?fields="+driveFields, data, map[string]string{"Content-Type": "application/json"})
	var item driveFile
	if err == nil {
		err = decodeDrive(resp, &item)
	}
	return item, err
}

func (d *drive) delete(ctx context.Context, accountID, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := d.request(ctx, accountID, "DELETE", d.api+"/files/"+id, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == 404 {
		_ = resp.Body.Close()
		return nil
	}
	return decodeDrive(resp, nil)
}

type digest struct {
	Size   int64
	SHA256 string
	MD5    string
}

func fingerprint(r io.Reader) (digest, error) {
	sha, md := sha256.New(), md5.New()
	size, err := io.Copy(io.MultiWriter(sha, md), r)
	return digest{size, hex.EncodeToString(sha.Sum(nil)), hex.EncodeToString(md.Sum(nil))}, err
}

func (f driveFile) verified() bool {
	return f.Size > 0 && len(f.Props["sha256"]) == 64 && len(f.Props["md5"]) == 32 &&
		f.MD5 == f.Props["md5"] && (f.SHA256 == "" || f.SHA256 == f.Props["sha256"])
}

func (d *drive) prepareUpload(ctx context.Context, accountID string, metadata map[string]any, size int64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, _ := json.Marshal(metadata)
	head := map[string]string{
		"Content-Type": "application/json; charset=UTF-8", "X-Upload-Content-Type": "application/octet-stream",
		"X-Upload-Content-Length": strconv.FormatInt(size, 10),
	}
	resp, err := d.request(ctx, accountID, "POST", d.uploadAPI+"/files?uploadType=resumable&fields="+driveFields, data, head)
	if err != nil {
		return "", err
	}
	location, _ := url.Parse(resp.Header.Get("Location"))
	err = decodeDrive(resp, nil)
	if err != nil {
		return "", err
	}
	if location == nil || location.User != nil || location.Host == "" {
		return "", errors.New("sessão de upload inválida")
	}
	if strings.HasPrefix(d.uploadAPI, "https://") && (location.Scheme != "https" ||
		(location.Host != "www.googleapis.com" && location.Host != "drive.googleapis.com" && location.Host != "content.googleapis.com")) {
		return "", errors.New("destino de upload inválido")
	}
	return location.String(), nil
}

var rangeHeader = regexp.MustCompile("^bytes=0-([0-9]+)$")
var errUploadInterrupted = errors.New("upload interrompido. Tente realizar o snapshot novamente")
var errUploadRetry = errors.New("envio temporariamente indisponível")

func (d *drive) uploadPart(ctx context.Context, accountID, session, contentRange string, data []byte, total int64) (driveFile, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resp, err := d.request(ctx, accountID, "PUT", session, data, map[string]string{
		"Content-Type": "application/octet-stream", "Content-Range": contentRange,
	})
	if err != nil {
		if errors.Is(err, errDriveNetwork) {
			err = errUploadRetry
		}
		return driveFile{}, 0, err
	}
	switch {
	case resp.StatusCode == 308:
		defer resp.Body.Close()
		raw := resp.Header.Get("Range")
		if raw == "" {
			return driveFile{}, 0, nil
		}
		match := rangeHeader.FindStringSubmatch(raw)
		if match == nil {
			return driveFile{}, 0, errors.New("o Google Drive retornou um intervalo inválido")
		}
		end, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || end >= total {
			return driveFile{}, 0, errors.New("o Google Drive retornou um intervalo inválido")
		}
		return driveFile{}, end + 1, nil
	case resp.StatusCode == 429 || resp.StatusCode >= 500:
		_ = resp.Body.Close()
		return driveFile{}, 0, errUploadRetry
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		_ = resp.Body.Close()
		return driveFile{}, 0, errUploadInterrupted
	default:
		var file driveFile
		err = decodeDrive(resp, &file)
		return file, total, err
	}
}

func (d *drive) upload(ctx context.Context, accountID string, file *os.File, size int64, metadata map[string]any, progress func(int)) (driveFile, error) {
	session, err := d.prepareUpload(ctx, accountID, metadata, size)
	if err != nil {
		return driveFile{}, err
	}
	const chunk = 8 * 1024 * 1024
	buf := make([]byte, chunk)
	var offset int64
	retries := 0
	for offset < size {
		n := min(int64(len(buf)), size-offset)
		if _, err := file.ReadAt(buf[:n], offset); err != nil {
			return driveFile{}, err
		}
		item, next, err := d.uploadPart(ctx, accountID, session, fmt.Sprintf("bytes %d-%d/%d", offset, offset+n-1, size), buf[:n], size)
		for errors.Is(err, errUploadRetry) {
			retries++
			if retries > 5 {
				return driveFile{}, errUploadInterrupted
			}
			select {
			case <-ctx.Done():
				return driveFile{}, ctx.Err()
			case <-time.After(time.Duration(1<<uint(retries-1)) * retryDelay):
			}
			item, next, err = d.uploadPart(ctx, accountID, session, fmt.Sprintf("bytes */%d", size), nil, size)
		}
		if err != nil {
			return driveFile{}, err
		}
		if item.ID != "" {
			progress(100)
			return item, nil
		}
		if next <= offset {
			retries++
			if retries > 5 {
				return driveFile{}, errUploadInterrupted
			}
			offset = next
			continue
		}
		retries, offset = 0, next
		progress(int(float64(offset) * 100 / float64(size)))
	}
	return driveFile{}, errors.New("o Google Drive não confirmou a conclusão do upload")
}

const retryDelay = time.Second

func (d *drive) download(ctx context.Context, accountID string, item driveFile, out io.Writer, progress func(int)) error {
	resp, err := d.request(ctx, accountID, "GET", d.api+"/files/"+item.ID+"?alt=media", nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return googleHTTPError(resp)
	}
	sha, md := sha256.New(), md5.New()
	writer := io.MultiWriter(out, sha, md)
	buffer := make([]byte, 256*1024)
	var size int64
	for {
		n, err := resp.Body.Read(buffer)
		if size+int64(n) > item.Size {
			return errors.New("o download excedeu o tamanho registrado")
		}
		if n > 0 {
			if _, writeErr := writer.Write(buffer[:n]); writeErr != nil {
				return errors.New("falha ao gravar o arquivo de destino")
			}
			size += int64(n)
			progress(int(float64(size) * 100 / float64(item.Size)))
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("download interrompido")
		}
	}
	if size != item.Size || hex.EncodeToString(sha.Sum(nil)) != item.Props["sha256"] || hex.EncodeToString(md.Sum(nil)) != item.MD5 {
		return errors.New("o arquivo baixado falhou na verificação de integridade")
	}
	return nil
}
