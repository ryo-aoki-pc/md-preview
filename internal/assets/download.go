package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

func readBoundedFile(name string, max int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBounded(f, max, "公式資産の大きさが上限を超えています。")
}
func readBounded(reader io.Reader, max int64, message string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New(message)
	}
	return data, nil
}

type downloader struct{ client *http.Client }

func newDownloader(client *http.Client) *downloader {
	if client == nil {
		client = http.DefaultClient
	}
	copy := *client
	previous := copy.CheckRedirect
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := validateURL(req.URL.String(), "取得元URLはHTTPSが必要です。"); err != nil {
			return err
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("取得元の転送が多すぎます。")
		}
		return nil
	}
	return &downloader{&copy}
}
func (d *downloader) response(ctx context.Context, url string, headers http.Header) (*http.Response, error) {
	if err := validateURL(url, "取得元URLはHTTPSが必要です。"); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("取得元URLが不正です。")
	}
	req.Header = headers.Clone()
	res, err := d.client.Do(req)
	if err != nil {
		return nil, errors.New("取得に失敗しました。")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		return nil, fmt.Errorf("取得に失敗しました（HTTP %d）。", res.StatusCode)
	}
	return res, nil
}
func (d *downloader) small(ctx context.Context, r Record) ([]byte, error) {
	res, err := d.response(ctx, r.URL, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := readBounded(res.Body, min(r.Size, 16*1024*1024), "取得した原文が固定サイズを超えています。")
	if err != nil {
		return nil, err
	}
	if err := checkedBytes(data, r.SHA256, r.Size, "原文"); err != nil {
		return nil, err
	}
	return data, nil
}
func (d *downloader) metadata(ctx context.Context, url string, headers http.Header) ([]byte, error) {
	res, err := d.response(ctx, url, headers)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return readBounded(res.Body, 16*1024*1024, "公式レジストリの応答が大きすぎます。")
}

func (d *downloader) registryLayer(ctx context.Context, a Archive) (*http.Response, error) {
	// 公開 pull 専用の短命な応答だけを使う。環境変数や認証ファイルの資格情報は読まない。
	auth, err := d.metadata(ctx, "https://gitlab.com/jwt/auth?service=container_registry&scope=repository:"+a.Repository+":pull", nil)
	if err != nil {
		return nil, err
	}
	var session struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(auth, &session) != nil || session.Token == "" {
		return nil, errors.New("公開レジストリの応答が不正です。")
	}
	headers := http.Header{"Authorization": []string{"Bearer " + session.Token}, "Accept": []string{"application/vnd.oci.image.index.v1+json"}}
	base := a.Registry + "/v2/" + a.Repository
	indexData, err := d.metadata(ctx, base+"/manifests/sha256:"+a.IndexSHA256, headers)
	if err != nil {
		return nil, err
	}
	if digest(indexData) != a.IndexSHA256 {
		return nil, errors.New("公式indexのSHA256が一致しません。")
	}
	var index struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if json.Unmarshal(indexData, &index) != nil {
		return nil, errors.New("公式indexの応答が不正です。")
	}
	matched := false
	for _, entry := range index.Manifests {
		if entry.Digest == "sha256:"+a.ManifestSHA256 && entry.Platform.OS == "linux" && entry.Platform.Architecture == "amd64" {
			matched = true
		}
	}
	if !matched {
		return nil, errors.New("公式indexの構成が固定値と異なります。")
	}
	headers.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	data, err := d.metadata(ctx, base+"/manifests/sha256:"+a.ManifestSHA256, headers)
	if err != nil {
		return nil, err
	}
	if digest(data) != a.ManifestSHA256 {
		return nil, errors.New("公式manifestのSHA256が一致しません。")
	}
	var manifest struct {
		Layers []struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
			Size      int64  `json:"size"`
		} `json:"layers"`
	}
	if json.Unmarshal(data, &manifest) != nil || len(manifest.Layers) != 1 || manifest.Layers[0].MediaType != "application/vnd.oci.image.layer.v1.tar+gzip" || manifest.Layers[0].Digest != "sha256:"+a.SHA256 || manifest.Layers[0].Size != a.Size {
		return nil, errors.New("公式manifestのレイヤーが固定値と異なります。")
	}
	return d.response(ctx, base+"/blobs/sha256:"+a.SHA256, headers)
}

func (d *downloader) archive(ctx context.Context, a Archive, target string) error {
	var res *http.Response
	var err error
	if a.Registry != "" {
		res, err = d.registryLayer(ctx, a)
	} else {
		res, err = d.response(ctx, a.URL, nil)
	}
	if err != nil {
		return err
	}
	defer res.Body.Close()
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(res.Body, a.Size+1))
	closeErr := f.Close()
	if size > a.Size {
		return errors.New("資産アーカイブが固定サイズを超えています。")
	}
	if copyErr != nil {
		return errors.New("資産アーカイブの取得が途中で終わりました。")
	}
	if closeErr != nil {
		return closeErr
	}
	if size != a.Size || hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
		return errors.New("資産アーカイブのサイズまたはSHA256が一致しません。")
	}
	return ctx.Err()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type expansionReader struct {
	reader   io.Reader
	consumed int64
}

func (r *expansionReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.consumed += int64(n)
	if r.consumed > 16*1024*1024*1024 {
		return n, errors.New("展開サイズの上限を超えています。")
	}
	return n, err
}

func writeNew(name string, data []byte) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
