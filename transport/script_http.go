package transport

import (
	"bytes"
	"io"
	"log"
	"net/http"

	"github.com/PuerkitoBio/goquery"
)

type httpclient struct {
	obj *Interpreter
}

func (o *httpclient) Get(url string) string {
	log.Printf("JS.http.Get: %s %s", o.obj.user.Username, url)
	client := http.Client{
		//Timeout: time.Duration(config.Config.Timeouts.HTTPTimeout) * time.Second,
	}
	request, _ := http.NewRequest("GET", url, nil)
	request.Header.Set("User-Agent", "upload-server/2.0")
	response, err := client.Do(request)
	if err != nil {
		log.Printf("JS.http.Get: %s %s: The HTTP request failed with error %s", o.obj.user.Username, url, err.Error())
		return ""
	}
	data, _ := io.ReadAll(response.Body)
	return string(data)
}

func (o *httpclient) GetWithBasicAuth(url string, username string, password string) string {
	log.Printf("JS.http.GetWithBasicAuth: %s %s", o.obj.user.Username, url)
	client := http.Client{
		//Timeout: time.Duration(config.Config.Timeouts.HTTPTimeout) * time.Second,
	}
	request, _ := http.NewRequest("GET", url, nil)
	request.Header.Set("User-Agent", "remitt/0.9")
	request.SetBasicAuth(username, password)
	response, err := client.Do(request)
	if err != nil {
		log.Printf("JS.http.GetWithBasicAuth: %s %s: The HTTP request failed with error %s", o.obj.user.Username, url, err.Error())
		return ""
	}
	data, _ := io.ReadAll(response.Body)
	return string(data)
}

func (o *httpclient) GoQuery(body []byte) *goquery.Document {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		log.Printf("JS.http.GoQuery: %s: The HTTP request failed with error %s", o.obj.user.Username, err.Error())
		return nil
	}
	return doc
}
