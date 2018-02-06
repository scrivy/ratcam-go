package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

func main() {
	index, err := ioutil.ReadFile("index.html")
	if err != nil {
		panic(err)
	}

	go takePictures()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, http.StatusText(405), 405)
			return
		}
		w.Write(index)
	})

	http.HandleFunc("/latest.jpeg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		pictureMutex.RLock()
		w.Write(latestPicture)
		pictureMutex.RUnlock()

		lastRequestLock.Lock()
		lastRequest = time.Now()
		lastRequestLock.Unlock()

		log.Println("served somebody")
	})

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

var (
	latestPicture   []byte
	pictureMutex    = &sync.RWMutex{}
	lastRequest     = time.Now()
	lastRequestLock = &sync.RWMutex{}
)

func takePictures() {
	var imageBytes, stdErr bytes.Buffer
	var start time.Time
	var err error
	var cmd *exec.Cmd
	for {
		lastRequestLock.RLock()
		if !lastRequest.Add(5 * time.Second).After(time.Now()) {
			time.Sleep(500 * time.Millisecond)
			lastRequestLock.RUnlock()
			continue
		}
		lastRequestLock.RUnlock()

		pictureMutex.Lock()
		start = time.Now()
		cmd = exec.Command("fswebcam", "-r", "1920x1080", "--jpeg", "90", "-q", "--no-banner", "-")
		cmd.Stdout = &imageBytes
		cmd.Stderr = &stdErr
		err = cmd.Run()
		if err != nil {
			pictureMutex.Unlock()
			log.Println(err.Error())
			continue
		}
		if stdErr.Len() > 0 {
			log.Println(stdErr.String())
		} else {
			latestPicture = imageBytes.Bytes()
			log.Printf("captured image in %s", time.Since(start).String())
		}
		pictureMutex.Unlock()
		imageBytes.Reset()
		stdErr.Reset()
	}
}
