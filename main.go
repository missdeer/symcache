package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	cubeSymbolServer = "http://172.16.206.19:8080"
	msSymbolServer   = "https://msdl.microsoft.com/download/symbols"
	socketBufferSize = 1024 * 16
)

var (
	cacheDir = "./cache"
)

func requestHead(requestUrl string) (int, error) {
	resp, err := http.Head(requestUrl)
	if err != nil {
		return http.StatusNotFound, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func requestBody(requestUrl string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", requestUrl, nil)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

func pipeBody(w http.ResponseWriter, req *http.Request, requestUrl string, localPath string) bool {
	if _, err := requestHead(requestUrl); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Error getting remote resource information: %+v", err)
		return false
	}
	stream, err := requestBody(requestUrl)
	if stream == nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Error requesting body: %+v", err)
		return false
	}
	defer stream.Close()

	dir := filepath.Dir(localPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.MkdirAll(dir, 0755)
	}
	fd, err := os.Create(localPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Error creating file: %+v", err)
		return false
	}
	defer fd.Close()

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/octet-stream")
	var nr int

	for err != io.EOF && err != io.ErrUnexpectedEOF {
		buf := make([]byte, socketBufferSize)
		nr, err = io.ReadFull(stream, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Error reading file: %+v", err)
			return false
		}
		// write to local file
		fd.Write(buf[:nr])
		// write to remote client
		w.Write(buf[:nr])
	}
	return true
}

func requestHandler(w http.ResponseWriter, req *http.Request) {
	log.Println(req.URL.Path)
	// 1. check local cache
	localPath := filepath.Join(cacheDir, req.URL.Path)
	if _, err := os.Stat(localPath); err == nil {
		fileBytes, err := ioutil.ReadFile(localPath)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Error reading file: %+v", err)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(fileBytes)
		log.Println("hit local cache")
		return
	}
	// 2. if not found, fetch from remote, check Cube Symbol server
	requestUrl := cubeSymbolServer + req.URL.Path
	if pipeBody(w, req, requestUrl, localPath) {
		log.Println("found in Cube Symbol server")
		return
	}
	// 3. if not found, fetch from remote, check Micorsoft Symbol server
	requestUrl = msSymbolServer + req.URL.Path
	if pipeBody(w, req, requestUrl, localPath) {
		log.Println("found in Microsoft Symbol server")
		return
	}
	// 4. if not found, return 404 not found
	log.Println("404 not found")
	http.NotFound(w, req)
}

func main() {
	http.HandleFunc("/", requestHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
