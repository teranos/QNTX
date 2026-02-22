package ipfs

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/teranos/QNTX/errors"
)

const pinataURL = "https://api.pinata.cloud/pinning/pinFileToIPFS"

// PinResponse is the response from Pinata's pinFileToIPFS endpoint.
type PinResponse struct {
	IpfsHash    string `json:"IpfsHash"`
	PinSize     int    `json:"PinSize"`
	Timestamp   string `json:"Timestamp"`
	IsDuplicate bool   `json:"isDuplicate"`
}

// PinFile pins a file to IPFS via Pinata and returns the CID.
func PinFile(jwt string, filename string, content []byte) (*PinResponse, error) {
	if jwt == "" {
		return nil, errors.New("pinata.jwt not configured — set QNTX_PINATA_JWT or pinata.jwt in am.toml")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create multipart file field")
	}
	if _, err := part.Write(content); err != nil {
		return nil, errors.Wrap(err, "failed to write content to multipart form")
	}

	metadata, _ := json.Marshal(map[string]any{
		"name": filename,
	})
	_ = writer.WriteField("pinataMetadata", string(metadata))

	options, _ := json.Marshal(map[string]any{
		"cidVersion": 1,
	})
	_ = writer.WriteField("pinataOptions", string(options))

	if err := writer.Close(); err != nil {
		return nil, errors.Wrap(err, "failed to close multipart writer")
	}

	req, err := http.NewRequest(http.MethodPost, pinataURL, body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create Pinata HTTP request")
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "Pinata pin request failed")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read Pinata response")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("Pinata returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var pinResp PinResponse
	if err := json.Unmarshal(respBody, &pinResp); err != nil {
		return nil, errors.Wrapf(err, "failed to parse Pinata response: %s", string(respBody))
	}

	return &pinResp, nil
}
