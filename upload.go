package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/snapas/go-snapas"
	"github.com/writeas/impart"
	"io"
	"mime/multipart"
	"net/http"
	"os"
)

// UploadPhoto uploads a photo, and returns a Snap.as Photo. See:
// https://developers.snap.as/docs/api/#upload-a-photo
func UploadPhoto(client *snapas.Client, fileName, fileTag string) (*snapas.Photo, error) {
	f, err := os.Open(fileName)
	if err != nil {
		return nil, fmt.Errorf("open file: %s", err)
	}
	defer func() { _ = f.Close() }()

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile("file", fileTag)
	if err != nil {
		return nil, fmt.Errorf("create form file: %s", err)
	}

	_, err = io.Copy(part, f)
	if err != nil {
		return nil, fmt.Errorf("copy file: %s", err)
	}
	err = w.Close()
	if err != nil {
		return nil, fmt.Errorf("close writer: %s", err)
	}

	url := fmt.Sprintf("%s%s", client.Config.BaseURL, "/photos/upload")
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %s", err)
	}
	req.Header.Add("User-Agent", client.Config.UserAgent)
	req.Header.Add("Content-Type", w.FormDataContentType())
	req.Header.Add("Authorization", client.Token)

	resp, err := client.Config.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %s", err)
	}
	defer func() { _ = resp.Body.Close() }()

	env := &impart.Envelope{
		Code: resp.StatusCode,
		Data: &snapas.Photo{},
	}
	err = json.NewDecoder(resp.Body).Decode(&env)
	if err != nil {
		return nil, err
	}
	if env.Code != http.StatusCreated {
		return nil, fmt.Errorf("%s", env.ErrorMessage)
	}

	return env.Data.(*snapas.Photo), nil
}
