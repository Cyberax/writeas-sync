package main

import (
	"encoding/json"
	"fmt"
	"github.com/writeas/go-writeas/v2"
	"github.com/writeas/impart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GetCollectionPostsPaginated retrieves a collection's posts, returning the Posts
// and any error (in user-friendly form) that occurs. See
// https://developers.write.as/docs/api/#retrieve-collection-posts
// The page parameter is 1-based. Once the pages are exhausted, the returned slice will be empty.
func GetCollectionPostsPaginated(client *writeas.Client, alias string, page uint64) (*[]writeas.Post, error) {
	metaDataEditUrl := client.BaseURL() + fmt.Sprintf("/collections/%s/posts?page=%d", alias, page)

	req, err := http.NewRequest("GET", metaDataEditUrl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+client.Token())

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	status := response.StatusCode
	if status == http.StatusOK {
		coll := &writeas.Collection{}
		env := &impart.Envelope{
			Data: coll,
		}
		err = json.NewDecoder(response.Body).Decode(&env)
		if err != nil {
			return nil, err
		}

		return coll.Posts, nil
	} else if status == http.StatusNotFound {
		return nil, fmt.Errorf("Collection not found.")
	} else {
		return nil, fmt.Errorf("Problem getting collection: %d. %v\n", status, err)
	}
}

func SetPostCtime(client *writeas.Client, newPost writeas.Post, collAlias, title string, ctime time.Time) error {
	data := make(url.Values)
	data["slug"] = []string{newPost.Slug}
	data["title"] = []string{title}
	data["created"] = []string{ctime.Format("2006-01-02+15:04:05")}

	// The JSON API appears to be broken, it can't be used to set the post's mtime or ctime. So emulate the UI access.
	metaDataEditUrl := client.BaseURL() + "/collections/" + collAlias + "/posts/" + newPost.ID

	req, err := http.NewRequest("POST", metaDataEditUrl, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Token "+client.Token())

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusFound && response.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update the ctime for %s, status=%s", newPost.Slug, response.Status)
	}

	return nil
}
